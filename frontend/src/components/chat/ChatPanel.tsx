import { useEffect, useRef, useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';
import { useChatSession } from '../../hooks/useChatSession';
import { getChatAgents, createChatSession } from '../../api';
import { ChatMessage } from './ChatMessage';
import { ChatInput } from './ChatInput';
import { ChatQueue } from './ChatQueue';
import { PermissionDialog } from './PermissionDialog';
import { AgentPicker, ConfigBar } from './AgentPicker';
import { ChatTabs } from './ChatTabs';
import { ChatSessionList } from './ChatSessionList';
import type { ChatSessionInfo } from '../../types';

export function ChatPanel() {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const activeSession = useChatStore((s) => s.sessions.find((sess) => sess.id === s.activeSessionId));
  const activeProject = useWorkspaceStore((s) => s.projects.find((p) => p.id === s.activeProjectId) ?? null);
  const agents = useChatStore((s) => s.agents);
  const sessions = useChatStore((s) => s.sessions);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const loadAgents = useChatStore((s) => s.loadAgents);
  const createSessionStore = useChatStore((s) => s.createSession);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const deleteSessionStore = useChatStore((s) => s.deleteSession);
  const restoring = useChatStore((s) => s.restoring);
  const { sendMessage, cancel, setConfigOption, respondPermission, rejectPermission, setAutoApprove, connected } = useChatSession();
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const [creating, setCreating] = useState(false);

  const isStreaming = activeSession?.status === 'streaming';
  const isConnecting = activeSession?.status === 'connecting' || creating;
  const isArchived = activeSession?.kind === 'archived';

  useEffect(() => {
    getChatAgents()
      .then(loadAgents)
      .catch(() => {});
  }, [loadAgents]);

  const messages = activeSession?.messages;
  const lastMsgContent = messages?.[messages.length - 1]?.content;
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages?.length, lastMsgContent]);

  const handleNewChat = async () => {
    const agentId = selectedAgentId;
    if (!agentId) return;
    const agent = agents.find((a) => a.id === agentId);
    if (!agent?.available) return;

    setCreating(true);
    try {
      const { id } = await createChatSession(agentId, activeProject?.path);
      const session: ChatSessionInfo = {
        id,
        recordId: id,
        agentId,
        title: agent.label ?? 'Chat',
        messages: [],
        status: 'idle',
        createdAt: Date.now(),
        kind: 'live',
      };
      createSessionStore(session);
    } catch {
    } finally {
      setCreating(false);
    }
  };

  const handleCloseTab = (sessionId: string) => {
    const idx = sessions.findIndex((s) => s.id === sessionId);
    if (activeSessionId === sessionId) {
      const fallback = sessions[idx + 1] ?? sessions[idx - 1] ?? null;
      setActiveSession(fallback?.id ?? null);
    }
    deleteSessionStore(sessionId);
  };

  const handleSend = (content: string, attachments?: import('./ChatInput').Attachment[]) => {
    sendMessage(content, undefined, attachments);
  };

  if (agents.length === 0) {
    return (
      <div className="chat-panel">
        <div className="chat-empty">No agents available</div>
      </div>
    );
  }

  return (
    <div className="chat-panel">
      <div className="chat-header">
        <div className="chat-tab-session-area">
          <ChatTabs
            tabs={sessions}
            activeTabId={activeSessionId}
            agents={agents}
            onSelectTab={setActiveSession}
            onCloseTab={handleCloseTab}
          />
        </div>
        <div className="chat-header-actions">
          <AgentPicker />
          <ChatSessionList />
          <button
            className="chat-new-btn chat-new-btn-icon"
            onClick={() => void handleNewChat()}
            type="button"
            title={creating ? 'Creating new chat...' : 'New chat'}
            disabled={creating || !selectedAgentId}
          >
            {creating ? 'Creating...' : '+'}
          </button>
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
        <div className="chat-empty">Select an agent and press + to start</div>
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
            {!isArchived && activeSession && <ChatQueue sessionId={activeSession.id} onSendNow={handleSend} />}
            {!isArchived && <ConfigBar setConfigOption={setConfigOption} setAutoApprove={setAutoApprove} />}
            <ChatInput
              onSend={handleSend}
              onCancel={cancel}
              streaming={isStreaming}
              disabled={isArchived || !connected || isConnecting}
            />
          </div>
        </>
      )}
    </div>
  );
}
