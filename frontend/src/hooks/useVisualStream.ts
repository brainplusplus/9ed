/**
 * useVisualStream — WebRTC client for collaborative visual streaming (ADR-0001).
 *
 * Connects to the /ws/visual/{sessionId} signaling WebSocket, exchanges SDP
 * offers/answers + ICE candidates, and renders incoming JPEG tile frames to
 * a <canvas>. Also sends mouse/keyboard input events back via the DataChannel.
 *
 * Used by the browser panel to enable collaborative viewing of the headless
 * Chromium tab with low-latency JPEG tile diff streaming.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import type { VisualSignalingMessage } from '../types';

const VISUAL_WS_BASE = '/ws/visual/';
const TILE_MSG_TYPE = 0x01;
const INPUT_MSG_TYPE = 0x02;

type VisualStreamState = {
  connected: boolean;
  connecting: boolean;
  frameCount: number;
};

type InputEventPayload = {
  type: string;
  x?: number;
  y?: number;
  button?: number;
  key?: string;
  code?: string;
  modifiers?: number;
  text?: string;
  deltaX?: number;
  deltaY?: number;
};

export function useVisualStream(
  sessionId: string | null,
  canvasRef: React.RefObject<HTMLCanvasElement | null>,
): VisualStreamState & {
  sendInput: (event: InputEventPayload) => void;
} {
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [frameCount, setFrameCount] = useState(0);
  const wsRef = useRef<WebSocket | null>(null);
  const pcRef = useRef<RTCPeerConnection | null>(null);
  const dcRef = useRef<RTCDataChannel | null>(null);
  const tilesRef = useRef<Map<string, HTMLImageElement>>(new Map());
  const rafRef = useRef<number | null>(null);
  const pendingTilesRef = useRef<Array<{ x: number; y: number; w: number; h: number; blob: Blob }>>([]);

  // Draw pending tiles to the canvas.
  const flushTiles = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas || pendingTilesRef.current.length === 0) {
      rafRef.current = null;
      return;
    }
    const ctx = canvas.getContext('2d');
    if (!ctx) {
      rafRef.current = null;
      return;
    }
    const tiles = pendingTilesRef.current.splice(0);
    let needsAnotherFlush = false;
    for (const tile of tiles) {
      const url = URL.createObjectURL(tile.blob);
      const img = new Image();
      img.onload = () => {
        ctx.drawImage(img, tile.x, tile.y, tile.w, tile.h);
        URL.revokeObjectURL(url);
        setFrameCount((c) => c + 1);
      };
      img.onerror = () => URL.revokeObjectURL(url);
      img.src = url;
      needsAnotherFlush = true;
    }
    if (needsAnotherFlush) {
      rafRef.current = window.requestAnimationFrame(flushTiles);
    } else {
      rafRef.current = null;
    }
  }, [canvasRef]);

  const scheduleFlush = useCallback(() => {
    if (rafRef.current === null) {
      rafRef.current = window.requestAnimationFrame(flushTiles);
    }
  }, [flushTiles]);

  // Send an input event via the DataChannel.
  const sendInput = useCallback((event: InputEventPayload) => {
    const dc = dcRef.current;
    if (!dc || dc.readyState !== 'open') return;
    const json = JSON.stringify(event);
    const msg = new Uint8Array(1 + json.length);
    msg[0] = INPUT_MSG_TYPE;
    msg.set(new TextEncoder().encode(json), 1);
    dc.send(msg);
  }, []);

  useEffect(() => {
    if (!sessionId) {
      setConnected(false);
      setConnecting(false);
      return;
    }

    let closed = false;
    setConnecting(true);
    const ws = new WebSocket(`${VISUAL_WS_BASE}${sessionId}`);
    wsRef.current = ws;

    const pc = new RTCPeerConnection({
      iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
    });
    pcRef.current = pc;

    // Create DataChannel for input + JPEG tiles.
    const dc = pc.createDataChannel('visual', { ordered: false });
    dcRef.current = dc;

    dc.binaryType = 'arraybuffer';
    dc.onopen = () => {
      setConnected(true);
      setConnecting(false);
    };
    dc.onclose = () => {
      setConnected(false);
      setConnecting(false);
    };

    dc.onmessage = (event) => {
      if (!(event.data instanceof ArrayBuffer)) return;
      const data = new Uint8Array(event.data);
      if (data.length === 0) return;

      const msgType = data[0];
      if (msgType === TILE_MSG_TYPE) {
        // Parse tile header: [1 type][4 x][4 y][4 w][4 h][4 jpegLen][jpeg bytes]
        const view = new DataView(event.data);
        const x = view.getUint32(1);
        const y = view.getUint32(5);
        const w = view.getUint32(9);
        const h = view.getUint32(13);
        const jpegLen = view.getUint32(17);
        if (data.length < 21 + jpegLen) return;

        const jpegBytes = data.slice(21, 21 + jpegLen);
        const blob = new Blob([jpegBytes], { type: 'image/jpeg' });

        // Resize canvas to match tile grid if needed.
        const canvas = canvasRef.current;
        if (canvas && (canvas.width < x + w || canvas.height < y + h)) {
          canvas.width = Math.max(canvas.width, x + w);
          canvas.height = Math.max(canvas.height, y + h);
        }

        pendingTilesRef.current.push({ x, y, w, h, blob });
        scheduleFlush();
      }
    };

    pc.onicecandidate = (event) => {
      if (event.candidate) {
        const msg: VisualSignalingMessage = {
          type: 'ice-candidate',
          sessionId,
          ice: event.candidate.toJSON(),
        };
        ws.send(JSON.stringify(msg));
      }
    };

    pc.onconnectionstatechange = () => {
      if (pc.connectionState === 'connected') {
        setConnected(true);
        setConnecting(false);
      } else if (pc.connectionState === 'failed' || pc.connectionState === 'disconnected') {
        setConnected(false);
        setConnecting(false);
      }
    };

    ws.onopen = async () => {
      // Create and send SDP offer.
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      const msg: VisualSignalingMessage = {
        type: 'offer',
        sessionId,
        sdp: offer.sdp ?? '',
      };
      ws.send(JSON.stringify(msg));
    };

    ws.onmessage = async (event) => {
      if (closed) return;
      try {
        const msg = JSON.parse(event.data) as VisualSignalingMessage;
        if (msg.type === 'answer' && msg.sdp) {
          await pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp });
        } else if (msg.type === 'ice-candidate' && msg.ice) {
          await pc.addIceCandidate(msg.ice);
        }
      } catch {
        // Ignore malformed messages.
      }
    };

    ws.onclose = () => {
      setConnected(false);
      setConnecting(false);
    };

    return () => {
      closed = true;
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      pendingTilesRef.current = [];
      tilesRef.current.clear();
      dcRef.current = null;
      pcRef.current = null;
      wsRef.current = null;
      dc.close();
      pc.close();
      ws.close();
      setConnected(false);
      setConnecting(false);
    };
  }, [sessionId, canvasRef, scheduleFlush]);

  return { connected, connecting, frameCount, sendInput };
}
