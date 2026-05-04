import { useCallback, useEffect, useRef, useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { useChatSession } from '../../hooks/useChatSession';
import type { CodeContext } from '../../types';

type InlinePromptProps = {
  context: CodeContext;
  position: { top: number; left: number };
  onDismiss: () => void;
};

export function InlinePrompt({ context, position, onDismiss }: InlinePromptProps) {
  const [input, setInput] = useState('');
  const containerRef = useRef<HTMLDivElement>(null);
  const { sendMessage } = useChatSession();
  const chatVisible = useChatStore((s) => s.chatVisible);
  const toggleChat = useChatStore((s) => s.toggleChat);
  const activeSessionId = useChatStore((s) => s.activeSessionId);

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onDismiss();
      }
    }
    function handleEscape(e: KeyboardEvent) {
      if (e.key === 'Escape') onDismiss();
    }
    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [onDismiss]);

  const send = useCallback(
    (content: string) => {
      if (!activeSessionId) return;
      if (!chatVisible) toggleChat();
      sendMessage(content, context);
      onDismiss();
    },
    [activeSessionId, chatVisible, toggleChat, sendMessage, context, onDismiss],
  );

  const handleSubmit = useCallback(() => {
    const trimmed = input.trim();
    if (trimmed) send(trimmed);
  }, [input, send]);

  const quickActions = [
    { label: 'Explain', prompt: 'Explain this code' },
    { label: 'Refactor', prompt: 'Refactor this code' },
    { label: 'Test', prompt: 'Write tests for this code' },
    { label: 'Fix', prompt: 'Fix any issues in this code' },
  ];

  return (
    <div
      ref={containerRef}
      className="inline-prompt"
      style={{ top: position.top, left: position.left }}
    >
      <input
        className="inline-prompt-input"
        placeholder="Ask about selection..."
        value={input}
        onChange={(e) => setInput(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            handleSubmit();
          }
        }}
        autoFocus
      />
      <div className="inline-prompt-actions">
        {quickActions.map((action) => (
          <button
            key={action.label}
            className="inline-prompt-btn"
            onClick={() => send(action.prompt)}
            type="button"
            disabled={!activeSessionId}
          >
            {action.label}
          </button>
        ))}
      </div>
    </div>
  );
}
