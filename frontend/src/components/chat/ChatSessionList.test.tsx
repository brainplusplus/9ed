import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ChatSessionList } from './ChatSessionList';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const deleteChatSession = vi.fn();
const loadHistorySessionSpy = vi.fn();

vi.mock('../../api', () => ({
  deleteChatSession: (sessionId: string) => deleteChatSession(sessionId),
}));

function resetStores() {
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
  useChatStore.setState({
    sessions: [{
      id: 'live-1',
      recordId: 'live-1',
      agentId: 'claude',
      title: 'Active',
      messages: [],
      status: 'idle',
      createdAt: Date.now(),
      kind: 'live',
    }],
    activeSessionId: 'live-1',
    agents: [],
    chatVisible: false,
    historySessions: [{
      id: 'record-2',
      agentId: 'opencode',
      title: 'History title',
      workDir: '/repo',
      acpSessionId: 'acp-2',
      createdAt: Date.now() - 1000,
      updatedAt: Date.now(),
    }],
    historyLoaded: true,
    queuedMessages: {},
    includeIgnoredInMentions: false,
    autoApprove: false,
    restoring: false,
    lastRestoreError: null,
  });
}

describe('ChatSessionList', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    resetStores();
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

  it('renders dropdown in overlay root with resumable badge when opened', () => {
    act(() => {
      root.render(<ChatSessionList />);
    });

    const trigger = container.querySelector('.chat-new-btn');
    expect(trigger).not.toBeNull();

    act(() => {
      trigger?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const overlay = document.body.querySelector('.chat-session-dropdown.chat-session-dropdown-overlay');
    expect(overlay).not.toBeNull();
    expect(overlay?.querySelector('.session-resumable-badge')?.textContent).toContain('resumable');
    expect(overlay?.getAttribute('data-overlay')).toBe('true');
  });

  it('shows connecting state in trigger while history session is loading', async () => {
    let releaseLoad: (() => void) | null = null;
    const pendingLoadHistorySession = ((_: string) => new Promise<void>((resolve) => {
      loadHistorySessionSpy();
      releaseLoad = resolve;
    })) as (sessionId: string) => Promise<void>;
    useChatStore.setState({
      loadHistorySession: pendingLoadHistorySession,
    });

    act(() => {
      root.render(<ChatSessionList />);
    });

    const trigger = container.querySelector('.chat-new-btn');
    expect(trigger?.textContent).toContain('Sessions');

    act(() => {
      trigger?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const historyButton = document.body.querySelector('.chat-session-row-btn-history');
    expect(historyButton).not.toBeNull();

    await act(async () => {
      historyButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await Promise.resolve();
    });

    expect(loadHistorySessionSpy).toHaveBeenCalled();
    expect(container.querySelector('.chat-new-btn')?.textContent).toContain('Connecting');

    await act(async () => {
      releaseLoad?.();
      await Promise.resolve();
    });
  });
});
