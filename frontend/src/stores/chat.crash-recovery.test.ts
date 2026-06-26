import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useChatStore } from './chat';
import type { HistoryMessageRecord, HistorySessionRecord } from '../types';

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

describe('ACP crash recovery / reconnect resilience (ADR-0004 / VAL-RESUME-005)', () => {
  beforeEach(() => {
    resetChatStore();
    vi.clearAllMocks();
    getChatHistory.mockResolvedValue([]);
    getChatSessionMessages.mockResolvedValue([]);
    getChatSessionState.mockResolvedValue({ session: null, messages: [], events: [], snapshot: null });
    saveChatMessage.mockResolvedValue(undefined);
    deleteChatHistory.mockResolvedValue(undefined);
    useChatStore.setState({
      sessions: [{
        id: 'live-crash',
        recordId: 'record-crash',
        agentId: 'opencode',
        title: 'Crash test',
        messages: [],
        status: 'streaming',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-crash',
      }],
      activeSessionId: 'live-crash',
    });
  });

  it('handleChatEvent done with agent_crash_unrecoverable + canResume sets crashed/canResume flags', () => {
    useChatStore.getState().handleChatEvent('live-crash', {
      type: 'done',
      stopReason: 'agent_crash_unrecoverable',
      canResume: true,
    });
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-crash');
    expect(session?.status).toBe('idle');
    expect(session?.crashed).toBe(true);
    expect(session?.canResume).toBe(true);
  });

  it('handleChatEvent done with agent_crash_unrecoverable + canResume=false records canResume=false', () => {
    useChatStore.getState().handleChatEvent('live-crash', {
      type: 'done',
      stopReason: 'agent_crash_unrecoverable',
      canResume: false,
    });
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-crash');
    expect(session?.crashed).toBe(true);
    expect(session?.canResume).toBe(false);
  });

  it('handleChatEvent normal done (end_turn) clears crashed/canResume flags', () => {
    // First crash the session.
    useChatStore.getState().handleChatEvent('live-crash', {
      type: 'done',
      stopReason: 'agent_crash_unrecoverable',
      canResume: true,
    });
    expect(useChatStore.getState().sessions[0].crashed).toBe(true);

    // A normal done must clear the crash state.
    useChatStore.getState().handleChatEvent('live-crash', {
      type: 'done',
      stopReason: 'end_turn',
    });
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-crash');
    expect(session?.crashed).toBe(false);
    expect(session?.canResume).toBe(false);
  });

  it('clearCrashState clears crashed and canResume', () => {
    useChatStore.getState().handleChatEvent('live-crash', {
      type: 'done',
      stopReason: 'agent_crash_unrecoverable',
      canResume: true,
    });
    useChatStore.getState().clearCrashState('live-crash');
    const session = useChatStore.getState().sessions.find((s) => s.id === 'live-crash');
    expect(session?.crashed).toBe(false);
    expect(session?.canResume).toBe(false);
  });

  it('crash done does not fire a deferred pendingBrowserToggle restart (session is dead)', () => {
    // Queue a pending browser toggle while busy.
    useChatStore.setState({
      sessions: [{
        id: 'live-crash',
        recordId: 'record-crash',
        agentId: 'opencode',
        title: 'Crash test',
        messages: [],
        status: 'streaming',
        createdAt: 1,
        kind: 'live',
        workDir: '/repo',
        acpSessionId: 'acp-crash',
        pendingBrowserToggle: true,
      }],
      activeSessionId: 'live-crash',
    });
    const restartSpy = vi.spyOn(useChatStore.getState(), 'restartActiveSessionForBrowser').mockResolvedValue(true);

    useChatStore.getState().handleChatEvent('live-crash', {
      type: 'done',
      stopReason: 'agent_crash_unrecoverable',
      canResume: true,
    });

    // The deferred browser-toggle restart must NOT fire on an unrecoverable
    // crash — the session is dead and must be recovered explicitly.
    expect(restartSpy).not.toHaveBeenCalled();
    restartSpy.mockRestore();
  });

  it('resumeSession surfaces HTTP failure via lastRestoreError', async () => {
    resumeChatSession.mockResolvedValue({
      found: true,
      status: 'error',
      resumeError: 'agent process not found',
    });
    const ok = await useChatStore.getState().resumeSession('live-crash');
    expect(ok).toBe(false);
    const state = useChatStore.getState();
    expect(state.lastRestoreError).not.toBeNull();
    expect(state.lastRestoreError?.sessionId).toBe('record-crash');
    expect(state.lastRestoreError?.reason).toBe('agent process not found');
  });

  it('resumeSession network rejection sets lastRestoreError with a reason', async () => {
    resumeChatSession.mockRejectedValue(new Error('network timeout'));
    const ok = await useChatStore.getState().resumeSession('live-crash');
    expect(ok).toBe(false);
    const state = useChatStore.getState();
    expect(state.lastRestoreError).not.toBeNull();
    expect(state.lastRestoreError?.reason).toContain('network timeout');
  });
});
