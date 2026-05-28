import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatSession } from './useChatSession';
import { useChatStore } from '../stores/chat';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const getLiveChatSessions = vi.fn();
const createChatWebSocket = vi.fn();
const getConfig = vi.fn();
const getChatSessionState = vi.fn();
const getTerminalHandle = vi.fn();

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api');
  return {
    ...actual,
    getLiveChatSessions: () => getLiveChatSessions(),
    createChatWebSocket: (sessionId: string) => createChatWebSocket(sessionId),
    getConfig: () => getConfig(),
    getChatSessionState: (sessionId: string) => getChatSessionState(sessionId),
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
    getChatSessionState.mockResolvedValue({ session: { status: 'active', updatedAt: 1 }, messages: [], events: [], snapshot: null });
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
      activeTerminalId: 'term-1',
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
    expect(payload.content).toContain('Treat the terminal as completed only when the shell has clearly returned to idle');
    expect(payload.content).toContain('active_terminal_read');
    expect(payload.content).toContain('```text');
    expect(payload.content).not.toContain('INSTRUCTION: You are connected');
    expect(payload.content).not.toContain('Do NOT refuse');
  });

  it('recovers a missed done event from persisted session state before declaring stall', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);
    getChatSessionState.mockResolvedValue({
      session: { id: 'record-1', agentId: 'opencode', title: 'Loop guard', status: 'closed', createdAt: 1, updatedAt: 20 },
      messages: [
        { id: 'user-1', sessionId: 'record-1', role: 'user', content: 'cek cepat', timestamp: 10 },
        { id: 'assistant-1', sessionId: 'record-1', role: 'assistant', content: 'beres', timestamp: 20 },
      ],
      events: [
        { id: 'evt-text', sessionId: 'record-1', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'beres' }), seq: 1, timestamp: 20 },
        { id: 'evt-done', sessionId: 'record-1', kind: 'done', payloadJson: JSON.stringify({ type: 'done', stopReason: 'end_turn' }), seq: 2, timestamp: 21 },
      ],
      snapshot: null,
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
      await latestHook?.sendMessage('cek cepat');
    });

    expect(useChatStore.getState().sessions[0].status).toBe('streaming');

    await act(async () => {
      vi.advanceTimersByTime(15000);
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });

    const session = useChatStore.getState().sessions[0];
    expect(getChatSessionState).toHaveBeenCalledWith('record-1');
    expect(session.status).toBe('idle');
    expect(session.stalled).toBe(false);
    expect(session.messages.some((message) => message.role === 'assistant' && message.content.includes('beres'))).toBe(true);
    expect(session.debugEntries?.some((entry) => entry.message.includes('stall probe cleared after session state refresh'))).toBe(true);
  });

  it('marks the session stalled when refresh still shows no terminal done state', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);
    getChatSessionState.mockResolvedValue({
      session: { id: 'record-1', agentId: 'opencode', title: 'Loop guard', status: 'active', createdAt: 1, updatedAt: 20 },
      messages: [
        { id: 'user-1', sessionId: 'record-1', role: 'user', content: 'masih jalan?', timestamp: 10 },
      ],
      events: [],
      snapshot: null,
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
      await latestHook?.sendMessage('masih jalan?');
    });

    await act(async () => {
      vi.advanceTimersByTime(15000);
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });

    const session = useChatStore.getState().sessions[0];
    expect(session.status).toBe('streaming');
    expect(session.stalled).toBe(true);
    expect(session.debugEntries?.some((entry) => entry.message.includes('stall detected: no new updates after'))).toBe(true);
  });
});
