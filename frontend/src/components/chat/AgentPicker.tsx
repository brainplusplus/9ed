import { useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { createChatSession } from '../../api';
import type { ChatSessionInfo, ConfigOptionInfo } from '../../types';

export function AgentPicker() {
  const agents = useChatStore((s) => s.agents);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const sessions = useChatStore((s) => s.sessions);
  const createSessionStore = useChatStore((s) => s.createSession);
  const [agentOpen, setAgentOpen] = useState(false);
  const [connecting, setConnecting] = useState(false);

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const currentAgent = agents.find((a) => a.id === activeSession?.agentId);

  const handleSelectAgent = async (agentId: string) => {
    setAgentOpen(false);
    setConnecting(true);
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
    } finally {
      setConnecting(false);
    }
  };

  const buttonLabel = connecting
    ? 'Connecting...'
    : currentAgent?.label ?? 'Select agent';

  return (
    <div style={{ position: 'relative' }}>
      <button
        className="agent-picker"
        onClick={() => setAgentOpen(!agentOpen)}
        type="button"
        disabled={connecting}
        style={connecting ? { opacity: 0.7 } : undefined}
      >
        {connecting && <span className="chat-connecting-spinner" style={{ width: 10, height: 10, marginRight: 6, display: 'inline-block', verticalAlign: 'middle' }} />}
        {buttonLabel} ▾
      </button>
      {agentOpen && !connecting && (
        <div className="picker-dropdown">
          {agents.map((agent) => (
            <button
              key={agent.id}
              className="picker-dropdown-item"
              style={{
                background: agent.id === currentAgent?.id ? 'var(--list-active-bg, rgba(255,255,255,0.1))' : 'none',
                opacity: agent.available ? 1 : 0.5,
                cursor: agent.available ? 'pointer' : 'not-allowed',
              }}
              onClick={() => agent.available && handleSelectAgent(agent.id)}
              disabled={!agent.available}
              type="button"
            >
              {agent.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

type ConfigBarProps = {
  setConfigOption?: (configId: string, value: string) => void;
  setAutoApprove?: (enabled: boolean) => void;
};

export function ConfigBar({ setConfigOption, setAutoApprove }: ConfigBarProps) {
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const sessions = useChatStore((s) => s.sessions);
  const includeIgnored = useChatStore((s) => s.includeIgnoredInMentions);
  const toggleIncludeIgnored = useChatStore((s) => s.toggleIncludeIgnored);
  const autoApprove = useChatStore((s) => s.autoApprove);
  const toggleAutoApprove = useChatStore((s) => s.toggleAutoApprove);
  const [openDropdown, setOpenDropdown] = useState<string | null>(null);

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const configOptions = activeSession?.configOptions ?? [];

  const handleChange = (configId: string, value: string) => {
    setOpenDropdown(null);
    setConfigOption?.(configId, value);
  };

  const handleAutoApproveToggle = () => {
    const newValue = !autoApprove;
    toggleAutoApprove();
    setAutoApprove?.(newValue);
  };

  return (
    <div className="chat-config-bar">
      {configOptions.map((opt) => (
        <ConfigDropdown
          key={opt.id}
          option={opt}
          isOpen={openDropdown === opt.id}
          onToggle={() => setOpenDropdown(openDropdown === opt.id ? null : opt.id)}
          onChange={(value) => handleChange(opt.id, value)}
        />
      ))}
      <label className="chat-config-toggle" title="Auto-approve all tool permissions (yolo mode)">
        <input
          type="checkbox"
          checked={autoApprove}
          onChange={handleAutoApproveToggle}
        />
        <span className="chat-config-toggle-label">🔓 Auto</span>
      </label>
      <label className="chat-config-toggle" title="Include gitignored files in @ mentions">
        <input
          type="checkbox"
          checked={includeIgnored}
          onChange={toggleIncludeIgnored}
        />
        <span className="chat-config-toggle-label">@ ignored</span>
      </label>
    </div>
  );
}

function ConfigDropdown({ option, isOpen, onToggle, onChange }: {
  option: ConfigOptionInfo;
  isOpen: boolean;
  onToggle: () => void;
  onChange: (value: string) => void;
}) {
  const currentOption = option.options.find((o) => o.value === option.currentValue);
  const label = currentOption?.name ?? option.currentValue;

  return (
    <div style={{ position: 'relative' }}>
      <button
        className="agent-picker"
        onClick={onToggle}
        type="button"
        style={{ fontSize: '0.72rem', padding: '2px 6px' }}
        title={option.name}
      >
        {label} ▾
      </button>
      {isOpen && (
        <div className="picker-dropdown picker-dropdown-up">
          {option.options.map((val) => (
            <button
              key={val.value}
              className="picker-dropdown-item"
              style={{
                background: val.value === option.currentValue ? 'var(--list-active-bg, rgba(255,255,255,0.1))' : 'none',
              }}
              onClick={() => onChange(val.value)}
              type="button"
            >
              {val.name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
