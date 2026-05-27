import { useEffect, useRef } from 'react';
import { isBackendTemporarilyUnavailable } from '../api';

type FileEvent = {
  type: 'create' | 'modify' | 'delete' | 'rename';
  path: string;
  name: string;
};

type UseFileWatcherOptions = {
  root: string | null;
  onFileChange: (event: FileEvent) => void;
};

const WATCH_CONNECT_TIMEOUT_MS = 8000;
const WATCH_INITIAL_CONNECT_DELAY_MS = 150;
const WATCH_RECONNECT_DELAYS_MS = [500, 1000, 2000, 4000, 8000, 15000];
const WATCH_EVENT_FLUSH_MS = 120;
const WATCH_MAX_BUFFERED_EVENTS = 200;

function isTransientWatchPath(path: string, name: string): boolean {
  const fileName = name || path.replace(/\\/g, '/').split('/').pop() || '';
  return (
    fileName === '.DS_Store' ||
    fileName === 'Thumbs.db' ||
    fileName.endsWith('~') ||
    fileName.endsWith('.swp') ||
    fileName.endsWith('.swx') ||
    fileName.endsWith('.tmp') ||
    fileName.includes('.tmp-')
  );
}

export function useFileWatcher({ root, onFileChange }: UseFileWatcherOptions) {
  const callbackRef = useRef(onFileChange);
  callbackRef.current = onFileChange;

  useEffect(() => {
    if (!root) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/ws/watch?root=${encodeURIComponent(root)}`;
    let ws: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let connectTimer: ReturnType<typeof setTimeout> | null = null;
    let flushTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;
    let reconnectAttempt = 0;
    let connectionToken = 0;
    const pendingEvents = new Map<string, FileEvent>();

    function clearConnectTimer() {
      if (connectTimer) {
        clearTimeout(connectTimer);
        connectTimer = null;
      }
    }

    function flushEvents() {
      flushTimer = null;
      const events = Array.from(pendingEvents.values());
      pendingEvents.clear();
      for (const event of events) {
        if (disposed) return;
        callbackRef.current(event);
      }
    }

    function queueEvent(event: FileEvent) {
      if (isTransientWatchPath(event.path, event.name)) return;
      pendingEvents.set(event.path, event);
      if (pendingEvents.size > WATCH_MAX_BUFFERED_EVENTS) {
        const firstKey = pendingEvents.keys().next().value;
        if (firstKey) pendingEvents.delete(firstKey);
      }
      if (!flushTimer) {
        flushTimer = setTimeout(flushEvents, WATCH_EVENT_FLUSH_MS);
      }
    }

    function scheduleReconnect() {
      if (disposed || reconnectTimer) return;
      const delay = WATCH_RECONNECT_DELAYS_MS[Math.min(reconnectAttempt, WATCH_RECONNECT_DELAYS_MS.length - 1)];
      reconnectAttempt += 1;
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        connect();
      }, delay);
    }

    function connect() {
      if (disposed) return;
      if (isBackendTemporarilyUnavailable()) {
        scheduleReconnect();
        return;
      }
      const token = ++connectionToken;

      ws = new WebSocket(url);
      connectTimer = setTimeout(() => {
        if (disposed || token !== connectionToken) return;
        if (ws?.readyState === WebSocket.CONNECTING) {
          ws.close();
        }
      }, WATCH_CONNECT_TIMEOUT_MS);

      ws.addEventListener('open', () => {
        if (disposed || token !== connectionToken) return;
        clearConnectTimer();
        reconnectAttempt = 0;
      });

      ws.addEventListener('message', (e) => {
        if (disposed || token !== connectionToken) return;
        try {
          const event = JSON.parse(e.data) as FileEvent;
          queueEvent(event);
        } catch {}
      });

      ws.addEventListener('close', () => {
        if (token !== connectionToken) return;
        clearConnectTimer();
        connectionToken += 1;
        scheduleReconnect();
      });

      ws.addEventListener('error', () => {
        if (token !== connectionToken) return;
        ws?.close();
      });
    }

    const initialTimer = setTimeout(connect, WATCH_INITIAL_CONNECT_DELAY_MS);

    return () => {
      disposed = true;
      connectionToken += 1;
      clearTimeout(initialTimer);
      if (reconnectTimer) clearTimeout(reconnectTimer);
      clearConnectTimer();
      if (flushTimer) clearTimeout(flushTimer);
      pendingEvents.clear();
      ws?.close();
    };
  }, [root]);
}
