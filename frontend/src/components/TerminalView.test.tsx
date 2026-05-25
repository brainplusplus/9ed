import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { TerminalView } from './TerminalView';
import { disposeTerminalConnection } from '../terminalConnection';
import type { SessionTab } from '../types';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const createSessionWebSocket = vi.fn();
const terminalDispose = vi.fn();
const terminalOpen = vi.fn();
const terminalWrite = vi.fn();
const terminalLoadAddon = vi.fn();
const terminalOnDataDispose = vi.fn();
const terminalClear = vi.fn();
const fitAddonFit = vi.fn();

vi.mock('../api', () => ({
  createSessionWebSocket: (sessionId: string) => createSessionWebSocket(sessionId),
}));

vi.mock('@xterm/xterm/css/xterm.css', () => ({}));

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80;
    rows = 24;

    loadAddon(addon: unknown) {
      terminalLoadAddon(addon);
    }

    open(element: Element) {
      terminalOpen(element);
    }

    write(data: string) {
      terminalWrite(data);
    }

    clear() {
      terminalClear();
    }

    buffer = {
      active: {
        type: 'normal' as const,
        baseY: 0,
        cursorX: 20,
        cursorY: 0,
        getLine: vi.fn(() => ({
          translateToString: vi.fn((_trimRight?: boolean) => 'PS D:\\golang\\9ed> '),
          isWrapped: false,
        })),
      },
    };

    onData() {
      return {
        dispose: terminalOnDataDispose,
      };
    }

    dispose() {
      terminalDispose();
    }
  },
}));

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {
      fitAddonFit();
    }
  },
}));

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;

  readyState = FakeWebSocket.OPEN;
  close = vi.fn(() => {
    this.readyState = FakeWebSocket.CLOSED;
  });
  send = vi.fn();

  addEventListener() {
    return undefined;
  }
}

describe('TerminalView', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.useFakeTimers();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    createSessionWebSocket.mockReset();
    terminalDispose.mockReset();
    terminalOpen.mockReset();
    terminalWrite.mockReset();
    terminalLoadAddon.mockReset();
    terminalOnDataDispose.mockReset();
    terminalClear.mockReset();
    fitAddonFit.mockReset();
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    disposeTerminalConnection('session-1');
    container.remove();
    vi.useRealTimers();
  });

  it('does not recreate the websocket when only the status callback identity changes', () => {
    createSessionWebSocket.mockReturnValue(new FakeWebSocket());

    const tab: SessionTab = {
      id: 'session-1',
      profile: {
        id: 'pwsh',
        label: 'PowerShell 7',
        command: 'pwsh.exe',
        args: [],
      },
      status: 'connecting',
    };

    act(() => {
      root.render(<TerminalView tab={tab} active onStatusChange={() => undefined} />);
    });
    act(() => {
      vi.advanceTimersByTime(450);
    });

    expect(createSessionWebSocket).toHaveBeenCalledTimes(1);

    act(() => {
      root.render(<TerminalView tab={tab} active onStatusChange={() => undefined} />);
    });

    expect(createSessionWebSocket).toHaveBeenCalledTimes(1);
  });

  it('calls clear and write when clear-terminal action is dispatched', () => {
    createSessionWebSocket.mockReturnValue(new FakeWebSocket());

    const tab: SessionTab = {
      id: 'session-1',
      profile: {
        id: 'pwsh',
        label: 'PowerShell 7',
        command: 'pwsh.exe',
        args: [],
      },
      status: 'ready',
    };

    act(() => {
      root.render(<TerminalView tab={tab} active onStatusChange={() => undefined} />);
    });
    act(() => {
      vi.advanceTimersByTime(450);
    });

    act(() => {
      root.render(
        <TerminalView
          tab={tab}
          active
          onStatusChange={() => undefined}
          action={{ targetTabId: 'session-1', kind: 'clear-terminal', nonce: Date.now() }}
        />
      );
    });

    expect(terminalClear).toHaveBeenCalled();
    expect(terminalWrite).toHaveBeenCalled();
  });

  it('waits for backend replay instead of writing client scrollback on remount', () => {
    createSessionWebSocket.mockReturnValue(new FakeWebSocket());

    const tab: SessionTab = {
      id: 'session-1',
      profile: {
        id: 'pwsh',
        label: 'PowerShell 7',
        command: 'pwsh.exe',
        args: [],
      },
      status: 'ready',
      scrollback: 'dir\r\npackage.json\r\nPS D:\\golang\\go-webttyd> ',
    };

    act(() => {
      root.render(<TerminalView tab={tab} active onStatusChange={() => undefined} />);
    });

    expect(terminalWrite).not.toHaveBeenCalledWith(tab.scrollback);
  });
});
