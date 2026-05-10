import { useCallback, useEffect, useRef, useState } from 'react';
import { createChatWebSocket } from '../api';
import { useChatStore } from '../stores/chat';
import type { Attachment } from '../components/chat/ChatInput';
import type { ChatEvent, CodeContext } from '../types';
import type { QueuedMessage } from '../stores/chat';

type UseChatSessionResult = {
  sendMessage: (content: string, context?: CodeContext, attachments?: Attachment[]) => void;
  cancel: () => void;
  setConfigOption: (configId: string, value: string) => void;
  respondPermission: (permissionId: string, optionId: string) => void;
  rejectPermission: (permissionId: string) => void;
  setAutoApprove: (enabled: boolean) => void;
  connected: boolean;
};

export function useChatSession(): UseChatSessionResult {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const activeSession = useChatStore((s) => s.sessions.find((sess) => sess.id === s.activeSessionId) ?? null);
  const addMessage = useChatStore((s) => s.addMessage);
  const handleChatEvent = useChatStore((s) => s.handleChatEvent);
  const setSessionStatus = useChatStore((s) => s.setSessionStatus);
  const setSessionKind = useChatStore((s) => s.setSessionKind);
  const finalizeAssistantMessage = useChatStore((s) => s.finalizeAssistantMessage);
  const dequeueMessage = useChatStore((s) => s.dequeueMessage);

  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);

  const handleChatEventRef = useRef(handleChatEvent);
  handleChatEventRef.current = handleChatEvent;
  const finalizeRef = useRef(finalizeAssistantMessage);
  finalizeRef.current = finalizeAssistantMessage;
  const dequeueRef = useRef(dequeueMessage);
  dequeueRef.current = dequeueMessage;
  const sendQueuedRef = useRef<((queued: QueuedMessage) => void) | null>(null);
  const setSessionKindRef = useRef(setSessionKind);
  setSessionKindRef.current = setSessionKind;
  const upgradedRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    if (!activeSessionId) {
      setConnected(false);
      return;
    }

    if (activeSession?.kind === 'archived') {
      setConnected(false);
      return;
    }

    const ws = createChatWebSocket(activeSessionId);
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      setSessionStatus(activeSessionId, 'idle');
      if (activeSession?.kind === 'resumable' && !upgradedRef.current.has(activeSessionId)) {
        setSessionKindRef.current(activeSessionId, 'live');
        upgradedRef.current.add(activeSessionId);
      }
    };

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as ChatEvent;
        handleChatEventRef.current(activeSessionId, data);

        if (data.type === 'done') {
          finalizeRef.current(activeSessionId);

          const next = dequeueRef.current(activeSessionId);
          if (next && sendQueuedRef.current) {
            setTimeout(() => sendQueuedRef.current?.(next), 100);
          }
        }
      } catch {
      }
    };

    ws.onclose = () => {
      setConnected(false);
      if (activeSessionId) {
        const session = useChatStore.getState().sessions.find((s) => s.id === activeSessionId);
        if (session && session.kind !== 'archived') {
          useChatStore.setState({
            sessions: useChatStore.getState().sessions.map((s) =>
              s.id === activeSessionId ? { ...s, status: 'error' as const } : s
            ),
          });
        }
      }
    };
    ws.onerror = () => setConnected(false);

    return () => {
      ws.close();
      wsRef.current = null;
      setConnected(false);
    };
  }, [activeSessionId, activeSession?.kind]);

  const sendMessage = useCallback(
    (content: string, context?: CodeContext, attachments?: Attachment[]) => {
      if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;

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

      const payload: { type: string; content: string; context?: unknown; attachments?: Attachment[] } = { type: 'message', content };
      if (context) {
        payload.context = context;
      }
      if (attachments?.length) {
        payload.attachments = attachments;
      }
      wsRef.current.send(JSON.stringify(payload));
    },
    [activeSessionId, addMessage, setSessionStatus],
  );

  sendQueuedRef.current = (queued: QueuedMessage) => {
    sendMessage(queued.content, undefined, queued.attachments as Attachment[] | undefined);
  };

  const cancel = useCallback(() => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    const session = useChatStore.getState().sessions.find((s) => s.id === activeSessionId);
    if (session?.pendingPermission) {
      wsRef.current.send(JSON.stringify({
        type: 'permission_response',
        permissionId: session.pendingPermission.permissionId,
        cancelled: true,
      }));
    }
    wsRef.current.send(JSON.stringify({ type: 'cancel' }));
    setSessionStatus(activeSessionId, 'idle');
  }, [activeSessionId, setSessionStatus]);

  const setConfigOption = useCallback((configId: string, value: string) => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'set_config_option', configId, value }));
  }, [activeSessionId]);

  const respondPermission = useCallback((permissionId: string, optionId: string) => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'permission_response', permissionId, optionId }));
    const { sessions } = useChatStore.getState();
    const session = sessions.find((s) => s.id === activeSessionId);
    if (session?.pendingPermission?.permissionId === permissionId) {
      useChatStore.setState({
        sessions: sessions.map((s) =>
          s.id === activeSessionId ? { ...s, pendingPermission: undefined } : s
        ),
      });
    }
  }, [activeSessionId]);

  const rejectPermission = useCallback((permissionId: string) => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'permission_response', permissionId, cancelled: true }));
    const { sessions } = useChatStore.getState();
    const session = sessions.find((s) => s.id === activeSessionId);
    if (session?.pendingPermission?.permissionId === permissionId) {
      useChatStore.setState({
        sessions: sessions.map((s) =>
          s.id === activeSessionId ? { ...s, pendingPermission: undefined } : s
        ),
      });
    }
  }, [activeSessionId]);

  const setAutoApprove = useCallback((enabled: boolean) => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'set_auto_approve', autoApprove: enabled }));
  }, [activeSessionId]);

  return { sendMessage, cancel, setConfigOption, respondPermission, rejectPermission, setAutoApprove, connected };
}
