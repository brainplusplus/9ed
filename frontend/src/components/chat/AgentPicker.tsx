import { useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { createChatSession } from '../../api';
import type { ChatSessionInfo } from '../../types';

export function AgentPicker() {
  const agents = useChatStore((s) => s.agents);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const sessions = useChatStore((s) => s.sessions);
  const createSessionStore = useChatStore((s) => s.createSession);
  const [open, setOpen] = useState(false);

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const currentAgent = agents.find((a) => a.id === activeSession?.agentId);

  const handleSelect = async (agentId: string) => {
    setOpen(false);
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
      // session creation failed silently
    }
  };

  return (
    <div style={{ position: 'relative' }}>
      <button className="agent-picker" onClick={() => setOpen(!open)} type="button">
        {currentAgent?.label ?? 'Select agent'} ▾
      </button>
      {open && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            background: 'var(--sidebar-bg)',
            border: '1px solid var(--border-color)',
            borderRadius: '4px',
            zIndex: 100,
            minWidth: '160px',
          }}
        >
          {agents.map((agent) => (
            <button
              key={agent.id}
              style={{
                display: 'block',
                width: '100%',
                padding: '6px 12px',
                background: 'none',
                border: 'none',
                color: agent.available ? 'var(--sidebar-fg)' : 'var(--activity-fg)',
                cursor: agent.available ? 'pointer' : 'not-allowed',
                textAlign: 'left',
                fontSize: '0.82rem',
                opacity: agent.available ? 1 : 0.5,
              }}
              onClick={() => agent.available && handleSelect(agent.id)}
              disabled={!agent.available}
              type="button"
            >
              {agent.label}
            </button>
          ))}
          {agents.length === 0 && (
            <div style={{ padding: '8px 12px', fontSize: '0.8rem', color: 'var(--activity-fg)' }}>
              No agents available
            </div>
          )}
        </div>
      )}
    </div>
  );
}
