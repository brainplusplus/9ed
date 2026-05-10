import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useWorkspaceStatePersistence } from './useWorkspaceStatePersistence';
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
    }],
    activeSessionId: 'live-9',
    agents: [],
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
});
