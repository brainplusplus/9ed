import { useState } from 'react';
import type { ChatMessage as ChatMessageType } from '../../types';

type ChatMessageProps = {
  message: ChatMessageType;
};

type ContentBlock =
  | { kind: 'text'; text: string }
  | { kind: 'code'; lang: string; code: string };

function parseContent(raw: string): ContentBlock[] {
  const blocks: ContentBlock[] = [];
  const parts = raw.split('```');

  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 0) {
      const text = parts[i].trim();
      if (text) blocks.push({ kind: 'text', text });
    } else {
      const newlineIdx = parts[i].indexOf('\n');
      const lang = newlineIdx > 0 ? parts[i].slice(0, newlineIdx).trim() : '';
      const code = newlineIdx > 0 ? parts[i].slice(newlineIdx + 1).trim() : parts[i].trim();
      blocks.push({ kind: 'code', lang, code });
    }
  }

  return blocks;
}

export function ChatMessage({ message }: ChatMessageProps) {
  const [contextExpanded, setContextExpanded] = useState(false);
  const blocks = parseContent(message.content);

  return (
    <div className={`chat-message ${message.role}`}>
      {message.context && (
        <div className="chat-message-context" onClick={() => setContextExpanded(!contextExpanded)}>
          <div className="chat-message-context-header">
            {message.context.filePath}:{message.context.startLine}-{message.context.endLine}
          </div>
          {contextExpanded && (
            <pre className="chat-message-code">{message.context.selectedCode}</pre>
          )}
        </div>
      )}
      <div className="chat-message-bubble">
        {blocks.map((block, i) =>
          block.kind === 'code' ? (
            <pre key={i} className="chat-message-code">
              <code>{block.code}</code>
            </pre>
          ) : (
            block.text.split('\n\n').map((para, j) => (
              <p key={`${i}-${j}`} style={{ margin: '4px 0' }}>{para}</p>
            ))
          ),
        )}
        {message.content === '' && message.role === 'assistant' && (
          <span style={{ opacity: 0.6 }}>...</span>
        )}
      </div>
    </div>
  );
}
