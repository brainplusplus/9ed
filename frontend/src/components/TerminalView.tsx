import { useEffect, useMemo, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

import { getTerminalConnection } from '../terminalConnection';
import { registerTerminal, unregisterTerminal } from '../terminalRegistry';
import type { SessionTab, TerminalAction, WebSocketOutgoingMessage } from '../types';

const TERMINAL_INITIAL_CONNECT_DELAY_MS = 450;

type TerminalViewProps = {
  tab: SessionTab;
  active: boolean;
  action?: TerminalAction | null;
  cwd?: string;
  onStatusChange: (sessionId: string, status: SessionTab['status'], errorMessage?: string) => void;
  onScrollbackSnapshot?: (sessionId: string, scrollback: string) => void;
  onTerminalOutput?: (sessionId: string, data: string) => void;
};

export function TerminalView(props: TerminalViewProps) {
  const { tab, active, action, cwd, onStatusChange, onScrollbackSnapshot, onTerminalOutput } = props;
  const hostRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const connectionRef = useRef<ReturnType<typeof getTerminalConnection> | null>(null);
  const onStatusChangeRef = useRef(onStatusChange);
  const onScrollbackSnapshotRef = useRef(onScrollbackSnapshot);
  const onTerminalOutputRef = useRef(onTerminalOutput);

  useEffect(() => {
    onStatusChangeRef.current = onStatusChange;
  }, [onStatusChange]);

  useEffect(() => {
    onScrollbackSnapshotRef.current = onScrollbackSnapshot;
  }, [onScrollbackSnapshot]);

  useEffect(() => {
    onTerminalOutputRef.current = onTerminalOutput;
  }, [onTerminalOutput]);

  const statusText = useMemo(() => {
    if (tab.status === 'error' && tab.errorMessage) {
      return tab.errorMessage;
    }
    return tab.status;
  }, [tab.errorMessage, tab.status]);

  const readScrollback = (maxLines: number) => {
    const terminal = terminalRef.current;
    if (!terminal) return '';

    const buf = terminal.buffer.active;
    const start = Math.max(0, buf.length - maxLines);
    const lines: string[] = [];
    for (let i = start; i < buf.length; i++) {
      const line = buf.getLine(i);
      if (line) {
        lines.push(line.translateToString(true));
      }
    }

    return lines.join('\r\n').trimStart();
  };

  useEffect(() => {
    if (!hostRef.current || terminalRef.current) {
      return;
    }

    const terminal = new Terminal({
      convertEol: true,
      cursorBlink: true,
      fontFamily: 'IBM Plex Mono, Consolas, monospace',
      fontSize: 14,
      theme: {
        background: '#111827',
        foreground: '#e5eefb',
        cursor: '#f4d35e',
        selectionBackground: 'rgba(244, 211, 94, 0.22)',
      },
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(hostRef.current);

    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;
    fitAddon.fit();

    const connection = getTerminalConnection(tab.id);
    connectionRef.current = connection;

    const sendMessage = (message: WebSocketOutgoingMessage) => {
      connection.send(message);
    };

    const initialTimer = setTimeout(() => {
      sendMessage({ type: 'resize', cols: terminal.cols, rows: terminal.rows });
    }, TERMINAL_INITIAL_CONNECT_DELAY_MS);

    const unsubscribeStatus = connection.subscribeStatus((status, errorMessage) => {
      onStatusChangeRef.current(tab.id, status, errorMessage);
      if (status === 'ready') {
        fitAddon.fit();
        sendMessage({ type: 'resize', cols: terminal.cols, rows: terminal.rows });
      }
    });

    const unsubscribeOutput = connection.subscribeOutput((data) => {
      terminal.write(data);
      onTerminalOutputRef.current?.(tab.id, data);
    });

    const disposable = terminal.onData((data) => {
      sendMessage({ type: 'input', data });
    });

    const handleResize = () => {
      fitAddon.fit();
      sendMessage({ type: 'resize', cols: terminal.cols, rows: terminal.rows });
    };

    let resizeFrame: number | null = null;
    const scheduleResize = () => {
      if (resizeFrame !== null) return;
      resizeFrame = window.requestAnimationFrame(() => {
        resizeFrame = null;
        handleResize();
      });
    };

    const ResizeObserverCtor = window.ResizeObserver;
    const resizeObserver = ResizeObserverCtor ? new ResizeObserverCtor(scheduleResize) : null;
    resizeObserver?.observe(hostRef.current);

    window.addEventListener('resize', handleResize);

    // Register terminal in the global registry for AI chat integration.
    const shellType = tab.profile.command?.split(/[/\\]/).pop()?.replace('.exe', '') ?? 'shell';
    registerTerminal(tab.id, {
      getScrollback: (maxLines: number) => {
        return readScrollback(maxLines).replace(/\r\n/g, '\n');
      },
      sendCommand: (command: string) => {
        sendMessage({ type: 'input', data: command + '\r' });
      },
      cwd: cwd ?? '',
      shellType,
    });

    return () => {
      unregisterTerminal(tab.id);
      clearTimeout(initialTimer);
      unsubscribeStatus();
      unsubscribeOutput();
      if (resizeFrame !== null) {
        window.cancelAnimationFrame(resizeFrame);
      }
      resizeObserver?.disconnect();
      window.removeEventListener('resize', handleResize);
      disposable.dispose();
      terminal.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
      connectionRef.current = null;
    };
  }, [tab.id, cwd, tab.profile.command]);

  useEffect(() => {
    if (!active || !terminalRef.current || !fitAddonRef.current || !connectionRef.current) {
      return;
    }

    fitAddonRef.current.fit();
    connectionRef.current.send({
      type: 'resize',
      cols: terminalRef.current.cols,
      rows: terminalRef.current.rows,
    } satisfies WebSocketOutgoingMessage);
  }, [active]);

  useEffect(() => {
    if (!action || action.targetTabId !== tab.id) {
      return;
    }

    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }

    if (action.kind === 'clear-terminal') {
      const buf = terminal.buffer.active;

      // Alternate buffer (vim, nano, less) — full clear, no smart behavior
      if (buf.type === 'alternate') {
        terminal.clear();
        terminal.write('\x1b[2J\x1b[3J\x1b[H');
        return;
      }

      // Read the line where the cursor sits
      const cursorLineIdx = buf.baseY + buf.cursorY;
      const cursorLine = buf.getLine(cursorLineIdx);
      const lineText = cursorLine?.translateToString(true) ?? '';

      // Detect prompt: non-empty line ending with common prompt characters
      // AND cursor is at or near end of line (shell waiting for input)
      const trimmed = lineText.trimEnd();
      const endsWithPromptChar = trimmed.length > 0 && (
        trimmed.endsWith('>') ||
        trimmed.endsWith('$') ||
        trimmed.endsWith('#') ||
        trimmed.endsWith('%')
      );
      const cursorAtEnd = buf.cursorX >= trimmed.length - 1;

      if (endsWithPromptChar && cursorAtEnd) {
        // Prompt visible — clear everything then rewrite prompt
        terminal.clear();
        terminal.write('\x1b[2J\x1b[3J\x1b[H');
        terminal.write(lineText);
        // Position cursor at end of prompt line
        terminal.write(`\x1b[${lineText.length + 1}G`);
        onScrollbackSnapshotRef.current?.(tab.id, lineText);
      } else {
        // No prompt (process running or ambiguous) — clear everything
        terminal.clear();
        terminal.write('\x1b[2J\x1b[3J\x1b[H');
        onScrollbackSnapshotRef.current?.(tab.id, '');
      }
      return;
    }
  }, [action, tab.id]);

  return (
    <section className={`terminal-panel${active ? ' visible' : ''}`} aria-hidden={!active}>
      <div className="terminal-meta">
        <div>
          <strong>{tab.profile.label}</strong>
          <span>{tab.profile.command}</span>
        </div>
        <p>{statusText}</p>
      </div>
      <div className="terminal-host" ref={hostRef} />
    </section>
  );
}
