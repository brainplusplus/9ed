import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatStore } from './chat';
import { useWorkspaceStore } from './workspace';
import { registerTerminal, unregisterTerminal } from '../terminalRegistry';
import type { ChatSessionInfo, HistoryMessageRecord, HistorySessionRecord } from '../types';
import { replayTranscriptToMessages, parseSnapshotJson } from './chat';

const getChatHistory = vi.fn<() => Promise<HistorySessionRecord[]>>();
const getChatSessionMessages = vi.fn<(sessionId: string) => Promise<HistoryMessageRecord[]>>();
const getChatSessionState = vi.fn();
const saveChatMessage = vi.fn();
const deleteChatHistory = vi.fn();
const getRestorableChatSession = vi.fn();
const resumeChatSession = vi.fn();
const saveRecentProject = vi.fn();

vi.mock('../api', () => ({
  getChatHistory: () => getChatHistory(),
  getChatSessionMessages: (sessionId: string) => getChatSessionMessages(sessionId),
  getChatSessionState: (...args: unknown[]) => getChatSessionState(...args),
  saveChatMessage: (...args: unknown[]) => saveChatMessage(...args),
  deleteChatHistory: (sessionId: string) => deleteChatHistory(sessionId),
  getRestorableChatSession: (...args: unknown[]) => getRestorableChatSession(...args),
  resumeChatSession: (...args: unknown[]) => resumeChatSession(...args),
  saveRecentProject: (...args: unknown[]) => saveRecentProject(...args),
}));

function resetChatStore() {
  window.sessionStorage.clear();
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    agents: [],
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

describe('useChatStore restore/session identity', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    getChatHistory.mockResolvedValue([]);
    getChatSessionMessages.mockResolvedValue([]);
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });
    saveChatMessage.mockResolvedValue(undefined);
    deleteChatHistory.mockResolvedValue(undefined);
  });

  it('persists user history against record id for resumed sessions', () => {
    useChatStore.setState({
      sessions: [{
        id: 'live-50',
        recordId: 'record-50',
        agentId: 'claude',
        title: 'History persistence',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-50',
      }],
      activeSessionId: 'live-50',
    });

    useChatStore.getState().addMessage('live-50', {
      id: 'user-1',
      role: 'user',
      content: 'buat file .env.local.example',
      timestamp: 10,
    });

    expect(saveChatMessage).toHaveBeenCalledWith(expect.objectContaining({
      sessionId: 'record-50',
      role: 'user',
      content: 'buat file .env.local.example',
      workDir: '/repo',
      acpSessionId: 'acp-50',
    }));
    expect(saveChatMessage).toHaveBeenCalledTimes(1);
  });

  it('restores cached agents from session storage', async () => {
    window.sessionStorage.setItem('9ed.chatAgents.v1', JSON.stringify({
      agents: [{ id: 'opencode', label: 'OpenCode', available: true, configFound: true, activeModel: '', models: [], providers: [] }],
      selectedAgentId: 'opencode',
    }));

    vi.resetModules();
    const reloaded = await import('./chat');
    const state = reloaded.useChatStore.getState();

    expect(state.agents).toHaveLength(1);
    expect(state.agents[0].id).toBe('opencode');
    expect(state.selectedAgentId).toBe('opencode');
  });

  it('preserves persisted record identity when restoring project session', async () => {
    getRestorableChatSession.mockResolvedValue({
      found: true,
      sessionId: 'record-1',
      agentId: 'claude',
      workDir: '/repo',
      acpSessionId: 'acp-1',
      title: 'Resume me',
      isLive: false,
      canResume: true,
    });
    resumeChatSession.mockResolvedValue({ id: 'live-9', mode: 'acp' });

    await useChatStore.getState().restoreSessionForProject('/repo', 'record-1');

    const active = useChatStore.getState().sessions[0];
    expect(active.id).toBe('live-9');
    expect(active.recordId).toBe('record-1');
    expect(active.agentId).toBe('claude');
    expect(useChatStore.getState().activeSessionId).toBe('live-9');
  });

  it('resumes chosen history session with exact persisted agent metadata', async () => {
    useChatStore.setState({
      historyLoaded: true,
      historyWorkDir: '/repo',
      historySessions: [{
        id: 'record-2',
        agentId: 'opencode',
        title: '',
        workDir: '/repo',
        acpSessionId: 'acp-2',
        createdAt: 10,
        updatedAt: 20,
      }],
    });
    getChatSessionState.mockResolvedValue({
      session: { id: 'record-2', agentId: 'opencode', title: '', workDir: '/repo', acpSessionId: 'acp-2', createdAt: 10, updatedAt: 20 },
      messages: [{ id: 'user-2', sessionId: 'record-2', role: 'user', content: 'create file .env.local.example', timestamp: 11 }],
      events: [{ id: 'evt-1', sessionId: 'record-2', kind: 'tool_call', payloadJson: JSON.stringify({ type: 'tool_call', toolCallId: 'tc1', toolTitle: '.env.local.example', toolKind: 'edit', toolStatus: 'completed' }), seq: 1, timestamp: 12 }],
      snapshot: { sessionId: 'record-2', commandsJson: JSON.stringify([{ name: 'help', description: 'Show commands' }]), configOptsJson: JSON.stringify([{ id: 'model', name: 'Model', type: 'string', currentValue: 'gpt-5', options: [{ value: 'gpt-5', name: 'GPT-5' }] }]), updatedAt: 20 },
    });
    resumeChatSession.mockResolvedValue({ id: 'live-22', mode: 'acp' });

    await useChatStore.getState().loadHistorySession('record-2');

    const active = useChatStore.getState().sessions[0];
    expect(active.id).toBe('live-22');
    expect(active.recordId).toBe('record-2');
    expect(active.agentId).toBe('opencode');
    expect(active.acpSessionId).toBe('acp-2');
    expect(active.title).toBe('OpenCode');
    expect(active.commands?.[0]?.name).toBe('help');
    expect(active.configOptions?.[0]?.id).toBe('model');
    expect(active.messages.some((message) => message.role === 'tool_call')).toBe(true);
    expect(useChatStore.getState().activeSessionId).toBe('live-22');
  });

  it('restores persisted user prompts alongside ACP response events', async () => {
    useChatStore.setState({
      historyLoaded: true,
      historyWorkDir: '/repo',
      historySessions: [{
        id: 'record-5',
        agentId: 'opencode',
        title: 'Greeting',
        workDir: '/repo',
        acpSessionId: 'acp-5',
        createdAt: 10,
        updatedAt: 20,
      }],
    });
    getChatSessionState.mockResolvedValue({
      session: { id: 'record-5', agentId: 'opencode', title: 'Greeting', workDir: '/repo', acpSessionId: 'acp-5', createdAt: 10, updatedAt: 20 },
      messages: [
        { id: 'user-5', sessionId: 'record-5', role: 'user', content: 'hi', timestamp: 11 },
        { id: 'assistant-5', sessionId: 'record-5', role: 'assistant', content: 'ya', timestamp: 13 },
      ],
      events: [{ id: 'evt-5', sessionId: 'record-5', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'ya' }), seq: 1, timestamp: 13 }],
      snapshot: null,
    });
    resumeChatSession.mockResolvedValue({ id: 'live-55', mode: 'acp' });

    await useChatStore.getState().loadHistorySession('record-5');

    const active = useChatStore.getState().sessions[0];
    expect(active.messages.map((message) => `${message.role}:${message.content}`)).toEqual(['user:hi', 'assistant:ya']);
  });

  it('keeps persisted assistant reply when transcript has non-text events only', async () => {
    useChatStore.setState({
      historyLoaded: true,
      historyWorkDir: '/repo',
      historySessions: [{
        id: 'record-non-text',
        agentId: 'opencode',
        title: 'Non text',
        workDir: '/repo',
        acpSessionId: 'acp-non-text',
        createdAt: 10,
        updatedAt: 20,
      }],
    });
    getChatSessionState.mockResolvedValue({
      session: { id: 'record-non-text', agentId: 'opencode', title: 'Non text', workDir: '/repo', acpSessionId: 'acp-non-text', createdAt: 10, updatedAt: 20 },
      messages: [
        { id: 'user-nt', sessionId: 'record-non-text', role: 'user', content: 'hmm', timestamp: 11 },
        { id: 'assistant-nt', sessionId: 'record-non-text', role: 'assistant', content: 'ya', timestamp: 13 },
      ],
      events: [{ id: 'evt-usage', sessionId: 'record-non-text', kind: 'usage_update', payloadJson: JSON.stringify({ type: 'usage_update', contextWindow: 200000, contextUsed: 10 }), seq: 1, timestamp: 12 }],
      snapshot: null,
    });
    resumeChatSession.mockResolvedValue({ id: 'live-non-text', mode: 'acp' });

    await useChatStore.getState().loadHistorySession('record-non-text');

    const active = useChatStore.getState().sessions[0];
    expect(active.messages.map((message) => `${message.role}:${message.content}`)).toEqual(['user:hmm', 'assistant:ya']);
  });

  it('marks restored history session as connecting before websocket upgrade finishes', async () => {
    let releaseResume: ((value: { id: string; mode: string }) => void) | undefined;
    resumeChatSession.mockImplementation(() => new Promise((resolve) => {
      releaseResume = resolve as (value: { id: string; mode: string }) => void;
    }));

    useChatStore.setState({
      historyLoaded: true,
      historyWorkDir: '/repo',
      historySessions: [{
        id: 'record-4',
        agentId: 'claude',
        title: 'Resume loading',
        workDir: '/repo',
        acpSessionId: 'acp-4',
        createdAt: 1,
        updatedAt: 2,
      }],
    });

    const pendingLoad = useChatStore.getState().loadHistorySession('record-4');
    await Promise.resolve();

    const connecting = useChatStore.getState().sessions[0];
    expect(connecting.status).toBe('connecting');
    expect(connecting.kind).toBe('archived');
    expect(connecting.recordId).toBe('record-4');

    expect(releaseResume).toBeDefined();
    releaseResume!({ id: 'live-44', mode: 'acp' });
    await pendingLoad;

    const resumed = useChatStore.getState().sessions[0];
    expect(resumed.id).toBe('live-44');
    expect(resumed.kind).toBe('resumable');
  });

  it('does not create duplicate active entries when same record resumes again', async () => {
    getRestorableChatSession.mockResolvedValue({
      found: true,
      sessionId: 'record-3',
      agentId: 'claude',
      workDir: '/repo',
      acpSessionId: 'acp-3',
      title: 'Dup test',
      isLive: false,
      canResume: true,
    });
    resumeChatSession.mockResolvedValueOnce({ id: 'live-31', mode: 'acp' });

    await useChatStore.getState().restoreSessionForProject('/repo', 'record-3');
    await useChatStore.getState().restoreSessionForProject('/repo', 'record-3');

    const state = useChatStore.getState();
    expect(state.sessions).toHaveLength(1);
    expect(state.sessions[0].recordId).toBe('record-3');
    expect(state.sessions[0].id).toBe('live-31');
    expect(state.activeSessionId).toBe('live-31');
    expect(resumeChatSession).toHaveBeenCalledTimes(1);
  });

  it('creates replacement live session when archived record has no ACP session id', async () => {
    useChatStore.setState({
      sessions: [{
        id: 'record-missing-acp',
        recordId: 'record-missing-acp',
        agentId: 'opencode',
        title: 'Missing ACP',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'archived',
        workDir: '/repo',
      }],
      activeSessionId: 'record-missing-acp',
    });
    resumeChatSession.mockResolvedValue({
      id: 'live-created',
      mode: 'acp',
      acpSessionId: 'fresh-acp',
      workDir: '/repo',
    });

    const ok = await useChatStore.getState().resumeSession('record-missing-acp');

    expect(ok).toBe(true);
    expect(resumeChatSession).toHaveBeenCalledWith('record-missing-acp', 'opencode', '/repo', undefined, false, undefined, false, undefined);
    const active = useChatStore.getState().sessions[0];
    expect(active.id).toBe('live-created');
    expect(active.recordId).toBe('record-missing-acp');
    expect(active.acpSessionId).toBe('fresh-acp');
    expect(active.kind).toBe('resumable');
  });

  it('deduplicates concurrent resume requests for the same record', async () => {
    let releaseResume: ((value: { id: string; mode: string; acpSessionId: string; workDir: string }) => void) | undefined;
    resumeChatSession.mockImplementation(() => new Promise((resolve) => {
      releaseResume = resolve as typeof releaseResume;
    }));
    useChatStore.setState({
      sessions: [{
        id: 'record-dedupe',
        recordId: 'record-dedupe',
        agentId: 'opencode',
        title: 'Dedupe',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'archived',
        workDir: '/repo',
      }],
      activeSessionId: 'record-dedupe',
    });

    const first = useChatStore.getState().resumeSession('record-dedupe');
    const second = useChatStore.getState().resumeSession('record-dedupe');

    expect(resumeChatSession).toHaveBeenCalledTimes(1);
    releaseResume!({ id: 'live-dedupe', mode: 'acp', acpSessionId: 'acp-dedupe', workDir: '/repo' });
    await expect(Promise.all([first, second])).resolves.toEqual([true, true]);
    expect(useChatStore.getState().sessions).toHaveLength(1);
    expect(useChatStore.getState().sessions[0].id).toBe('live-dedupe');
  });
});

describe('replayTranscriptToMessages', () => {
  it('concatenates consecutive text events into single assistant message', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'Hello' }), seq: 1, timestamp: 100 },
      { id: 'e2', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: ' World' }), seq: 2, timestamp: 101 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].content).toBe('Hello World');
    expect(result.messages[0].role).toBe('assistant');
  });

  it('appends thinking to last assistant message', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'thinking...' }), seq: 1, timestamp: 100 },
      { id: 'e2', sessionId: 's', kind: 'thinking', payloadJson: JSON.stringify({ type: 'thinking', thinking: 'hmm' }), seq: 2, timestamp: 101 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].thinking).toBe('hmm');
  });

  it('creates tool_call and updates it', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'tool_call', payloadJson: JSON.stringify({ type: 'tool_call', toolCallId: 'tc1', toolTitle: 'Read', toolKind: 'edit', toolStatus: 'pending' }), seq: 1, timestamp: 100 },
      { id: 'e2', sessionId: 's', kind: 'tool_call_update', payloadJson: JSON.stringify({ type: 'tool_call_update', toolCallId: 'tc1', toolStatus: 'completed', toolContent: 'file content' }), seq: 2, timestamp: 101 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].role).toBe('tool_call');
    expect(result.messages[0].toolCall?.status).toBe('completed');
    expect(result.messages[0].toolCall?.content).toBe('file content');
  });

  it('attaches diffs to last tool_call', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'tool_call', payloadJson: JSON.stringify({ type: 'tool_call', toolCallId: 'tc1', toolTitle: 'Edit', toolKind: 'edit', toolStatus: 'running' }), seq: 1, timestamp: 100 },
      { id: 'e2', sessionId: 's', kind: 'diff', payloadJson: JSON.stringify({ type: 'diff', diffPath: 'main.go', diffOldText: 'old', diffNewText: 'new' }), seq: 2, timestamp: 101 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].diffs).toHaveLength(1);
    expect(result.messages[0].diffs?.[0]?.path).toBe('main.go');
  });

  it('creates plan entries', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'plan', payloadJson: JSON.stringify({ type: 'plan', planEntries: [{ content: 'Step 1', status: 'pending' }] }), seq: 1, timestamp: 100 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].role).toBe('plan');
    expect(result.messages[0].plan?.[0]?.content).toBe('Step 1');
  });

  it('extracts commands, configOptions, and title from events', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'commands', payloadJson: JSON.stringify({ type: 'commands', commands: [{ name: 'help', description: 'Help' }] }), seq: 1, timestamp: 100 },
      { id: 'e2', sessionId: 's', kind: 'config_options', payloadJson: JSON.stringify({ type: 'config_options', configOptions: [{ id: 'model', name: 'Model', type: 'string', currentValue: 'opus', options: [] }] }), seq: 2, timestamp: 101 },
      { id: 'e3', sessionId: 's', kind: 'title', payloadJson: JSON.stringify({ type: 'title', title: 'My Chat' }), seq: 3, timestamp: 102 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.commands).toHaveLength(1);
    expect(result.commands?.[0]?.name).toBe('help');
    expect(result.configOptions).toHaveLength(1);
    expect(result.configOptions?.[0]?.id).toBe('model');
    expect(result.title).toBe('My Chat');
  });

  it('restores context usage and cost from usage updates', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'usage_update', payloadJson: JSON.stringify({ type: 'usage_update', contextWindow: 200000, contextUsed: 44152, costAmount: 0.0001, costCurrency: 'USD' }), seq: 1, timestamp: 100 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.contextWindow).toBe(200000);
    expect(result.contextUsed).toBe(44152);
    expect(result.costAmount).toBe(0.0001);
    expect(result.costCurrency).toBe('USD');
  });

  it('produces empty result for empty events', () => {
    const result = replayTranscriptToMessages([]);
    expect(result.messages).toHaveLength(0);
    expect(result.commands).toBeUndefined();
    expect(result.configOptions).toBeUndefined();
    expect(result.title).toBeUndefined();
  });

  it('replays a full transcript with interleaved events', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'Let me help' }), seq: 1, timestamp: 100 },
      { id: 'e2', sessionId: 's', kind: 'thinking', payloadJson: JSON.stringify({ type: 'thinking', thinking: 'user wants file' }), seq: 2, timestamp: 101 },
      { id: 'e3', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: ' with that.' }), seq: 3, timestamp: 102 },
      { id: 'e4', sessionId: 's', kind: 'tool_call', payloadJson: JSON.stringify({ type: 'tool_call', toolCallId: 'tc1', toolTitle: 'Write file', toolKind: 'edit', toolStatus: 'running' }), seq: 4, timestamp: 103 },
      { id: 'e5', sessionId: 's', kind: 'diff', payloadJson: JSON.stringify({ type: 'diff', diffPath: '.env', diffOldText: '', diffNewText: 'PORT=8080' }), seq: 5, timestamp: 104 },
      { id: 'e6', sessionId: 's', kind: 'tool_call_update', payloadJson: JSON.stringify({ type: 'tool_call_update', toolCallId: 'tc1', toolStatus: 'completed' }), seq: 6, timestamp: 105 },
      { id: 'e7', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'Done!' }), seq: 7, timestamp: 106 },
    ];
    const result = replayTranscriptToMessages(events);
    expect(result.messages).toHaveLength(3);
    expect(result.messages[0].role).toBe('assistant');
    expect(result.messages[0].content).toBe('Let me help with that.');
    expect(result.messages[0].thinking).toBe('user wants file');
    expect(result.messages[1].role).toBe('tool_call');
    expect(result.messages[1].toolCall?.status).toBe('completed');
    expect(result.messages[1].diffs).toHaveLength(1);
    expect(result.messages[2].role).toBe('assistant');
    expect(result.messages[2].content).toBe('Done!');
  });

  it('starts a new assistant message after a done event', () => {
    const events = [
      { id: 'e1', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'Pakai mode normal dulu ya?' }), seq: 1, timestamp: 100 },
      { id: 'e2', sessionId: 's', kind: 'done', payloadJson: JSON.stringify({ type: 'done', stopReason: 'end_turn' }), seq: 2, timestamp: 101 },
      { id: 'e3', sessionId: 's', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'Siap, tunggu instruksi selanjutnya.' }), seq: 3, timestamp: 102 },
      { id: 'e4', sessionId: 's', kind: 'done', payloadJson: JSON.stringify({ type: 'done', stopReason: 'end_turn' }), seq: 4, timestamp: 103 },
    ];

    const result = replayTranscriptToMessages(events);

    expect(result.messages).toHaveLength(2);
    expect(result.messages[0].role).toBe('assistant');
    expect(result.messages[0].content).toBe('Pakai mode normal dulu ya?');
    expect(result.messages[1].role).toBe('assistant');
    expect(result.messages[1].content).toBe('Siap, tunggu instruksi selanjutnya.');
  });
});

describe('active terminal routing', () => {
  beforeEach(() => {
    resetChatStore();
    unregisterTerminal('term-1');
  });

  it('sends routed terminal commands instantly instead of typing characters', () => {
    const sendCommand = vi.fn();
    registerTerminal('term-1', {
      getScrollback: () => '',
      sendCommand,
      serialize: () => '',
      write: () => {},
      cwd: '/repo',
      shellType: 'powershell',
    });

    useChatStore.setState({
      sessions: [{
        id: 's',
        recordId: 's',
        agentId: 'opencode',
        title: 'Terminal',
        messages: [],
        status: 'streaming',
        createdAt: 1,
        kind: 'live',
      }],
      activeSessionId: 's',
      useActiveTerminal: true,
      activeTerminalId: 'term-1',
    });

    useChatStore.getState().handleChatEvent('s', {
      type: 'terminal_execute',
      terminalCommand: 'Get-ChildItem',
      toolCallId: 'tc1',
      toolTitle: 'active_terminal_run',
      toolKind: 'execute',
    });

    expect(sendCommand).toHaveBeenCalledWith('Get-ChildItem');
    expect(useChatStore.getState().sessions[0].status).toBe('idle');

    unregisterTerminal('term-1');
  });
});

describe('refreshSessionState', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
  });

  it('restores idle status when persisted transcript already ended', async () => {
    useChatStore.setState({
      sessions: [{
        id: 'live-refresh',
        recordId: 'record-refresh',
        agentId: 'opencode',
        title: 'Refresh me',
        messages: [],
        status: 'streaming',
        stalled: true,
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
      }],
      activeSessionId: 'live-refresh',
    });
    getChatSessionState.mockResolvedValue({
      session: { id: 'record-refresh', agentId: 'opencode', title: 'Refresh me', workDir: '/repo', status: 'closed', createdAt: 1, updatedAt: 40 },
      messages: [
        { id: 'user-refresh', sessionId: 'record-refresh', role: 'user', content: 'halo', timestamp: 10 },
        { id: 'assistant-refresh', sessionId: 'record-refresh', role: 'assistant', content: 'selesai', timestamp: 30 },
      ],
      events: [
        { id: 'evt-refresh-text', sessionId: 'record-refresh', kind: 'text', payloadJson: JSON.stringify({ type: 'text', text: 'selesai' }), seq: 1, timestamp: 30 },
        { id: 'evt-refresh-done', sessionId: 'record-refresh', kind: 'done', payloadJson: JSON.stringify({ type: 'done', stopReason: 'end_turn' }), seq: 2, timestamp: 31 },
      ],
      snapshot: null,
    });

    await useChatStore.getState().refreshSessionState('live-refresh');

    const session = useChatStore.getState().sessions[0];
    expect(session.status).toBe('idle');
    expect(session.stalled).toBe(false);
    expect(session.lastEventAt).toBe(40);
    expect(session.messages.some((message) => message.role === 'assistant' && message.content === 'selesai')).toBe(true);
  });
});

describe('input_locked / input_unlocked (ADR-0005 VAL-PTY-007)', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    useChatStore.setState({
      sessions: [{
        id: 'live-lock',
        recordId: 'record-lock',
        agentId: 'opencode',
        title: 'Lock test',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
      }],
      activeSessionId: 'live-lock',
    });
  });

  it('setInputLocked sets inputLocked=true and lockedBy=holder', () => {
    useChatStore.getState().setInputLocked('live-lock', true, 'client-A');
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-lock');
    expect(session?.inputLocked).toBe(true);
    expect(session?.lockedBy).toBe('client-A');
  });

  it('setInputLocked(false) clears inputLocked and lockedBy', () => {
    useChatStore.getState().setInputLocked('live-lock', true, 'client-A');
    useChatStore.getState().setInputLocked('live-lock', false);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-lock');
    expect(session?.inputLocked).toBe(false);
    expect(session?.lockedBy).toBeUndefined();
  });

  it('handleChatEvent with input_locked sets inputLocked=true and lockedBy from holder', () => {
    useChatStore.getState().handleChatEvent('live-lock', {
      type: 'input_locked',
      holder: 'client-B',
      ttl: 2000,
    });
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-lock');
    expect(session?.inputLocked).toBe(true);
    expect(session?.lockedBy).toBe('client-B');
  });

  it('handleChatEvent with input_unlocked clears inputLocked and lockedBy', () => {
    useChatStore.getState().handleChatEvent('live-lock', {
      type: 'input_locked',
      holder: 'client-B',
      ttl: 2000,
    });
    useChatStore.getState().handleChatEvent('live-lock', {
      type: 'input_unlocked',
    });
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-lock');
    expect(session?.inputLocked).toBe(false);
    expect(session?.lockedBy).toBeUndefined();
  });

  it('handleChatEvent with input_locked uses fallback holder when holder field missing', () => {
    useChatStore.getState().handleChatEvent('live-lock', {
      type: 'input_locked',
    });
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-lock');
    expect(session?.inputLocked).toBe(true);
    expect(typeof session?.lockedBy).toBe('string');
    expect(session?.lockedBy).not.toBe('');
  });
});

describe('parseSnapshotJson', () => {
  it('parses valid JSON array', () => {
    const result = parseSnapshotJson<Array<{ name: string }>>('[{"name":"test"}]');
    expect(result).toEqual([{ name: 'test' }]);
  });

  it('returns undefined for empty string', () => {
    expect(parseSnapshotJson('')).toBeUndefined();
  });

  it('returns undefined for null', () => {
    expect(parseSnapshotJson(null)).toBeUndefined();
  });

  it('returns undefined for non-array JSON', () => {
    expect(parseSnapshotJson('{"key":"value"}')).toBeUndefined();
  });
});

describe('hard restart path removed (VAL-SOFT-TOGGLE-008)', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    getChatHistory.mockResolvedValue([]);
    getChatSessionMessages.mockResolvedValue([]);
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });
    saveChatMessage.mockResolvedValue(undefined);
    deleteChatHistory.mockResolvedValue(undefined);
  });

  it('does not expose restartActiveSessionForBrowser on the store (removed)', () => {
    // The hard-restart method was removed once soft toggle became the primary
    // path. Asserting it is gone prevents accidental reintroduction.
    expect((useChatStore.getState() as unknown as Record<string, unknown>).restartActiveSessionForBrowser)
      .toBeUndefined();
  });

  it('done event does not set pendingBrowserToggle (field removed)', () => {
    useChatStore.setState({
      sessions: [{
        id: 'live-done',
        recordId: 'record-done',
        agentId: 'opencode',
        title: 'Done',
        messages: [],
        status: 'streaming',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-done',
      }],
      activeSessionId: 'live-done',
    });

    useChatStore.getState().handleChatEvent('live-done', { type: 'done' });

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-done');
    // The field is gone entirely; accessing it yields undefined.
    expect((session as unknown as Record<string, unknown>)?.pendingBrowserToggle).toBeUndefined();
    // No resume/hard-restart fired.
    expect(resumeChatSession).not.toHaveBeenCalled();
  });
});

describe('hard restart path removed (VAL-SOFT-TOGGLE-008/009 cleanup)', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
  });

  it('does not expose restartActiveSessionForBrowser on the store', () => {
    // The deprecated hard-restart method has been removed; soft toggle
    // (setBrowserEnabled) is the only browser-toggle entry point.
    expect((useChatStore.getState() as Record<string, unknown>).restartActiveSessionForBrowser).toBeUndefined();
  });

  it('ChatSessionInfo no longer carries a pendingBrowserToggle field', () => {
    // After enabling the browser via the soft path, the session object must
    // not contain the removed pendingBrowserToggle queueing field.
    useChatStore.setState({
      sessions: [{
        id: 'live-1',
        recordId: 'record-1',
        agentId: 'opencode',
        title: 'Cleanup',
        messages: [],
        status: 'idle',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-1',
        useActiveBrowser: false,
      }],
      activeSessionId: 'live-1',
      useActiveBrowser: false,
    });

    return useChatStore.getState().setBrowserEnabled(true).then(() => {
      const session = useChatStore.getState().sessions.find((s) => s.id === 'live-1')!;
      expect(session).toBeDefined();
      expect((session as Record<string, unknown>).pendingBrowserToggle).toBeUndefined();
      expect(session.useActiveBrowser).toBe(true);
    });
  });

  it('done event no longer references pendingBrowserToggle (no deferred restart)', () => {
    // A done event on a session must not leave any pendingBrowserToggle
    // artifact behind (the deferred-restart queueing path is gone).
    useChatStore.setState({
      sessions: [{
        id: 'live-done',
        recordId: 'record-done',
        agentId: 'opencode',
        title: 'Done cleanup',
        messages: [],
        status: 'streaming',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-done',
        useActiveBrowser: true,
      }],
      activeSessionId: 'live-done',
      useActiveBrowser: true,
    });

    useChatStore.getState().handleChatEvent('live-done', { type: 'done', stopReason: 'end_turn' });

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-done')!;
    expect(session.status).toBe('idle');
    expect((session as Record<string, unknown>).pendingBrowserToggle).toBeUndefined();
  });
});

describe('setBrowserEnabled soft WS toggle (primary path)', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    getChatHistory.mockResolvedValue([]);
    getChatSessionMessages.mockResolvedValue([]);
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });
    saveChatMessage.mockResolvedValue(undefined);
    deleteChatHistory.mockResolvedValue(undefined);
    window.sessionStorage.clear();
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
    useWorkspaceStore.getState().addProject('/repo', 'repo');
    const project = useWorkspaceStore.getState().projects[0];
    useWorkspaceStore.getState().addBrowserTab(project.id, 'tab-1');
  });

  function makeSession(overrides: Partial<ChatSessionInfo> = {}): ChatSessionInfo {
    return {
      id: 'live-idle',
      recordId: 'record-idle',
      agentId: 'opencode',
      title: 'Idle',
      messages: [],
      status: 'idle',
      createdAt: 1,
      kind: 'live' as const,
      workDir: '/repo',
      acpSessionId: 'acp-idle',
      ...overrides,
    };
  }

  it('soft-toggles frontend state when there is no active session', async () => {
    useChatStore.setState({ sessions: [], activeSessionId: null, useActiveBrowser: false });

    const ok = await useChatStore.getState().setBrowserEnabled(true);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveBrowser).toBe(true);
    // No hard restart fired.
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('soft-toggles when an idle active session exists: updates store + session.useActiveBrowser, no resume', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-idle', useActiveBrowser: false })],
      activeSessionId: 'live-idle',
      useActiveBrowser: false,
    });

    const ok = await useChatStore.getState().setBrowserEnabled(true);

    expect(ok).toBe(true);
    // Store flag updated — drives the WS effect.
    expect(useChatStore.getState().useActiveBrowser).toBe(true);
    // Session object flag updated — UI (checkbox, AgentPicker, BrowserPanel) reflects immediately.
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    expect(session?.useActiveBrowser).toBe(true);
    // VAL-SOFTTOGGLE-001: NO hard restart / HTTP resume occurs.
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('does NOT change session status to connecting during the toggle (VAL-SOFTTOGGLE-001)', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-idle', status: 'idle', useActiveBrowser: false })],
      activeSessionId: 'live-idle',
      useActiveBrowser: false,
    });

    await useChatStore.getState().setBrowserEnabled(true);

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    // Session must remain idle (not 'connecting') — the WS stays open.
    expect(session?.status).toBe('idle');
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('soft-toggles when the active session is busy (streaming) without queueing a hard restart', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-busy', status: 'streaming', useActiveBrowser: false })],
      activeSessionId: 'live-busy',
      useActiveBrowser: false,
    });

    const ok = await useChatStore.getState().setBrowserEnabled(true);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveBrowser).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-busy');
    expect(session?.useActiveBrowser).toBe(true);
    // Soft toggle works while busy — no hard-restart queue needed. The
    // pendingBrowserToggle field has been removed entirely.
    expect((session as unknown as Record<string, unknown>)?.pendingBrowserToggle).toBeUndefined();
    // Session status unchanged (still streaming).
    expect(session?.status).toBe('streaming');
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('soft-toggles when the active session is connecting', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-connecting', status: 'connecting', useActiveBrowser: false })],
      activeSessionId: 'live-connecting',
      useActiveBrowser: false,
    });

    const ok = await useChatStore.getState().setBrowserEnabled(true);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveBrowser).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-connecting');
    expect(session?.useActiveBrowser).toBe(true);
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('soft-toggles when active session is in error state', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-err', status: 'error', useActiveBrowser: false })],
      activeSessionId: 'live-err',
      useActiveBrowser: false,
    });

    const ok = await useChatStore.getState().setBrowserEnabled(true);

    // Error state still gets a soft toggle so the user's intent is captured.
    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveBrowser).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-err');
    expect(session?.useActiveBrowser).toBe(true);
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('toggling off updates store + session.useActiveBrowser without resume (VAL-SOFTTOGGLE-003)', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-on', status: 'idle', useActiveBrowser: true })],
      activeSessionId: 'live-on',
      useActiveBrowser: true,
    });

    const ok = await useChatStore.getState().setBrowserEnabled(false);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveBrowser).toBe(false);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-on');
    expect(session?.useActiveBrowser).toBe(false);
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('preserves other session fields when updating useActiveBrowser', async () => {
    useChatStore.setState({
      sessions: [makeSession({
        id: 'live-idle',
        status: 'idle',
        useActiveBrowser: false,
        useActiveTerminal: true,
        terminalId: 'term-1',
        acpSessionId: 'acp-preserved',
      })],
      activeSessionId: 'live-idle',
      useActiveBrowser: false,
    });

    await useChatStore.getState().setBrowserEnabled(true);

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    expect(session?.useActiveBrowser).toBe(true);
    // Other fields untouched.
    expect(session?.useActiveTerminal).toBe(true);
    expect(session?.terminalId).toBe('term-1');
    expect(session?.acpSessionId).toBe('acp-preserved');
    expect(session?.status).toBe('idle');
  });

  it('warns via session debug entry when enabling with no browser tab open (VAL-SOFTTOGGLE-002)', async () => {
    // Remove the browser tab the shared beforeEach added so no tab is open.
    const project = useWorkspaceStore.getState().projects[0];
    useWorkspaceStore.getState().removeBrowserTab(project.id, 'tab-1');

    useChatStore.setState({
      sessions: [makeSession({ id: 'live-idle', status: 'idle', useActiveBrowser: false })],
      activeSessionId: 'live-idle',
      useActiveBrowser: false,
    });

    const ok = await useChatStore.getState().setBrowserEnabled(true);

    expect(ok).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    expect(session?.useActiveBrowser).toBe(true);
    // Toggle is NOT blocked; a transient warn debug entry is surfaced.
    const warn = session?.debugEntries?.find(
      (e) => e.level === 'warn' && e.source === 'client' && e.message.includes('no browser tab is open'),
    );
    expect(warn).toBeTruthy();
  });

  it('does not warn when enabling with a browser tab open', async () => {
    // Shared beforeEach already added 'tab-1' to the /repo project.
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-idle', status: 'idle', useActiveBrowser: false })],
      activeSessionId: 'live-idle',
      useActiveBrowser: false,
    });

    await useChatStore.getState().setBrowserEnabled(true);

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    const warn = session?.debugEntries?.find(
      (e) => e.level === 'warn' && e.message.includes('no browser tab is open'),
    );
    expect(warn).toBeUndefined();
  });

  it('does not warn when toggling off even with no browser tab open', async () => {
    const project = useWorkspaceStore.getState().projects[0];
    useWorkspaceStore.getState().removeBrowserTab(project.id, 'tab-1');

    useChatStore.setState({
      sessions: [makeSession({ id: 'live-on', status: 'idle', useActiveBrowser: true })],
      activeSessionId: 'live-on',
      useActiveBrowser: true,
    });

    await useChatStore.getState().setBrowserEnabled(false);

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-on');
    const warn = session?.debugEntries?.find(
      (e) => e.level === 'warn' && e.message.includes('no browser tab is open'),
    );
    expect(warn).toBeUndefined();
  });
});

describe('setTerminalEnabled soft WS toggle (primary path) VAL-HARDEN-001', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    getChatHistory.mockResolvedValue([]);
    getChatSessionMessages.mockResolvedValue([]);
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });
    saveChatMessage.mockResolvedValue(undefined);
    deleteChatHistory.mockResolvedValue(undefined);
    window.sessionStorage.clear();
  });

  function makeSession(overrides: Partial<ChatSessionInfo> = {}): ChatSessionInfo {
    return {
      id: 'live-idle',
      recordId: 'record-idle',
      agentId: 'opencode',
      title: 'Idle',
      messages: [],
      status: 'idle',
      createdAt: 1,
      kind: 'live' as const,
      workDir: '/repo',
      acpSessionId: 'acp-idle',
      ...overrides,
    };
  }

  it('soft-toggles frontend state when there is no active session', async () => {
    useChatStore.setState({
      sessions: [],
      activeSessionId: null,
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    const ok = await useChatStore.getState().setTerminalEnabled(true);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveTerminal).toBe(true);
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('soft-toggles when an idle active session exists: updates store + session, no resume (VAL-HARDEN-001)', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-idle', useActiveTerminal: false })],
      activeSessionId: 'live-idle',
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    const ok = await useChatStore.getState().setTerminalEnabled(true);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveTerminal).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    expect(session?.useActiveTerminal).toBe(true);
    expect(session?.terminalId).toBe('term-1');
    // VAL-HARDEN-001: NO hard restart / HTTP resume occurs.
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('does NOT change session status to connecting during the toggle (VAL-HARDEN-001)', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-idle', status: 'idle', useActiveTerminal: false })],
      activeSessionId: 'live-idle',
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    await useChatStore.getState().setTerminalEnabled(true);

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    expect(session?.status).toBe('idle');
    expect(session?.kind).toBe('live');
  });

  it('soft-toggles while session is streaming without resume or status flip', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-stream', status: 'streaming', useActiveTerminal: false })],
      activeSessionId: 'live-stream',
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    const ok = await useChatStore.getState().setTerminalEnabled(true);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveTerminal).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-stream');
    expect(session?.useActiveTerminal).toBe(true);
    expect(session?.status).toBe('streaming');
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('soft-toggles when active session is in error state (captures intent)', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-err', status: 'error', useActiveTerminal: false })],
      activeSessionId: 'live-err',
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    const ok = await useChatStore.getState().setTerminalEnabled(true);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveTerminal).toBe(true);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-err');
    expect(session?.useActiveTerminal).toBe(true);
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('toggling off updates store + session without resume', async () => {
    useChatStore.setState({
      sessions: [makeSession({ id: 'live-on', status: 'idle', useActiveTerminal: true, terminalId: 'term-1' })],
      activeSessionId: 'live-on',
      useActiveTerminal: true,
      activeTerminalId: 'term-1',
    });

    const ok = await useChatStore.getState().setTerminalEnabled(false);

    expect(ok).toBe(true);
    expect(useChatStore.getState().useActiveTerminal).toBe(false);
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-on');
    expect(session?.useActiveTerminal).toBe(false);
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('preserves other session fields when updating useActiveTerminal', async () => {
    useChatStore.setState({
      sessions: [makeSession({
        id: 'live-idle',
        status: 'idle',
        useActiveTerminal: false,
        useActiveBrowser: true,
        acpSessionId: 'acp-preserved',
      })],
      activeSessionId: 'live-idle',
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    await useChatStore.getState().setTerminalEnabled(true);

    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-idle');
    expect(session?.useActiveTerminal).toBe(true);
    expect(session?.useActiveBrowser).toBe(true);
    expect(session?.acpSessionId).toBe('acp-preserved');
    expect(session?.status).toBe('idle');
  });

  it('does not expose restartActiveSessionForTerminal as the primary toggle path (removed)', () => {
    // Soft setTerminalEnabled replaces the hard-restart method for UI toggle.
    expect((useChatStore.getState() as unknown as Record<string, unknown>).restartActiveSessionForTerminal)
      .toBeUndefined();
    expect(typeof useChatStore.getState().setTerminalEnabled).toBe('function');
  });
});

describe('concurrent browser+terminal soft toggles preserve both intents (VAL-HARDEN-004)', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    getChatHistory.mockResolvedValue([]);
    getChatSessionMessages.mockResolvedValue([]);
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });
    saveChatMessage.mockResolvedValue(undefined);
    deleteChatHistory.mockResolvedValue(undefined);
    window.sessionStorage.clear();
    useWorkspaceStore.setState({
      projects: [{
        id: 'proj-repo',
        path: '/repo',
        name: 'repo',
        openFiles: [],
        activeFileId: null,
        terminalTabs: [],
        activeTerminalTabId: null,
        terminalSessions: [],
        browserTabIds: ['tab-1'],
        activeBrowserTabId: 'tab-1',
      }],
      activeProjectId: 'proj-repo',
    });
  });

  function makeSession(overrides: Partial<ChatSessionInfo> = {}): ChatSessionInfo {
    return {
      id: 'live-idle',
      recordId: 'record-idle',
      agentId: 'opencode',
      title: 'Idle',
      messages: [],
      status: 'idle',
      createdAt: 1,
      kind: 'live' as const,
      workDir: '/repo',
      acpSessionId: 'acp-idle',
      ...overrides,
    };
  }

  it('sequential dual toggles leave both store and session intents true', async () => {
    useChatStore.setState({
      sessions: [makeSession({ useActiveBrowser: false, useActiveTerminal: false })],
      activeSessionId: 'live-idle',
      useActiveBrowser: false,
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    await useChatStore.getState().setBrowserEnabled(true);
    await useChatStore.getState().setTerminalEnabled(true);

    const state = useChatStore.getState();
    const session = state.sessions.find((s) => s.id === 'live-idle');
    expect(state.useActiveBrowser).toBe(true);
    expect(state.useActiveTerminal).toBe(true);
    expect(session?.useActiveBrowser).toBe(true);
    expect(session?.useActiveTerminal).toBe(true);
    expect(session?.status).toBe('idle');
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('concurrent dual toggles do not lose either intent (no snapshot freeze)', async () => {
    useChatStore.setState({
      sessions: [makeSession({ useActiveBrowser: false, useActiveTerminal: false })],
      activeSessionId: 'live-idle',
      useActiveBrowser: false,
      useActiveTerminal: false,
      activeTerminalId: 'term-1',
    });

    await Promise.all([
      useChatStore.getState().setBrowserEnabled(true),
      useChatStore.getState().setTerminalEnabled(true),
    ]);

    const state = useChatStore.getState();
    const session = state.sessions.find((s) => s.id === 'live-idle');
    expect(state.useActiveBrowser).toBe(true);
    expect(state.useActiveTerminal).toBe(true);
    expect(session?.useActiveBrowser).toBe(true);
    expect(session?.useActiveTerminal).toBe(true);
    expect(session?.status).toBe('idle');
    expect(session?.kind).toBe('live');
    expect(resumeChatSession).not.toHaveBeenCalled();
  });

  it('concurrent dual toggles OFF both apply after starting ON', async () => {
    useChatStore.setState({
      sessions: [makeSession({ useActiveBrowser: true, useActiveTerminal: true, terminalId: 'term-1' })],
      activeSessionId: 'live-idle',
      useActiveBrowser: true,
      useActiveTerminal: true,
      activeTerminalId: 'term-1',
    });

    await Promise.all([
      useChatStore.getState().setBrowserEnabled(false),
      useChatStore.getState().setTerminalEnabled(false),
    ]);

    const state = useChatStore.getState();
    const session = state.sessions.find((s) => s.id === 'live-idle');
    expect(state.useActiveBrowser).toBe(false);
    expect(state.useActiveTerminal).toBe(false);
    expect(session?.useActiveBrowser).toBe(false);
    expect(session?.useActiveTerminal).toBe(false);
  });
});

describe('resume/restore browser intent vs effective enablement (VAL-HARDEN-005)', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    getChatHistory.mockResolvedValue([]);
    getChatSessionMessages.mockResolvedValue([]);
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });
    saveChatMessage.mockResolvedValue(undefined);
    deleteChatHistory.mockResolvedValue(undefined);
    getRestorableChatSession.mockResolvedValue(null);
    resumeChatSession.mockResolvedValue({ id: 'live-resumed', acpSessionId: 'acp-resumed', workDir: '/repo' });
    window.sessionStorage.clear();
    useWorkspaceStore.setState({
      projects: [{
        id: 'proj-repo',
        path: '/repo',
        name: 'repo',
        openFiles: [],
        activeFileId: null,
        terminalTabs: [],
        activeTerminalTabId: null,
        terminalSessions: [],
        browserTabIds: [],
        activeBrowserTabId: null,
      }],
      activeProjectId: 'proj-repo',
    });
  });

  function makeSession(overrides: Partial<ChatSessionInfo> = {}): ChatSessionInfo {
    return {
      id: 'live-idle',
      recordId: 'record-idle',
      agentId: 'opencode',
      title: 'Idle',
      messages: [],
      status: 'idle',
      createdAt: 1,
      kind: 'live' as const,
      workDir: '/repo',
      acpSessionId: 'acp-idle',
      ...overrides,
    };
  }

  it('resume with intent ON + no tab keeps session intent ON and passes effective false to API', async () => {
    // No browser tab open for /repo.
    useChatStore.setState({
      sessions: [makeSession({
        id: 'archived-1',
        recordId: 'record-1',
        useActiveBrowser: true,
        useActiveTerminal: false,
        status: 'idle',
        kind: 'archived',
      })],
      activeSessionId: 'archived-1',
      useActiveBrowser: true,
      useActiveTerminal: false,
      activeTerminalId: null,
    });

    const ok = await useChatStore.getState().resumeSession('archived-1');
    expect(ok).toBe(true);

    // Backend resume opts: effective useActiveBrowser is false (intent && hasTab).
    expect(resumeChatSession).toHaveBeenCalledWith(
      'record-1',
      'opencode',
      '/repo',
      'acp-idle',
      false,
      undefined,
      false, // effective enablement for backend
      undefined,
    );

    const state = useChatStore.getState();
    // Store intent remains ON (user preference preserved).
    expect(state.useActiveBrowser).toBe(true);
    const session = state.sessions.find((s) => s.recordId === 'record-1' || s.id === 'live-resumed');
    expect(session).toBeTruthy();
    // Session intent remains ON — no permanent store/session split brain.
    expect(session?.useActiveBrowser).toBe(true);
    // Warning surfaced for no-tab case.
    const warn = session?.debugEntries?.find(
      (e) => e.level === 'warn' && e.message.includes('no browser tab is open'),
    );
    expect(warn).toBeTruthy();
  });

  it('resume with intent ON + tab present passes effective true and keeps intent ON', async () => {
    useWorkspaceStore.setState({
      projects: [{
        id: 'proj-repo',
        path: '/repo',
        name: 'repo',
        openFiles: [],
        activeFileId: null,
        terminalTabs: [],
        activeTerminalTabId: null,
        terminalSessions: [],
        browserTabIds: ['tab-9'],
        activeBrowserTabId: 'tab-9',
      }],
      activeProjectId: 'proj-repo',
    });

    useChatStore.setState({
      sessions: [makeSession({
        id: 'archived-2',
        recordId: 'record-2',
        useActiveBrowser: true,
        status: 'idle',
        kind: 'archived',
      })],
      activeSessionId: 'archived-2',
      useActiveBrowser: true,
    });

    const ok = await useChatStore.getState().resumeSession('archived-2');
    expect(ok).toBe(true);

    expect(resumeChatSession).toHaveBeenCalledWith(
      'record-2',
      'opencode',
      '/repo',
      'acp-idle',
      false,
      undefined,
      true, // effective enablement
      'tab-9',
    );

    const state = useChatStore.getState();
    expect(state.useActiveBrowser).toBe(true);
    const session = state.sessions.find((s) => s.recordId === 'record-2' || s.id === 'live-resumed');
    expect(session?.useActiveBrowser).toBe(true);
  });

  it('restoreSessionForProject with intent ON + no tab preserves intent and does not force session OFF', async () => {
    resumeChatSession.mockResolvedValue({ id: 'live-restored', acpSessionId: 'acp-r', workDir: '/repo' });
    getRestorableChatSession.mockResolvedValue({
      found: true,
      sessionId: 'record-restore',
      agentId: 'opencode',
      workDir: '/repo',
      acpSessionId: 'acp-old',
      isLive: false,
      title: 'Restorable',
    });
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });

    useChatStore.setState({
      sessions: [],
      activeSessionId: null,
      useActiveBrowser: true,
      historyLoaded: true,
      historyWorkDir: '/repo',
      historySessions: [{
        id: 'record-restore',
        agentId: 'opencode',
        title: 'Restorable',
        createdAt: 1,
        updatedAt: 1,
        workDir: '/repo',
        acpSessionId: 'acp-old',
      }],
    });

    await useChatStore.getState().restoreSessionForProject('/repo');

    const state = useChatStore.getState();
    expect(state.useActiveBrowser).toBe(true);
    const session = state.sessions.find((s) => s.recordId === 'record-restore' || s.id === 'live-restored');
    expect(session).toBeTruthy();
    // Intent preserved on session even though no tab (effective was false for resume API).
    expect(session?.useActiveBrowser).toBe(true);

    // Resume API received effective false + undefined tab (and terminal effective false).
    expect(resumeChatSession).toHaveBeenCalledWith(
      'record-restore',
      'opencode',
      '/repo',
      'acp-old',
      false,
      undefined,
      false,
      undefined,
    );
  });

  it('loadHistorySession with intent ON + no tab preserves intent on restored session', async () => {
    resumeChatSession.mockResolvedValue({ id: 'live-hist', acpSessionId: 'acp-h', workDir: '/repo' });
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });

    useChatStore.setState({
      sessions: [],
      activeSessionId: null,
      useActiveBrowser: true,
      historyLoaded: true,
      historyWorkDir: '/repo',
      historySessions: [{
        id: 'hist-1',
        agentId: 'opencode',
        title: 'History',
        createdAt: 1,
        updatedAt: 1,
        workDir: '/repo',
        acpSessionId: 'acp-hist',
      }],
    });

    await useChatStore.getState().loadHistorySession('hist-1');

    const state = useChatStore.getState();
    expect(state.useActiveBrowser).toBe(true);
    const session = state.sessions.find((s) => s.recordId === 'hist-1' || s.id === 'live-hist');
    expect(session).toBeTruthy();
    expect(session?.useActiveBrowser).toBe(true);
  });
});
