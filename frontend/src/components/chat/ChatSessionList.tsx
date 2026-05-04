import { useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { deleteChatSession } from '../../api';

export function ChatSessionList() {
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const deleteSessionStore = useChatStore((s) => s.deleteSession);
  const [open, setOpen] = useState(false);

  if (sessions.length === 0) return null;

  const activeSession = sessions.find((s) => s.id === activeSessionId);

  return (
    <div style={{ position: 'relative' }}>
      <button
        className="chat-new-btn"
        onClick={() => setOpen(!open)}
        type="button"
        title="Switch session"
      >
        {activeSession ? activeSession.title : 'Sessions'} ▾
      </button>
      {open && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            right: 0,
            background: 'var(--sidebar-bg)',
            border: '1px solid var(--border-color)',
            borderRadius: '4px',
            zIndex: 100,
            minWidth: '200px',
            maxHeight: '240px',
            overflowY: 'auto',
          }}
        >
          {sessions.map((session) => (
            <div
              key={session.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '6px 8px',
                background: session.id === activeSessionId ? 'var(--sidebar-hover)' : 'transparent',
                cursor: 'pointer',
                fontSize: '0.82rem',
              }}
            >
              <button
                style={{
                  flex: 1,
                  background: 'none',
                  border: 'none',
                  color: 'var(--sidebar-fg)',
                  cursor: 'pointer',
                  textAlign: 'left',
                  padding: '2px 4px',
                  fontSize: '0.82rem',
                }}
                onClick={() => {
                  setActiveSession(session.id);
                  setOpen(false);
                }}
                type="button"
              >
                <div>{session.title}</div>
                <div style={{ fontSize: '0.7rem', color: 'var(--activity-fg)' }}>
                  {new Date(session.createdAt).toLocaleTimeString()}
                </div>
              </button>
              <button
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--activity-fg)',
                  cursor: 'pointer',
                  padding: '2px 6px',
                  fontSize: '0.85rem',
                }}
                onClick={(e) => {
                  e.stopPropagation();
                  deleteChatSession(session.id).catch(() => {});
                  deleteSessionStore(session.id);
                }}
                type="button"
                title="Delete session"
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
