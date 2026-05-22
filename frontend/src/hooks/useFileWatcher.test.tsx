import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useFileWatcher } from './useFileWatcher';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

class FakeWebSocket extends EventTarget {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 3;

  static instances: FakeWebSocket[] = [];

  readyState = FakeWebSocket.CONNECTING;
  url: string;

  constructor(url: string) {
    super();
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.dispatchEvent(new Event('open'));
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

function Harness({ root, onFileChange }: { root: string | null; onFileChange: (event: { type: string; path: string; name: string }) => void }) {
  useFileWatcher({ root, onFileChange: onFileChange as Parameters<typeof useFileWatcher>[0]['onFileChange'] });
  return null;
}

describe('useFileWatcher', () => {
  let container: HTMLDivElement;
  let root: Root;
  const originalWebSocket = globalThis.WebSocket;

  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.instances = [];
    vi.stubGlobal('WebSocket', FakeWebSocket);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    vi.stubGlobal('WebSocket', originalWebSocket);
    vi.useRealTimers();
  });

  it('drops transient temp-file events and flushes stable events once', () => {
    const onFileChange = vi.fn();
    act(() => {
      root.render(<Harness root="/repo" onFileChange={onFileChange} />);
    });
    act(() => {
      vi.advanceTimersByTime(150);
    });

    const ws = FakeWebSocket.instances[0];
    expect(ws.url).toContain('/ws/watch?root=');

    act(() => {
      ws.open();
      ws.emitMessage({ type: 'rename', path: '/repo/orchestration-state.json.tmp-123', name: 'orchestration-state.json.tmp-123' });
      ws.emitMessage({ type: 'modify', path: '/repo/src/main.ts', name: 'main.ts' });
      vi.advanceTimersByTime(120);
    });

    expect(onFileChange).toHaveBeenCalledTimes(1);
    expect(onFileChange).toHaveBeenCalledWith({ type: 'modify', path: '/repo/src/main.ts', name: 'main.ts' });
  });

  it('reconnects with backoff after close and ignores stale socket messages', () => {
    const onFileChange = vi.fn();
    act(() => {
      root.render(<Harness root="/repo" onFileChange={onFileChange} />);
    });
    act(() => {
      vi.advanceTimersByTime(150);
    });

    const first = FakeWebSocket.instances[0];
    act(() => {
      first.close();
      first.emitMessage({ type: 'modify', path: '/repo/stale.ts', name: 'stale.ts' });
      vi.advanceTimersByTime(499);
    });

    expect(FakeWebSocket.instances).toHaveLength(1);

    act(() => {
      vi.advanceTimersByTime(1);
    });

    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(onFileChange).not.toHaveBeenCalled();
  });
});
