import { create } from 'zustand';
import type { ChatAgent, ChatMessage, ChatSessionInfo, HistorySessionRecord } from '../types';
import { getChatHistory, getChatSessionMessages, saveChatMessage, deleteChatHistory } from '../api';

type ChatState = {
  sessions: ChatSessionInfo[];
  activeSessionId: string | null;
  agents: ChatAgent[];
  chatVisible: boolean;
  historySessions: HistorySessionRecord[];
  historyLoaded: boolean;

  loadAgents: (agents: ChatAgent[]) => void;
  createSession: (session: ChatSessionInfo) => void;
  setActiveSession: (id: string | null) => void;
  addMessage: (sessionId: string, message: ChatMessage) => void;
  appendToLastMessage: (sessionId: string, chunk: string) => void;
  finalizeAssistantMessage: (sessionId: string) => void;
  setSessionStatus: (sessionId: string, status: ChatSessionInfo['status']) => void;
  toggleChat: () => void;
  deleteSession: (id: string) => void;
  loadHistory: () => Promise<void>;
  loadHistorySession: (sessionId: string) => Promise<void>;
  deleteHistorySession: (sessionId: string) => Promise<void>;
};

function updateSession(sessions: ChatSessionInfo[], id: string, updater: (s: ChatSessionInfo) => ChatSessionInfo): ChatSessionInfo[] {
  return sessions.map((s) => (s.id === id ? updater(s) : s));
}

export const useChatStore = create<ChatState>((set, get) => ({
  sessions: [],
  activeSessionId: null,
  agents: [],
  chatVisible: false,
  historySessions: [],
  historyLoaded: false,

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
      sessions: updateSession(state.sessions, sessionId, (s) => ({
        ...s,
        messages: [...s.messages, message],
      })),
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
}));
