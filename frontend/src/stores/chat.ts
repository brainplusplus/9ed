import { create } from 'zustand';
import type { ChatAgent, ChatMessage, ChatSessionInfo, ChatEvent, HistorySessionRecord, ToolCallInfo } from '../types';
import { getChatHistory, getChatSessionMessages, saveChatMessage, deleteChatHistory } from '../api';

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
  chatVisible: boolean;
  historySessions: HistorySessionRecord[];
  historyLoaded: boolean;
  queuedMessages: Record<string, QueuedMessage[]>;
  includeIgnoredInMentions: boolean;
  autoApprove: boolean;

  loadAgents: (agents: ChatAgent[]) => void;
  createSession: (session: ChatSessionInfo) => void;
  setActiveSession: (id: string | null) => void;
  addMessage: (sessionId: string, message: ChatMessage) => void;
  appendToLastMessage: (sessionId: string, chunk: string) => void;
  handleChatEvent: (sessionId: string, event: ChatEvent) => void;
  finalizeAssistantMessage: (sessionId: string) => void;
  setSessionStatus: (sessionId: string, status: ChatSessionInfo['status']) => void;
  toggleChat: () => void;
  deleteSession: (id: string) => void;
  loadHistory: () => Promise<void>;
  loadHistorySession: (sessionId: string) => Promise<void>;
  deleteHistorySession: (sessionId: string) => Promise<void>;
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

function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
  for (let i = arr.length - 1; i >= 0; i--) {
    if (predicate(arr[i])) return i;
  }
  return -1;
}

export const useChatStore = create<ChatState>((set, get) => ({
  sessions: [],
  activeSessionId: null,
  agents: [],
  chatVisible: false,
  historySessions: [],
  historyLoaded: false,
  queuedMessages: {},
  includeIgnoredInMentions: false,
  autoApprove: false,

  loadAgents: (agents) => set({ agents }),

  createSession: (session) => {
    set((state) => ({
      sessions: [...state.sessions, session],
      activeSessionId: session.id,
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
      const session = get().sessions.find((s) => s.id === sessionId);
      saveChatMessage({
        sessionId,
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
    const session = get().sessions.find((s) => s.id === sessionId);
    if (!session) return;
    const lastMsg = session.messages[session.messages.length - 1];
    if (lastMsg && lastMsg.role === 'assistant' && lastMsg.content) {
      saveChatMessage({
        sessionId,
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

  toggleChat: () => set((state) => ({ chatVisible: !state.chatVisible })),

  deleteSession: (id) =>
    set((state) => {
      const next = state.sessions.filter((s) => s.id !== id);
      return {
        sessions: next,
        activeSessionId: state.activeSessionId === id ? (next[0]?.id ?? null) : state.activeSessionId,
      };
    }),

  loadHistory: async () => {
    try {
      const sessions = await getChatHistory();
      set({ historySessions: sessions, historyLoaded: true });
    } catch {
      set({ historyLoaded: true });
    }
  },

  loadHistorySession: async (sessionId: string) => {
    const existing = get().sessions.find((s) => s.id === sessionId);
    if (existing) {
      set({ activeSessionId: sessionId });
      return;
    }

    try {
      const messages = await getChatSessionMessages(sessionId);
      const historyEntry = get().historySessions.find((h) => h.id === sessionId);
      const session: ChatSessionInfo = {
        id: sessionId,
        agentId: historyEntry?.agentId ?? 'unknown',
        title: historyEntry?.title ?? 'History',
        messages: messages.map((m) => ({
          id: m.id,
          role: m.role,
          content: m.content,
          context: m.contextFile ? {
            filePath: m.contextFile,
            startLine: m.contextStartLine ?? 0,
            endLine: m.contextEndLine ?? 0,
            selectedCode: m.contextCode ?? '',
            language: m.contextLanguage ?? '',
          } : undefined,
          timestamp: m.timestamp,
        })),
        status: 'idle',
        createdAt: historyEntry?.createdAt ?? Date.now(),
      };
      set((state) => ({
        sessions: [...state.sessions, session],
        activeSessionId: sessionId,
      }));
    } catch {
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
