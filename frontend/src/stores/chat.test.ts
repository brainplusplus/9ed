import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatStore } from './chat';
import { registerTerminal, unregisterTerminal } from '../terminalRegistry';
import type { HistoryMessageRecord, HistorySessionRecord } from '../types';
import { replayTranscriptToMessages, parseSnapshotJson } from './chat';

const getChatHistory = vi.fn<() => Promise<HistorySessionRecord[]>>();
const getChatSessionMessages = vi.fn<(sessionId: string) => Promise<HistoryMessageRecord[]>>();
const getChatSessionState = vi.fn();
const saveChatMessage = vi.fn();
const deleteChatHistory = vi.fn();
const getRestorableChatSession = vi.fn();
const resumeChatSession = vi.fn();

vi.mock('../api', () => ({
  getChatHistory: () => getChatHistory(),
  getChatSessionMessages: (sessionId: string) => getChatSessionMessages(sessionId),
  getChatSessionState: (...args: unknown[]) => getChatSessionState(...args),
  saveChatMessage: (...args: unknown[]) => saveChatMessage(...args),
  deleteChatHistory: (sessionId: string) => deleteChatHistory(sessionId),
  getRestorableChatSession: (...args: unknown[]) => getRestorableChatSession(...args),
  resumeChatSession: (...args: unknown[]) => resumeChatSession(...args),
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
    expect(resumeChatSession).toHaveBeenCalledWith('record-missing-acp', 'opencode', '/repo', undefined);
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
