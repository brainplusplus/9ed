import { useCallback, useRef, useState } from 'react';

type ChatInputProps = {
  onSend: (content: string) => void;
  onCancel: () => void;
  streaming: boolean;
  disabled: boolean;
};

export function ChatInput({ onSend, onCancel, streaming, disabled }: ChatInputProps) {
  const [value, setValue] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

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
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  }, [value, streaming, disabled, onSend]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  return (
    <div className="chat-input-area">
      <textarea
        ref={textareaRef}
        className="chat-textarea"
        placeholder="Ask something..."
        value={value}
        onChange={(e) => {
          setValue(e.target.value);
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
