import { create } from 'zustand';
import type { ChatAgent, ChatMessage, ChatSessionInfo, ChatEvent, ChatSessionKind, HistorySessionRecord, ToolCallInfo, TranscriptEventRecord, SlashCommandInfo, ConfigOptionInfo } from '../types';
import { getChatHistory, getChatSessionState, saveChatMessage, deleteChatHistory, getRestorableChatSession, resumeChatSession } from '../api';

export type ChatRestoreError = {
  sessionId: string;
  reason: string;
};

export type QueuedMessage = {
  id: string;
  content: string;
  attachments?: { type: 'file' | 'image'; path: string; name: string }[];
  createdAt: number;
};

type ChatState = {
  sessions: ChatSessionInfo[];
  activeSessionId: string | null;
  agents: ChatAgent[];
  selectedAgentId: string | null;
  chatVisible: boolean;
  historySessions: HistorySessionRecord[];
  historyLoaded: boolean;
  queuedMessages: Record<string, QueuedMessage[]>;
  includeIgnoredInMentions: boolean;
  autoApprove: boolean;
  restoring: boolean;
  lastRestoreError: ChatRestoreError | null;

  loadAgents: (agents: ChatAgent[]) => void;
  setSelectedAgent: (id: string) => void;
  createSession: (session: ChatSessionInfo) => void;
  setActiveSession: (id: string | null) => void;
  addMessage: (sessionId: string, message: ChatMessage) => void;
  appendToLastMessage: (sessionId: string, chunk: string) => void;
  handleChatEvent: (sessionId: string, event: ChatEvent) => void;
  finalizeAssistantMessage: (sessionId: string) => void;
  setSessionStatus: (sessionId: string, status: ChatSessionInfo['status']) => void;
  setSessionKind: (sessionId: string, kind: ChatSessionKind) => void;
  toggleChat: () => void;
  deleteSession: (id: string) => void;
  loadHistory: (workDir?: string) => Promise<void>;
  loadHistorySession: (sessionId: string) => Promise<void>;
  deleteHistorySession: (sessionId: string) => Promise<void>;
  restoreSessionForProject: (projectPath: string, preferredSessionId?: string) => Promise<void>;
  enqueueMessage: (sessionId: string, msg: QueuedMessage) => void;
  dequeueMessage: (sessionId: string) => QueuedMessage | undefined;
  removeQueuedMessage: (sessionId: string, msgId: string) => void;
  editQueuedMessage: (sessionId: string, msgId: string, content: string) => void;
  reorderQueuedMessages: (sessionId: string, fromIdx: number, toIdx: number) => void;
  clearQueue: (sessionId: string) => void;
  toggleIncludeIgnored: () => void;
  toggleAutoApprove: () => void;
};

function updateSession(sessions: ChatSessionInfo[], id: string, updater: (s: ChatSessionInfo) => ChatSessionInfo): ChatSessionInfo[] {
  return sessions.map((s) => (s.id === id ? updater(s) : s));
}

function findSessionByIdentity(sessions: ChatSessionInfo[], identity: string): ChatSessionInfo | undefined {
  return sessions.find((s) => s.id === identity || s.recordId === identity);
}

function fallbackTitle(agentId: string, title?: string): string {
  const agentLabels: Record<string, string> = {
    opencode: 'OpenCode',
    claude: 'Claude Code',
    codex: 'Codex CLI',
    gemini: 'Gemini CLI',
    pi: 'Pi',
    amp: 'Amp',
    copilot: 'Copilot',
  };
  const agentLabel = agentLabels[agentId] ?? agentId ?? 'Chat';
  return title && title.trim() ? title : agentLabel;
}

function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
  for (let i = arr.length - 1; i >= 0; i--) {
    if (predicate(arr[i])) return i;
  }
  return -1;
}

type ReplayResult = {
  messages: ChatMessage[];
  commands?: SlashCommandInfo[];
  configOptions?: ConfigOptionInfo[];
  title?: string;
};

export function replayTranscriptToMessages(events: TranscriptEventRecord[]): ReplayResult {
  const msgs: ChatMessage[] = [];
  let commands: SlashCommandInfo[] | undefined;
  let configOptions: ConfigOptionInfo[] | undefined;
  let title: string | undefined;
  const genId = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 6);

  for (const evt of events) {
    let payload: ChatEvent;
    try {
      payload = JSON.parse(evt.payloadJson) as ChatEvent;
    } catch {
      continue;
    }

    const last = msgs[msgs.length - 1];

    switch (evt.kind) {
      case 'text': {
        if (last && last.role === 'assistant') {
          msgs[msgs.length - 1] = { ...last, content: last.content + (payload.text ?? '') };
        } else {
          msgs.push({ id: genId(), role: 'assistant', content: payload.text ?? '', timestamp: evt.timestamp });
        }
        break;
      }

      case 'thinking':
        if (last && last.role === 'assistant') {
          msgs[msgs.length - 1] = { ...last, thinking: (last.thinking ?? '') + (payload.thinking ?? '') };
        }
        break;

      case 'tool_call': {
        const tc: ToolCallInfo = {
          toolCallId: payload.toolCallId ?? '',
          title: payload.toolTitle ?? '',
          kind: payload.toolKind ?? '',
          status: payload.toolStatus ?? 'pending',
          locations: payload.toolLocations,
        };
        msgs.push({ id: genId(), role: 'tool_call', content: '', toolCall: tc, timestamp: evt.timestamp });
        break;
      }

      case 'tool_call_update': {
        const idx = findLastIndex(msgs, (m) => m.role === 'tool_call' && m.toolCall?.toolCallId === payload.toolCallId);
        if (idx >= 0) {
          const entry = msgs[idx];
          const tc = entry.toolCall!;
          msgs[idx] = {
            ...entry,
            toolCall: {
              ...tc,
              status: payload.toolStatus ?? tc.status,
              title: payload.toolTitle ?? tc.title,
              content: payload.toolContent ?? tc.content,
            },
          };
        }
        break;
      }

      case 'diff': {
        const diff = { path: payload.diffPath ?? '', oldText: payload.diffOldText ?? '', newText: payload.diffNewText ?? '' };
        const tcIdx = findLastIndex(msgs, (m) => m.role === 'tool_call');
        if (tcIdx >= 0) {
          const entry = msgs[tcIdx];
          msgs[tcIdx] = { ...entry, diffs: [...(entry.diffs ?? []), diff] };
        } else if (last && last.role === 'assistant') {
          msgs[msgs.length - 1] = { ...last, diffs: [...(last.diffs ?? []), diff] };
        }
        break;
      }

      case 'plan':
        msgs.push({ id: genId(), role: 'plan', content: '', plan: payload.planEntries ?? [], timestamp: evt.timestamp });
        break;

      case 'commands':
        commands = payload.commands ?? [];
        break;

      case 'config_options':
        configOptions = payload.configOptions ?? [];
        break;

      case 'title':
        title = payload.title;
        break;

      case 'done':
      case 'error':
        break;
    }
  }

  return { messages: msgs, commands, configOptions, title };
}

export function parseSnapshotJson<T>(json: string | undefined | null): T[] | undefined {
  if (!json) return undefined;
  try {
    const parsed = JSON.parse(json);
    return Array.isArray(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

export const useChatStore = create<ChatState>((set, get) => ({
  sessions: [],
  activeSessionId: null,
  agents: [],
  selectedAgentId: null,
  chatVisible: false,
  historySessions: [],
  historyLoaded: false,
  queuedMessages: {},
  includeIgnoredInMentions: false,
  autoApprove: false,
  restoring: false,
  lastRestoreError: null,

  loadAgents: (agents) => set({ agents, selectedAgentId: agents.find((a) => a.available)?.id ?? null }),

  setSelectedAgent: (id) => set({ selectedAgentId: id }),

  createSession: (session) => {
    const normalized = { ...session, recordId: session.recordId ?? session.id, kind: session.kind ?? 'live' };
    set((state) => ({
      sessions: [...state.sessions.filter((s) => s.id !== normalized.id && s.recordId !== normalized.recordId), normalized],
      activeSessionId: normalized.id,
    }));
  },

  setActiveSession: (id) => set({ activeSessionId: id }),

  addMessage: (sessionId, message) => {
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => {
        const updated = { ...s, messages: [...s.messages, message] };
        if (message.role === 'user' && s.messages.filter((m) => m.role === 'user').length === 0) {
          updated.title = message.content.slice(0, 60).replace(/\n/g, ' ');
        }
        return updated;
      }),
    }));
    if (message.role === 'user') {
      const session = findSessionByIdentity(get().sessions, sessionId);
      saveChatMessage({
        sessionId: session?.recordId ?? sessionId,
        agentId: session?.agentId,
        title: session?.title,
        role: message.role,
        content: message.content,
        context: message.context,
      }).catch(() => {});
    }
  },

  appendToLastMessage: (sessionId, chunk) =>
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => {
        const msgs = [...s.messages];
        const last = msgs[msgs.length - 1];
        if (last && last.role === 'assistant') {
          msgs[msgs.length - 1] = { ...last, content: last.content + chunk };
        }
        return { ...s, messages: msgs };
      }),
    })),

  handleChatEvent: (sessionId, event) => {
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => {
        const msgs = [...s.messages];
        const last = msgs[msgs.length - 1];
        const genId = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 6);

        switch (event.type) {
          case 'text': {
            if (last && last.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, content: last.content + (event.text ?? '') };
            } else {
              msgs.push({ id: genId(), role: 'assistant', content: event.text ?? '', timestamp: Date.now() });
            }
            break;
          }

          case 'thinking':
            if (last && last.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, thinking: (last.thinking ?? '') + (event.thinking ?? '') };
            }
            break;

          case 'tool_call': {
            const tc: ToolCallInfo = {
              toolCallId: event.toolCallId ?? '',
              title: event.toolTitle ?? '',
              kind: event.toolKind ?? '',
              status: event.toolStatus ?? 'pending',
              locations: event.toolLocations,
            };
            msgs.push({ id: genId(), role: 'tool_call', content: '', toolCall: tc, timestamp: Date.now() });
            break;
          }

          case 'tool_call_update': {
            const idx = findLastIndex(msgs, (m: ChatMessage) => m.role === 'tool_call' && m.toolCall?.toolCallId === event.toolCallId);
            if (idx >= 0) {
              const entry = msgs[idx];
              const tc = entry.toolCall!;
              msgs[idx] = {
                ...entry,
                toolCall: {
                  ...tc,
                  status: event.toolStatus ?? tc.status,
                  title: event.toolTitle ?? tc.title,
                  content: event.toolContent ?? tc.content,
                },
              };
            }
            break;
          }

          case 'diff': {
            const diff = { path: event.diffPath ?? '', oldText: event.diffOldText ?? '', newText: event.diffNewText ?? '' };
            const tcIdx = findLastIndex(msgs, (m: ChatMessage) => m.role === 'tool_call');
            if (tcIdx >= 0) {
              const entry = msgs[tcIdx];
              msgs[tcIdx] = { ...entry, diffs: [...(entry.diffs ?? []), diff] };
            } else if (last && last.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, diffs: [...(last.diffs ?? []), diff] };
            }
            break;
          }

          case 'plan':
            msgs.push({ id: genId(), role: 'plan', content: '', plan: event.planEntries ?? [], timestamp: Date.now() });
            break;

          case 'commands':
            return { ...s, commands: event.commands ?? [], messages: msgs };

          case 'config_options':
            return { ...s, configOptions: event.configOptions ?? [], messages: msgs };

          case 'title':
            return { ...s, title: event.title ?? s.title, messages: msgs };

          case 'permission_request':
            return {
              ...s,
              messages: msgs,
              pendingPermission: {
                permissionId: event.permissionId ?? '',
                title: event.permissionTitle ?? '',
                toolCallId: event.toolCallId,
                toolKind: event.toolKind,
                options: event.permissionOptions ?? [],
              },
            };

          case 'done':
            return { ...s, messages: msgs, status: 'idle', pendingPermission: undefined };

          case 'error': {
            if (last && last.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, content: last.content + `\n\n⚠️ ${event.error ?? 'Unknown error'}` };
            }
            return { ...s, messages: msgs, status: 'error', pendingPermission: undefined };
          }
        }

        return { ...s, messages: msgs };
      }),
    }));
  },

  finalizeAssistantMessage: (sessionId) => {
    const session = findSessionByIdentity(get().sessions, sessionId);
    if (!session) return;
    const lastMsg = session.messages[session.messages.length - 1];
    if (lastMsg && lastMsg.role === 'assistant' && lastMsg.content) {
      saveChatMessage({
        sessionId: session.recordId ?? sessionId,
        agentId: session.agentId,
        title: session.title,
        role: 'assistant',
        content: lastMsg.content,
      }).catch(() => {});
    }
  },

  setSessionStatus: (sessionId, status) =>
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => ({ ...s, status })),
    })),

  setSessionKind: (sessionId, kind) =>
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => ({ ...s, kind })),
    })),

  toggleChat: () => set((state) => ({ chatVisible: !state.chatVisible })),

  deleteSession: (id) =>
    set((state) => {
      const next = state.sessions.filter((s) => s.id !== id);
      return {
        sessions: next,
        activeSessionId: state.activeSessionId === id ? (next[0]?.id ?? null) : state.activeSessionId,
      };
    }),

  loadHistory: async (workDir?: string) => {
    try {
      const sessions = await getChatHistory(workDir);
      set({ historySessions: sessions, historyLoaded: true });
    } catch {
      set({ historyLoaded: true });
    }
  },

  loadHistorySession: async (sessionId: string) => {
    const existing = findSessionByIdentity(get().sessions, sessionId);
    if (existing) {
      set({ activeSessionId: existing.id, lastRestoreError: null });
      return;
    }

    try {
      const historyEntry = get().historySessions.find((h) => h.id === sessionId);
      let liveSessionId = sessionId;
      let kind: ChatSessionKind = 'archived';
      let acpSessionId: string | undefined;

      if (historyEntry?.workDir && historyEntry.agentId && historyEntry.acpSessionId) {
        set((state) => ({
          sessions: [...state.sessions.filter((s) => s.id !== sessionId && s.recordId !== sessionId), {
            id: sessionId,
            recordId: sessionId,
            agentId: historyEntry.agentId,
            title: fallbackTitle(historyEntry.agentId, historyEntry.title),
            messages: [],
            status: 'connecting',
            createdAt: historyEntry.createdAt ?? Date.now(),
            kind: 'resumable',
            acpSessionId: historyEntry.acpSessionId,
          }],
        }));
        try {
          const resumed = await resumeChatSession(sessionId, historyEntry.agentId, historyEntry.workDir, historyEntry.acpSessionId);
          if ('id' in resumed) {
            liveSessionId = resumed.id;
            kind = 'resumable';
            acpSessionId = historyEntry.acpSessionId;
          } else {
            kind = 'archived';
          }
        } catch {
          kind = 'archived';
        }
      }

      const sessionState = await getChatSessionState(sessionId);
      const replayed = replayTranscriptToMessages(sessionState.events);
      const snapshotCommands = parseSnapshotJson<SlashCommandInfo>(sessionState.snapshot?.commandsJson);
      const snapshotConfig = parseSnapshotJson<ConfigOptionInfo>(sessionState.snapshot?.configOptsJson);
      const session: ChatSessionInfo = {
        id: liveSessionId,
        recordId: sessionId,
        agentId: historyEntry?.agentId ?? 'unknown',
        title: replayed.title ?? fallbackTitle(historyEntry?.agentId ?? 'unknown', historyEntry?.title),
        messages: replayed.messages,
        status: kind === 'resumable' ? 'connecting' : 'idle',
        createdAt: historyEntry?.createdAt ?? Date.now(),
        kind,
        acpSessionId,
        commands: replayed.commands ?? snapshotCommands,
        configOptions: replayed.configOptions ?? snapshotConfig,
      };
      set((state) => {
        const idsToRemove = new Set([sessionId, liveSessionId]);
        return {
          sessions: [...state.sessions.filter((s) => !idsToRemove.has(s.id) && !idsToRemove.has(s.recordId)), session],
          activeSessionId: session.id,
          lastRestoreError: kind === 'archived' ? state.lastRestoreError : null,
        };
      });
    } catch (err) {
      console.error(`Failed to load history session ${sessionId}:`, err);
    }
  },

  deleteHistorySession: async (sessionId: string) => {
    try {
      await deleteChatHistory(sessionId);
      set((state) => ({
        historySessions: state.historySessions.filter((h) => h.id !== sessionId),
      }));
    } catch {
    }
  },

  restoreSessionForProject: async (projectPath, preferredSessionId) => {
    if (!projectPath) return;

    set({ restoring: true, lastRestoreError: null });
    try {
      if (!get().historyLoaded) {
        await get().loadHistory(projectPath);
      }

      const restore = await getRestorableChatSession(projectPath, preferredSessionId);
      if (!restore?.found || !restore.sessionId) {
        set({ restoring: false });
        return;
      }

      const targetSessionId = restore.sessionId;

      if (preferredSessionId && preferredSessionId !== targetSessionId) {
        set({
          restoring: false,
          lastRestoreError: {
            sessionId: preferredSessionId,
            reason: restore.resumeError ?? `Preferred session ${preferredSessionId} not restorable, got ${targetSessionId} instead`,
          },
        });
        return;
      }

      const existing = findSessionByIdentity(get().sessions, targetSessionId);
      if (existing) {
        set({ activeSessionId: existing.id, restoring: false, lastRestoreError: null });
        return;
      }

      let liveSessionId = targetSessionId;
      let kind: ChatSessionKind = 'archived';
      let acpSessionId: string | undefined = restore.acpSessionId;
      const restoreAgentId = restore.agentId;

      if (restore.isLive) {
        kind = 'live';
      } else if (restore.canResume && restoreAgentId && restore.acpSessionId) {
        set((state) => ({
          sessions: [...state.sessions.filter((s) => s.id !== targetSessionId && s.recordId !== targetSessionId), {
            id: targetSessionId,
            recordId: targetSessionId,
            agentId: restoreAgentId,
            title: fallbackTitle(restoreAgentId, restore.title),
            messages: [],
            status: 'connecting',
            createdAt: Date.now(),
            kind: 'resumable',
            acpSessionId: restore.acpSessionId,
          }],
        }));
        try {
          const resumed = await resumeChatSession(targetSessionId, restoreAgentId, restore.workDir ?? projectPath, restore.acpSessionId);
          if ('id' in resumed) {
            liveSessionId = resumed.id;
            kind = 'resumable';
            acpSessionId = restore.acpSessionId;
          } else {
            kind = 'archived';
          }
        } catch (resumeErr) {
          kind = 'archived';
        }
      }

      const sessionState = await getChatSessionState(targetSessionId).catch(() => null);
      const historyEntry = get().historySessions.find((h) => h.id === targetSessionId);

      const replayed = sessionState ? replayTranscriptToMessages(sessionState.events) : { messages: [] as ChatMessage[] };
      const snapshotCommands = sessionState ? parseSnapshotJson<SlashCommandInfo>(sessionState.snapshot?.commandsJson) : undefined;
      const snapshotConfig = sessionState ? parseSnapshotJson<ConfigOptionInfo>(sessionState.snapshot?.configOptsJson) : undefined;

      const session: ChatSessionInfo = {
        id: liveSessionId,
        recordId: targetSessionId,
        agentId: restore.agentId ?? historyEntry?.agentId ?? 'unknown',
        title: replayed.title ?? fallbackTitle(restore.agentId ?? historyEntry?.agentId ?? 'unknown', restore.title ?? historyEntry?.title),
        messages: replayed.messages,
        status: kind === 'resumable' ? 'connecting' : 'idle',
        createdAt: historyEntry?.createdAt ?? Date.now(),
        kind,
        acpSessionId,
        commands: replayed.commands ?? snapshotCommands,
        configOptions: replayed.configOptions ?? snapshotConfig,
      };

      set((state) => {
        const idsToRemove = new Set([targetSessionId, liveSessionId]);
        return {
          sessions: [...state.sessions.filter((s) => !idsToRemove.has(s.id) && !idsToRemove.has(s.recordId)), session],
          activeSessionId: session.id,
          restoring: false,
        };
      });
    } catch (err) {
      set({
        restoring: false,
        lastRestoreError: {
          sessionId: preferredSessionId ?? 'unknown',
          reason: `Restore failed: ${err instanceof Error ? err.message : String(err)}`,
        },
      });
    }
  },

  enqueueMessage: (sessionId, msg) =>
    set((state) => ({
      queuedMessages: {
        ...state.queuedMessages,
        [sessionId]: [...(state.queuedMessages[sessionId] ?? []), msg],
      },
    })),

  dequeueMessage: (sessionId) => {
    const queue = get().queuedMessages[sessionId];
    if (!queue || queue.length === 0) return undefined;
    const [first, ...rest] = queue;
    set((state) => ({
      queuedMessages: { ...state.queuedMessages, [sessionId]: rest },
    }));
    return first;
  },

  removeQueuedMessage: (sessionId, msgId) =>
    set((state) => ({
      queuedMessages: {
        ...state.queuedMessages,
        [sessionId]: (state.queuedMessages[sessionId] ?? []).filter((m) => m.id !== msgId),
      },
    })),

  editQueuedMessage: (sessionId, msgId, content) =>
    set((state) => ({
      queuedMessages: {
        ...state.queuedMessages,
        [sessionId]: (state.queuedMessages[sessionId] ?? []).map((m) =>
          m.id === msgId ? { ...m, content } : m
        ),
      },
    })),

  reorderQueuedMessages: (sessionId, fromIdx, toIdx) =>
    set((state) => {
      const queue = [...(state.queuedMessages[sessionId] ?? [])];
      if (fromIdx < 0 || fromIdx >= queue.length || toIdx < 0 || toIdx >= queue.length) return state;
      const [item] = queue.splice(fromIdx, 1);
      queue.splice(toIdx, 0, item);
      return { queuedMessages: { ...state.queuedMessages, [sessionId]: queue } };
    }),

  clearQueue: (sessionId) =>
    set((state) => ({
      queuedMessages: { ...state.queuedMessages, [sessionId]: [] },
    })),

  toggleIncludeIgnored: () =>
    set((state) => ({ includeIgnoredInMentions: !state.includeIgnoredInMentions })),

  toggleAutoApprove: () =>
    set((state) => ({ autoApprove: !state.autoApprove })),
}));
