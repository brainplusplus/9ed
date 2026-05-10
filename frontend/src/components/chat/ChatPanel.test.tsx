import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ChatPanel } from './ChatPanel';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

globalThis.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof globalThis.ResizeObserver;

Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
  value: vi.fn(),
  configurable: true,
});

const getChatAgents = vi.fn();
const createChatSession = vi.fn();

vi.mock('../../api', () => ({
  getChatAgents: () => getChatAgents(),
  createChatSession: (...args: unknown[]) => createChatSession(...args),
}));

vi.mock('../../hooks/useChatSession', () => ({
  useChatSession: () => ({
    sendMessage: vi.fn(),
    cancel: vi.fn(),
    setConfigOption: vi.fn(),
    respondPermission: vi.fn(),
    rejectPermission: vi.fn(),
    setAutoApprove: vi.fn(),
    connected: true,
  }),
}));

vi.mock('./ChatMessage', () => ({ ChatMessage: () => null }));
vi.mock('./ChatInput', () => ({ ChatInput: () => null }));
vi.mock('./ChatQueue', () => ({ ChatQueue: () => null }));
vi.mock('./PermissionDialog', () => ({ PermissionDialog: () => null }));
vi.mock('./ChatSessionList', () => ({ ChatSessionList: () => null }));
vi.mock('./AgentPicker', () => ({
  AgentPicker: () => null,
  ConfigBar: () => null,
}));

function resetStores() {
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    agents: [{ id: 'opencode', label: 'OpenCode', available: true, configFound: true, activeModel: '', models: [], providers: [] }],
    selectedAgentId: 'opencode',
    chatVisible: false,
    historySessions: [],
    historyLoaded: false,
    queuedMessages: {},
    includeIgnoredInMentions: false,
    autoApprove: false,
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

describe('ChatPanel new chat', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    resetStores();
    getChatAgents.mockResolvedValue([{ id: 'opencode', label: 'OpenCode', available: true, configFound: true, activeModel: '', models: [], providers: [] }]);
    createChatSession.mockResolvedValue({ id: 'live-99' });
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

  it('passes active project path when creating new chat session', async () => {
    await act(async () => {
      root.render(<ChatPanel />);
    });

    const newChatButton = container.querySelector('.chat-new-btn-icon');
    expect(newChatButton).not.toBeNull();

    await act(async () => {
      newChatButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(createChatSession).toHaveBeenCalledWith('opencode', '/repo');
  });
});
