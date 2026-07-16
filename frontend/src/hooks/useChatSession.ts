import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createChatWebSocket, getClientId, getBrowserState, getConfig, getLiveChatSessions, inspectBrowserTab, isBackendTemporarilyUnavailable } from '../api';
import { normalizeWorkDir, useChatStore } from '../stores/chat';
import { useWorkspaceStore } from '../stores/workspace';
import { getTerminalHandle } from '../terminalRegistry';
import { terminalCommandDialect, terminalShellLabel } from '../terminalIntegration';
import { reconnectDelay, MAX_RECONNECT_ATTEMPTS } from '../utils/reconnect';
import type { Attachment } from '../components/chat/ChatInput';
import type { BrowserElementSelection, BrowserInspectResult, BrowserSelectionMode, ChatEvent, ChatSessionKind, CodeContext, TerminalContext, TimelineResponse } from '../types';
import type { QueuedMessage } from '../stores/chat';

const CONNECT_TIMEOUT_MS = 10000;
const INITIAL_CONNECT_DELAY_MS = 250;
const STREAM_STALL_MS = 15000;
// ADR-0006: client-side app-level ping interval (server also sends RFC645 protocol pings).
const LIVENESS_PING_INTERVAL_MS = 10000;
// Fix H-1: Maximum time a session is allowed to remain in 'connecting' before
// the watchdog flips it to 'error' so the UI can surface a reconnect affordance
// instead of spinning forever.
const CONNECTING_WATCHDOG_MS = 30000;
const CONNECTING_WATCHDOG_TICK_MS = 2000;

type UseChatSessionResult = {
  sendMessage: (content: string, context?: CodeContext, attachments?: Attachment[]) => Promise<void>;
  cancel: () => void;
  setConfigOption: (configId: string, value: string) => void;
  respondPermission: (permissionId: string, optionId: string) => void;
  rejectPermission: (permissionId: string) => void;
  setAutoApprove: (enabled: boolean) => void;
  connected: boolean;
  fetchTimeline: (sessionId: string, afterSeq?: number) => void;
};

type ChatConnection = {
  sessionId: string;
  kind: ChatSessionKind;
  ws: WebSocket | null;
  connecting: boolean;
  connectTimeout?: number;
  reconnectTimer?: number;
  reconnectAttempts: number;
  seq: number;
  hasConnected: boolean;
  disposed: boolean;
  // ADR-0006: client-side app-level ping timer.
  livenessTimer?: number;
  // Fix H-1: timestamp the most recent transition into 'connecting' so the
  // global watchdog can detect sessions stuck in that state past
  // CONNECTING_WATCHDOG_MS and surface an error to the user.
  connectingSince?: number;
};

function socketIsOpen(conn: ChatConnection | undefined): boolean {
  return conn?.ws?.readyState === WebSocket.OPEN;
}

function isConnectableSession(session: { kind: ChatSessionKind; status?: string }): boolean {
  return session.kind !== 'archived' && session.status !== 'error';
}

function summarizeChatEvent(event: ChatEvent): string {
  switch (event.type) {
    case 'tool_call':
    case 'tool_call_update': {
      const title = event.toolTitle ?? 'tool';
      const status = event.toolStatus ? ` ${event.toolStatus}` : '';
      return `${event.type} ${title}${status}`;
    }
    case 'done':
      return `done${event.stopReason ? ` (${event.stopReason})` : ''}`;
    case 'error':
      return `error${event.error ? `: ${event.error}` : ''}`;
    case 'permission_request':
      return `permission_request ${event.permissionTitle ?? ''}`.trim();
    case 'text':
      return `text chunk (${(event.text ?? '').length} chars)`;
    case 'thinking':
      return `thinking chunk (${(event.thinking ?? '').length} chars)`;
    default:
      return event.type;
  }
}

function isInvalidBrowserToolEvent(event: ChatEvent): boolean {
  const title = (event.toolTitle ?? '').toLowerCase();
  const kind = (event.toolKind ?? '').toLowerCase();
  const status = (event.toolStatus ?? '').toLowerCase();
  const error = (event.error ?? '').toLowerCase();
  const text = `${title} ${kind} ${status} ${error}`;
  if (!text.includes('invalid tool') && !text.includes('unknown tool') && !text.includes('tool not found')) {
    return false;
  }
  return (
    text.includes('browser')
    || text.includes('active_browser_')
    || text.includes('active browser')
  );
}

async function buildActiveBrowserContext(selection: BrowserElementSelection | null, mode: BrowserSelectionMode, preferredTabId?: string | null): Promise<string | null> {
  const state = await getBrowserState();
  const linkedTabId = preferredTabId?.trim() || selection?.tabId?.trim() || '';
  const activeTab = linkedTabId
    ? state.tabs.find((tab) => tab.id === linkedTabId) ?? null
    : (state.tabs.find((tab) => tab.id === state.activeTabId) ?? state.tabs[0] ?? null);
  if (!activeTab) {
    if (linkedTabId) {
      return [
        '[Active browser context]',
        `Linked browser tab ${linkedTabId} is no longer available for this session.`,
        'Browser control status: unavailable until this project has an active WebRTC browser tab again.',
      ].join('\n');
    }
    return null;
  }

  const lines = [
    '[Active browser context]',
    `Title: ${activeTab.title || '(untitled)'}`,
    `URL: ${activeTab.url}`,
  ];

  try {
    let inspected: BrowserInspectResult | null = null;
    if (activeTab.transport === 'webrtc') {
      inspected = await inspectBrowserTab(activeTab.id);
    }
    if (inspected?.title && inspected.title !== activeTab.title) {
      lines.push(`Automation title: ${inspected.title}`);
    }
    if (inspected?.text) {
      lines.push('Visible page text:');
      lines.push(inspected.text.slice(0, 6000));
    }
  } catch (error) {
    lines.push(`Browser inspect status: ${error instanceof Error ? error.message : 'unavailable'}`);
  }

  if (selection && mode === 'detail') {
    lines.push('[Selected browser element]');
    lines.push(`Selector: ${selection.selector}`);
    if (selection.uniqueSelector) {
      lines.push(`Unique selector: ${selection.uniqueSelector}`);
    }
    lines.push(`Tag: ${selection.tagName}`);
    if (selection.role) {
      lines.push(`Role: ${selection.role}`);
    }
    if (selection.text) {
      lines.push(`Text: ${selection.text}`);
    }
    if (selection.attributes && Object.keys(selection.attributes).length > 0) {
      lines.push(`Attributes: ${Object.entries(selection.attributes).map(([k, v]) => `${k}="${v}"`).join(' ')}`);
    }
    if (selection.computedStyle) {
      const cs = selection.computedStyle;
      lines.push(`Computed: display=${cs.display}, position=${cs.position}, ${cs.width}×${cs.height}, visibility=${cs.visibility}`);
    }
    if (selection.boundingRect) {
      lines.push(`Dimensions: ${selection.boundingRect.width}×${selection.boundingRect.height}px`);
    }
    if (selection.boxModel) {
      const bm = selection.boxModel;
      const m = bm.margin;
      const p = bm.padding;
      lines.push(`Box model: margin=${Math.round(m.top)} ${Math.round(m.right)} ${Math.round(m.bottom)} ${Math.round(m.left)}, padding=${Math.round(p.top)} ${Math.round(p.right)} ${Math.round(p.bottom)} ${Math.round(p.left)}, content=${Math.round(bm.contentRect.width)}×${Math.round(bm.contentRect.height)}`);
    }
    if (selection.parentChain && selection.parentChain.length > 0) {
      const path = selection.parentChain.map((item) => {
        let s = item.tagName;
        if (item.id) s += `#${item.id}`;
        if (item.classes.length > 0) s += `.${item.classes.slice(0, 2).join('.')}`;
        return s;
      }).join(' > ');
      lines.push(`DOM path: ${path}`);
    }
    if (selection.accessibilityInfo) {
      const a11y = selection.accessibilityInfo;
      const parts: string[] = [];
      if (a11y.role) parts.push(`role=${a11y.role}`);
      if (a11y.label) parts.push(`label="${a11y.label}"`);
      parts.push(`focusable=${a11y.focusable}`);
      if (a11y.tabIndex !== undefined) parts.push(`tabIndex=${a11y.tabIndex}`);
      if (parts.length > 0) lines.push(`Accessibility: ${parts.join(', ')}`);
    }
    if (selection.eventListeners && selection.eventListeners.length > 0) {
      lines.push(`Events: ${selection.eventListeners.map((l) => l.type).join(', ')}`);
    }
    lines.push('Outer HTML:');
    lines.push(selection.outerHTML.slice(0, 3000));
  }

  if (activeTab.transport === 'webrtc') {
    lines.push('Browser control: Use MCP tools 9ed_browser_goto, 9ed_browser_click, 9ed_browser_type, 9ed_browser_press, 9ed_browser_scroll, 9ed_browser_inspect, 9ed_browser_page_source, 9ed_browser_screenshot, 9ed_browser_console_logs, and 9ed_browser_network_requests for browser actions in this active tab. Browser actions accept timeoutMs when the page or action may be slow; default is 15000ms and max is 60000ms. Use the shortest useful workflow chain: navigate, inspect/page_source/console/network when needed, interact with the necessary button/CTA/link/form/control, then answer when the task is satisfied.');
    lines.push('Browser interaction: use click/type/press when the workflow requires page interaction, whether that need comes from the user request or from your own debugging analysis of the current page state.');
    lines.push('Screenshot guardrail: only call 9ed_browser_screenshot when visual proof/debug, layout verification, or image analysis is needed. For normal DOM/text/navigation/selector tasks, prefer inspect or page_source instead of screenshot.');
  } else {
    lines.push('Browser control: unavailable for this linked tab because it uses Proxy transport. Switch this project to an active WebRTC browser tab if you want the agent to control the browser directly.');
  }
  lines.push('Browser workflow: after every browser tool result, inspect the returned observation and immediately decide whether to call another browser/terminal tool or answer naturally. Do not wait silently after a completed browser tool, and do not repeat inspection once URL/title/visible text already confirms success.');

  return lines.join('\n');
}

function buildActiveTerminalContext(context: TerminalContext): string {
  const lines = [
    '[Active terminal integration]',
    'Status: enabled',
    'Execution target: active visible terminal',
    `Session ID: ${context.sessionId}`,
    `Shell: ${terminalShellLabel(context.shellType)}`,
    `Command dialect: ${terminalCommandDialect(context.shellType)}`,
    `CWD: ${context.cwd || '(unknown)'}`,
    'Use MCP tool active_terminal_run for terminal work when the command is expected to finish and return the shell to idle. Use active_terminal_start for long-running commands like npm run start, dev servers, log tails, watchers, or anything expected to keep running while you debug with browser MCP or inspect logs. Prefer one information-dense command over several confirmation commands. Treat the terminal as completed only when the shell has clearly returned to idle. Chain another targeted terminal or browser action when completed output reveals the next necessary diagnostic, fix, test, or reproduction step; answer when the current task is satisfied. Do not call tasklist/Get-Process/read again for the same fact. Do not wait silently after a completed terminal tool. Use active_terminal_read to inspect a command that is still running, gather logs after browser reproduction, or check whether the shell is waiting for input again before sending another terminal command.',
  ];

  if (context.scrollback.trim()) {
    lines.push('Recent terminal output:');
    lines.push('```text');
    lines.push(context.scrollback);
    lines.push('```');
  } else {
    lines.push('Recent terminal output: (empty)');
  }

  return lines.join('\n');
}

export function useChatSession(): UseChatSessionResult {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const sessions = useChatStore((s) => s.sessions);
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);
  const browserSelection = useChatStore((s) => s.browserSelection);
  const browserSelectionMode = useChatStore((s) => s.browserSelectionMode);
  const browserSelectionCapture = useChatStore((s) => s.browserSelectionCapture);
  const useActiveTerminal = useChatStore((s) => s.useActiveTerminal);
  const activeTerminalId = useChatStore((s) => s.activeTerminalId);
  const projects = useWorkspaceStore((s) => s.projects);
  const addMessage = useChatStore((s) => s.addMessage);
  const handleChatEvent = useChatStore((s) => s.handleChatEvent);
  const setSessionStatus = useChatStore((s) => s.setSessionStatus);
  const setSessionStalled = useChatStore((s) => s.setSessionStalled);
  const appendSessionDebug = useChatStore((s) => s.appendSessionDebug);
  const setSessionKind = useChatStore((s) => s.setSessionKind);
  const finalizeAssistantMessage = useChatStore((s) => s.finalizeAssistantMessage);
  const dequeueMessage = useChatStore((s) => s.dequeueMessage);
  const refreshSessionState = useChatStore((s) => s.refreshSessionState);
  const resumeSession = useChatStore((s) => s.resumeSession);
  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId) ?? null,
    [activeSessionId, sessions],
  );

  const connectionsRef = useRef<Map<string, ChatConnection>>(new Map());
  const activeSessionIdRef = useRef<string | null>(activeSessionId);
  const [connected, setConnected] = useState(false);

  const handleChatEventRef = useRef(handleChatEvent);
  handleChatEventRef.current = handleChatEvent;
  const finalizeRef = useRef(finalizeAssistantMessage);
  finalizeRef.current = finalizeAssistantMessage;
  const dequeueRef = useRef(dequeueMessage);
  dequeueRef.current = dequeueMessage;
  const refreshSessionStateRef = useRef(refreshSessionState);
  refreshSessionStateRef.current = refreshSessionState;
  const resumeSessionRef = useRef(resumeSession);
  resumeSessionRef.current = resumeSession;
  const sendQueuedRef = useRef<((queued: QueuedMessage) => Promise<void>) | null>(null);
  const setSessionKindRef = useRef(setSessionKind);
  setSessionKindRef.current = setSessionKind;
  const appendSessionDebugRef = useRef(appendSessionDebug);
  appendSessionDebugRef.current = appendSessionDebug;
  const upgradedRef = useRef<Set<string>>(new Set());
  const browserToolRecoveryRef = useRef<Map<string, number>>(new Map());
  const stallTimersRef = useRef<Map<string, number>>(new Map());
  const lastTerminalControlKeyRef = useRef<string>('');
  const lastBrowserControlKeyRef = useRef<string>('');
  // Ref populated later with reassertSoftControls; used from ws.onopen (VAL-HARDEN-003).
  const reassertSoftControlsRef = useRef<() => void>(() => {});
  // ADR-0002: per-session cursor tracking for stale/gap detection.
  const cursorRef = useRef<Map<string, { seq: number; epoch: string }>>(new Map());
  // Fix M-8: per-session highest seq already applied to the chat store. Used
  // to dedupe events when timeline catch-up overlaps with live stream — the
  // server's replay can re-deliver events we've already rendered, which would
  // otherwise duplicate assistant messages or tool calls.
  const appliedSeqRef = useRef<Map<string, number>>(new Map());

  const connectionTargets = useMemo(
    () => sessions
      .filter(isConnectableSession)
      .filter((s) => s.id === activeSessionId || s.status === 'streaming' || s.pendingPermission)
      .map((s) => ({ id: s.id, kind: s.kind })),
    [activeSessionId, sessions],
  );
  const connectionKey = connectionTargets.map((s) => `${s.id}:${s.kind}`).join('|');
  const liveSessionKey = sessions
    .filter(isConnectableSession)
    .map((s) => `${s.id}:${s.kind}:${s.status}`)
    .join('|');

  const updateActiveConnected = useCallback(() => {
    const activeId = activeSessionIdRef.current;
    setConnected(Boolean(activeId && socketIsOpen(connectionsRef.current.get(activeId))));
  }, []);

  const clearConnectionTimers = useCallback((conn: ChatConnection) => {
    if (conn.connectTimeout !== undefined) {
      window.clearTimeout(conn.connectTimeout);
      conn.connectTimeout = undefined;
    }
    if (conn.reconnectTimer !== undefined) {
      window.clearTimeout(conn.reconnectTimer);
      conn.reconnectTimer = undefined;
    }
    // ADR-0006: clear client-side app-level ping timer.
    if (conn.livenessTimer !== undefined) {
      window.clearInterval(conn.livenessTimer);
      conn.livenessTimer = undefined;
    }
  }, []);

  const clearStallTimer = useCallback((sessionId: string) => {
    const timer = stallTimersRef.current.get(sessionId);
    if (timer !== undefined) {
      window.clearTimeout(timer);
      stallTimersRef.current.delete(sessionId);
    }
  }, []);

  const scheduleStallTimer = useCallback((sessionId: string) => {
    clearStallTimer(sessionId);
    stallTimersRef.current.set(sessionId, window.setTimeout(() => {
      void (async () => {
        const beforeRefresh = useChatStore.getState().sessions.find((s) => s.id === sessionId);
        if (beforeRefresh?.status !== 'streaming') return;
        const beforeEventAt = beforeRefresh.lastEventAt ?? 0;
        const beforeMessageCount = beforeRefresh.messages.length;

        appendSessionDebugRef.current(sessionId, {
          source: 'session',
          level: 'info',
          message: 'stall probe: refreshing persisted session state before marking stalled',
        });
        await refreshSessionStateRef.current(sessionId).catch(() => {});

        const session = useChatStore.getState().sessions.find((s) => s.id === sessionId);
        if (session?.status !== 'streaming') {
          appendSessionDebugRef.current(sessionId, {
            source: 'session',
            level: 'info',
            message: 'stall probe cleared after session state refresh',
          });
          return;
        }
        const afterEventAt = session.lastEventAt ?? 0;
        const replayRecovered = afterEventAt > beforeEventAt || session.messages.length > beforeMessageCount;
        if (replayRecovered) {
          appendSessionDebugRef.current(sessionId, {
            source: 'session',
            level: 'info',
            message: 'stall guard: recovered by state resync',
          });
          scheduleStallTimer(sessionId);
          return;
        }

        useChatStore.getState().setSessionStalled(sessionId, true);
        const lastTool = [...session.messages].reverse().find((msg) => msg.role === 'tool_call' && msg.toolCall);
        const lastToolStatus = lastTool?.toolCall?.status;
        const lastToolTitle = lastTool?.toolCall?.title ?? '(belum ada tool event)';
        appendSessionDebugRef.current(sessionId, {
          source: 'session',
          level: 'warn',
          message: lastToolStatus === 'completed'
            ? `stall detected: last tool completed (${lastToolTitle}) but no done event arrived`
            : `stall detected: no new updates after ${Math.round(STREAM_STALL_MS / 1000)}s; last step ${lastToolTitle}`,
        });
      })();
    }, STREAM_STALL_MS));
  }, [clearStallTimer]);

  const stopConnection = useCallback((sessionId: string) => {
    const conn = connectionsRef.current.get(sessionId);
    if (!conn) return;
    conn.disposed = true;
    conn.seq += 1;
    clearConnectionTimers(conn);
    const ws = conn.ws;
    conn.ws = null;
    connectionsRef.current.delete(sessionId);
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      ws.close();
    }
    browserToolRecoveryRef.current.delete(sessionId);
    clearStallTimer(sessionId);
    appendSessionDebugRef.current(sessionId, {
      source: 'ws',
      level: 'info',
      message: 'socket disposed',
    });
    updateActiveConnected();
  }, [clearConnectionTimers, clearStallTimer, updateActiveConnected]);

  const startConnection = useCallback((sessionId: string, kind: ChatSessionKind) => {
    const existing = connectionsRef.current.get(sessionId);
    if (existing) {
      existing.kind = kind;
      if (
        existing.connecting ||
        existing.ws?.readyState === WebSocket.OPEN ||
        existing.ws?.readyState === WebSocket.CONNECTING ||
        existing.reconnectTimer !== undefined
      ) {
        return;
      }
      stopConnection(sessionId);
    }

    const conn: ChatConnection = {
      sessionId,
      kind,
      ws: null,
      connecting: false,
      reconnectAttempts: 0,
      seq: 0,
      hasConnected: false,
      disposed: false,
    };
    connectionsRef.current.set(sessionId, conn);

    const getLiveSession = () => useChatStore.getState().sessions.find((s) => s.id === sessionId);

    const setConnecting = () => {
      const session = getLiveSession();
      if (!session || !isConnectableSession(session) || session.status === 'streaming') return;
      setSessionStatus(sessionId, 'connecting');
      // Fix H-1: only record the transition timestamp if we don't already have
      // one in-flight; the watchdog measures contiguous connecting time, not
      // per-attempt time, so back-to-back reconnect attempts still trigger.
      if (conn.connectingSince === undefined) {
        conn.connectingSince = Date.now();
      }
    };

    const markConnectionError = (seq: number) => {
      if (seq !== conn.seq || conn.disposed) return;
      const session = getLiveSession();
      if (!session || session.kind === 'archived') return;
      setSessionStatus(sessionId, 'error');
      // Fix H-1: terminal state — clear watchdog so a later retry starts fresh.
      conn.connectingSince = undefined;
      stopConnection(sessionId);
      updateActiveConnected();
    };

    const connect = async () => {
      if (conn.disposed) return;
      if (conn.connecting) return;
      if (isBackendTemporarilyUnavailable()) {
        const delay = reconnectDelay(conn.reconnectAttempts);
        conn.reconnectAttempts += 1;
        conn.reconnectTimer = window.setTimeout(() => {
          conn.reconnectTimer = undefined;
          void connect();
        }, delay);
        return;
      }
      const session = getLiveSession();
      if (!session || !isConnectableSession(session)) {
        stopConnection(sessionId);
        return;
      }

      const seq = conn.seq + 1;
      conn.seq = seq;
      conn.connecting = true;
      setConnecting();
      appendSessionDebugRef.current(sessionId, {
        source: 'ws',
        level: 'info',
        message: `connecting (attempt ${conn.reconnectAttempts + 1})`,
      });

      try {
        const liveSessions = await getLiveChatSessions();
        if (seq !== conn.seq || conn.disposed) return;
        if (!liveSessions.some((live) => live.id === sessionId)) {
          void refreshSessionStateRef.current(sessionId);
          stopConnection(sessionId);
          const resumed = await resumeSessionRef.current(sessionId);
          if (!resumed) {
            setSessionStatus(sessionId, 'error');
          }
          appendSessionDebugRef.current(sessionId, {
            source: 'session',
            level: 'warn',
            message: 'live session missing; attempted resume',
          });
          return;
        }
      } catch (error) {
        if (seq !== conn.seq || conn.disposed) return;
        conn.connecting = false;
        if (isBackendTemporarilyUnavailable()) {
          const delay = reconnectDelay(conn.reconnectAttempts);
          conn.reconnectAttempts += 1;
          conn.reconnectTimer = window.setTimeout(() => {
            conn.reconnectTimer = undefined;
            void connect();
          }, delay);
          return;
        }
        setSessionStatus(sessionId, 'error');
        stopConnection(sessionId);
        appendSessionDebugRef.current(sessionId, {
          source: 'ws',
          level: 'error',
          message: `preflight failed: ${error instanceof Error ? error.message : 'unknown error'}`,
        });
        return;
      }

      const ws = createChatWebSocket(sessionId);
      conn.ws = ws;
      conn.connecting = false;
      updateActiveConnected();

      if (conn.connectTimeout !== undefined) {
        window.clearTimeout(conn.connectTimeout);
      }
      conn.connectTimeout = window.setTimeout(() => {
        if (seq !== conn.seq || conn.disposed) return;
        if (ws.readyState === WebSocket.CONNECTING) {
          ws.close();
        }
      }, CONNECT_TIMEOUT_MS);

      const scheduleReconnect = () => {
        if (seq !== conn.seq || conn.disposed) return;
        const live = getLiveSession();
        if (!live || !isConnectableSession(live)) {
          stopConnection(sessionId);
          return;
        }
        if (conn.reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
          markConnectionError(seq);
          return;
        }

        const delay = reconnectDelay(conn.reconnectAttempts);
        conn.reconnectAttempts += 1;
        setConnecting();
        conn.reconnectTimer = window.setTimeout(() => {
          conn.reconnectTimer = undefined;
          void connect();
        }, delay);
      };

      ws.onopen = () => {
        if (seq !== conn.seq || conn.disposed) {
          ws.close();
          return;
        }
        if (conn.connectTimeout !== undefined) {
          window.clearTimeout(conn.connectTimeout);
          conn.connectTimeout = undefined;
        }
        const shouldSync = conn.hasConnected || conn.reconnectAttempts > 0;
        conn.hasConnected = true;
        conn.connecting = false;
        conn.reconnectAttempts = 0;
        // Fix H-1: connection is established; reset watchdog timer.
        conn.connectingSince = undefined;
        setSessionStatus(sessionId, 'idle');
        setSessionStalled(sessionId, false);
        appendSessionDebugRef.current(sessionId, {
          source: 'ws',
          level: 'info',
          message: shouldSync ? 'socket opened and state resynced' : 'socket opened',
        });
        // VAL-HARDEN-003: clear soft-control dedupe keys so the browser and
        // terminal effects re-send current intent after open/reconnect.
        // Without this, mid-flight toggles leave last*ControlKeyRef equal to
        // the desired payload and the effects early-return with backend stale.
        lastBrowserControlKeyRef.current = '';
        lastTerminalControlKeyRef.current = '';
        // Explicit force-resend: do not rely only on the `connected` dependency
        // flip (same true→true path on some reconnect races still works).
        reassertSoftControlsRef.current();
        updateActiveConnected();
        if (shouldSync) {
          void refreshSessionStateRef.current(sessionId);
        }
        if (conn.kind === 'resumable' && !upgradedRef.current.has(sessionId)) {
          setSessionKindRef.current(sessionId, 'live');
          upgradedRef.current.add(sessionId);
        }
        // ADR-0006: send hello with clientId for multi-device / multi-tab resume.
        const clientId = getClientId();
        ws.send(JSON.stringify({ type: 'hello', clientId, ts: Date.now() }));
        // ADR-0002: on reconnect, fetch timeline events after last known cursor
        // to fill any gaps that occurred while the socket was disconnected.
        // Include epoch so the server can detect stale cursors.
        const cursor = cursorRef.current.get(sessionId);
        if (cursor && cursor.seq > 0) {
          ws.send(JSON.stringify({ type: 'fetch_timeline', afterSeq: cursor.seq, epoch: cursor.epoch }));
        }
        // ADR-0006: start client-side app-level ping (server also sends RFC645 protocol pings).
        conn.livenessTimer = window.setInterval(() => {
          if (seq !== conn.seq || conn.disposed || !socketIsOpen(conn)) {
            return;
          }
          ws.send(JSON.stringify({ type: 'ping', ts: Date.now() }));
        }, LIVENESS_PING_INTERVAL_MS);
      };

      ws.onmessage = (event) => {
        if (seq !== conn.seq || conn.disposed) return;
        try {
          const data = JSON.parse(event.data) as ChatEvent;
          // ADR-0006: skip liveness control messages (pong, hello_ack).
          if (data.type === 'pong' || data.type === 'hello_ack') {
            return;
          }
          // ADR-0002: replay-on-subscribe metadata envelope — initialize cursor
          // with the current window and epoch so subsequent fetch_timeline
          // requests can detect stale cursors and gaps.
          if (data.type === 'replay_meta') {
            const meta = data as unknown as { type: string; epoch: string; window: { minSeq: number; maxSeq: number; nextSeq: number } };
            if (meta.epoch && meta.window) {
              // Initialize cursor to maxSeq of the replay window so the client
              // only fetches events that arrived after the replay.
              const cursorSeq = meta.window.maxSeq > 0 ? meta.window.maxSeq : 0;
              cursorRef.current.set(sessionId, { seq: cursorSeq, epoch: meta.epoch });
              appendSessionDebugRef.current(sessionId, {
                source: 'ws',
                level: 'info',
                message: `replay_meta: cursor initialized seq=${cursorSeq} epoch=${meta.epoch.slice(0, 8)} window=${meta.window.minSeq}..${meta.window.maxSeq}`,
              });
            }
            return;
          }
          // ADR-0002: timeline catch-up response — replay events, update cursor,
          // and handle staleCursor/gap/reset flags by resetting cursor and
          // re-fetching the timeline tail.
          if (data.type === 'timeline') {
            const timeline = data as unknown as TimelineResponse;
            // ADR-0002: if the server signals reset (staleCursor, gap, or
            // explicit reset), delete the cursor and re-fetch the tail.
            if (timeline.reset || timeline.staleCursor || timeline.gap) {
              cursorRef.current.delete(sessionId);
              appendSessionDebugRef.current(sessionId, {
                source: 'ws',
                level: 'warn',
                message: `timeline reset${timeline.staleCursor ? ' (staleCursor)' : ''}${timeline.gap ? ' (gap)' : ''}: re-fetching tail`,
              });
              conn.ws?.send(JSON.stringify({ type: 'fetch_timeline', afterSeq: 0 }));
              return;
            }
            if (timeline.events && timeline.events.length > 0) {
              for (const tevt of timeline.events) {
                const evt = { ...tevt.event, seq: tevt.seq, epoch: tevt.epoch };
                // Fix M-8: dedupe by seq. Events with seq <= the highest
                // seq we already applied for this session are skipped
                // to avoid double-rendering messages when catch-up
                // overlaps with the live stream.
                const applied = appliedSeqRef.current.get(sessionId) ?? 0;
                if (tevt.seq > 0 && tevt.seq <= applied) {
                  continue;
                }
                handleChatEventRef.current(sessionId, evt);
                if (tevt.seq > applied) {
                  appliedSeqRef.current.set(sessionId, tevt.seq);
                }
              }
              const last = timeline.events[timeline.events.length - 1];
              cursorRef.current.set(sessionId, { seq: last.seq, epoch: last.epoch ?? timeline.epoch });
              // If hasNewer (or legacy hasMore), fetch the next page.
              if (timeline.hasNewer || timeline.hasMore) {
                conn.ws?.send(JSON.stringify({ type: 'fetch_timeline', afterSeq: last.seq, epoch: last.epoch ?? timeline.epoch }));
              }
            } else {
              // No events returned — update epoch from the response.
              if (timeline.epoch) {
                const existing = cursorRef.current.get(sessionId);
                cursorRef.current.set(sessionId, { seq: existing?.seq ?? 0, epoch: timeline.epoch });
              }
            }
            return;
          }
          // ADR-0005: PTY replay — write raw bytes to the associated terminal.
          if (data.type === 'pty_replay') {
            const sessionState = useChatStore.getState().sessions.find((s) => s.id === sessionId);
            const terminalId = sessionState?.terminalId ?? sessionId;
            const handle = getTerminalHandle(terminalId);
            if (handle && data.text) {
              handle.write(data.text);
            }
            appendSessionDebugRef.current(sessionId, {
              source: 'ws',
              level: 'info',
              message: `pty replay ${data.text?.length ?? 0} bytes`,
            });
            return;
          }
          // ADR-0004: session resumed — refresh state, don't show as error.
          if (data.type === 'session_resumed') {
            // ADR-0002: reset cursor on epoch change (session resumed = new
            // epoch). Seed the cursor with the fresh epoch carried by the
            // event so the subsequent fetch_timeline is not treated as stale
            // and so the client tracks the new timeline.
            const newEpoch = data.epoch ?? '';
            cursorRef.current.set(sessionId, { seq: 0, epoch: newEpoch });
            // Fix M-8: reset dedupe state on epoch change; the new epoch
            // re-numbers seq from 0, so prior values are not comparable.
            appliedSeqRef.current.set(sessionId, 0);
            // ADR-0004 / VAL-RESUME-005: the agent recovered — clear any
            // prior crash flag so the reconnect prompt disappears.
            useChatStore.getState().clearCrashState(sessionId);
            appendSessionDebugRef.current(sessionId, {
              source: 'ws',
              level: 'info',
              message: `agent session resumed after crash (epoch=${newEpoch.slice(0, 8) || 'none'})`,
            });
            // ADR-0002: re-fetch the timeline tail to catch up on events that
            // occurred during the epoch change (VAL-CATCHUP-005). Pass the new
            // epoch so the server treats the cursor as current.
            conn.ws?.send(JSON.stringify({ type: 'fetch_timeline', afterSeq: 0, epoch: newEpoch }));
            void refreshSessionStateRef.current(sessionId);
            return;
          }
          // ADR-0005: TUI snapshot request — serialize terminal and send back.
          if (data.type === 'tui_snapshot_request') {
            const sessionState = useChatStore.getState().sessions.find((s) => s.id === sessionId);
            const terminalId = sessionState?.terminalId ?? sessionId;
            const handle = getTerminalHandle(terminalId);
            if (handle) {
              const snapshot = handle.serialize();
              if (snapshot && conn.ws && socketIsOpen(conn)) {
                conn.ws.send(JSON.stringify({ type: 'tui_snapshot', content: snapshot }));
              }
            }
            return;
          }
          // ADR-0005: TUI snapshot — write serialized terminal state.
          if (data.type === 'tui_snapshot') {
            const sessionState = useChatStore.getState().sessions.find((s) => s.id === sessionId);
            const terminalId = sessionState?.terminalId ?? sessionId;
            const handle = getTerminalHandle(terminalId);
            if (handle && data.text) {
              handle.write(data.text);
            }
            appendSessionDebugRef.current(sessionId, {
              source: 'ws',
              level: 'info',
              message: `tui snapshot ${data.text?.length ?? 0} bytes`,
            });
            return;
          }
          // ADR-0005: collaborative cursor overlay — another client's cursor.
          if (data.type === 'cursor_position') {
            appendSessionDebugRef.current(sessionId, {
              source: 'ws',
              level: 'info',
              message: `cursor from ${data.text ?? 'unknown'} at ${data.toolTitle ?? '0:0'}`,
            });
            return;
          }
          // ADR-0005: input locked — another client is typing.
          // Dedicated event type (VAL-PTY-004) with Holder and TTL fields,
          // not a piggyback on the generic error event.
          if (data.type === 'input_locked') {
            appendSessionDebugRef.current(sessionId, {
              source: 'ws',
              level: 'warn',
              message: `input locked by ${data.holder ?? 'another client'} (ttl ${data.ttl ?? 0}ms)`,
            });
            return;
          }
          // Legacy fallback: some older servers piggyback on error events.
          if (data.type === 'error' && data.error === 'input_locked') {
            appendSessionDebugRef.current(sessionId, {
              source: 'ws',
              level: 'warn',
              message: `input locked by ${data.toolTitle ?? 'another client'}`,
            });
            return;
          }
          // ADR-0003: client backpressure — agent cancelled due to overflow.
          // The client may have missed events (seq gap), so re-fetch the
          // timeline tail to fill any gaps.
          if (data.type === 'done' && data.stopReason === 'client_backpressure') {
            appendSessionDebugRef.current(sessionId, {
              source: 'ws',
              level: 'warn',
              message: 'agent cancelled due to client backpressure; re-fetching timeline for seq gap',
            });
            const cursor = cursorRef.current.get(sessionId);
            const afterSeq = cursor?.seq ?? 0;
            const epoch = cursor?.epoch ?? '';
            conn.ws?.send(JSON.stringify({ type: 'fetch_timeline', afterSeq, epoch }));
          }
          // ADR-0002: update cursor tracking (seq + epoch).
          if (data.seq !== undefined && data.seq > 0) {
            cursorRef.current.set(sessionId, { seq: data.seq, epoch: data.epoch ?? cursorRef.current.get(sessionId)?.epoch ?? '' });
          }
          // Fix M-8: dedupe live events. If we've already applied an event
          // with this seq (or higher) the server is replaying overlap
          // between catch-up and live stream — skip to avoid duplicates.
          if (data.seq !== undefined && data.seq > 0) {
            const applied = appliedSeqRef.current.get(sessionId) ?? 0;
            if (data.seq <= applied) {
              return;
            }
            appliedSeqRef.current.set(sessionId, data.seq);
          }
          appendSessionDebugRef.current(sessionId, {
            source: data.type === 'tool_call' || data.type === 'tool_call_update' ? 'tool' : 'ws',
            level: data.type === 'error' ? 'error' : 'info',
            message: `recv ${summarizeChatEvent(data)}`,
          });
          handleChatEventRef.current(sessionId, data);
          if (data.type === 'done' || data.type === 'error') {
            clearStallTimer(sessionId);
          } else {
            scheduleStallTimer(sessionId);
          }

          if (isInvalidBrowserToolEvent(data)) {
            const now = Date.now();
            const lastRecoveryAt = browserToolRecoveryRef.current.get(sessionId) ?? 0;
            if (now - lastRecoveryAt > 10_000) {
              browserToolRecoveryRef.current.set(sessionId, now);
              const state = useChatStore.getState();
              const session = state.sessions.find((s) => s.id === sessionId);
              // Soft-toggle recovery (VAL-SOFT-TOGGLE-009): when the agent
              // emits an invalid/unknown browser tool event, the backend has
              // likely lost its UseActiveBrowser state. Instead of a hard
              // restart (which destroys the ACP subprocess and causes seconds
              // of disconnection), re-send the lightweight `set_use_active_browser`
              // WebSocket message to re-sync the backend's in-memory flag.
              // Only recover if the session that emitted the event is still
              // the active one AND still has the browser enabled, so we never
              // re-enable browser MCP on a session the user has moved away
              // from or explicitly disabled.
              if (session?.useActiveBrowser && state.activeSessionId === sessionId) {
                const recoveryProject = useWorkspaceStore.getState().projects.find(
                  (project) => normalizeWorkDir(project.path) === normalizeWorkDir(session.workDir),
                );
                const recoveryTabId = recoveryProject?.activeBrowserTabId ?? null;
                if (conn.ws && conn.ws.readyState === WebSocket.OPEN) {
                  conn.ws.send(JSON.stringify({
                    type: 'set_use_active_browser',
                    useActiveBrowser: true && !!recoveryTabId,
                    activeBrowserTabId: recoveryTabId,
                  }));
                  appendSessionDebugRef.current(sessionId, {
                    source: 'client',
                    level: 'info',
                    message: 'invalid browser tool event: re-sent set_use_active_browser (soft recovery)',
                  });
                }
              }
            }
          }

          if (data.type === 'done') {
            finalizeRef.current(sessionId);

            const next = dequeueRef.current(sessionId);
            if (next && sendQueuedRef.current) {
              setTimeout(() => { void sendQueuedRef.current?.(next); }, 100);
            }
          }
        } catch {
          appendSessionDebugRef.current(sessionId, {
            source: 'ws',
            level: 'error',
            message: 'failed to parse ws message',
          });
        }
      };

      ws.onclose = () => {
        if (seq !== conn.seq || conn.disposed) return;
        if (conn.connectTimeout !== undefined) {
          window.clearTimeout(conn.connectTimeout);
          conn.connectTimeout = undefined;
        }
        if (conn.ws === ws) {
          conn.ws = null;
        }
        conn.connecting = false;
        clearStallTimer(sessionId);
        appendSessionDebugRef.current(sessionId, {
          source: 'ws',
          level: 'warn',
          message: 'socket closed; scheduling reconnect',
        });
        updateActiveConnected();
        scheduleReconnect();
      };

      ws.onerror = () => {
        if (seq !== conn.seq || conn.disposed) return;
        if (conn.connectTimeout !== undefined) {
          window.clearTimeout(conn.connectTimeout);
          conn.connectTimeout = undefined;
        }
        updateActiveConnected();
        conn.connecting = false;
        clearStallTimer(sessionId);
        appendSessionDebugRef.current(sessionId, {
          source: 'ws',
          level: 'error',
          message: 'socket error',
        });
        if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
          ws.close();
        } else {
          scheduleReconnect();
        }
      };
    };

    conn.reconnectTimer = window.setTimeout(() => {
      conn.reconnectTimer = undefined;
      void connect();
    }, INITIAL_CONNECT_DELAY_MS);
  }, [setSessionStatus, stopConnection, updateActiveConnected]);

  useEffect(() => {
    activeSessionIdRef.current = activeSessionId;
    updateActiveConnected();
  }, [activeSessionId, updateActiveConnected]);

  useEffect(() => {
    const desired = new Map(connectionTargets.map((target) => [target.id, target.kind]));
    const liveSessions = new Map(
      useChatStore.getState().sessions
        .filter(isConnectableSession)
        .map((session) => [session.id, session.kind]),
    );

    for (const sessionId of Array.from(connectionsRef.current.keys())) {
      const stillLive = liveSessions.get(sessionId);
      if (!stillLive) {
        stopConnection(sessionId);
      } else if (!desired.has(sessionId)) {
        desired.set(sessionId, stillLive);
      }
    }

    for (const [sessionId, kind] of desired) {
      startConnection(sessionId, kind);
    }

    updateActiveConnected();
  }, [connectionKey, connectionTargets, liveSessionKey, startConnection, stopConnection, updateActiveConnected]);

  // Fix H-1: global "connecting forever" watchdog. Periodically scans all
  // tracked connections; any session that has remained in 'connecting' beyond
  // CONNECTING_WATCHDOG_MS is forcibly transitioned to 'error', and the
  // ongoing connect cycle is stopped so the user can retry instead of staring
  // at a perpetual spinner. The watchdog is conservative: it only fires for
  // connections whose store status is still 'connecting' (so a session that
  // already became idle/streaming will not be affected even if connectingSince
  // is briefly stale).
  useEffect(() => {
    const interval = window.setInterval(() => {
      const now = Date.now();
      for (const [sessionId, conn] of connectionsRef.current.entries()) {
        if (conn.disposed) continue;
        if (conn.connectingSince === undefined) continue;
        if (now - conn.connectingSince < CONNECTING_WATCHDOG_MS) continue;
        const session = useChatStore.getState().sessions.find((s) => s.id === sessionId);
        if (!session) {
          conn.connectingSince = undefined;
          continue;
        }
        if (session.status !== 'connecting') {
          conn.connectingSince = undefined;
          continue;
        }
        appendSessionDebugRef.current(sessionId, {
          source: 'ws',
          level: 'error',
          message: `connection stuck in 'connecting' for ${Math.round((now - conn.connectingSince) / 1000)}s; surfacing error`,
        });
        setSessionStatus(sessionId, 'error');
        conn.connectingSince = undefined;
        stopConnection(sessionId);
      }
      updateActiveConnected();
    }, CONNECTING_WATCHDOG_TICK_MS);
    return () => window.clearInterval(interval);
  }, [setSessionStatus, stopConnection, updateActiveConnected]);

  useEffect(() => {
    return () => {
      for (const sessionId of Array.from(connectionsRef.current.keys())) {
        stopConnection(sessionId);
      }
    };
  }, [stopConnection]);

  const sendMessage = useCallback(
    async (content: string, context?: CodeContext, attachments?: Attachment[]) => {
      if (!activeSessionId) return;
      const conn = connectionsRef.current.get(activeSessionId);
      if (!conn?.ws || conn.ws.readyState !== WebSocket.OPEN) return;
      const activeSession = useChatStore.getState().sessions.find((session) => session.id === activeSessionId);

      const displayContent = attachments?.length
        ? content + '\n\n' + attachments.map((a) => `📎 ${a.name}`).join('\n')
        : content;

      const msgId = Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
      const userMessage = {
        id: msgId,
        role: 'user' as const,
        content: displayContent,
        context,
        timestamp: Date.now(),
      };
      addMessage(activeSessionId, userMessage);
      setSessionStatus(activeSessionId, 'streaming');
      setSessionStalled(activeSessionId, false);
      scheduleStallTimer(activeSessionId);
      appendSessionDebugRef.current(activeSessionId, {
        source: 'client',
        level: 'info',
        message: `sent user message (${content.length} chars${attachments?.length ? `, ${attachments.length} attachment(s)` : ''})`,
      });

      let outboundContent = content;
      const browserEnabled = activeSession?.useActiveBrowser ?? useActiveBrowser;
      const browserSelectionForSession = activeSession?.browserSelection ?? browserSelection;
      const browserSelectionModeForSession = activeSession?.browserSelectionMode ?? browserSelectionMode;
      const browserSelectionCaptureForSession = activeSession?.browserSelectionCapture ?? browserSelectionCapture;
      const effectiveBrowserSelectionMode: BrowserSelectionMode =
        browserSelectionModeForSession === 'screenshot' && !browserSelectionCaptureForSession?.path
          ? 'detail'
          : browserSelectionModeForSession;
      let outboundAttachments = attachments ? [...attachments] : undefined;
      if (browserEnabled) {
        try {
          const browserProject = useWorkspaceStore.getState().projects.find((project) => normalizeWorkDir(project.path) === normalizeWorkDir(activeSession?.workDir));
          const browserContext = await buildActiveBrowserContext(browserSelectionForSession, effectiveBrowserSelectionMode, browserProject?.activeBrowserTabId);
          if (browserContext) {
            outboundContent = `${browserContext}\n\n[User request]\n${content}`;
          }
        } catch {
        }

        if (browserSelectionModeForSession === 'screenshot' && browserSelectionCaptureForSession?.path) {
          outboundAttachments = [
            ...(outboundAttachments ?? []),
            {
              type: 'image',
              path: browserSelectionCaptureForSession.path,
              name: browserSelectionCaptureForSession.name,
              previewUrl: browserSelectionCaptureForSession.dataUrl,
            } as Attachment,
          ];
        }
      }

      // Append terminal context if active
      const terminalIdForSession = activeSession?.terminalId ?? activeTerminalId;
      const terminalEnabled = activeSession?.useActiveTerminal ?? useActiveTerminal;
      if (terminalEnabled && terminalIdForSession) {
        try {
          const handle = getTerminalHandle(terminalIdForSession);
          if (handle) {
            const maxLines = (await getConfig().catch(() => ({ terminalAiMaxLines: 100 }))).terminalAiMaxLines ?? 100;
            const scrollback = handle.getScrollback(maxLines || 10000);
            const terminalContext = buildActiveTerminalContext({
              sessionId: terminalIdForSession,
              cwd: handle.cwd,
              shellType: handle.shellType,
              scrollback,
            });
            outboundContent = `${outboundContent}\n\n${terminalContext}`;
          }
        } catch {
        }
      }

      const payload: { type: string; content: string; context?: unknown; attachments?: Attachment[] } = { type: 'message', content: outboundContent };
      if (context) {
        payload.context = context;
      }
      if (outboundAttachments?.length) {
        payload.attachments = outboundAttachments;
      }
      conn.ws.send(JSON.stringify(payload));
      appendSessionDebugRef.current(activeSessionId, {
        source: 'ws',
        level: 'info',
        message: 'ws message payload sent',
      });
    },
    [activeSessionId, addMessage, browserSelection, browserSelectionCapture, browserSelectionMode, scheduleStallTimer, setSessionStalled, setSessionStatus, useActiveBrowser, useActiveTerminal, activeTerminalId],
  );

  sendQueuedRef.current = async (queued: QueuedMessage) => {
    await sendMessage(queued.content, undefined, queued.attachments as Attachment[] | undefined);
  };

  const sendControl = useCallback((payload: unknown): boolean => {
    if (!activeSessionId) return false;
    const ws = connectionsRef.current.get(activeSessionId)?.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) return false;
    ws.send(JSON.stringify(payload));
    const type = typeof payload === 'object' && payload !== null && 'type' in (payload as Record<string, unknown>)
      ? String((payload as Record<string, unknown>).type)
      : 'control';
    appendSessionDebugRef.current(activeSessionId, {
      source: 'client',
      level: 'info',
      message: `sent control ${type}`,
    });
    return true;
  }, [activeSessionId]);

  // Build + send both soft control messages from current store state.
  // Called by the normal effects AND by WS onopen (VAL-HARDEN-003) so
  // reconnect forces resend even when the `connected` dep alone is not enough
  // to re-run effects (e.g. same desired payload after mid-flight toggle).
  const reassertSoftControls = useCallback(() => {
    if (!activeSessionId) return;
    const state = useChatStore.getState();
    const session = state.sessions.find((s) => s.id === activeSessionId);
    const workspaceProjects = useWorkspaceStore.getState().projects;

    // Terminal soft control — session-scoped primary source (VAL-HARDEN-002).
    const desiredUseActiveTerminal = session?.useActiveTerminal ?? state.useActiveTerminal;
    const terminalIdForSession = session?.terminalId ?? state.activeTerminalId ?? null;
    const terminalPayload = {
      type: 'set_use_active_terminal',
      useActiveTerminal: desiredUseActiveTerminal && !!terminalIdForSession,
      activeTerminalId: desiredUseActiveTerminal ? terminalIdForSession : null,
    };
    const terminalKey = `${activeSessionId}:${JSON.stringify(terminalPayload)}`;
    if (terminalKey !== lastTerminalControlKeyRef.current) {
      if (sendControl(terminalPayload)) {
        lastTerminalControlKeyRef.current = terminalKey;
      }
    }

    // Browser soft control — session-scoped intent, tab from workspace project.
    const project = workspaceProjects.find(
      (entry) => normalizeWorkDir(entry.path) === normalizeWorkDir(session?.workDir),
    );
    const browserTabId = project?.activeBrowserTabId ?? null;
    const desiredUseActiveBrowser = session?.useActiveBrowser ?? state.useActiveBrowser;
    const browserPayload = {
      type: 'set_use_active_browser',
      useActiveBrowser: desiredUseActiveBrowser && !!browserTabId,
      activeBrowserTabId: browserTabId,
    };
    const browserKey = `${activeSessionId}:${JSON.stringify(browserPayload)}`;
    if (browserKey !== lastBrowserControlKeyRef.current) {
      // Warn when intent ON but no tab: send effective false, keep intent in store.
      if (desiredUseActiveBrowser && !browserTabId) {
        appendSessionDebugRef.current(activeSessionId, {
          source: 'client',
          level: 'warn',
          message: 'Browser toggle is on but no browser tab is open; browser MCP disabled until a tab is opened.',
        });
      }
      if (sendControl(browserPayload)) {
        lastBrowserControlKeyRef.current = browserKey;
      }
    }
  }, [activeSessionId, sendControl]);

  reassertSoftControlsRef.current = reassertSoftControls;

  useEffect(() => {
    reassertSoftControls();
  }, [
    activeSession?.terminalId,
    activeSession?.useActiveTerminal,
    activeSession?.useActiveBrowser,
    activeSession?.workDir,
    activeSessionId,
    activeTerminalId,
    connected,
    projects,
    reassertSoftControls,
    useActiveBrowser,
    useActiveTerminal,
  ]);

  const cancel = useCallback(() => {
    if (!activeSessionId) return;
    const session = useChatStore.getState().sessions.find((s) => s.id === activeSessionId);
    if (session?.pendingPermission) {
      sendControl({
        type: 'permission_response',
        permissionId: session.pendingPermission.permissionId,
        cancelled: true,
      });
    }
    if (sendControl({ type: 'cancel' })) {
      clearStallTimer(activeSessionId);
      setSessionStatus(activeSessionId, 'idle');
      setSessionStalled(activeSessionId, false);
      appendSessionDebugRef.current(activeSessionId, {
        source: 'session',
        level: 'warn',
        message: 'turn cancelled by user',
      });
    }
  }, [activeSessionId, clearStallTimer, sendControl, setSessionStalled, setSessionStatus]);

  const setConfigOption = useCallback((configId: string, value: string) => {
    sendControl({ type: 'set_config_option', configId, value });
  }, [sendControl]);

  const respondPermission = useCallback((permissionId: string, optionId: string) => {
    if (!activeSessionId || !sendControl({ type: 'permission_response', permissionId, optionId })) return;
    const { sessions } = useChatStore.getState();
    const session = sessions.find((s) => s.id === activeSessionId);
    if (session?.pendingPermission?.permissionId === permissionId) {
      useChatStore.setState({
        sessions: sessions.map((s) =>
          s.id === activeSessionId ? { ...s, pendingPermission: undefined } : s
        ),
      });
    }
  }, [activeSessionId, sendControl]);

  const rejectPermission = useCallback((permissionId: string) => {
    if (!activeSessionId || !sendControl({ type: 'permission_response', permissionId, cancelled: true })) return;
    const { sessions } = useChatStore.getState();
    const session = sessions.find((s) => s.id === activeSessionId);
    if (session?.pendingPermission?.permissionId === permissionId) {
      useChatStore.setState({
        sessions: sessions.map((s) =>
          s.id === activeSessionId ? { ...s, pendingPermission: undefined } : s
        ),
      });
    }
  }, [activeSessionId, sendControl]);

  const setAutoApprove = useCallback((enabled: boolean) => {
    sendControl({ type: 'set_auto_approve', autoApprove: enabled });
  }, [sendControl]);

  // ADR-0002: fetch timeline events after a cursor for catch-up on reconnect.
  // Called when a WS reconnects and the client needs to fill gaps in event
  // history. The server returns events after the given seq, grouped by epoch.
  // Includes epoch so the server can detect stale cursors.
  const fetchTimeline = useCallback((sessionId: string, afterSeq?: number) => {
    const conn = connectionsRef.current.get(sessionId);
    if (!conn || !socketIsOpen(conn)) return;
    const cursor = cursorRef.current.get(sessionId);
    const seq = afterSeq ?? cursor?.seq ?? 0;
    const epoch = cursor?.epoch ?? '';
    conn.ws?.send(JSON.stringify({ type: 'fetch_timeline', afterSeq: seq, epoch }));
  }, []);

  return { sendMessage, cancel, setConfigOption, respondPermission, rejectPermission, setAutoApprove, connected, fetchTimeline };
}
