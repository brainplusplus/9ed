import { create } from 'zustand';
import type { ChatAgent, ChatMessage, ChatSessionInfo } from '../types';

type ChatState = {
  sessions: ChatSessionInfo[];
  activeSessionId: string | null;
  agents: ChatAgent[];
  chatVisible: boolean;

  loadAgents: (agents: ChatAgent[]) => void;
  createSession: (session: ChatSessionInfo) => void;
  setActiveSession: (id: string | null) => void;
  addMessage: (sessionId: string, message: ChatMessage) => void;
  appendToLastMessage: (sessionId: string, chunk: string) => void;
  setSessionStatus: (sessionId: string, status: ChatSessionInfo['status']) => void;
  toggleChat: () => void;
  deleteSession: (id: string) => void;
};

function updateSession(sessions: ChatSessionInfo[], id: string, updater: (s: ChatSessionInfo) => ChatSessionInfo): ChatSessionInfo[] {
  return sessions.map((s) => (s.id === id ? updater(s) : s));
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: [],
  activeSessionId: null,
  agents: [],
  chatVisible: false,

  loadAgents: (agents) => set({ agents }),

  createSession: (session) =>
    set((state) => ({
      sessions: [...state.sessions, session],
      activeSessionId: session.id,
    })),

  setActiveSession: (id) => set({ activeSessionId: id }),

  addMessage: (sessionId, message) =>
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => ({
        ...s,
        messages: [...s.messages, message],
      })),
    })),

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
}));
