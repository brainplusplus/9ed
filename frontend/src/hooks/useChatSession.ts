import { useCallback, useEffect, useRef, useState } from 'react';
import { createChatWebSocket } from '../api';
import { useChatStore } from '../stores/chat';
import type { CodeContext } from '../types';

type UseChatSessionResult = {
  sendMessage: (content: string, context?: CodeContext) => void;
  cancel: () => void;
  connected: boolean;
};

export function useChatSession(): UseChatSessionResult {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const addMessage = useChatStore((s) => s.addMessage);
  const appendToLastMessage = useChatStore((s) => s.appendToLastMessage);
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
        const data = JSON.parse(event.data) as { type: string; content?: string; error?: string };
        switch (data.type) {
          case 'stream':
            if (data.content) {
              appendToLastMessage(activeSessionId, data.content);
            }
            break;
          case 'stream_end':
            finalizeAssistantMessage(activeSessionId);
            setSessionStatus(activeSessionId, 'idle');
            break;
          case 'error':
            setSessionStatus(activeSessionId, 'error');
            break;
          case 'session_reset':
            setSessionStatus(activeSessionId, 'idle');
            break;
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
  }, [activeSessionId, appendToLastMessage, setSessionStatus, finalizeAssistantMessage]);

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

      const assistantId = Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
      addMessage(activeSessionId, {
        id: assistantId,
        role: 'assistant',
        content: '',
        timestamp: Date.now(),
      });

      setSessionStatus(activeSessionId, 'streaming');

      wsRef.current.send(JSON.stringify({ type: 'message', content, context }));
    },
    [activeSessionId, addMessage, setSessionStatus],
  );

  const cancel = useCallback(() => {
    if (!activeSessionId || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'cancel' }));
    setSessionStatus(activeSessionId, 'idle');
  }, [activeSessionId, setSessionStatus]);

  return { sendMessage, cancel, connected };
}
