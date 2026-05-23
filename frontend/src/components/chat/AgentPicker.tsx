import { createPortal } from 'react-dom';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useChatStore } from '../../stores/chat';
import type { ConfigOptionInfo } from '../../types';

export function AgentPicker() {
  const agents = useChatStore((s) => s.agents);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const setSelectedAgent = useChatStore((s) => s.setSelectedAgent);
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const [overlayStyle, setOverlayStyle] = useState<Record<string, string>>({});

  const currentAgent = agents.find((a) => a.id === selectedAgentId);

  const handleSelectAgent = (agentId: string) => {
    setSelectedAgent(agentId);
    setOpen(false);
  };

  const buttonLabel = currentAgent?.label ?? 'Select agent';

  useLayoutEffect(() => {
    if (!open || !triggerRef.current) return;

    const updatePosition = () => {
      if (!triggerRef.current) return;
      const rect = triggerRef.current.getBoundingClientRect();
      const width = Math.min(260, Math.max(220, Math.floor(window.innerWidth * 0.28)));
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
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (triggerRef.current?.contains(target)) return;
      const overlay = document.querySelector('.picker-dropdown-overlay');
      if (overlay?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handlePointerDown);
    return () => document.removeEventListener('mousedown', handlePointerDown);
  }, [open]);

  const dropdown = useMemo(() => createPortal(
    <div className="picker-dropdown picker-dropdown-overlay" data-overlay="true" style={overlayStyle}>
      {agents.map((agent) => (
        <button
          key={agent.id}
          className="picker-dropdown-item"
          data-active={agent.id === selectedAgentId ? 'true' : 'false'}
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
  ), [agents, selectedAgentId, overlayStyle]);

  return (
    <div className="chat-picker-wrap">
      <button
        ref={triggerRef}
        className="agent-picker"
        onClick={() => setOpen(!open)}
        type="button"
      >
        <span className="agent-picker-label">{buttonLabel}</span>
        <span className="agent-picker-caret" aria-hidden="true">▾</span>
      </button>
      {open ? dropdown : null}
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
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);
  const toggleUseActiveBrowser = useChatStore((s) => s.toggleUseActiveBrowser);
  const [openDropdown, setOpenDropdown] = useState<string | null>(null);

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const configOptions = activeSession?.configOptions ?? [];
  const hasActiveSession = !!activeSessionId;

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
          variant={getConfigDropdownVariant(opt)}
          isOpen={!hasActiveSession ? false : openDropdown === opt.id}
          onToggle={() => hasActiveSession && setOpenDropdown(openDropdown === opt.id ? null : opt.id)}
          onChange={(value) => handleChange(opt.id, value)}
          disabled={!hasActiveSession}
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
      <label className="chat-config-toggle" title="Send the active in-app browser page as extra context to the agent">
        <input
          type="checkbox"
          checked={useActiveBrowser}
          onChange={toggleUseActiveBrowser}
        />
        <span className="chat-config-toggle-label">Browser</span>
      </label>
    </div>
  );
}

type ConfigDropdownVariant = 'compact' | 'rich';

function getConfigDropdownVariant(option: ConfigOptionInfo): ConfigDropdownVariant {
  if (option.id === 'model') return 'rich';
  return 'compact';
}

function ConfigDropdown({ option, variant, isOpen, onToggle, onChange, disabled }: {
  option: ConfigOptionInfo;
  variant: ConfigDropdownVariant;
  isOpen: boolean;
  onToggle: () => void;
  onChange: (value: string) => void;
  disabled?: boolean;
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
      const minWidth = variant === 'rich' ? 220 : 180;
      const maxWidth = variant === 'rich' ? 320 : 260;
      const preferredWidth = variant === 'rich'
        ? Math.floor(window.innerWidth * 0.28)
        : Math.floor(window.innerWidth * 0.2);
      const width = Math.min(maxWidth, Math.max(minWidth, preferredWidth));
      const left = Math.max(12, Math.min(rect.left, window.innerWidth - width - 12));
      const topViewportPadding = 12;
      const bottomViewportPadding = 12;
      const gap = 6;
      const aboveSpace = Math.max(0, rect.top - topViewportPadding - gap);
      const belowSpace = Math.max(0, window.innerHeight - rect.bottom - bottomViewportPadding - gap);
      const heightCap = variant === 'rich' ? 320 : 240;
      const minPreferredHeight = variant === 'rich' ? 140 : 100;
      const shouldOpenDownward = aboveSpace < minPreferredHeight && belowSpace > aboveSpace;

      if (shouldOpenDownward) {
        const top = rect.bottom + gap;
        const belowMaxHeight = Math.max(80, Math.min(heightCap, belowSpace));
        setOverlayStyle({
          position: 'fixed',
          left: `${left}px`,
          top: `${top}px`,
          width: `${width}px`,
          maxHeight: `${belowMaxHeight}px`,
        });
        return;
      }

      const upwardMaxHeight = Math.max(80, Math.min(heightCap, aboveSpace));
      const bottom = window.innerHeight - rect.top + gap;

      setOverlayStyle({
        position: 'fixed',
        left: `${left}px`,
        top: 'auto',
        bottom: `${bottom}px`,
        width: `${width}px`,
        maxHeight: `${upwardMaxHeight}px`,
      });
    };

    updatePosition();
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);
    return () => {
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [isOpen, variant]);

  const dropdown = createPortal(
    <div className={`picker-dropdown picker-dropdown-overlay picker-dropdown-${variant}`} data-overlay="true" style={overlayStyle}>
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
    <div className={`chat-picker-wrap ${variant === 'rich' ? 'chat-picker-wrap-rich' : 'chat-picker-wrap-compact'}`}>
      <button
        ref={triggerRef}
        className={`agent-picker ${variant === 'rich' ? 'agent-picker-rich' : 'agent-picker-compact'}`}
        onClick={onToggle}
        type="button"
        data-compact={variant === 'compact' ? 'true' : undefined}
        title={option.name}
        disabled={disabled}
      >
        <span className={`agent-picker-label ${variant === 'rich' ? 'agent-picker-label-multiline' : 'agent-picker-label-compact'}`}>{label}</span>
        <span className="agent-picker-caret" aria-hidden="true">▾</span>
      </button>
      {isOpen ? dropdown : null}
    </div>
  );
}
