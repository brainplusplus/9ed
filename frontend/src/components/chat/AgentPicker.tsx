import { createPortal } from 'react-dom';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';
import { createChatSession } from '../../api';
import type { ChatSessionInfo, ConfigOptionInfo } from '../../types';

export function AgentPicker() {
  const agents = useChatStore((s) => s.agents);
  const activeProject = useWorkspaceStore((s) => s.projects.find((p) => p.id === s.activeProjectId) ?? null);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const sessions = useChatStore((s) => s.sessions);
  const createSessionStore = useChatStore((s) => s.createSession);
  const [agentOpen, setAgentOpen] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const [overlayStyle, setOverlayStyle] = useState<Record<string, string>>({});

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const currentAgent = agents.find((a) => a.id === activeSession?.agentId);

  const handleSelectAgent = async (agentId: string) => {
    setAgentOpen(false);
    setConnecting(true);
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
      setConnecting(false);
    }
  };

  const buttonLabel = connecting
    ? 'Connecting...'
    : currentAgent?.label ?? 'Select agent';

  useLayoutEffect(() => {
    if (!agentOpen || !triggerRef.current) return;

    const updatePosition = () => {
      if (!triggerRef.current) return;
      const rect = triggerRef.current.getBoundingClientRect();
      const width = Math.min(220, Math.max(180, Math.floor(window.innerWidth * 0.6)));
      const left = Math.max(12, Math.min(rect.left, window.innerWidth - width - 12));
      const top = Math.min(rect.bottom + 6, window.innerHeight - 24);
      const maxHeight = Math.max(160, Math.min(280, window.innerHeight - top - 12));
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
  }, [agentOpen]);

  useEffect(() => {
    if (!agentOpen) return;
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (triggerRef.current?.contains(target)) return;
      const overlay = document.querySelector('.picker-dropdown-overlay');
      if (overlay?.contains(target)) return;
      setAgentOpen(false);
    };
    document.addEventListener('mousedown', handlePointerDown);
    return () => document.removeEventListener('mousedown', handlePointerDown);
  }, [agentOpen]);

  const dropdown = useMemo(() => createPortal(
    <div className="picker-dropdown picker-dropdown-overlay" data-overlay="true" style={overlayStyle}>
      {agents.map((agent) => (
        <button
          key={agent.id}
          className="picker-dropdown-item"
          data-active={agent.id === currentAgent?.id ? 'true' : 'false'}
          data-available={agent.available ? 'true' : 'false'}
          onClick={() => agent.available && handleSelectAgent(agent.id)}
          disabled={!agent.available}
          type="button"
        >
          {agent.label}
        </button>
      ))}
    </div>,
    document.body,
  ), [agents, currentAgent?.id, overlayStyle]);

  return (
    <div className="chat-picker-wrap">
      <button
        ref={triggerRef}
        className="agent-picker"
        onClick={() => setAgentOpen(!agentOpen)}
        type="button"
        disabled={connecting}
        data-connecting={connecting ? 'true' : 'false'}
      >
        {connecting && <span className="chat-connecting-spinner chat-inline-spinner" />}
        <span className="agent-picker-label">{buttonLabel}</span>
        <span className="agent-picker-caret" aria-hidden="true">▾</span>
      </button>
      {agentOpen && !connecting ? dropdown : null}
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
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const [overlayStyle, setOverlayStyle] = useState<Record<string, string>>({});

  useLayoutEffect(() => {
    if (!isOpen || !triggerRef.current) return;

    const updatePosition = () => {
      if (!triggerRef.current) return;
      const rect = triggerRef.current.getBoundingClientRect();
      const width = Math.min(220, Math.max(180, Math.floor(window.innerWidth * 0.6)));
      const left = Math.max(12, Math.min(rect.left, window.innerWidth - width - 12));
      const bottomSpace = rect.top - 12;
      const maxHeight = Math.max(120, Math.min(280, bottomSpace));
      setOverlayStyle({
        position: 'fixed',
        left: `${left}px`,
        top: `${Math.max(12, rect.top - maxHeight - 6)}px`,
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
  }, [isOpen]);

  const dropdown = createPortal(
    <div className="picker-dropdown picker-dropdown-up picker-dropdown-overlay" data-overlay="true" style={overlayStyle}>
      {option.options.map((val) => (
        <button
          key={val.value}
          className="picker-dropdown-item"
          data-active={val.value === option.currentValue ? 'true' : 'false'}
          onClick={() => onChange(val.value)}
          type="button"
        >
          {val.name}
        </button>
      ))}
    </div>,
    document.body,
  );

  return (
    <div className="chat-picker-wrap chat-picker-wrap-compact">
      <button
        ref={triggerRef}
        className="agent-picker"
        onClick={onToggle}
        type="button"
        data-compact="true"
        title={option.name}
      >
        <span className="agent-picker-label">{label}</span>
        <span className="agent-picker-caret" aria-hidden="true">▾</span>
      </button>
      {isOpen ? dropdown : null}
    </div>
  );
}
