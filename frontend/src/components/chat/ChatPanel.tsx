import { useEffect, useRef } from 'react';
import { useChatStore } from '../../stores/chat';
import { useChatSession } from '../../hooks/useChatSession';
import { getChatAgents, createChatSession } from '../../api';
import { ChatMessage } from './ChatMessage';
import { ChatInput } from './ChatInput';
import { AgentPicker } from './AgentPicker';
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

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const isStreaming = activeSession?.status === 'streaming';

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
          <button className="chat-new-btn" onClick={handleNewChat} type="button" title="New chat">
            +
          </button>
          <ChatSessionList />
        </div>
      </div>

      {!activeSession ? (
        <div className="chat-empty">Select an agent to start</div>
      ) : (
        <>
          <div className="chat-messages">
            {activeSession.messages.map((msg) => (
              <ChatMessage key={msg.id} message={msg} />
            ))}
            {isStreaming && activeSession.messages[activeSession.messages.length - 1]?.role === 'user' && (
              <div className="chat-streaming">● Thinking...</div>
            )}
            <div ref={messagesEndRef} />
          </div>
          <ChatInput
            onSend={handleSend}
            onCancel={cancel}
            streaming={isStreaming}
            disabled={!connected}
          />
        </>
      )}
    </div>
  );
}
