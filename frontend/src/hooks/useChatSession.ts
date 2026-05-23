import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createChatWebSocket, getBrowserState, getLiveChatSessions, inspectBrowserAutomation, navigateBrowserAutomation, startBrowserAutomation } from '../api';
import { useChatStore } from '../stores/chat';
import type { Attachment } from '../components/chat/ChatInput';
import type { BrowserElementSelection, ChatEvent, ChatSessionKind, CodeContext } from '../types';
import type { QueuedMessage } from '../stores/chat';

const CONNECT_TIMEOUT_MS = 10000;
const INITIAL_CONNECT_DELAY_MS = 250;
const MAX_RECONNECT_ATTEMPTS = 6;
const RECONNECT_DELAYS_MS = [250, 500, 1000, 2000, 4000, 8000];

type UseChatSessionResult = {
  sendMessage: (content: string, context?: CodeContext, attachments?: Attachment[]) => Promise<void>;
  cancel: () => void;
  setConfigOption: (configId: string, value: string) => void;
  respondPermission: (permissionId: string, optionId: string) => void;
  rejectPermission: (permissionId: string) => void;
  setAutoApprove: (enabled: boolean) => void;
  connected: boolean;
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
};

function socketIsOpen(conn: ChatConnection | undefined): boolean {
  return conn?.ws?.readyState === WebSocket.OPEN;
}

function isConnectableSession(session: { kind: ChatSessionKind; status?: string }): boolean {
  return session.kind !== 'archived' && session.status !== 'error';
}

async function buildActiveBrowserContext(selection: BrowserElementSelection | null): Promise<string | null> {
  const state = await getBrowserState();
  const activeTab = state.tabs.find((tab) => tab.id === state.activeTabId) ?? state.tabs[0];
  if (!activeTab) {
    return null;
  }

  const lines = [
    '[Active browser context]',
    `Title: ${activeTab.title || '(untitled)'}`,
    `URL: ${activeTab.url}`,
  ];

  try {
    await startBrowserAutomation();
    let inspected = await navigateBrowserAutomation(activeTab.url);
    if (!inspected.text) {
      inspected = await inspectBrowserAutomation();
    }
    if (inspected.title && inspected.title !== activeTab.title) {
      lines.push(`Automation title: ${inspected.title}`);
    }
    if (inspected.text) {
      lines.push('Visible page text:');
      lines.push(inspected.text.slice(0, 6000));
    }
  } catch (error) {
    lines.push(`Browser inspect status: ${error instanceof Error ? error.message : 'unavailable'}`);
  }

  if (selection) {
    lines.push('[Selected browser element]');
    lines.push(`Selector: ${selection.selector}`);
    lines.push(`Tag: ${selection.tagName}`);
    if (selection.role) {
      lines.push(`Role: ${selection.role}`);
    }
    if (selection.text) {
      lines.push(`Text: ${selection.text}`);
    }
    lines.push('Outer HTML:');
    lines.push(selection.outerHTML.slice(0, 3000));
  }

  return lines.join('\n');
}

export function useChatSession(): UseChatSessionResult {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const sessions = useChatStore((s) => s.sessions);
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);
  const browserSelection = useChatStore((s) => s.browserSelection);
  const addMessage = useChatStore((s) => s.addMessage);
  const handleChatEvent = useChatStore((s) => s.handleChatEvent);
  const setSessionStatus = useChatStore((s) => s.setSessionStatus);
  const setSessionKind = useChatStore((s) => s.setSessionKind);
  const finalizeAssistantMessage = useChatStore((s) => s.finalizeAssistantMessage);
  const dequeueMessage = useChatStore((s) => s.dequeueMessage);
  const refreshSessionState = useChatStore((s) => s.refreshSessionState);
  const resumeSession = useChatStore((s) => s.resumeSession);

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
  const upgradedRef = useRef<Set<string>>(new Set());

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
  }, []);

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
    updateActiveConnected();
  }, [clearConnectionTimers, updateActiveConnected]);

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
    };

    const markConnectionError = (seq: number) => {
      if (seq !== conn.seq || conn.disposed) return;
      const session = getLiveSession();
      if (!session || session.kind === 'archived') return;
      setSessionStatus(sessionId, 'error');
      stopConnection(sessionId);
      updateActiveConnected();
    };

    const connect = async () => {
      if (conn.disposed) return;
      if (conn.connecting) return;
      const session = getLiveSession();
      if (!session || !isConnectableSession(session)) {
        stopConnection(sessionId);
        return;
      }

      const seq = conn.seq + 1;
      conn.seq = seq;
      conn.connecting = true;
      setConnecting();

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
          return;
        }
      } catch {
        if (seq !== conn.seq || conn.disposed) return;
        conn.connecting = false;
        setSessionStatus(sessionId, 'error');
        stopConnection(sessionId);
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

        const delay = RECONNECT_DELAYS_MS[Math.min(conn.reconnectAttempts, RECONNECT_DELAYS_MS.length - 1)];
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
        setSessionStatus(sessionId, 'idle');
        updateActiveConnected();
        if (shouldSync) {
          void refreshSessionStateRef.current(sessionId);
        }
        if (conn.kind === 'resumable' && !upgradedRef.current.has(sessionId)) {
          setSessionKindRef.current(sessionId, 'live');
          upgradedRef.current.add(sessionId);
        }
      };

      ws.onmessage = (event) => {
        if (seq !== conn.seq || conn.disposed) return;
        try {
          const data = JSON.parse(event.data) as ChatEvent;
          handleChatEventRef.current(sessionId, data);

          if (data.type === 'done') {
            finalizeRef.current(sessionId);

            const next = dequeueRef.current(sessionId);
            if (next && sendQueuedRef.current) {
              setTimeout(() => { void sendQueuedRef.current?.(next); }, 100);
            }
          }
        } catch {
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

      const displayContent = attachments?.length
        ? content + '\n\n' + attachments.map((a) => `ðŸ“Ž ${a.name}`).join('\n')
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

      let outboundContent = content;
      if (useActiveBrowser) {
        try {
          const browserContext = await buildActiveBrowserContext(browserSelection);
          if (browserContext) {
            outboundContent = `${browserContext}\n\n[User request]\n${content}`;
          }
        } catch {
        }
      }

      const payload: { type: string; content: string; context?: unknown; attachments?: Attachment[] } = { type: 'message', content: outboundContent };
      if (context) {
        payload.context = context;
      }
      if (attachments?.length) {
        payload.attachments = attachments;
      }
      conn.ws.send(JSON.stringify(payload));
    },
    [activeSessionId, addMessage, browserSelection, setSessionStatus, useActiveBrowser],
  );

  sendQueuedRef.current = async (queued: QueuedMessage) => {
    await sendMessage(queued.content, undefined, queued.attachments as Attachment[] | undefined);
  };

  const sendControl = useCallback((payload: unknown): boolean => {
    if (!activeSessionId) return false;
    const ws = connectionsRef.current.get(activeSessionId)?.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) return false;
    ws.send(JSON.stringify(payload));
    return true;
  }, [activeSessionId]);

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
      setSessionStatus(activeSessionId, 'idle');
    }
  }, [activeSessionId, sendControl, setSessionStatus]);

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

  return { sendMessage, cancel, setConfigOption, respondPermission, rejectPermission, setAutoApprove, connected };
}
