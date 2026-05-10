import { useEffect, useMemo, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

import { createSessionWebSocket } from '../api';
import type { SessionTab, TerminalAction, WebSocketIncomingMessage, WebSocketOutgoingMessage } from '../types';

type TerminalViewProps = {
  tab: SessionTab;
  active: boolean;
  action?: TerminalAction | null;
  onStatusChange: (sessionId: string, status: SessionTab['status'], errorMessage?: string) => void;
};

export function TerminalView(props: TerminalViewProps) {
  const { tab, active, action, onStatusChange } = props;
  const hostRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const onStatusChangeRef = useRef(onStatusChange);

  useEffect(() => {
    onStatusChangeRef.current = onStatusChange;
  }, [onStatusChange]);

  const statusText = useMemo(() => {
    if (tab.status === 'error' && tab.errorMessage) {
      return tab.errorMessage;
    }
    return tab.status;
  }, [tab.errorMessage, tab.status]);

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

    const socket = createSessionWebSocket(tab.id);
    socketRef.current = socket;

    const sendMessage = (message: WebSocketOutgoingMessage) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify(message));
      }
    };

    socket.addEventListener('open', () => {
      onStatusChangeRef.current(tab.id, 'ready');
      fitAddon.fit();
      sendMessage({ type: 'resize', cols: terminal.cols, rows: terminal.rows });
    });

    socket.addEventListener('message', (event) => {
      const message = JSON.parse(String(event.data)) as WebSocketIncomingMessage;
      if (message.type === 'output') {
        terminal.write(message.data);
        return;
      }

      if (message.type === 'error') {
        onStatusChangeRef.current(tab.id, 'error', message.data);
      }
    });

    socket.addEventListener('close', () => {
      onStatusChangeRef.current(tab.id, 'disconnected');
    });

    socket.addEventListener('error', () => {
      onStatusChangeRef.current(tab.id, 'error', 'WebSocket connection failed');
    });

    const disposable = terminal.onData((data) => {
      sendMessage({ type: 'input', data });
    });

    const handleResize = () => {
      fitAddon.fit();
      sendMessage({ type: 'resize', cols: terminal.cols, rows: terminal.rows });
    };

    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);
      disposable.dispose();
      socket.close();
      terminal.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
      socketRef.current = null;
    };
  }, [tab.id]);

  useEffect(() => {
    if (!active || !terminalRef.current || !fitAddonRef.current || !socketRef.current) {
      return;
    }

    fitAddonRef.current.fit();
    if (socketRef.current.readyState === WebSocket.OPEN) {
      socketRef.current.send(
        JSON.stringify({
          type: 'resize',
          cols: terminalRef.current.cols,
          rows: terminalRef.current.rows,
        } satisfies WebSocketOutgoingMessage),
      );
    }
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
      } else {
        // No prompt (process running or ambiguous) — clear everything
        terminal.clear();
        terminal.write('\x1b[2J\x1b[3J\x1b[H');
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
