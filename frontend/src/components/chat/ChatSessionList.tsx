import { useEffect, useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { deleteChatSession } from '../../api';

const agentLabels: Record<string, string> = {
  opencode: 'OpenCode',
  claude: 'Claude Code',
  codex: 'Codex CLI',
  gemini: 'Gemini CLI',
  pi: 'Pi',
  amp: 'Amp',
  copilot: 'Copilot',
};

function formatSessionTitle(agentId: string, title: string): string {
  const agent = agentLabels[agentId] ?? agentId;
  if (!title || title === agent) return agent;
  return `${agent} — ${title}`;
}

function SessionStatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'streaming': return <span className="session-status-icon session-status-active" title="Working">⟳</span>;
    case 'connecting': return <span className="session-status-icon session-status-connecting" title="Connecting">◌</span>;
    case 'error': return <span className="session-status-icon session-status-error" title="Error">✗</span>;
    default: return null;
  }
}

export function ChatSessionList() {
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const deleteSessionStore = useChatStore((s) => s.deleteSession);
  const historySessions = useChatStore((s) => s.historySessions);
  const historyLoaded = useChatStore((s) => s.historyLoaded);
  const loadHistory = useChatStore((s) => s.loadHistory);
  const loadHistorySession = useChatStore((s) => s.loadHistorySession);
  const deleteHistorySession = useChatStore((s) => s.deleteHistorySession);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!historyLoaded) {
      loadHistory();
    }
  }, [historyLoaded, loadHistory]);

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const activeSessionIds = new Set(sessions.map((s) => s.id));
  const pastSessions = historySessions.filter((h) => !activeSessionIds.has(h.id));

  const hasItems = sessions.length > 0 || pastSessions.length > 0;
  if (!hasItems) return null;

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
            minWidth: '220px',
            maxHeight: '320px',
            overflowY: 'auto',
          }}
        >
          {sessions.length > 0 && (
            <div style={{ padding: '4px 8px', fontSize: '0.7rem', color: 'var(--activity-fg)', borderBottom: '1px solid var(--border-color)' }}>
              Active
            </div>
          )}
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
              <SessionStatusIcon status={session.status} />
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
                <div>{formatSessionTitle(session.agentId, session.title)}</div>
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

          {pastSessions.length > 0 && (
            <div style={{ padding: '4px 8px', fontSize: '0.7rem', color: 'var(--activity-fg)', borderBottom: '1px solid var(--border-color)', borderTop: '1px solid var(--border-color)' }}>
              History
            </div>
          )}
          {pastSessions.map((hist) => (
            <div
              key={hist.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '6px 8px',
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
                  opacity: 0.8,
                }}
                onClick={() => {
                  loadHistorySession(hist.id);
                  setOpen(false);
                }}
                type="button"
              >
                <div>{formatSessionTitle(hist.agentId, hist.title)}</div>
                <div style={{ fontSize: '0.7rem', color: 'var(--activity-fg)' }}>
                  {new Date(hist.updatedAt).toLocaleDateString()}
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
                  deleteHistorySession(hist.id);
                }}
                type="button"
                title="Delete from history"
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
