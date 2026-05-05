import { useCallback, useState } from 'react';
import { useChatStore } from '../../stores/chat';
import type { Attachment } from './ChatInput';

type ChatQueueProps = {
  sessionId: string;
  onSendNow: (content: string, attachments?: Attachment[]) => void;
};

const EMPTY_QUEUE: never[] = [];

export function ChatQueue({ sessionId, onSendNow }: ChatQueueProps) {
  const queue = useChatStore((s) => s.queuedMessages[sessionId] ?? EMPTY_QUEUE);
  const removeQueuedMessage = useChatStore((s) => s.removeQueuedMessage);
  const editQueuedMessage = useChatStore((s) => s.editQueuedMessage);
  const reorderQueuedMessages = useChatStore((s) => s.reorderQueuedMessages);
  const clearQueue = useChatStore((s) => s.clearQueue);

  const [editingId, setEditingId] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');

  const handleEdit = useCallback((id: string, content: string) => {
    setEditingId(id);
    setEditValue(content);
  }, []);

  const handleEditSave = useCallback(() => {
    if (editingId && editValue.trim()) {
      editQueuedMessage(sessionId, editingId, editValue.trim());
    }
    setEditingId(null);
    setEditValue('');
  }, [editingId, editValue, sessionId, editQueuedMessage]);

  const handleSendNow = useCallback((id: string) => {
    const msg = queue.find((m) => m.id === id);
    if (!msg) return;
    removeQueuedMessage(sessionId, id);
    onSendNow(msg.content, msg.attachments as Attachment[] | undefined);
  }, [queue, sessionId, removeQueuedMessage, onSendNow]);

  const handleMoveUp = useCallback((idx: number) => {
    if (idx > 0) reorderQueuedMessages(sessionId, idx, idx - 1);
  }, [sessionId, reorderQueuedMessages]);

  const handleMoveDown = useCallback((idx: number) => {
    if (idx < queue.length - 1) reorderQueuedMessages(sessionId, idx, idx + 1);
  }, [sessionId, queue.length, reorderQueuedMessages]);

  if (queue.length === 0) return null;

  return (
    <div className="chat-queue">
      <div className="chat-queue-header">
        <span className="chat-queue-title">Queued ({queue.length})</span>
        <button className="chat-queue-clear" onClick={() => clearQueue(sessionId)} type="button" title="Clear all">
          ✕
        </button>
      </div>
      <div className="chat-queue-list">
        {queue.map((msg, idx) => (
          <div key={msg.id} className="chat-queue-item">
            {editingId === msg.id ? (
              <div className="chat-queue-edit">
                <input
                  className="chat-queue-edit-input"
                  value={editValue}
                  onChange={(e) => setEditValue(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleEditSave();
                    if (e.key === 'Escape') setEditingId(null);
                  }}
                  autoFocus
                />
                <button className="chat-queue-action" onClick={handleEditSave} type="button">✓</button>
              </div>
            ) : (
              <>
                <span className="chat-queue-content" title={msg.content}>
                  {msg.content.length > 50 ? msg.content.slice(0, 50) + '…' : msg.content}
                </span>
                <div className="chat-queue-actions">
                  <button className="chat-queue-action" onClick={() => handleMoveUp(idx)} disabled={idx === 0} type="button" title="Move up">↑</button>
                  <button className="chat-queue-action" onClick={() => handleMoveDown(idx)} disabled={idx === queue.length - 1} type="button" title="Move down">↓</button>
                  <button className="chat-queue-action" onClick={() => handleEdit(msg.id, msg.content)} type="button" title="Edit">✎</button>
                  <button className="chat-queue-action" onClick={() => handleSendNow(msg.id)} type="button" title="Send now">▶</button>
                  <button className="chat-queue-action" onClick={() => removeQueuedMessage(sessionId, msg.id)} type="button" title="Remove">✕</button>
                </div>
              </>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
