import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AgentPicker, ConfigBar } from './AgentPicker';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';
import type { ChatSessionInfo } from '../../types';

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

function makeSession(overrides: Partial<ChatSessionInfo> = {}): ChatSessionInfo {
  return {
    id: 'live-1',
    recordId: 'record-1',
    agentId: 'opencode',
    title: 'Browser toggle',
    messages: [],
    status: 'idle',
    createdAt: 1,
    kind: 'live',
    workDir: '/repo',
    acpSessionId: 'acp-1',
    ...overrides,
  };
}

function resetConfigStores({ withTab }: { withTab: boolean }) {
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    agents: [{ id: 'opencode', label: 'OpenCode', available: true, configFound: true, activeModel: '', models: [], providers: [] }],
    selectedAgentId: 'opencode',
    chatVisible: false,
    historySessions: [],
    historyLoaded: false,
    historyWorkDir: null,
    queuedMessages: {},
    includeIgnoredInMentions: false,
    autoApprove: false,
    useActiveBrowser: false,
    browserSelection: null,
    useActiveTerminal: false,
    activeTerminalId: null,
    restoring: false,
    lastRestoreError: null,
  });
  useWorkspaceStore.setState({
    projects: [{
      id: 'project-1',
      path: '/repo',
      name: 'repo',
      openFiles: [],
      activeFileId: null,
      terminalTabs: [],
      activeTerminalTabId: null,
      terminalSessions: [],
      browserTabIds: withTab ? ['tab-1'] : [],
      activeBrowserTabId: withTab ? 'tab-1' : null,
    }],
    activeProjectId: 'project-1',
    activePanel: 'explorer',
    sidebarVisible: true,
    terminalVisible: true,
    chatVisible: true,
    browserVisible: false,
    showPicker: false,
  });
}

describe('ConfigBar browser toggle', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
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

  it('checkbox reflects session.useActiveBrowser (not just global flag)', () => {
    resetConfigStores({ withTab: true });
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-1', useActiveBrowser: true })],
      activeSessionId: 'live-1',
      // Global flag disagrees with the session flag: the checkbox must
      // follow the session (the primary source) per VAL-SOFTTOGGLE-001.
      useActiveBrowser: false,
    });

    act(() => {
      root.render(<ConfigBar connected />);
    });

    const checkbox = container.querySelector<HTMLInputElement>('input[type="checkbox"][title*="browser" i], .chat-config-toggle-mcp input[type="checkbox"]');
    expect(checkbox).not.toBeNull();
    expect(checkbox?.checked).toBe(true);
  });

  it('clicking the checkbox updates useActiveBrowser instantly with no connecting status', async () => {
    resetConfigStores({ withTab: true });
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-1', useActiveBrowser: false })],
      activeSessionId: 'live-1',
      useActiveBrowser: false,
    });

    act(() => {
      root.render(<ConfigBar connected />);
    });

    const label = container.querySelector<HTMLLabelElement>('.chat-config-toggle-mcp');
    const checkbox = label?.querySelector<HTMLInputElement>('input[type="checkbox"]');
    expect(checkbox?.checked).toBe(false);

    await act(async () => {
      checkbox?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Instant: store + session flag flipped synchronously.
    expect(useChatStore.getState().useActiveBrowser).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-1');
    expect(session?.useActiveBrowser).toBe(true);
    // VAL-SOFTTOGGLE-001: no connecting status introduced by the toggle.
    expect(session?.status).toBe('idle');
  });
});
