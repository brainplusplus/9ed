import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { normalizeWorkDir, sessionBelongsToWorkDir, useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';
import { useChatSession } from '../../hooks/useChatSession';
import { getChatAgents, createChatSession } from '../../api';
import { ChatMessage } from './ChatMessage';
import { ChatInput } from './ChatInput';
import { ChatQueue } from './ChatQueue';
import { PermissionDialog } from './PermissionDialog';
import { ConfigBar } from './AgentPicker';
import { ChatTabs } from './ChatTabs';
import { ChatSessionList } from './ChatSessionList';
import type { ChatSessionInfo } from '../../types';

const AGENT_RETRY_DELAYS_MS = [500, 1500, 4000];

function ContextUsageBar({ contextUsed, contextWindow, costAmount, costCurrency }: { contextUsed?: number; contextWindow?: number; costAmount?: number; costCurrency?: string }) {
  if (!contextUsed || !contextWindow || contextWindow <= 0) return null;

  const pct = Math.min((contextUsed / contextWindow) * 100, 100);
  const usedK = (contextUsed / 1000).toFixed(1).replace(/\.0$/, '');
  const windowK = (contextWindow / 1000).toFixed(0);
  const pctDisplay = pct.toFixed(0);

  let barColor = 'var(--ctx-bar-low)';
  if (pct > 80) barColor = 'var(--ctx-bar-high, #ef4444)';
  else if (pct > 60) barColor = 'var(--ctx-bar-mid, #f59e0b)';

  const costStr = costAmount && costAmount > 0 ? `${costCurrency === 'USD' ? '$' : costCurrency ? costCurrency + ' ' : '$'}${costAmount.toFixed(4)}` : '';

  return (
    <div className="ctx-usage" title={`${contextUsed.toLocaleString()} / ${contextWindow.toLocaleString()} tokens`}>
      <div className="ctx-usage-label">
        <span className="ctx-usage-icon">🧠</span>
        <span className="ctx-usage-text">{usedK}k / {windowK}k</span>
        <span className="ctx-usage-pct">{pctDisplay}%</span>
        {costStr && <span className="ctx-usage-cost">{costStr}</span>}
      </div>
      <div className="ctx-usage-track">
        <div className="ctx-usage-fill" style={{ width: `${pct}%`, background: barColor }} />
      </div>
    </div>
  );
}

export function ChatPanel() {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const activeProject = useWorkspaceStore((s) => s.projects.find((p) => p.id === s.activeProjectId) ?? null);
  const agents = useChatStore((s) => s.agents);
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionByWorkDir = useChatStore((s) => s.activeSessionByWorkDir);
  const setSelectedAgent = useChatStore((s) => s.setSelectedAgent);
  const useActiveTerminal = useChatStore((s) => s.useActiveTerminal);
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);
  const activeTerminalId = useChatStore((s) => s.activeTerminalId);
  const browserSelection = useChatStore((s) => s.browserSelection);
  const browserSelectionMode = useChatStore((s) => s.browserSelectionMode);
  const browserSelectionCapture = useChatStore((s) => s.browserSelectionCapture);
  const loadAgents = useChatStore((s) => s.loadAgents);
  const createSessionStore = useChatStore((s) => s.createSession);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const deleteSessionStore = useChatStore((s) => s.deleteSession);
  const resumeSession = useChatStore((s) => s.resumeSession);
  const restoring = useChatStore((s) => s.restoring);
  const { sendMessage, cancel, setConfigOption, respondPermission, rejectPermission, setAutoApprove, connected } = useChatSession();
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const autoResumeKeysRef = useRef<Set<string>>(new Set());
  const createMenuButtonRef = useRef<HTMLButtonElement | null>(null);
  const [creating, setCreating] = useState(false);
  const [createMenuOpen, setCreateMenuOpen] = useState(false);
  const [createMenuStyle, setCreateMenuStyle] = useState<Record<string, string>>({});
  const [agentsLoading, setAgentsLoading] = useState(agents.length === 0);
  const [agentsError, setAgentsError] = useState<string | null>(null);

  const projectSessions = sessions.filter((session) => sessionBelongsToWorkDir(session, activeProject?.path));
  const activeSession = projectSessions.find((sess) => sess.id === activeSessionId) ?? null;
  const projectWorkDirKey = normalizeWorkDir(activeProject?.path);
  const rememberedProjectSessionId = projectWorkDirKey ? activeSessionByWorkDir[projectWorkDirKey] : undefined;
  const projectSessionIds = projectSessions.map((session) => session.id).join('|');
  const isStreaming = activeSession?.status === 'streaming';
  const isConnecting = activeSession?.status === 'connecting' || creating;
  const isArchived = activeSession?.kind === 'archived';
  const canResumeArchived = Boolean(activeSession?.kind === 'archived' && activeSession.agentId && activeSession.workDir);

  useEffect(() => {
    let cancelled = false;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;

    const load = async (attempt: number) => {
      if (cancelled) return;
      if (attempt === 0) setAgentsLoading(true);
      try {
        const nextAgents = await getChatAgents();
        if (cancelled) return;
        loadAgents(nextAgents);
        setAgentsError(null);
        setAgentsLoading(false);
        if (nextAgents.length === 0 && attempt < AGENT_RETRY_DELAYS_MS.length) {
          retryTimer = setTimeout(() => void load(attempt + 1), AGENT_RETRY_DELAYS_MS[attempt]);
        }
      } catch (err) {
        if (cancelled) return;
        setAgentsError(err instanceof Error ? err.message : 'Failed to load agents');
        if (attempt < AGENT_RETRY_DELAYS_MS.length) {
          retryTimer = setTimeout(() => void load(attempt + 1), AGENT_RETRY_DELAYS_MS[attempt]);
          return;
        }
        setAgentsLoading(false);
      }
    };

    void load(0);
    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
    };
  }, [loadAgents]);

  useEffect(() => {
    if (!activeSession || activeSession.status !== 'idle') return;
    if (activeSession.kind !== 'archived' || !activeSession.agentId || !activeSession.workDir) return;
    const resumeKey = activeSession.recordId ?? activeSession.id;
    if (autoResumeKeysRef.current.has(resumeKey)) return;
    autoResumeKeysRef.current.add(resumeKey);
    void resumeSession(activeSession.id).finally(() => {
      autoResumeKeysRef.current.delete(resumeKey);
    });
  }, [activeSession?.id, activeSession?.recordId, activeSession?.kind, activeSession?.status, activeSession?.agentId, activeSession?.workDir, resumeSession]);

  useEffect(() => {
    if (!activeProject) return;
    if (activeSession) return;

    const remembered = projectSessions.find((session) => session.id === rememberedProjectSessionId);
    const fallback = remembered ?? projectSessions[projectSessions.length - 1] ?? null;
    if ((activeSessionId ?? null) !== (fallback?.id ?? null)) {
      setActiveSession(fallback?.id ?? null);
    }
  }, [activeProject?.path, activeSession, activeSessionId, projectSessionIds, projectSessions, rememberedProjectSessionId, setActiveSession]);

  const messages = activeSession?.messages;
  const lastMsgContent = messages?.[messages.length - 1]?.content;
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages?.length, lastMsgContent]);

  useLayoutEffect(() => {
    if (!createMenuOpen || !createMenuButtonRef.current) return;

    const updatePosition = () => {
      if (!createMenuButtonRef.current) return;
      const rect = createMenuButtonRef.current.getBoundingClientRect();
      const width = Math.min(280, Math.max(220, Math.floor(window.innerWidth * 0.72)));
      const left = Math.max(12, Math.min(rect.right - width, window.innerWidth - width - 12));
      const top = Math.min(rect.bottom + 6, window.innerHeight - 24);
      const maxHeight = Math.max(160, Math.min(320, window.innerHeight - top - 12));
      setCreateMenuStyle({
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
  }, [createMenuOpen]);

  useEffect(() => {
    if (!createMenuOpen) return;
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (createMenuButtonRef.current?.contains(target)) return;
      const overlay = document.querySelector('.chat-agent-create-dropdown');
      if (overlay?.contains(target)) return;
      setCreateMenuOpen(false);
    };

    document.addEventListener('mousedown', handlePointerDown);
    return () => document.removeEventListener('mousedown', handlePointerDown);
  }, [createMenuOpen]);

  const handleNewChat = async (agentId: string) => {
    const agent = agents.find((a) => a.id === agentId);
    if (!agent?.available) return;

    setSelectedAgent(agentId);
    setCreateMenuOpen(false);
    setCreating(true);
    try {
      const terminalEnabled = useActiveTerminal && !!activeTerminalId;
      const created = await createChatSession(agentId, activeProject?.path, terminalEnabled);
      const session: ChatSessionInfo = {
        id: created.id,
        recordId: created.resumedFrom ?? created.id,
        agentId,
        title: agent.label ?? 'Chat',
        messages: [],
        status: 'idle',
        createdAt: Date.now(),
        kind: 'live',
        workDir: created.workDir ?? activeProject?.path,
        acpSessionId: created.acpSessionId,
        useActiveTerminal: terminalEnabled,
        terminalId: terminalEnabled ? activeTerminalId : undefined,
        useActiveBrowser,
        browserSelection,
        browserSelectionMode,
        browserSelectionCapture,
      };
      createSessionStore(session);
    } catch {
    } finally {
      setCreating(false);
    }
  };

  const availableAgents = agents.filter((agent) => agent.available);
  const canCreateChat = !creating && !agentsLoading && availableAgents.length > 0;

  const handleCloseTab = (sessionId: string) => {
    const idx = projectSessions.findIndex((s) => s.id === sessionId);
    if (activeSessionId === sessionId) {
      const fallback = projectSessions[idx + 1] ?? projectSessions[idx - 1] ?? null;
      setActiveSession(fallback?.id ?? null);
    }
    deleteSessionStore(sessionId);
  };

  const handleSend = (content: string, attachments?: import('./ChatInput').Attachment[]) => {
    sendMessage(content, undefined, attachments);
  };

  if (agents.length === 0 && !activeSession && !restoring) {
    return (
      <div className="chat-panel">
        <div className="chat-empty">{agentsLoading ? 'Loading agents...' : (agentsError ?? 'No agents available')}</div>
      </div>
    );
  }

  return (
    <div className="chat-panel">
      <div className="chat-header">
        <div className="chat-tab-session-area">
          <ChatTabs
            tabs={projectSessions}
            activeTabId={activeSessionId}
            agents={agents}
            onSelectTab={setActiveSession}
            onCloseTab={handleCloseTab}
          />
        </div>
        <div className="chat-header-actions">
          <ChatSessionList />
          <div className="chat-agent-create-wrap">
            <button
              ref={createMenuButtonRef}
              className="chat-new-btn chat-new-btn-icon"
              onClick={() => canCreateChat && setCreateMenuOpen((open) => !open)}
              type="button"
              aria-label={creating ? 'Creating new chat' : 'New chat'}
              aria-haspopup="menu"
              aria-expanded={createMenuOpen}
              title={creating ? 'Creating new chat...' : agentsLoading ? 'Loading agents...' : 'New chat'}
              disabled={!canCreateChat}
            >
              {creating ? <span className="chat-connecting-spinner chat-inline-spinner" /> : '+'}
            </button>
            {createMenuOpen ? createPortal(
              <div
                className="picker-dropdown picker-dropdown-overlay chat-agent-create-dropdown"
                data-overlay="true"
                role="menu"
                aria-label="Create chat with agent"
                style={createMenuStyle}
              >
                {agents.map((agent) => (
                  <button
                    key={agent.id}
                    className="picker-dropdown-item chat-agent-create-item"
                    data-available={agent.available ? 'true' : 'false'}
                    onClick={() => agent.available && void handleNewChat(agent.id)}
                    disabled={!agent.available || creating}
                    type="button"
                    role="menuitem"
                  >
                    <span>{agent.label}</span>
                    {!agent.available && <span className="chat-agent-create-unavailable">Unavailable</span>}
                  </button>
                ))}
              </div>,
              document.body,
            ) : null}
          </div>
        </div>
      </div>

      {restoring ? (
        <div className="chat-empty">
          <div className="chat-connecting">
            <span className="chat-connecting-spinner" />
            Restoring session...
          </div>
        </div>
      ) : isConnecting && !activeSession ? (
        <div className="chat-empty">
          <div className="chat-connecting">
            <span className="chat-connecting-spinner" />
            Connecting to agent...
          </div>
        </div>
      ) : !activeSession ? (
        <div className="chat-empty">Press + to start a chat</div>
      ) : (
        <>
          <div className="chat-messages">
            {activeSession.messages.map((msg, idx) => (
              <ChatMessage
                key={msg.id}
                message={msg}
                streaming={isStreaming && msg.role === 'assistant' && idx === activeSession.messages.length - 1}
              />
            ))}
            {activeSession.pendingPermission && (
              <PermissionDialog
                permission={activeSession.pendingPermission}
                onRespond={respondPermission}
                onReject={rejectPermission}
              />
            )}
            {isStreaming && !activeSession.pendingPermission && (
              <div className="chat-streaming">
                <span className="chat-typing-indicator"><span /><span /><span /></span>
              </div>
            )}
            <div ref={messagesEndRef} />
          </div>
          <div className="chat-bottom-bar">
            <ContextUsageBar contextUsed={activeSession?.contextUsed} contextWindow={activeSession?.contextWindow} costAmount={activeSession?.costAmount} costCurrency={activeSession?.costCurrency} />
            {!isArchived && activeSession && <ChatQueue sessionId={activeSession.id} onSendNow={handleSend} />}
            {!isArchived && <ConfigBar setConfigOption={setConfigOption} setAutoApprove={setAutoApprove} connected={connected} />}
            <ChatInput
              onSend={handleSend}
              onCancel={cancel}
              streaming={isStreaming}
              disabled={(isArchived && !canResumeArchived) || isConnecting}
              canSend={connected}
            />
          </div>
        </>
      )}
    </div>
  );
}
