import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AgentPicker } from './AgentPicker';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const createChatSession = vi.fn();

vi.mock('../../api', () => ({
  createChatSession: (...args: unknown[]) => createChatSession(...args),
}));

function resetChatStore() {
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    agents: [
      { id: 'opencode', label: 'OpenCode', available: true, configFound: true, activeModel: '', models: [], providers: [] },
      { id: 'claude', label: 'Claude Code', available: true, configFound: true, activeModel: '', models: [], providers: [] },
    ],
    chatVisible: false,
    historySessions: [],
    historyLoaded: false,
    queuedMessages: {},
    includeIgnoredInMentions: false,
    autoApprove: false,
    restoring: false,
    lastRestoreError: null,
  });
}

function resetWorkspaceStore() {
  useWorkspaceStore.setState({
    projects: [{
      id: 'project-1',
      path: '/repo',
      name: 'repo',
      openFiles: [],
      activeFileId: null,
      terminalSessions: [],
    }],
    activeProjectId: 'project-1',
    activePanel: 'explorer',
    sidebarVisible: true,
    terminalVisible: true,
    chatVisible: true,
    showPicker: false,
  });
}

describe('AgentPicker', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    resetChatStore();
    resetWorkspaceStore();
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

  it('passes active project path when creating new agent session', async () => {
    createChatSession.mockResolvedValue({ id: 'live-88' });

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

    expect(createChatSession).toHaveBeenCalledWith('opencode', '/repo');
  });
});
