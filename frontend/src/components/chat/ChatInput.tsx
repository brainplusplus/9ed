import { useCallback, useMemo, useRef, useState } from 'react';
import { useChatStore } from '../../stores/chat';

type ChatInputProps = {
  onSend: (content: string) => void;
  onCancel: () => void;
  streaming: boolean;
  disabled: boolean;
};

export function ChatInput({ onSend, onCancel, streaming, disabled }: ChatInputProps) {
  const [value, setValue] = useState('');
  const [selectedIdx, setSelectedIdx] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const sessions = useChatStore((s) => s.sessions);
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const commands = activeSession?.commands ?? [];

  const showCommands = value.startsWith('/') && !value.includes(' ') && commands.length > 0;
  const filteredCommands = useMemo(() => {
    if (!showCommands) return [];
    const query = value.slice(1).toLowerCase();
    return commands.filter((c) => c.name.toLowerCase().startsWith(query));
  }, [showCommands, value, commands]);

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 144) + 'px';
  }, []);

  const handleSend = useCallback(() => {
    const trimmed = value.trim();
    if (!trimmed || streaming || disabled) return;
    onSend(trimmed);
    setValue('');
    setSelectedIdx(0);
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  }, [value, streaming, disabled, onSend]);

  const selectCommand = useCallback((name: string) => {
    setValue('/' + name + ' ');
    setSelectedIdx(0);
    textareaRef.current?.focus();
  }, []);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (filteredCommands.length > 0) {
        if (e.key === 'ArrowDown') {
          e.preventDefault();
          setSelectedIdx((i) => Math.min(i + 1, filteredCommands.length - 1));
          return;
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault();
          setSelectedIdx((i) => Math.max(i - 1, 0));
          return;
        }
        if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
          e.preventDefault();
          selectCommand(filteredCommands[selectedIdx].name);
          return;
        }
        if (e.key === 'Escape') {
          setValue('');
          return;
        }
      } else if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend, filteredCommands, selectedIdx, selectCommand],
  );

  return (
    <div className="chat-input-area" style={{ position: 'relative' }}>
      {filteredCommands.length > 0 && (
        <div className="chat-commands-popup">
          {filteredCommands.map((cmd, i) => (
            <div
              key={cmd.name}
              className={`chat-command-item ${i === selectedIdx ? 'active' : ''}`}
              onClick={() => selectCommand(cmd.name)}
            >
              <span className="chat-command-name">/{cmd.name}</span>
            </div>
          ))}
        </div>
      )}
      <textarea
        ref={textareaRef}
        className="chat-textarea"
        placeholder="Ask something..."
        value={value}
        onChange={(e) => {
          setValue(e.target.value);
          setSelectedIdx(0);
          adjustHeight();
        }}
        onKeyDown={handleKeyDown}
        rows={1}
        disabled={disabled}
      />
      {streaming ? (
        <button className="chat-send-btn stop" onClick={onCancel} type="button">
          ■
        </button>
      ) : (
        <button
          className="chat-send-btn"
          onClick={handleSend}
          disabled={!value.trim() || disabled}
          type="button"
        >
          ⏎
        </button>
      )}
    </div>
  );
}
