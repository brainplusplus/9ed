import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatSession } from './useChatSession';
import { useChatStore } from '../stores/chat';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const getLiveChatSessions = vi.fn();
const createChatWebSocket = vi.fn();

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api');
  return {
    ...actual,
    getLiveChatSessions: () => getLiveChatSessions(),
    createChatWebSocket: (sessionId: string) => createChatWebSocket(sessionId),
  };
});

function Harness() {
  useChatSession();
  return null;
}

function resetChatStore() {
  useChatStore.setState({
    sessions: [{
      id: 'live-1',
      recordId: 'record-1',
      agentId: 'opencode',
      title: 'Loop guard',
      messages: [],
      status: 'idle',
      createdAt: 1,
      kind: 'live',
      workDir: '/repo',
      acpSessionId: 'acp-1',
    }],
    activeSessionId: 'live-1',
    agents: [],
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

describe('useChatSession', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.useFakeTimers();
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
    vi.useRealTimers();
  });

  it('does not restart live-session preflight while the first preflight is still pending', async () => {
    getLiveChatSessions.mockReturnValue(new Promise(() => {}));

    await act(async () => {
      root.render(<Harness />);
      await Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => {
      vi.advanceTimersByTime(250);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(useChatStore.getState().sessions[0].status).toBe('connecting');
    expect(getLiveChatSessions).toHaveBeenCalledTimes(1);

    await act(async () => {
      useChatStore.getState().setSessionStatus('live-1', 'connecting');
      await Promise.resolve();
    });

    expect(getLiveChatSessions).toHaveBeenCalledTimes(1);
    expect(createChatWebSocket).not.toHaveBeenCalled();
  });
});
