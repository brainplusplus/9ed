import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatSession } from './useChatSession';
import { useChatStore } from '../stores/chat';
import { useWorkspaceStore } from '../stores/workspace';

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
  // Ensure no browser tab is associated with the /repo project so the
  // browser WS effect treats browserTabId as null by default.
  useWorkspaceStore.setState({
    projects: [],
    activeProjectId: null,
    activePanel: 'explorer',
    sidebarVisible: true,
    terminalVisible: true,
    chatVisible: true,
    browserVisible: false,
    showPicker: false,
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

  // --- ADR-0002 / VAL-CATCHUP-005: epoch change re-fetch ---

  it('deletes cursor and re-fetches timeline tail on session_resumed event', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

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

    // Simulate a session_resumed event
    await act(async () => {
      socket.onmessage?.({ data: JSON.stringify({ type: 'session_resumed', epoch: 'new-epoch-123' }) } as MessageEvent<string>);
      await Promise.resolve();
    });

    // The client should send a fetch_timeline with afterSeq:0 to re-fetch tail
    const fetchCalls = socket.send.mock.calls
      .map(([payload]) => JSON.parse(String(payload)) as { type: string; afterSeq?: number })
      .filter((msg) => msg.type === 'fetch_timeline');

    expect(fetchCalls.length).toBeGreaterThanOrEqual(1);
    expect(fetchCalls.some((msg) => msg.afterSeq === 0)).toBe(true);

    // Debug log should mention the session resumed
    const session = useChatStore.getState().sessions[0];
    expect(session.debugEntries?.some((entry) => entry.message.includes('agent session resumed after crash'))).toBe(true);
  });

  // --- ADR-0002 / VAL-CATCHUP-006: replay_meta initializes cursor ---

  it('initializes cursor from replay_meta envelope', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

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

    // Simulate replay_meta envelope from replay-on-subscribe
    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({
          type: 'replay_meta',
          epoch: 'epoch-abc',
          window: { minSeq: 1, maxSeq: 10, nextSeq: 11 },
        }),
      } as MessageEvent<string>);
      await Promise.resolve();
    });

    const session = useChatStore.getState().sessions[0];
    expect(session.debugEntries?.some((entry) => entry.message.includes('replay_meta: cursor initialized'))).toBe(true);
    expect(session.debugEntries?.some((entry) => entry.message.includes('seq=10'))).toBe(true);
    expect(session.debugEntries?.some((entry) => entry.message.includes('epoch=epoch-ab'))).toBe(true);
  });

  // --- ADR-0002: staleCursor flag triggers reset + re-fetch ---

  it('resets cursor and re-fetches tail when timeline response has staleCursor:true', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

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

    // Clear any initial sends (hello, etc.)
    socket.send.mockClear();

    // Simulate a timeline response with staleCursor:true
    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({
          type: 'timeline',
          epoch: 'new-epoch',
          reset: true,
          staleCursor: true,
          gap: false,
          window: { minSeq: 1, maxSeq: 5, nextSeq: 6 },
          hasOlder: false,
          hasNewer: true,
          endCursor: 0,
          events: [],
        }),
      } as MessageEvent<string>);
      await Promise.resolve();
    });

    const fetchCalls = socket.send.mock.calls
      .map(([payload]) => JSON.parse(String(payload)) as { type: string; afterSeq?: number })
      .filter((msg) => msg.type === 'fetch_timeline');

    expect(fetchCalls.length).toBeGreaterThanOrEqual(1);
    expect(fetchCalls.some((msg) => msg.afterSeq === 0)).toBe(true);

    const session = useChatStore.getState().sessions[0];
    expect(session.debugEntries?.some((entry) => entry.message.includes('timeline reset') && entry.message.includes('staleCursor'))).toBe(true);
  });

  // --- ADR-0002: gap flag triggers reset + re-fetch ---

  it('resets cursor and re-fetches tail when timeline response has gap:true', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

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

    socket.send.mockClear();

    // Simulate a timeline response with gap:true
    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({
          type: 'timeline',
          epoch: 'epoch-1',
          reset: true,
          staleCursor: false,
          gap: true,
          window: { minSeq: 5, maxSeq: 10, nextSeq: 11 },
          hasOlder: false,
          hasNewer: false,
          endCursor: 0,
          events: [],
        }),
      } as MessageEvent<string>);
      await Promise.resolve();
    });

    const fetchCalls = socket.send.mock.calls
      .map(([payload]) => JSON.parse(String(payload)) as { type: string; afterSeq?: number })
      .filter((msg) => msg.type === 'fetch_timeline');

    expect(fetchCalls.length).toBeGreaterThanOrEqual(1);
    expect(fetchCalls.some((msg) => msg.afterSeq === 0)).toBe(true);

    const session = useChatStore.getState().sessions[0];
    expect(session.debugEntries?.some((entry) => entry.message.includes('timeline reset') && entry.message.includes('gap'))).toBe(true);
  });

  // --- ADR-0002: timeline response with events updates cursor ---

  it('replays timeline events and updates cursor', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

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

    socket.send.mockClear();

    // Simulate a timeline response with events
    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({
          type: 'timeline',
          epoch: 'epoch-1',
          reset: false,
          staleCursor: false,
          gap: false,
          window: { minSeq: 1, maxSeq: 3, nextSeq: 4 },
          hasOlder: false,
          hasNewer: false,
          endCursor: 3,
          events: [
            { type: 'timeline_event', seq: 2, epoch: 'epoch-1', event: { type: 'text', text: 'hello' } },
            { type: 'timeline_event', seq: 3, epoch: 'epoch-1', event: { type: 'done', stopReason: 'end_turn' } },
          ],
        }),
      } as MessageEvent<string>);
      await Promise.resolve();
    });

    const session = useChatStore.getState().sessions[0];
    // Events should have been replayed into the store
    expect(session.messages.length).toBeGreaterThanOrEqual(1);

    // No re-fetch should have been triggered (no reset, no hasNewer)
    const fetchCalls = socket.send.mock.calls
      .map(([payload]) => JSON.parse(String(payload)) as { type: string })
      .filter((msg) => msg.type === 'fetch_timeline');
    expect(fetchCalls.length).toBe(0);
  });

  // --- ADR-0002: timeline response with hasNewer triggers pagination ---

  it('fetches next page when timeline response has hasNewer:true', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

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

    socket.send.mockClear();

    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({
          type: 'timeline',
          epoch: 'epoch-1',
          reset: false,
          staleCursor: false,
          gap: false,
          window: { minSeq: 1, maxSeq: 10, nextSeq: 11 },
          hasOlder: false,
          hasNewer: true,
          endCursor: 5,
          events: [
            { type: 'timeline_event', seq: 5, epoch: 'epoch-1', event: { type: 'text', text: 'page1' } },
          ],
        }),
      } as MessageEvent<string>);
      await Promise.resolve();
    });

    const fetchCalls = socket.send.mock.calls
      .map(([payload]) => JSON.parse(String(payload)) as { type: string; afterSeq?: number; epoch?: string })
      .filter((msg) => msg.type === 'fetch_timeline');

    expect(fetchCalls.length).toBe(1);
    expect(fetchCalls[0].afterSeq).toBe(5);
    expect(fetchCalls[0].epoch).toBe('epoch-1');
  });

  // --- ADR-0003: client_backpressure triggers seq-gap re-fetch ---

  it('re-fetches timeline on client_backpressure done event for seq-gap recovery', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

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

    // First set a cursor by receiving a normal event
    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({ type: 'text', text: 'some text', seq: 5, epoch: 'epoch-1' }),
      } as MessageEvent<string>);
      await Promise.resolve();
    });

    socket.send.mockClear();

    // Now receive a client_backpressure done event
    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({ type: 'done', stopReason: 'client_backpressure', seq: 6, epoch: 'epoch-1' }),
      } as MessageEvent<string>);
      await Promise.resolve();
    });

    const fetchCalls = socket.send.mock.calls
      .map(([payload]) => JSON.parse(String(payload)) as { type: string; afterSeq?: number; epoch?: string })
      .filter((msg) => msg.type === 'fetch_timeline');

    expect(fetchCalls.length).toBeGreaterThanOrEqual(1);
    expect(fetchCalls.some((msg) => msg.afterSeq === 5 || msg.afterSeq === 6)).toBe(true);

    const session = useChatStore.getState().sessions[0];
    expect(session.debugEntries?.some((entry) => entry.message.includes('client backpressure') && entry.message.includes('re-fetching timeline'))).toBe(true);
  });

  // --- Browser tool recovery (soft-toggle, not hard restart) ---

  it('soft-toggles (re-sends set_use_active_browser WS) on an invalid browser tool event instead of hard-restarting', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

    useChatStore.setState({
      sessions: [{
        id: 'live-1',
        recordId: 'record-1',
        agentId: 'opencode',
        title: 'Browser recovery',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-1',
        useActiveBrowser: true,
      }],
      activeSessionId: 'live-1',
      useActiveBrowser: true,
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

    // Snapshot the send calls emitted by the WS-open handshake (e.g. the
    // set_use_active_terminal / set_use_active_browser WS effects) so we can
    // isolate the recovery message sent in response to the invalid-tool event.
    const preRecoverySendCount = socket.send.mock.calls.length;

    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({
          type: 'tool_call',
          toolTitle: 'browser_navigate',
          toolKind: 'unknown tool: browser_navigate',
          toolStatus: 'error',
          error: 'tool not found',
          seq: 1,
        }),
      } as MessageEvent<string>);
      await Promise.resolve();
      await Promise.resolve();
    });

    // Recovery must re-send the soft `set_use_active_browser` WS message —
    // NOT call restartActiveSessionForBrowser (which was removed).
    const recoveryMessages = socket.send.mock.calls
      .slice(preRecoverySendCount)
      .map(([payload]) => JSON.parse(String(payload)) as { type: string; useActiveBrowser?: boolean })
      .filter((msg) => msg.type === 'set_use_active_browser');
    expect(recoveryMessages.length).toBeGreaterThanOrEqual(1);
    expect(recoveryMessages.some((msg) => msg.useActiveBrowser === true || msg.useActiveBrowser === false)).toBe(true);

    // The removed hard-restart method must not exist on the store.
    expect((useChatStore.getState() as unknown as Record<string, unknown>).restartActiveSessionForBrowser).toBeUndefined();

    // Debug entry should record the soft recovery.
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-1');
    expect(session?.debugEntries?.some((entry) => entry.message.includes('soft recovery'))).toBe(true);
  });

  it('does not soft-recover when the session that emitted the invalid browser tool event is no longer active', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

    useChatStore.setState({
      sessions: [{
        id: 'live-1',
        recordId: 'record-1',
        agentId: 'opencode',
        title: 'Stale browser recovery',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-1',
        useActiveBrowser: true,
      }],
      activeSessionId: 'live-1',
      useActiveBrowser: true,
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

    // The user switched to a different session before the invalid-tool event
    // arrived, so re-enabling browser MCP would target the wrong session.
    useChatStore.setState({ activeSessionId: 'other-session' });

    // Snapshot the send count AFTER the open-handshake control messages so we
    // only count recovery messages emitted by the invalid-tool event.
    const preRecoverySendCount = socket.send.mock.calls.length;

    await act(async () => {
      socket.onmessage?.({
        data: JSON.stringify({
          type: 'tool_call',
          toolTitle: 'browser_navigate',
          toolKind: 'unknown tool: browser_navigate',
          toolStatus: 'error',
          error: 'tool not found',
          seq: 1,
        }),
      } as MessageEvent<string>);
      await Promise.resolve();
      await Promise.resolve();
    });

    // No soft-recovery `set_use_active_browser` message should be sent for a
    // session that is no longer the active one.
    const recoveryMessages = socket.send.mock.calls
      .slice(preRecoverySendCount)
      .map(([payload]) => JSON.parse(String(payload)) as { type: string })
      .filter((msg) => msg.type === 'set_use_active_browser');
    expect(recoveryMessages).toHaveLength(0);

    // The removed hard-restart method must not exist on the store.
    expect((useChatStore.getState() as unknown as Record<string, unknown>).restartActiveSessionForBrowser).toBeUndefined();
  });

  // --- ADR-0006: exponential reconnect backoff ---

  it('uses exponential backoff delays for reconnection', async () => {
    const socket1 = createMockSocket();
    const socket2 = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValueOnce(socket1).mockReturnValueOnce(socket2);

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
      socket1.readyState = WebSocket.OPEN;
      socket1.onopen?.(new Event('open'));
      await Promise.resolve();
    });

    // Close the socket to trigger reconnect
    await act(async () => {
      socket1.readyState = WebSocket.CLOSED;
      socket1.onclose?.(new CloseEvent('close'));
      await Promise.resolve();
    });

    const session = useChatStore.getState().sessions[0];
    expect(session.debugEntries?.some((entry) => entry.message.includes('socket closed; scheduling reconnect'))).toBe(true);

    // First reconnect delay should be 150ms (exponential backoff base)
    await act(async () => {
      vi.advanceTimersByTime(150);
      await Promise.resolve();
      await Promise.resolve();
    });

    // socket2 should have been created for the reconnect attempt
    expect(createChatWebSocket).toHaveBeenCalledTimes(2);
  });

  // --- VAL-SOFTTOGGLE-002: WS effect warns when browser toggle is on but no tab is open ---

  it('emits a warn debug entry when useActiveBrowser is on but no browser tab is open', async () => {
    const socket = createMockSocket();
    getLiveChatSessions.mockResolvedValue([{ id: 'live-1' }]);
    createChatWebSocket.mockReturnValue(socket);

    // Session has the browser toggle ON, but no project/tab exists for /repo
    // (resetChatStore leaves useWorkspaceStore with empty projects).
    useChatStore.setState({
      sessions: [{
        id: 'live-1',
        recordId: 'record-1',
        agentId: 'opencode',
        title: 'No tab',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-1',
        useActiveBrowser: true,
      }],
      activeSessionId: 'live-1',
      useActiveBrowser: true,
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

    // The WS effect sends useActiveBrowser:false (no tab) and surfaces a warn.
    expect(socket.send).toHaveBeenCalledWith(JSON.stringify({
      type: 'set_use_active_browser',
      useActiveBrowser: false,
      activeBrowserTabId: null,
    }));
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-1');
    const warn = session?.debugEntries?.find(
      (entry) => entry.level === 'warn' && entry.source === 'client' && entry.message.includes('no browser tab is open'),
    );
    expect(warn).toBeTruthy();
  });
});
