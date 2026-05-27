import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { sessionBelongsToWorkDir, useChatStore } from '../../stores/chat';
import { deleteChatSession } from '../../api';
import { useWorkspaceStore } from '../../stores/workspace';

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

function SessionStatusIcon({ status, kind }: { status: string; kind: string }) {
  if (status === 'connecting') return <span className="session-status-icon session-status-connecting" title="Connecting">○</span>;
  if (kind === 'archived') return <span className="session-status-icon session-status-archived" title="Archived">◷</span>;
  if (kind === 'resumable') return <span className="session-status-icon session-status-resumable" title="Resumable">↻</span>;
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
  const historyWorkDir = useChatStore((s) => s.historyWorkDir);
  const loadHistory = useChatStore((s) => s.loadHistory);
  const loadHistorySession = useChatStore((s) => s.loadHistorySession);
  const deleteHistorySession = useChatStore((s) => s.deleteHistorySession);
  const lastRestoreError = useChatStore((s) => s.lastRestoreError);
  const activeProject = useWorkspaceStore((s) => s.projects.find((p) => p.id === s.activeProjectId) ?? null);
  const [open, setOpen] = useState(false);
  const [loadingHistorySessionId, setLoadingHistorySessionId] = useState<string | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const [overlayStyle, setOverlayStyle] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!historyLoaded || historyWorkDir !== (activeProject?.path ?? null)) {
      loadHistory(activeProject?.path);
    }
  }, [activeProject?.path, historyLoaded, historyWorkDir, loadHistory]);

  useLayoutEffect(() => {
    if (!open || !triggerRef.current) return;

    const updatePosition = () => {
      if (!triggerRef.current) return;
      const rect = triggerRef.current.getBoundingClientRect();
      const width = Math.min(320, Math.max(250, Math.floor(window.innerWidth * 0.72)));
      const left = Math.max(12, Math.min(rect.right - width, window.innerWidth - width - 12));
      const top = Math.min(rect.bottom + 6, window.innerHeight - 24);
      const maxHeight = Math.max(180, Math.min(320, window.innerHeight - top - 12));
      setOverlayStyle({
        position: 'fixed',
        top: `${top}px`,
        left: `${left}px`,
        width: `${width}px`,
        maxHeight: `${maxHeight}px`,
      });
    };

    updatePosition();
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);
    return () => {
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (triggerRef.current?.contains(target)) return;
      const overlay = document.querySelector('.chat-session-dropdown-overlay');
      if (overlay?.contains(target)) return;
      setOpen(false);
    };

    document.addEventListener('mousedown', handlePointerDown);
    return () => document.removeEventListener('mousedown', handlePointerDown);
  }, [open]);

  const projectSessions = sessions.filter((session) => sessionBelongsToWorkDir(session, activeProject?.path));
  const activeSessionIds = new Set(projectSessions.map((s) => s.id));
  const activeAcpSessionIds = new Set(projectSessions.map((s) => s.acpSessionId).filter(Boolean));
  const pastSessions = historySessions.filter((h) => !activeSessionIds.has(h.id) && (!h.acpSessionId || !activeAcpSessionIds.has(h.acpSessionId)));

  const dropdownContent = useMemo(() => (
    <div
      className="chat-session-dropdown chat-session-dropdown-overlay"
      data-overlay="true"
      style={overlayStyle}
    >
      {lastRestoreError && (
        <div className="chat-session-restore-error">
          ⚠ Restore failed: {lastRestoreError.reason}
        </div>
      )}
      {projectSessions.length > 0 && (
        <div className="chat-session-section-label">
          Active
        </div>
      )}
      {projectSessions.map((session) => (
        <div
          key={session.id}
          className={`chat-session-row${session.id === activeSessionId ? ' active' : ''}`}
        >
          <SessionStatusIcon status={session.status} kind={session.kind} />
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
          {hist.acpSessionId && (
            <span className="session-status-icon session-status-resumable" title="ACP session — resumable">↻</span>
          )}
          <button
            className="chat-session-row-btn chat-session-row-btn-history"
            onClick={async () => {
              setLoadingHistorySessionId(hist.id);
              try {
                await loadHistorySession(hist.id);
              } finally {
                setLoadingHistorySessionId(null);
                setOpen(false);
              }
            }}
            type="button"
          >
            <div>{formatSessionTitle(hist.agentId, hist.title)}</div>
            <div className="chat-session-row-meta">
              {new Date(hist.updatedAt).toLocaleDateString()}
              {hist.acpSessionId && <span className="session-resumable-badge">resumable</span>}
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
  ), [activeSessionId, deleteHistorySession, deleteSessionStore, lastRestoreError, loadHistorySession, overlayStyle, pastSessions, projectSessions, setActiveSession]);

  const hasItems = projectSessions.length > 0 || pastSessions.length > 0;
  if (!hasItems) return null;

  return (
    <div className="chat-picker-wrap chat-session-wrap">
      <button
        ref={triggerRef}
        className="chat-history-btn"
        onClick={() => !loadingHistorySessionId && setOpen(!open)}
        type="button"
        title="Chat history"
        disabled={loadingHistorySessionId !== null}
      >
        {loadingHistorySessionId ? <span className="chat-connecting-spinner chat-inline-spinner" /> : '📋'}
      </button>
      {open ? createPortal(dropdownContent, document.body) : null}
    </div>
  );
}
