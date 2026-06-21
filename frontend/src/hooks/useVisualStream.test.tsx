import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { useRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useVisualStream } from './useVisualStream';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

// ── Fake WebSocket ──

class FakeWebSocket extends EventTarget {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 3;

  static instances: FakeWebSocket[] = [];

  readyState = FakeWebSocket.CONNECTING;
  url: string;
  sentMessages: string[] = [];

  private _onopen: ((ev: Event) => any) | null = null;
  private _onmessage: ((ev: MessageEvent) => any) | null = null;
  private _onclose: ((ev: CloseEvent) => any) | null = null;

  constructor(url: string) {
    super();
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  get onopen() { return this._onopen; }
  set onopen(fn: ((ev: Event) => any) | null) {
    if (this._onopen) this.removeEventListener('open', this._onopen as EventListener);
    this._onopen = fn;
    if (fn) this.addEventListener('open', fn as EventListener);
  }

  get onmessage() { return this._onmessage; }
  set onmessage(fn: ((ev: MessageEvent) => any) | null) {
    if (this._onmessage) this.removeEventListener('message', this._onmessage as EventListener);
    this._onmessage = fn;
    if (fn) this.addEventListener('message', fn as EventListener);
  }

  get onclose() { return this._onclose; }
  set onclose(fn: ((ev: CloseEvent) => any) | null) {
    if (this._onclose) this.removeEventListener('close', this._onclose as EventListener);
    this._onclose = fn;
    if (fn) this.addEventListener('close', fn as EventListener);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.dispatchEvent(new Event('open'));
  }

  send(data: string) {
    this.sentMessages.push(data);
  }

  emitMessage(data: unknown) {
    const event = new MessageEvent('message', {
      data: typeof data === 'string' ? data : JSON.stringify(data),
    });
    this.dispatchEvent(event);
  }

  close() {
    if (this.readyState === FakeWebSocket.CLOSED) return;
    this.readyState = FakeWebSocket.CLOSED;
    this.dispatchEvent(new CloseEvent('close'));
  }
}

// ── Fake RTCPeerConnection ──

class FakeRTCDataChannel extends EventTarget {
  readyState = 'connecting';
  binaryType: string = 'blob';

  private _onopen: ((ev: Event) => any) | null = null;
  private _onclose: ((ev: Event) => any) | null = null;
  private _onmessage: ((ev: MessageEvent) => any) | null = null;

  get onopen() { return this._onopen; }
  set onopen(fn: ((ev: Event) => any) | null) {
    if (this._onopen) this.removeEventListener('open', this._onopen as EventListener);
    this._onopen = fn;
    if (fn) this.addEventListener('open', fn as EventListener);
  }

  get onclose() { return this._onclose; }
  set onclose(fn: ((ev: Event) => any) | null) {
    if (this._onclose) this.removeEventListener('close', this._onclose as EventListener);
    this._onclose = fn;
    if (fn) this.addEventListener('close', fn as EventListener);
  }

  get onmessage() { return this._onmessage; }
  set onmessage(fn: ((ev: MessageEvent) => any) | null) {
    if (this._onmessage) this.removeEventListener('message', this._onmessage as EventListener);
    this._onmessage = fn;
    if (fn) this.addEventListener('message', fn as EventListener);
  }

  send(_data: ArrayBuffer | string) {
    // no-op
  }

  close() {
    this.readyState = 'closed';
    this.dispatchEvent(new Event('close'));
  }

  open() {
    this.readyState = 'open';
    this.dispatchEvent(new Event('open'));
  }

  emitMessage(data: ArrayBuffer) {
    this.dispatchEvent(new MessageEvent('message', { data }));
  }
}

class FakeRTCPeerConnection extends EventTarget {
  static instances: FakeRTCPeerConnection[] = [];

  connectionState: RTCPeerConnectionState = 'new';
  localDescription: { sdp: string; type: RTCSdpType } | null = null;
  remoteDescription: { sdp: string; type: RTCSdpType } | null = null;
  dataChannel: FakeRTCDataChannel | null = null;

  private _onicecandidate: ((event: RTCPeerConnectionIceEvent) => any) | null = null;
  private _onconnectionstatechange: ((event: Event) => any) | null = null;

  constructor() {
    super();
    FakeRTCPeerConnection.instances.push(this);
  }

  get onicecandidate() { return this._onicecandidate; }
  set onicecandidate(fn: ((event: RTCPeerConnectionIceEvent) => any) | null) {
    if (this._onicecandidate) this.removeEventListener('icecandidate', this._onicecandidate as EventListener);
    this._onicecandidate = fn;
    if (fn) this.addEventListener('icecandidate', fn as EventListener);
  }

  get onconnectionstatechange() { return this._onconnectionstatechange; }
  set onconnectionstatechange(fn: ((event: Event) => any) | null) {
    if (this._onconnectionstatechange) this.removeEventListener('connectionstatechange', this._onconnectionstatechange as EventListener);
    this._onconnectionstatechange = fn;
    if (fn) this.addEventListener('connectionstatechange', fn as EventListener);
  }

  createDataChannel(_label: string, _init?: RTCDataChannelInit): FakeRTCDataChannel {
    this.dataChannel = new FakeRTCDataChannel();
    return this.dataChannel;
  }

  async createOffer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'offer', sdp: 'fake-offer-sdp' };
  }

  async createAnswer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'answer', sdp: 'fake-answer-sdp' };
  }

  async setLocalDescription(desc: RTCSessionDescriptionInit): Promise<void> {
    this.localDescription = { sdp: desc.sdp!, type: desc.type };
  }

  async setRemoteDescription(desc: RTCSessionDescriptionInit): Promise<void> {
    this.remoteDescription = { sdp: desc.sdp!, type: desc.type };
  }

  async addIceCandidate(_candidate: RTCIceCandidateInit): Promise<void> {
    // no-op
  }

  close() {
    // no-op
  }

  setConnectionState(state: RTCPeerConnectionState) {
    this.connectionState = state;
    this.dispatchEvent(new Event('connectionstatechange'));
  }
}

// ── Test harness ──

let capturedState: ReturnType<typeof useVisualStream> | null = null;

function Harness({ sessionId }: { sessionId: string | null }) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  capturedState = useVisualStream(sessionId, canvasRef);
  return null;
}

describe('useVisualStream', () => {
  let container: HTMLDivElement;
  let root: Root;
  const originalWebSocket = globalThis.WebSocket;
  const originalRTCPeerConnection = globalThis.RTCPeerConnection;

  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.instances = [];
    FakeRTCPeerConnection.instances = [];
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.stubGlobal('RTCPeerConnection', FakeRTCPeerConnection as any);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    capturedState = null;
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    vi.stubGlobal('WebSocket', originalWebSocket);
    vi.stubGlobal('RTCPeerConnection', originalRTCPeerConnection);
    vi.useRealTimers();
    capturedState = null;
  });

  it('returns disconnected state when sessionId is null', () => {
    act(() => {
      root.render(<Harness sessionId={null} />);
    });
    expect(capturedState!.connected).toBe(false);
    expect(capturedState!.connecting).toBe(false);
    expect(capturedState!.frameCount).toBe(0);
  });

  it('creates WebSocket connection when sessionId is provided', () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(FakeWebSocket.instances[0].url).toBe('/ws/visual/tab-1');
    expect(capturedState!.connecting).toBe(true);
    expect(capturedState!.connected).toBe(false);
  });

  it('does not create WebSocket when sessionId is null', () => {
    act(() => {
      root.render(<Harness sessionId={null} />);
    });
    expect(FakeWebSocket.instances).toHaveLength(0);
  });

  it('sends SDP offer when WebSocket opens', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    expect(ws.sentMessages.length).toBeGreaterThanOrEqual(1);
    const offer = JSON.parse(ws.sentMessages[0]);
    expect(offer.type).toBe('offer');
    expect(offer.sessionId).toBe('tab-1');
    expect(offer.sdp).toBe('fake-offer-sdp');
  });

  it('sets connected when DataChannel opens', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    const pc = FakeRTCPeerConnection.instances[0];
    expect(pc.dataChannel).not.toBeNull();

    await act(async () => {
      pc.dataChannel!.open();
    });

    expect(capturedState!.connected).toBe(true);
    expect(capturedState!.connecting).toBe(false);
  });

  it('sets disconnected when DataChannel closes', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    const pc = FakeRTCPeerConnection.instances[0];
    await act(async () => {
      pc.dataChannel!.open();
    });
    expect(capturedState!.connected).toBe(true);

    await act(async () => {
      pc.dataChannel!.close();
    });
    expect(capturedState!.connected).toBe(false);
  });

  it('handles SDP answer from server', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    const pc = FakeRTCPeerConnection.instances[0];

    // Server sends answer
    await act(async () => {
      ws.emitMessage({ type: 'answer', sessionId: 'tab-1', sdp: 'fake-answer-sdp' });
    });

    expect(pc.remoteDescription).not.toBeNull();
    expect(pc.remoteDescription!.sdp).toBe('fake-answer-sdp');
  });

  it('handles ICE candidate from server', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    const pc = FakeRTCPeerConnection.instances[0];
    const addIceSpy = vi.spyOn(pc, 'addIceCandidate');

    await act(async () => {
      ws.emitMessage({
        type: 'ice-candidate',
        sessionId: 'tab-1',
        ice: { candidate: 'candidate:123', sdpMid: '0', sdpMLineIndex: 0 },
      });
    });

    expect(addIceSpy).toHaveBeenCalled();
  });

  it('ignores malformed messages without crashing', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    // Should not throw
    await act(async () => {
      ws.emitMessage('not json');
      ws.emitMessage({ type: 'unknown', sessionId: 'tab-1' });
      ws.emitMessage({ type: 'answer' }); // missing fields
    });
  });

  it('sets disconnected when WebSocket closes', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    await act(async () => {
      ws.close();
    });

    expect(capturedState!.connected).toBe(false);
    expect(capturedState!.connecting).toBe(false);
  });

  it('sets connected when PC connection state becomes connected', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    const pc = FakeRTCPeerConnection.instances[0];
    await act(async () => {
      pc.setConnectionState('connected');
    });

    expect(capturedState!.connected).toBe(true);
  });

  it('sets disconnected when PC connection state becomes failed', async () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    await act(async () => {
      ws.open();
    });

    const pc = FakeRTCPeerConnection.instances[0];
    await act(async () => {
      pc.setConnectionState('connected');
    });
    expect(capturedState!.connected).toBe(true);

    await act(async () => {
      pc.setConnectionState('failed');
    });
    expect(capturedState!.connected).toBe(false);
  });

  it('sendInput is a no-op when DataChannel is not open', () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });
    // DataChannel not yet created / open
    expect(() => {
      capturedState!.sendInput({ type: 'mouse_move', x: 10, y: 20 });
    }).not.toThrow();
  });

  it('cleans up WebSocket and PeerConnection on unmount', () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });

    const ws = FakeWebSocket.instances[0];
    expect(ws.readyState).not.toBe(FakeWebSocket.CLOSED);

    act(() => {
      root.unmount();
    });

    expect(ws.readyState).toBe(FakeWebSocket.CLOSED);
  });

  it('reconnects when sessionId changes', () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });
    expect(FakeWebSocket.instances).toHaveLength(1);

    act(() => {
      root.render(<Harness sessionId="tab-2" />);
    });
    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(FakeWebSocket.instances[1].url).toBe('/ws/visual/tab-2');
  });

  it('disconnects when sessionId becomes null', () => {
    act(() => {
      root.render(<Harness sessionId="tab-1" />);
    });
    expect(FakeWebSocket.instances).toHaveLength(1);
    const ws = FakeWebSocket.instances[0];

    act(() => {
      root.render(<Harness sessionId={null} />);
    });

    expect(capturedState!.connected).toBe(false);
    expect(capturedState!.connecting).toBe(false);
    // Old WS should be closed
    expect(ws.readyState).toBe(FakeWebSocket.CLOSED);
  });
});
