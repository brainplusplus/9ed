import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatSession } from './useChatSession';
import { useChatStore } from '../stores/chat';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const getLiveChatSessions = vi.fn();
const createChatWebSocket = vi.fn();
const getConfig = vi.fn();
const getTerminalHandle = vi.fn();

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api');
  return {
    ...actual,
    getLiveChatSessions: () => getLiveChatSessions(),
    createChatWebSocket: (sessionId: string) => createChatWebSocket(sessionId),
    getConfig: () => getConfig(),
  };
});

vi.mock('../terminalRegistry', () => ({
  getTerminalHandle: (id: string) => getTerminalHandle(id),
}));

let latestHook: ReturnType<typeof useChatSession> | null = null;

function Harness() {
  latestHook = useChatSession();
  return null;
}

function createMockSocket() {
  return {
    readyState: WebSocket.CONNECTING as number,
    send: vi.fn(),
    close: vi.fn(),
    onopen: null as ((event: Event) => void) | null,
    onmessage: null as ((event: MessageEvent<string>) => void) | null,
    onclose: null as ((event: CloseEvent) => void) | null,
    onerror: null as ((event: Event) => void) | null,
  };
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
    useActiveBrowser: false,
    browserSelection: null,
    useActiveTerminal: false,
    activeTerminalId: null,
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
    latestHook = null;
    resetChatStore();
    getConfig.mockResolvedValue({ terminalAiMaxLines: 25 });
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

  it('sends terminal snapshot as neutral context without injection instructions', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);
    getTerminalHandle.mockReturnValue({
      cwd: 'D:\\repo',
      shellType: 'pwsh',
      getScrollback: vi.fn(() => 'PS D:\\repo> dir\nfile.txt'),
    });

    useChatStore.setState({
      useActiveTerminal: true,
      activeTerminalId: 'term-1',
    });

    await act(async () => {
      root.render(<Harness />);
      await Promise.resolve();
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
      await Promise.resolve();
      await Promise.resolve();
    });

    await act(async () => {
      socket.readyState = WebSocket.OPEN;
      socket.onopen?.(new Event('open'));
      await Promise.resolve();
    });

    await act(async () => {
      await latestHook?.sendMessage('tolong cek error');
    });

    expect(socket.send).toHaveBeenCalledWith(JSON.stringify({
      type: 'set_use_active_terminal',
      useActiveTerminal: true,
    }));
    const sentMessages = socket.send.mock.calls
      .map(([payload]) => JSON.parse(String(payload)) as { type: string; content?: string });
    const payload = sentMessages.find((message) => message.type === 'message') as { content: string };
    expect(payload).toBeTruthy();
    expect(payload.content).toContain('[Active terminal integration]');
    expect(payload.content).toContain('Status: enabled');
    expect(payload.content).toContain('Session ID: term-1');
    expect(payload.content).toContain('CWD: D:\\repo');
    expect(payload.content).toContain('Shell: PowerShell');
    expect(payload.content).toContain('Command dialect: PowerShell syntax');
    expect(payload.content).toContain('Use MCP tool active_terminal_run for terminal work');
    expect(payload.content).toContain('do not repeat the same command');
    expect(payload.content).toContain('```text');
    expect(payload.content).not.toContain('INSTRUCTION: You are connected');
    expect(payload.content).not.toContain('Do NOT refuse');
  });
});
