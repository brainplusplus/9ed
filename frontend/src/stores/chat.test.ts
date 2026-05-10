import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatStore } from './chat';
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
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
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

  it('persists user and assistant history against record id for resumed sessions', () => {
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
      }],
      activeSessionId: 'live-50',
    });

    useChatStore.getState().addMessage('live-50', {
      id: 'user-1',
      role: 'user',
      content: 'buat file .env.local.example',
      timestamp: 10,
    });

    useChatStore.getState().addMessage('live-50', {
      id: 'assistant-1',
      role: 'assistant',
      content: 'ok',
      timestamp: 20,
    });
    useChatStore.getState().finalizeAssistantMessage('live-50');

    expect(saveChatMessage).toHaveBeenCalledWith(expect.objectContaining({
      sessionId: 'record-50',
      role: 'user',
      content: 'buat file .env.local.example',
    }));
    expect(saveChatMessage).toHaveBeenCalledWith(expect.objectContaining({
      sessionId: 'record-50',
      role: 'assistant',
      content: 'ok',
    }));
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

  it('marks restored history session as connecting before websocket upgrade finishes', async () => {
    let releaseResume: ((value: { id: string; mode: string }) => void) | undefined;
    resumeChatSession.mockImplementation(() => new Promise((resolve) => {
      releaseResume = resolve as (value: { id: string; mode: string }) => void;
    }));

    useChatStore.setState({
      historyLoaded: true,
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
    expect(connecting.recordId).toBe('record-4');

    expect(releaseResume).toBeDefined();
    releaseResume!({ id: 'live-44', mode: 'acp' });
    await pendingLoad;
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
    resumeChatSession
      .mockResolvedValueOnce({ id: 'live-31', mode: 'acp' })
      .mockResolvedValueOnce({ id: 'live-32', mode: 'acp' });

    await useChatStore.getState().restoreSessionForProject('/repo', 'record-3');
    await useChatStore.getState().restoreSessionForProject('/repo', 'record-3');

    const state = useChatStore.getState();
    expect(state.sessions).toHaveLength(1);
    expect(state.sessions[0].recordId).toBe('record-3');
    expect(state.sessions[0].id).toBe('live-31');
    expect(state.activeSessionId).toBe('live-31');
    expect(resumeChatSession).toHaveBeenCalledTimes(1);
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
