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
import { ChatSessionList } from './ChatSessionList';
import type { ChatSessionInfo } from '../../types';

export function ChatPanel() {
  const activeSession = useChatStore((s) => s.sessions.find((sess) => sess.id === s.activeSessionId));
  const activeProject = useWorkspaceStore((s) => s.projects.find((p) => p.id === s.activeProjectId) ?? null);
  const agents = useChatStore((s) => s.agents);
  const loadAgents = useChatStore((s) => s.loadAgents);
  const createSessionStore = useChatStore((s) => s.createSession);
  const restoring = useChatStore((s) => s.restoring);
  const { sendMessage, cancel, setConfigOption, respondPermission, rejectPermission, setAutoApprove, connected } = useChatSession();
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const [creating, setCreating] = useState(false);

  const isStreaming = activeSession?.status === 'streaming';
  const isConnecting = activeSession?.status === 'connecting' || creating;
  const activeAgent = agents.find((agent) => agent.id === activeSession?.agentId);
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
    const available = agents.filter((a) => a.available);
    if (available.length === 0) return;
    const agentId = available[0].id;

    setCreating(true);
    try {
      const { id } = await createChatSession(agentId, activeProject?.path);
      const agent = agents.find((a) => a.id === agentId);
      const session: ChatSessionInfo = {
        id,
        recordId: id,
        agentId,
        title: agent?.label ?? 'Chat',
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
        <div className="chat-header-top">
          <div className="chat-title-block">
            <div className="chat-title-eyebrow">Assistant</div>
            <div className="chat-title-row">
              <span className="chat-title-main">{activeSession?.title || 'New Chat'}</span>
              {activeAgent && <span className="chat-title-subtle">{activeAgent.label}</span>}
              {isArchived && <span className="chat-title-badge chat-badge-archived">Archived</span>}
            </div>
          </div>
        </div>
        <div className="chat-header-actions">
          <AgentPicker />
          <ChatSessionList />
          <button
            className="chat-new-btn chat-new-btn-icon"
            onClick={handleNewChat}
            type="button"
            title="New chat"
            disabled={creating}
          >
            {creating ? '...' : '✎'}
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
        <div className="chat-empty">Select an agent to start</div>
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
