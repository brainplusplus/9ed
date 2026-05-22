import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AgentPicker } from './AgentPicker';
import { useChatStore } from '../../stores/chat';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

function resetChatStore() {
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    agents: [
      { id: 'opencode', label: 'OpenCode', available: true, configFound: true, activeModel: '', models: [], providers: [] },
      { id: 'claude', label: 'Claude Code', available: true, configFound: true, activeModel: '', models: [], providers: [] },
    ],
    selectedAgentId: null,
    chatVisible: false,
    historySessions: [],
    historyLoaded: false,
    historyWorkDir: null,
    queuedMessages: {},
    includeIgnoredInMentions: false,
    autoApprove: false,
    restoring: false,
    lastRestoreError: null,
  });
}

describe('AgentPicker', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    resetChatStore();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  it('renders select-agent dropdown in overlay root when opened', () => {
    act(() => {
      root.render(<AgentPicker />);
    });

    const trigger = container.querySelector('.agent-picker');
    expect(trigger).not.toBeNull();

    act(() => {
      trigger?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const overlay = document.body.querySelector('.picker-dropdown.picker-dropdown-overlay');
    expect(overlay).not.toBeNull();
    expect(overlay?.textContent).toContain('OpenCode');
    expect(overlay?.textContent).toContain('Claude Code');
    expect(overlay?.getAttribute('data-overlay')).toBe('true');
  });

  it('sets selectedAgentId in store when agent is picked', async () => {
    act(() => {
      root.render(<AgentPicker />);
    });

    const trigger = container.querySelector('.agent-picker');
    expect(trigger).not.toBeNull();

    act(() => {
      trigger?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const option = document.body.querySelector('.picker-dropdown-overlay .picker-dropdown-item');
    expect(option?.textContent).toContain('OpenCode');

    await act(async () => {
      option?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(useChatStore.getState().selectedAgentId).toBe('opencode');
  });
});
