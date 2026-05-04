import { useEffect, useRef, useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { useChatSession } from '../../hooks/useChatSession';
import { getChatAgents, createChatSession } from '../../api';
import { ChatMessage } from './ChatMessage';
import { ChatInput } from './ChatInput';
import { AgentPicker, ConfigBar } from './AgentPicker';
import { ChatSessionList } from './ChatSessionList';
import type { ChatSessionInfo } from '../../types';

export function ChatPanel() {
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const agents = useChatStore((s) => s.agents);
  const loadAgents = useChatStore((s) => s.loadAgents);
  const createSessionStore = useChatStore((s) => s.createSession);
  const { sendMessage, cancel, connected } = useChatSession();
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const [creating, setCreating] = useState(false);

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const isStreaming = activeSession?.status === 'streaming';
  const isConnecting = activeSession?.status === 'connecting' || creating;

  useEffect(() => {
    getChatAgents()
      .then(loadAgents)
      .catch(() => {});
  }, [loadAgents]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [activeSession?.messages.length, activeSession?.messages[activeSession.messages.length - 1]?.content]);

  const handleNewChat = async () => {
    const available = agents.filter((a) => a.available);
    if (available.length === 0) return;
    const agentId = available[0].id;

    setCreating(true);
    try {
      const { id } = await createChatSession(agentId);
      const agent = agents.find((a) => a.id === agentId);
      const session: ChatSessionInfo = {
        id,
        agentId,
        title: agent?.label ?? 'Chat',
        messages: [],
        status: 'idle',
        createdAt: Date.now(),
      };
      createSessionStore(session);
    } catch {
      // failed to create session
    } finally {
      setCreating(false);
    }
  };

  const handleSend = (content: string) => {
    sendMessage(content);
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
        <div className="chat-header-left">
          <AgentPicker />
        </div>
        <div className="chat-header-right">
          <button
            className="chat-new-btn"
            onClick={handleNewChat}
            type="button"
            title="New chat"
            disabled={creating}
          >
            {creating ? '...' : '+'}
          </button>
          <ChatSessionList />
        </div>
      </div>

      {isConnecting && !activeSession ? (
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
            {isStreaming && (
              <div className="chat-streaming">
                <span className="chat-typing-indicator"><span /><span /><span /></span>
              </div>
            )}
            <div ref={messagesEndRef} />
          </div>
          <div className="chat-bottom-bar">
            <ConfigBar />
            <ChatInput
              onSend={handleSend}
              onCancel={cancel}
              streaming={isStreaming}
              disabled={!connected || isConnecting}
            />
          </div>
        </>
      )}
    </div>
  );
}
