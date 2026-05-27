import { createSessionWebSocket } from './api';
import type { SessionTab, WebSocketIncomingMessage, WebSocketOutgoingMessage } from './types';

const TERMINAL_RECONNECT_DELAYS_MS = [500, 1000, 2000, 4000];
const TERMINAL_OUTPUT_BUFFER_MAX_BYTES = 200_000;

type OutputListener = (data: string) => void;
type StatusListener = (status: SessionTab['status'], errorMessage?: string) => void;

class TerminalConnection {
  private socket: WebSocket | null = null;
  private outputListeners = new Set<OutputListener>();
  private statusListeners = new Set<StatusListener>();
  private outputBuffer = '';
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private disposed = false;
  private includeReplayOnNextConnect = true;

  constructor(private readonly sessionId: string) {
    this.connect();
  }

  subscribeOutput(listener: OutputListener, replay = true): () => void {
    this.outputListeners.add(listener);
    if (replay && this.outputBuffer) {
      listener(this.outputBuffer);
    }
    return () => {
      this.outputListeners.delete(listener);
    };
  }

  subscribeStatus(listener: StatusListener): () => void {
    this.statusListeners.add(listener);
    return () => {
      this.statusListeners.delete(listener);
    };
  }

  send(message: WebSocketOutgoingMessage): void {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(message));
    }
  }

  sendInput(data: string): void {
    this.send({ type: 'input', data });
  }

  getScrollback(maxLines: number): string {
    return this.outputBuffer
      .replace(/\r\n/g, '\n')
      .split('\n')
      .slice(-maxLines)
      .join('\n')
      .trimStart();
  }

  dispose(): void {
    this.disposed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.outputListeners.clear();
    this.statusListeners.clear();
    this.socket?.close();
    this.socket = null;
  }

  private connect(): void {
    if (this.disposed) return;

    const includeReplay = this.includeReplayOnNextConnect;
    this.includeReplayOnNextConnect = false;
    const socket = createSessionWebSocket(this.sessionId, includeReplay);
    this.socket = socket;
    this.emitStatus('connecting');

    socket.addEventListener('open', () => {
      if (this.disposed || this.socket !== socket) return;
      this.reconnectAttempt = 0;
      this.emitStatus('ready');
    });

    socket.addEventListener('message', (event) => {
      if (this.disposed || this.socket !== socket) return;
      const message = JSON.parse(String(event.data)) as WebSocketIncomingMessage;
      if (message.type === 'output') {
        this.appendOutput(message.data);
        return;
      }
      if (message.type === 'error') {
        this.emitStatus('error', message.data);
      }
    });

    socket.addEventListener('close', () => {
      if (this.disposed || this.socket !== socket) return;
      this.scheduleReconnect();
    });

    socket.addEventListener('error', () => {
      if (this.disposed || this.socket !== socket) return;
      if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
        socket.close();
      } else {
        this.scheduleReconnect();
      }
    });
  }

  private scheduleReconnect(): void {
    if (this.disposed || this.reconnectTimer) return;
    if (this.reconnectAttempt >= TERMINAL_RECONNECT_DELAYS_MS.length) {
      this.emitStatus('error', 'Terminal connection failed');
      return;
    }

    this.emitStatus('disconnected');
    const delay = TERMINAL_RECONNECT_DELAYS_MS[this.reconnectAttempt];
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private appendOutput(data: string): void {
    this.outputBuffer = `${this.outputBuffer}${data}`.slice(-TERMINAL_OUTPUT_BUFFER_MAX_BYTES);
    for (const listener of this.outputListeners) {
      listener(data);
    }
  }

  private emitStatus(status: SessionTab['status'], errorMessage?: string): void {
    for (const listener of this.statusListeners) {
      listener(status, errorMessage);
    }
  }
}

const connections = new Map<string, TerminalConnection>();

export function getTerminalConnection(sessionId: string): TerminalConnection {
  let connection = connections.get(sessionId);
  if (!connection) {
    connection = new TerminalConnection(sessionId);
    connections.set(sessionId, connection);
  }
  return connection;
}

export function disposeTerminalConnection(sessionId: string): void {
  const connection = connections.get(sessionId);
  if (!connection) return;
  connection.dispose();
  connections.delete(sessionId);
}
