import { useCallback, useEffect, useRef, useState } from 'react';
import { createChatWebSocket } from '../api';
import { useChatStore } from '../stores/chat';
import type { ChatEvent, CodeContext } from '../types';

type UseChatSessionResult = {
  sendMessage: (content: string, context?: CodeContext) => void;
  cancel: () => void;
  setConfigOption: (configId: string, value: string) => void;
  connected: boolean;
};

export function useChatSession(): UseChatSessionResult {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const addMessage = useChatStore((s) => s.addMessage);
  const handleChatEvent = useChatStore((s) => s.handleChatEvent);
  const setSessionStatus = useChatStore((s) => s.setSessionStatus);
  const finalizeAssistantMessage = useChatStore((s) => s.finalizeAssistantMessage);

  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!activeSessionId) {
      setConnected(false);
      return;
    }

    const ws = createChatWebSocket(activeSessionId);
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as ChatEvent;
        handleChatEvent(activeSessionId, data);

        if (data.type === 'done') {
          finalizeAssistantMessage(activeSessionId);
        }
      } catch {
        // ignore malformed messages
      }
    };

    ws.onclose = () => setConnected(false);
    ws.onerror = () => setConnected(false);

    return () => {
      ws.close();
      wsRef.current = null;
      setConnected(false);
    };
  }, [activeSessionId, handleChatEvent, setSessionStatus, finalizeAssistantMessage]);

  const sendMessage = useCallback(
    (content: string, context?: CodeContext) => {
      if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;

      const msgId = Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
      const userMessage = {
        id: msgId,
        role: 'user' as const,
        content,
        context,
        timestamp: Date.now(),
      };
      addMessage(activeSessionId, userMessage);
      setSessionStatus(activeSessionId, 'streaming');

      const payload: { type: string; content: string; context?: unknown } = { type: 'message', content };
      if (context) {
        payload.context = context;
      }
      wsRef.current.send(JSON.stringify(payload));
    },
    [activeSessionId, addMessage, setSessionStatus],
  );

  const cancel = useCallback(() => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'cancel' }));
    setSessionStatus(activeSessionId, 'idle');
  }, [activeSessionId, setSessionStatus]);

  const setConfigOption = useCallback((configId: string, value: string) => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'set_config_option', configId, value }));
  }, [activeSessionId]);

  return { sendMessage, cancel, setConfigOption, connected };
}
