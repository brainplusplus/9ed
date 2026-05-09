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
    <div className="chat-picker-wrap chat-session-wrap">
      <button
        className="chat-new-btn"
        onClick={() => setOpen(!open)}
        type="button"
        title="Switch session"
      >
        <span className="chat-session-trigger-label">{activeSession ? activeSession.title : 'Sessions'}</span>
        <span className="agent-picker-caret" aria-hidden="true">▾</span>
      </button>
      {open && (
        <div className="chat-session-dropdown">
          {sessions.length > 0 && (
            <div className="chat-session-section-label">
              Active
            </div>
          )}
          {sessions.map((session) => (
            <div
              key={session.id}
              className={`chat-session-row${session.id === activeSessionId ? ' active' : ''}`}
            >
              <SessionStatusIcon status={session.status} />
              <button
                className="chat-session-row-btn"
                onClick={() => {
                  setActiveSession(session.id);
                  setOpen(false);
                }}
                type="button"
              >
                <div>{formatSessionTitle(session.agentId, session.title)}</div>
                <div className="chat-session-row-meta">
                  {new Date(session.createdAt).toLocaleTimeString()}
                </div>
              </button>
              <button
                className="chat-session-row-close"
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
            <div className="chat-session-section-label chat-session-section-label-history">
              History
            </div>
          )}
          {pastSessions.map((hist) => (
            <div key={hist.id} className="chat-session-row chat-session-row-history">
              <button
                className="chat-session-row-btn chat-session-row-btn-history"
                onClick={() => {
                  loadHistorySession(hist.id);
                  setOpen(false);
                }}
                type="button"
              >
                <div>{formatSessionTitle(hist.agentId, hist.title)}</div>
                <div className="chat-session-row-meta">
                  {new Date(hist.updatedAt).toLocaleDateString()}
                </div>
              </button>
              <button
                className="chat-session-row-close"
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
