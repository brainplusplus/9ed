import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { restoreWorkspaceState, useWorkspaceStatePersistence } from './useWorkspaceStatePersistence';
import { useWorkspaceStore } from '../stores/workspace';
import { useChatStore } from '../stores/chat';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const saveWorkspaceState = vi.fn();
const getWorkspaceState = vi.fn();
const getFileContent = vi.fn();

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api');
  return {
    ...actual,
    saveWorkspaceState: (...args: unknown[]) => saveWorkspaceState(...args),
    getWorkspaceState: (...args: unknown[]) => getWorkspaceState(...args),
    getFileContent: (...args: unknown[]) => getFileContent(...args),
  };
});

function Harness() {
  useWorkspaceStatePersistence();
  return null;
}

function resetStores() {
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
      id: 'live-9',
      recordId: 'record-1',
      agentId: 'claude',
      title: 'Resume me',
      messages: [],
      status: 'idle',
      createdAt: 1,
      kind: 'resumable',
      workDir: '/repo',
    }],
    activeSessionId: 'live-9',
    agents: [],
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

describe('useWorkspaceStatePersistence', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.useFakeTimers();
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
    vi.useRealTimers();
  });

  it('saves persisted record id instead of live runtime id', async () => {
    act(() => {
      root.render(<Harness />);
    });

    act(() => {
      useWorkspaceStore.getState().setActivePanel('git');
    });

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(saveWorkspaceState).toHaveBeenCalledTimes(1);
    expect(saveWorkspaceState).toHaveBeenCalledWith('/repo', expect.objectContaining({
      lastChatSessionId: 'record-1',
    }));
  });

  it('persists latest chat session when only active chat session changes', async () => {
    act(() => {
      root.render(<Harness />);
    });

    act(() => {
      useChatStore.setState({
        sessions: [{
          id: 'live-20',
          recordId: 'record-20',
          agentId: 'opencode',
          title: 'Latest',
          messages: [],
          status: 'idle',
          createdAt: 2,
          kind: 'live',
          workDir: '/repo',
        }],
        activeSessionId: 'live-20',
      });
    });

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(saveWorkspaceState).toHaveBeenCalledWith('/repo', expect.objectContaining({
      lastChatSessionId: 'record-20',
    }));
  });

  it('does not overwrite last chat session with undefined during bootstrap restore', async () => {
    getWorkspaceState.mockResolvedValue({
      openFiles: [],
      activeFilePath: null,
      sidebarPanel: 'explorer',
      chatVisible: true,
      lastChatSessionId: 'record-bootstrap',
    });
    const loadHistory = vi.fn().mockImplementation(async () => {
      act(() => {
        useWorkspaceStore.getState().setActivePanel('git');
      });
    });
    const restoreSessionForProject = vi.fn().mockResolvedValue(undefined);
    useChatStore.setState({
      sessions: [],
      activeSessionId: null,
      historyLoaded: false,
      historyWorkDir: null,
      loadHistory,
      restoreSessionForProject,
    });

    await act(async () => {
      root.render(<Harness />);
      await restoreWorkspaceState('/repo', 'project-1');
      vi.advanceTimersByTime(1000);
    });

    expect(restoreSessionForProject).toHaveBeenCalledWith('/repo', 'record-bootstrap');
    expect(saveWorkspaceState).not.toHaveBeenCalledWith('/repo', expect.objectContaining({
      lastChatSessionId: undefined,
    }));
  });

  it('restores latest project chat even when workspace state is missing', async () => {
    const loadHistory = vi.fn().mockResolvedValue(undefined);
    const restoreSessionForProject = vi.fn().mockResolvedValue(undefined);
    useChatStore.setState({
      historyLoaded: false,
      historyWorkDir: null,
      loadHistory,
      restoreSessionForProject,
    });
    getWorkspaceState.mockResolvedValue(null);

    await restoreWorkspaceState('/repo', 'project-1');

    expect(loadHistory).toHaveBeenCalledWith('/repo');
    expect(restoreSessionForProject).toHaveBeenCalledWith('/repo', undefined);
  });

  it('falls back to latest project chat when saved workspace has no last chat id', async () => {
    const loadHistory = vi.fn().mockResolvedValue(undefined);
    const restoreSessionForProject = vi.fn().mockResolvedValue(undefined);
    useChatStore.setState({
      historyLoaded: false,
      historyWorkDir: null,
      loadHistory,
      restoreSessionForProject,
    });
    getWorkspaceState.mockResolvedValue({
      openFiles: [],
      activeFilePath: null,
      sidebarPanel: 'explorer',
      chatVisible: true,
    });

    await restoreWorkspaceState('/repo', 'project-1');

    expect(loadHistory).toHaveBeenCalledWith('/repo');
    expect(restoreSessionForProject).toHaveBeenCalledWith('/repo', undefined);
  });
});
