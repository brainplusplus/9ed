import { create } from 'zustand';
import type { BrowserElementCapture, BrowserElementSelection, BrowserSelectionMode, ChatAgent, ChatMessage, ChatSessionInfo, ChatEvent, ChatSessionKind, HistoryMessageRecord, HistorySessionRecord, ToolCallInfo, TranscriptEventRecord, SlashCommandInfo, ConfigOptionInfo, ChatDebugEntry } from '../types';
import { getChatHistory, getChatSessionState, saveChatMessage, deleteChatHistory, getRestorableChatSession, resumeChatSession } from '../api';
import { getTerminalConnection } from '../terminalConnection';
import { getTerminalHandle } from '../terminalRegistry';
import { useWorkspaceStore } from './workspace';

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

const resumeRequests = new Map<string, Promise<boolean>>();
const CHAT_AGENTS_STORAGE_KEY = '9ed.chatAgents.v1';
const recentlyRoutedTerminalCommands = new Map<string, number>();
const TERMINAL_COMMAND_DEDUPE_MS = 1500;
const CHAT_DEBUG_MAX_ENTRIES = 120;

function chatStorage(): Storage | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
}

function readStoredAgents(): { agents: ChatAgent[]; selectedAgentId: string | null } {
  const store = chatStorage();
  if (!store) return { agents: [], selectedAgentId: null };
  try {
    const raw = store.getItem(CHAT_AGENTS_STORAGE_KEY);
    if (!raw) return { agents: [], selectedAgentId: null };
    const parsed = JSON.parse(raw) as { agents?: ChatAgent[]; selectedAgentId?: string | null };
    return {
      agents: Array.isArray(parsed.agents) ? parsed.agents : [],
      selectedAgentId: typeof parsed.selectedAgentId === 'string' ? parsed.selectedAgentId : null,
    };
  } catch {
    return { agents: [], selectedAgentId: null };
  }
}

function writeStoredAgents(agents: ChatAgent[], selectedAgentId: string | null): void {
  const store = chatStorage();
  if (!store) return;
  try {
    store.setItem(CHAT_AGENTS_STORAGE_KEY, JSON.stringify({ agents, selectedAgentId }));
  } catch {
  }
}

type ChatState = {
  sessions: ChatSessionInfo[];
  activeSessionId: string | null;
  activeSessionByWorkDir: Record<string, string>;
  agents: ChatAgent[];
  selectedAgentId: string | null;
  chatVisible: boolean;
  historySessions: HistorySessionRecord[];
  historyLoaded: boolean;
  historyWorkDir: string | null;
  queuedMessages: Record<string, QueuedMessage[]>;
  includeIgnoredInMentions: boolean;
  autoApprove: boolean;
  useActiveBrowser: boolean;
  browserSelection: BrowserElementSelection | null;
  browserSelectionMode: BrowserSelectionMode;
  browserSelectionCapture: BrowserElementCapture | null;
  useActiveTerminal: boolean;
  activeTerminalId: string | null;
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
  setSessionStalled: (sessionId: string, stalled: boolean) => void;
  appendSessionDebug: (sessionId: string, entry: Omit<ChatDebugEntry, 'timestamp'> & { timestamp?: number }) => void;
  setSessionKind: (sessionId: string, kind: ChatSessionKind) => void;
  setInputLocked: (sessionId: string, locked: boolean, holder?: string) => void;
  toggleChat: () => void;
  deleteSession: (id: string) => void;
  loadHistory: (workDir?: string) => Promise<void>;
  loadHistorySession: (sessionId: string) => Promise<void>;
  refreshSessionState: (sessionId: string) => Promise<void>;
  resumeSession: (sessionId: string) => Promise<boolean>;
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
  toggleUseActiveBrowser: () => void;
  /**
   * Unified browser-toggle entry point used by every UI surface that toggles
   * the active-browser MCP bridge (AgentPicker/ConfigBar, BrowserPanel,
   * useInspectMode). It routes to a hard restart when there is an active,
   * idle chat session (so the backend sessionOpts stay in sync), and falls
   * back to a frontend-only soft toggle when no session exists or the session
   * cannot be restarted right now. This eliminates the prior state desync
   * where some UI paths soft-toggled while others hard-restarted.
   *
   * Returns true if the requested state was applied (or queued), false if it
   * could not be applied.
   */
  setBrowserEnabled: (enabled: boolean) => Promise<boolean>;
  setBrowserSelection: (selection: BrowserElementSelection | null) => void;
  setBrowserSelectionMode: (mode: BrowserSelectionMode) => void;
  setBrowserSelectionCapture: (capture: BrowserElementCapture | null) => void;
  toggleUseActiveTerminal: () => void;
  setUseActiveTerminal: (enabled: boolean) => void;
  restartActiveSessionForTerminal: (enabled: boolean) => Promise<boolean>;
  restartActiveSessionForBrowser: (enabled: boolean, force?: boolean) => Promise<boolean>;
  setActiveTerminalId: (id: string | null) => void;
};

function updateSession(sessions: ChatSessionInfo[], id: string, updater: (s: ChatSessionInfo) => ChatSessionInfo): ChatSessionInfo[] {
  return sessions.map((s) => (s.id === id ? updater(s) : s));
}

function findSessionByIdentity(sessions: ChatSessionInfo[], identity: string): ChatSessionInfo | undefined {
  return sessions.find((s) => s.id === identity || s.recordId === identity);
}

export function normalizeWorkDir(path?: string | null): string | null {
  if (!path) return null;
  const normalized = path.replace(/\\/g, '/').replace(/\/+$/, '');
  return /^[a-z]:/i.test(normalized) ? normalized.toLowerCase() : normalized;
}

export function sessionBelongsToWorkDir(session: Pick<ChatSessionInfo, 'workDir'>, workDir?: string | null): boolean {
  return normalizeWorkDir(session.workDir) === normalizeWorkDir(workDir);
}

function findProjectSession(sessions: ChatSessionInfo[], projectPath: string): ChatSessionInfo | undefined {
  return [...sessions].reverse().find((session) => sessionBelongsToWorkDir(session, projectPath));
}

function activateSessionState(state: ChatState, id: string | null): Partial<ChatState> {
  if (!id) return { activeSessionId: null };

  const session = findSessionByIdentity(state.sessions, id);
  const workDir = normalizeWorkDir(session?.workDir);
  if (!workDir) return { activeSessionId: id };

  return {
    activeSessionId: id,
    activeSessionByWorkDir: {
      ...state.activeSessionByWorkDir,
      [workDir]: session?.id ?? id,
    },
  };
}

function shouldEnableTerminalForAgent(state: Pick<ChatState, 'useActiveTerminal' | 'activeTerminalId'>): boolean {
  return state.useActiveTerminal && !!state.activeTerminalId;
}

function activeBrowserTabForWorkDir(workDir?: string | null): string | undefined {
  const normalized = normalizeWorkDir(workDir);
  if (!normalized) return undefined;
  const project = useWorkspaceStore.getState().projects.find((entry) => normalizeWorkDir(entry.path) === normalized);
  return project?.activeBrowserTabId ?? undefined;
}

function activeBrowserStateForWorkDir(state: Pick<ChatState, 'useActiveBrowser' | 'browserSelection' | 'browserSelectionMode' | 'browserSelectionCapture'>, workDir?: string | null) {
  const tabId = activeBrowserTabForWorkDir(workDir);
  const enabled = state.useActiveBrowser && !!tabId;
  return {
    enabled,
    tabId,
    selection: enabled ? state.browserSelection : null,
    selectionMode: state.browserSelectionMode,
    selectionCapture: enabled ? state.browserSelectionCapture : null,
  };
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

function routeCommandToSessionTerminal(getState: () => ChatState, sessionId: string, command: string): void {
  const cmd = command.trim();
  if (!cmd) return;

  const state = getState();
  const session = findSessionByIdentity(state.sessions, sessionId);
  const terminalId = session?.terminalId ?? (state.activeSessionId === sessionId ? state.activeTerminalId : null);
  const terminalEnabled = session?.useActiveTerminal ?? (state.activeSessionId === sessionId ? state.useActiveTerminal : false);
  if (!terminalEnabled || !terminalId) return;

  const now = Date.now();
  const lastRoutedAt = recentlyRoutedTerminalCommands.get(cmd) ?? 0;
  if (now - lastRoutedAt < TERMINAL_COMMAND_DEDUPE_MS) return;
  recentlyRoutedTerminalCommands.set(cmd, now);

  const handle = getTerminalHandle(terminalId);
  if (handle) {
    handle.sendCommand(cmd);
    return;
  }

  getTerminalConnection(terminalId).sendInput(cmd + '\r');
}

function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
  for (let i = arr.length - 1; i >= 0; i--) {
    if (predicate(arr[i])) return i;
  }
  return -1;
}

function appendDebugEntry(entries: ChatDebugEntry[] | undefined, entry: Omit<ChatDebugEntry, 'timestamp'> & { timestamp?: number }): ChatDebugEntry[] {
  const nextEntry: ChatDebugEntry = {
    timestamp: entry.timestamp ?? Date.now(),
    source: entry.source,
    level: entry.level,
    message: entry.message,
  };
  const next = [...(entries ?? []), nextEntry];
  if (next.length <= CHAT_DEBUG_MAX_ENTRIES) return next;
  return next.slice(next.length - CHAT_DEBUG_MAX_ENTRIES);
}

type ReplayResult = {
  messages: ChatMessage[];
  commands?: SlashCommandInfo[];
  configOptions?: ConfigOptionInfo[];
  title?: string;
  contextWindow?: number;
  contextUsed?: number;
  costAmount?: number;
  costCurrency?: string;
  terminalState?: 'idle' | 'error' | 'streaming';
  lastEventAt?: number;
};

export function replayTranscriptToMessages(events: TranscriptEventRecord[]): ReplayResult {
  const msgs: ChatMessage[] = [];
  let commands: SlashCommandInfo[] | undefined;
  let configOptions: ConfigOptionInfo[] | undefined;
  let title: string | undefined;
  let contextWindow: number | undefined;
  let contextUsed: number | undefined;
  let costAmount: number | undefined;
  let costCurrency: string | undefined;
  let terminalState: ReplayResult['terminalState'];
  let lastEventAt: number | undefined;
  let assistantSegmentClosed = false;
  const genId = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 6);

  for (const evt of events) {
    lastEventAt = Math.max(lastEventAt ?? 0, evt.timestamp);
    let payload: ChatEvent;
    try {
      payload = JSON.parse(evt.payloadJson) as ChatEvent;
    } catch {
      continue;
    }

    const last = msgs[msgs.length - 1];

      switch (evt.kind) {
      case 'text': {
        if (last && last.role === 'assistant' && !assistantSegmentClosed) {
          msgs[msgs.length - 1] = { ...last, content: last.content + (payload.text ?? '') };
        } else {
          msgs.push({ id: genId(), role: 'assistant', content: payload.text ?? '', timestamp: evt.timestamp });
        }
        assistantSegmentClosed = false;
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
          rawInput: payload.toolRawInput,
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

      case 'session_info':
      case 'usage_update':
        if (payload.contextWindow !== undefined) contextWindow = payload.contextWindow;
        if (payload.contextUsed !== undefined) contextUsed = payload.contextUsed;
        if (payload.costAmount !== undefined) costAmount = payload.costAmount;
        if (payload.costCurrency !== undefined) costCurrency = payload.costCurrency;
        if (payload.title) title = payload.title;
        break;

      case 'done':
        assistantSegmentClosed = true;
        terminalState = 'idle';
        break;
      case 'error':
        assistantSegmentClosed = true;
        terminalState = 'error';
        break;
    }
  }

  return { messages: msgs, commands, configOptions, title, contextWindow, contextUsed, costAmount, costCurrency, terminalState, lastEventAt };
}

function historyMessageToChatMessage(message: HistoryMessageRecord): ChatMessage {
  const context = message.contextFile ? {
    filePath: message.contextFile,
    startLine: message.contextStartLine ?? 0,
    endLine: message.contextEndLine ?? 0,
    selectedCode: message.contextCode ?? '',
    language: message.contextLanguage ?? '',
  } : undefined;

  return {
    id: message.id,
    role: message.role,
    content: message.content,
    context,
    timestamp: message.timestamp,
  };
}

function replaySessionState(messages: HistoryMessageRecord[], events: TranscriptEventRecord[]): ReplayResult {
  const replayed = replayTranscriptToMessages(events);
  const hasAssistantTextEvents = events.some((event) => event.kind === 'text');
  const persisted = hasAssistantTextEvents
    ? messages.filter((message) => message.role === 'user').map(historyMessageToChatMessage)
    : messages.map(historyMessageToChatMessage);

  return {
    ...replayed,
    messages: [...persisted, ...replayed.messages].sort((a, b) => a.timestamp - b.timestamp),
  };
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
  activeSessionByWorkDir: {},
  agents: readStoredAgents().agents,
  selectedAgentId: readStoredAgents().selectedAgentId,
  chatVisible: false,
  historySessions: [],
  historyLoaded: false,
  historyWorkDir: null,
  queuedMessages: {},
  includeIgnoredInMentions: false,
  autoApprove: false,
  useActiveBrowser: false,
  browserSelectionMode: 'detail',
  useActiveTerminal: false,
  activeTerminalId: null,
  browserSelection: null,
  browserSelectionCapture: null,
  restoring: false,
  lastRestoreError: null,

  loadAgents: (agents) => set((state) => {
    const nextSelected = agents.some((agent) => agent.id === state.selectedAgentId && agent.available)
      ? state.selectedAgentId
      : (agents.find((agent) => agent.available)?.id ?? null);
    writeStoredAgents(agents, nextSelected);
    return { agents, selectedAgentId: nextSelected };
  }),

  setSelectedAgent: (id) => set((state) => {
    writeStoredAgents(state.agents, id);
    return { selectedAgentId: id };
  }),

  createSession: (session) => {
      const normalized = {
        ...session,
        recordId: session.recordId ?? session.id,
        kind: session.kind ?? 'live',
        lastEventAt: session.lastEventAt ?? session.createdAt,
        stalled: false,
        debugEntries: session.debugEntries ?? [],
      };
    set((state) => {
      const nextState = {
        ...state,
        sessions: [...state.sessions.filter((s) => s.id !== normalized.id && s.recordId !== normalized.recordId), normalized],
      };
      return {
        sessions: nextState.sessions,
        ...activateSessionState(nextState, normalized.id),
      };
    });
  },

  setActiveSession: (id) => set((state) => activateSessionState(state, id)),

  addMessage: (sessionId, message) => {
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => {
        const updated = {
          ...s,
          messages: [...s.messages, message],
          lastEventAt: Math.max(s.lastEventAt ?? 0, message.timestamp ?? Date.now()),
        };
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
        workDir: session?.workDir,
        acpSessionId: session?.acpSessionId,
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
        const eventAt = Date.now();

        switch (event.type) {
          case 'text': {
            if (last && last.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, content: last.content + (event.text ?? '') };
            } else {
              msgs.push({ id: genId(), role: 'assistant', content: event.text ?? '', timestamp: eventAt });
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
              rawInput: event.toolRawInput,
            };
            msgs.push({ id: genId(), role: 'tool_call', content: '', toolCall: tc, timestamp: eventAt });
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
                  rawInput: event.toolRawInput ?? tc.rawInput,
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
            msgs.push({ id: genId(), role: 'plan', content: '', plan: event.planEntries ?? [], timestamp: eventAt });
            break;

          case 'commands':
            return { ...s, commands: event.commands ?? [], messages: msgs, lastEventAt: eventAt, stalled: false };

          case 'config_options':
            return { ...s, configOptions: event.configOptions ?? [], messages: msgs, lastEventAt: eventAt, stalled: false };

          case 'title':
            return { ...s, title: event.title ?? s.title, messages: msgs, lastEventAt: eventAt, stalled: false };

          case 'session_info':
          case 'usage_update': {
            const patches: Partial<ChatSessionInfo> = { messages: msgs };
            if (event.title) patches.title = event.title;
            if (event.contextWindow !== undefined) patches.contextWindow = event.contextWindow;
            if (event.contextUsed !== undefined) patches.contextUsed = event.contextUsed;
            if (event.costAmount !== undefined) patches.costAmount = event.costAmount;
            if (event.costCurrency !== undefined) patches.costCurrency = event.costCurrency;
            return { ...s, ...patches, lastEventAt: eventAt, stalled: false };
          }

          case 'permission_request':
            return {
              ...s,
              messages: msgs,
              lastEventAt: eventAt,
              stalled: false,
              pendingPermission: {
                permissionId: event.permissionId ?? '',
                title: event.permissionTitle ?? '',
                toolCallId: event.toolCallId,
                toolKind: event.toolKind,
                options: event.permissionOptions ?? [],
              },
            };

          case 'done': {
            // When a browser toggle was queued while the session was busy
            // (restartActiveSessionForBrowser deferred it via
            // pendingBrowserToggle), fire the deferred restart now that the
            // session has returned to idle.
            if (s.pendingBrowserToggle !== undefined && s.pendingBrowserToggle !== null) {
              const desired = s.pendingBrowserToggle;
              // Defer so the state update (status -> idle) commits first;
              // restartActiveSessionForBrowser re-checks status itself.
              queueMicrotask(() => {
                get().restartActiveSessionForBrowser(desired, true);
              });
            }
            return { ...s, messages: msgs, status: 'idle', pendingPermission: undefined, pendingBrowserToggle: null, lastEventAt: eventAt, stalled: false };
          }

          case 'terminal_execute': {
            if (event.terminalCommand) {
              routeCommandToSessionTerminal(get, sessionId, event.terminalCommand);
            }
            const toolCallId = event.toolCallId || `active-terminal-${Date.now().toString(36)}`;
            const idx = findLastIndex(msgs, (m: ChatMessage) => m.role === 'tool_call' && m.toolCall?.toolCallId === toolCallId);
            const toolCall: ToolCallInfo = {
              toolCallId,
              title: event.toolTitle || 'active_terminal_run',
              kind: event.toolKind || 'execute',
              status: 'completed',
              content: event.terminalCommand ? `Sent to active terminal:\n${event.terminalCommand}` : undefined,
              rawInput: event.toolRawInput,
            };
            if (idx >= 0) {
              msgs[idx] = { ...msgs[idx], toolCall: { ...msgs[idx].toolCall!, ...toolCall } };
            } else {
              msgs.push({ id: genId(), role: 'tool_call', content: '', toolCall, timestamp: eventAt });
            }
            return { ...s, messages: msgs, status: 'idle', pendingPermission: undefined, lastEventAt: eventAt, stalled: false };
          }

          case 'input_locked': {
            // ADR-0005 VAL-PTY-007: dedicated input_locked event sets
            // per-pane soft lock state so the UI can disable ChatInput.
            const lockHolder = event.holder || 'unknown';
            return { ...s, inputLocked: true, lockedBy: lockHolder, lastEventAt: eventAt };
          }

          case 'input_unlocked': {
            // ADR-0005 VAL-PTY-007: clear the input soft lock.
            return { ...s, inputLocked: false, lockedBy: undefined, lastEventAt: eventAt };
          }

          case 'error': {
            if (last && last.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, content: last.content + `\n\n⚠️ ${event.error ?? 'Unknown error'}` };
            }
            return { ...s, messages: msgs, status: 'error', pendingPermission: undefined, lastEventAt: eventAt, stalled: false };
          }
        }

        return { ...s, messages: msgs, lastEventAt: eventAt, stalled: false };
      }),
    }));
  },

  finalizeAssistantMessage: () => {},

  setSessionStatus: (sessionId, status) =>
    set((state) => {
      const session = state.sessions.find((s) => s.id === sessionId);
      if (!session || session.status === status) return state;
      return {
        sessions: updateSession(state.sessions, sessionId, (s) => ({ ...s, status, stalled: status === 'streaming' ? s.stalled : false })),
      };
    }),

  setSessionStalled: (sessionId, stalled) =>
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => ({ ...s, stalled })),
    })),

  appendSessionDebug: (sessionId, entry) =>
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => ({
        ...s,
        debugEntries: appendDebugEntry(s.debugEntries, entry),
      })),
    })),

  setSessionKind: (sessionId, kind) =>
    set((state) => {
      const session = state.sessions.find((s) => s.id === sessionId);
      if (!session || session.kind === kind) return state;
      return {
        sessions: updateSession(state.sessions, sessionId, (s) => ({ ...s, kind })),
      };
    }),

  setInputLocked: (sessionId, locked, holder) =>
    set((state) => ({
      sessions: updateSession(state.sessions, sessionId, (s) => ({
        ...s,
        inputLocked: locked,
        lockedBy: locked ? holder : undefined,
      })),
    })),

  toggleChat: () => set((state) => ({ chatVisible: !state.chatVisible })),

  deleteSession: (id) =>
    set((state) => {
      const deleted = state.sessions.find((s) => s.id === id);
      const next = state.sessions.filter((s) => s.id !== id);
      const fallback = deleted?.workDir
        ? findProjectSession(next, deleted.workDir)
        : next[0];
      return {
        sessions: next,
        ...(state.activeSessionId === id ? activateSessionState({ ...state, sessions: next }, fallback?.id ?? null) : {}),
      };
    }),

  loadHistory: async (workDir?: string) => {
    try {
      const sessions = await getChatHistory(workDir);
      set({ historySessions: sessions, historyLoaded: true, historyWorkDir: workDir ?? null });
    } catch {
      set({ historyLoaded: true, historyWorkDir: workDir ?? null });
    }
  },

  loadHistorySession: async (sessionId: string) => {
    const existing = findSessionByIdentity(get().sessions, sessionId);
    if (existing) {
      set((state) => ({
        ...activateSessionState(state, existing.id),
        lastRestoreError: null,
      }));
      return;
    }

    try {
      const historyEntry = get().historySessions.find((h) => h.id === sessionId);
      let liveSessionId = sessionId;
      let kind: ChatSessionKind = 'archived';
      let acpSessionId: string | undefined;

      if (historyEntry?.workDir && historyEntry.agentId) {
        const browserState = activeBrowserStateForWorkDir(get(), historyEntry.workDir);
        set((state) => ({
          sessions: [...state.sessions.filter((s) => s.id !== sessionId && s.recordId !== sessionId), {
            id: sessionId,
            recordId: sessionId,
            agentId: historyEntry.agentId,
            title: fallbackTitle(historyEntry.agentId, historyEntry.title),
            messages: [],
            status: 'connecting',
            createdAt: historyEntry.createdAt ?? Date.now(),
            kind: 'archived',
            workDir: historyEntry.workDir,
            acpSessionId: historyEntry.acpSessionId,
            terminalId: shouldEnableTerminalForAgent(get()) ? get().activeTerminalId ?? undefined : undefined,
            useActiveBrowser: browserState.enabled,
            browserSelection: browserState.selection,
            browserSelectionMode: browserState.selectionMode,
            browserSelectionCapture: browserState.selectionCapture,
          }],
        }));
        try {
          const resumed = await resumeChatSession(
            sessionId,
            historyEntry.agentId,
            historyEntry.workDir,
            historyEntry.acpSessionId,
            shouldEnableTerminalForAgent(get()),
            shouldEnableTerminalForAgent(get()) ? get().activeTerminalId : undefined,
            browserState.enabled,
            browserState.tabId,
          );
          if ('id' in resumed) {
            liveSessionId = resumed.id;
            kind = 'resumable';
            acpSessionId = resumed.acpSessionId ?? historyEntry.acpSessionId;
          } else {
            kind = 'archived';
          }
        } catch {
          kind = 'archived';
        }
      }

      const sessionState = await getChatSessionState(sessionId);
      const replayed = replaySessionState(sessionState.messages ?? [], sessionState.events ?? []);
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
        workDir: historyEntry?.workDir,
        acpSessionId,
        useActiveTerminal: kind === 'resumable' ? shouldEnableTerminalForAgent(get()) : false,
        terminalId: kind === 'resumable' && shouldEnableTerminalForAgent(get()) ? get().activeTerminalId ?? undefined : undefined,
        useActiveBrowser: activeBrowserStateForWorkDir(get(), historyEntry?.workDir).enabled,
        browserSelection: activeBrowserStateForWorkDir(get(), historyEntry?.workDir).selection,
        browserSelectionMode: activeBrowserStateForWorkDir(get(), historyEntry?.workDir).selectionMode,
        browserSelectionCapture: activeBrowserStateForWorkDir(get(), historyEntry?.workDir).selectionCapture,
        commands: replayed.commands ?? snapshotCommands,
        configOptions: replayed.configOptions ?? snapshotConfig,
        contextWindow: replayed.contextWindow,
        contextUsed: replayed.contextUsed,
        costAmount: replayed.costAmount,
        costCurrency: replayed.costCurrency,
      };
      set((state) => {
        const idsToRemove = new Set([sessionId, liveSessionId]);
        const nextSessions = [...state.sessions.filter((s) => !idsToRemove.has(s.id) && !idsToRemove.has(s.recordId)), session];
        const nextState = { ...state, sessions: nextSessions };
        return {
          sessions: nextSessions,
          ...activateSessionState(nextState, session.id),
          lastRestoreError: kind === 'archived' ? state.lastRestoreError : null,
        };
      });
    } catch (err) {
      console.error(`Failed to load history session ${sessionId}:`, err);
      set((state) => ({
        sessions: updateSession(state.sessions, sessionId, (s) => ({ ...s, status: 'idle', kind: 'archived' })),
        lastRestoreError: {
          sessionId,
          reason: err instanceof Error ? err.message : String(err),
        },
      }));
    }
  },

  refreshSessionState: async (sessionId: string) => {
    const session = findSessionByIdentity(get().sessions, sessionId);
    if (!session?.recordId) return;

    try {
      const sessionState = await getChatSessionState(session.recordId);
      const replayed = replaySessionState(sessionState.messages ?? [], sessionState.events ?? []);
      const snapshotCommands = parseSnapshotJson<SlashCommandInfo>(sessionState.snapshot?.commandsJson);
      const snapshotConfig = parseSnapshotJson<ConfigOptionInfo>(sessionState.snapshot?.configOptsJson);
      const latestMessageAt = sessionState.messages.reduce((max, message) => Math.max(max, message.timestamp ?? 0), 0);
      const refreshedLastEventAt = Math.max(
        replayed.lastEventAt ?? 0,
        latestMessageAt,
        sessionState.snapshot?.updatedAt ?? 0,
        sessionState.session?.updatedAt ?? 0,
        session.lastEventAt ?? 0,
      );
      const streamClosed = replayed.terminalState === 'idle' || sessionState.session?.status === 'closed';
      const streamErrored = replayed.terminalState === 'error';
      set((state) => ({
        sessions: updateSession(state.sessions, session.id, (s) => {
          let nextStatus = s.status;
          if (streamErrored && s.status === 'streaming') {
            nextStatus = 'error';
          } else if (streamClosed && s.status === 'streaming') {
            nextStatus = 'idle';
          }
          return {
            ...s,
            title: replayed.title ?? s.title,
            messages: replayed.messages.length > 0 ? replayed.messages : s.messages,
            commands: replayed.commands ?? snapshotCommands ?? s.commands,
            configOptions: replayed.configOptions ?? snapshotConfig ?? s.configOptions,
            contextWindow: replayed.contextWindow ?? s.contextWindow,
            contextUsed: replayed.contextUsed ?? s.contextUsed,
            costAmount: replayed.costAmount ?? s.costAmount,
            costCurrency: replayed.costCurrency ?? s.costCurrency,
            status: nextStatus,
            pendingPermission: nextStatus === 'streaming' ? s.pendingPermission : undefined,
            stalled: false,
            lastEventAt: refreshedLastEventAt || s.lastEventAt,
          };
        }),
      }));
    } catch {
    }
  },

  resumeSession: async (sessionId: string) => {
    const session = findSessionByIdentity(get().sessions, sessionId);
    const recordId = session?.recordId ?? sessionId;
    const requestKey = recordId || sessionId;
    const existingRequest = resumeRequests.get(requestKey);
    if (existingRequest) return existingRequest;

    if (!session?.agentId || !session.workDir) {
      if (session) {
        set((state) => ({
          sessions: updateSession(state.sessions, session.id, (s) => ({ ...s, status: 'error' })),
        }));
      }
      return false;
    }
    const agentId = session.agentId;
    const workDir = session.workDir;
    const currentAcpSessionId = session.acpSessionId;
    const browserState = activeBrowserStateForWorkDir(get(), workDir);

    const request = (async () => {
      set((state) => ({
        sessions: updateSession(state.sessions, session.id, (s) => {
          if (s.status === 'connecting') return s;
          return { ...s, status: 'connecting', kind: 'archived' };
        }),
      }));

      const terminalEnabled = shouldEnableTerminalForAgent(get());
      const terminalId = terminalEnabled ? get().activeTerminalId ?? undefined : undefined;
      const resumed = await resumeChatSession(recordId, agentId, workDir, currentAcpSessionId, terminalEnabled, terminalId, browserState.enabled, browserState.tabId);
      if (!('id' in resumed)) {
        set((state) => ({
          sessions: updateSession(state.sessions, session.id, (s) => ({ ...s, status: 'error' })),
          lastRestoreError: {
            sessionId: recordId,
            reason: resumed.resumeError ?? 'Resume failed',
          },
        }));
        return false;
      }

      const liveSessionId = resumed.id;
      const acpSessionId = resumed.acpSessionId ?? currentAcpSessionId;
      const nextWorkDir = resumed.workDir ?? workDir;
      set((state) => {
        const idsToRemove = new Set([session.id, liveSessionId]);
        const nextSession: ChatSessionInfo = {
          ...session,
          id: liveSessionId,
          recordId,
          status: 'connecting',
          kind: 'resumable',
          workDir: nextWorkDir,
          acpSessionId,
          useActiveTerminal: terminalEnabled,
          terminalId,
          useActiveBrowser: browserState.enabled,
          browserSelection: browserState.selection,
          browserSelectionMode: browserState.selectionMode,
          browserSelectionCapture: browserState.selectionCapture,
        };
        const nextSessions = [...state.sessions.filter((s) => !idsToRemove.has(s.id) && s.recordId !== recordId), nextSession];
        const nextState = { ...state, sessions: nextSessions };
        return {
          sessions: nextSessions,
          ...(state.activeSessionId === session.id ? activateSessionState(nextState, liveSessionId) : {}),
        };
      });
      return true;
    })().catch(() => {
      set((state) => ({
        sessions: updateSession(state.sessions, session.id, (s) => ({ ...s, status: 'error' })),
        lastRestoreError: {
          sessionId: recordId,
          reason: 'Resume request failed',
        },
      }));
      return false;
    }).finally(() => {
      resumeRequests.delete(requestKey);
    });

    resumeRequests.set(requestKey, request);
    return request;
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
      if (!get().historyLoaded || get().historyWorkDir !== projectPath) {
        await get().loadHistory(projectPath);
      }

      let restore = await getRestorableChatSession(projectPath, preferredSessionId);
      if (preferredSessionId && (!restore?.found || !restore.sessionId)) {
        restore = await getRestorableChatSession(projectPath);
      }
      if (!restore?.found || !restore.sessionId) {
        const existingProjectSession = findProjectSession(get().sessions, projectPath);
        set((state) => ({
          ...activateSessionState(state, existingProjectSession?.id ?? null),
          restoring: false,
        }));
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
        set((state) => ({
          ...activateSessionState(state, sessionBelongsToWorkDir(existing, projectPath)
            ? existing.id
            : (findProjectSession(get().sessions, projectPath)?.id ?? null)),
          restoring: false,
          lastRestoreError: null,
        }));
        return;
      }

      let liveSessionId = targetSessionId;
      let kind: ChatSessionKind = 'archived';
      let acpSessionId: string | undefined = restore.acpSessionId;
      const restoreAgentId = restore.agentId;

      if (restore.isLive) {
        liveSessionId = restore.liveSessionId ?? targetSessionId;
        kind = 'live';
      } else if (restoreAgentId && (restore.workDir ?? projectPath)) {
        const browserState = activeBrowserStateForWorkDir(get(), restore.workDir ?? projectPath);
        set((state) => ({
          sessions: [...state.sessions.filter((s) => s.id !== targetSessionId && s.recordId !== targetSessionId), {
            id: targetSessionId,
            recordId: targetSessionId,
            agentId: restoreAgentId,
            title: fallbackTitle(restoreAgentId, restore.title),
            messages: [],
            status: 'connecting',
            createdAt: Date.now(),
            kind: 'archived',
            workDir: restore.workDir ?? projectPath,
            acpSessionId: restore.acpSessionId,
            terminalId: shouldEnableTerminalForAgent(get()) ? get().activeTerminalId ?? undefined : undefined,
            useActiveBrowser: browserState.enabled,
            browserSelection: browserState.selection,
            browserSelectionMode: browserState.selectionMode,
            browserSelectionCapture: browserState.selectionCapture,
          }],
        }));
        try {
          const resumed = await resumeChatSession(
            targetSessionId,
            restoreAgentId,
            restore.workDir ?? projectPath,
            restore.acpSessionId,
            shouldEnableTerminalForAgent(get()),
            shouldEnableTerminalForAgent(get()) ? get().activeTerminalId : undefined,
            browserState.enabled,
            browserState.tabId,
          );
          if ('id' in resumed) {
            liveSessionId = resumed.id;
            kind = 'resumable';
            acpSessionId = resumed.acpSessionId ?? restore.acpSessionId;
          } else {
            kind = 'archived';
          }
        } catch (resumeErr) {
          kind = 'archived';
        }
      }

      const sessionState = await getChatSessionState(targetSessionId).catch(() => null);
      const historyEntry = get().historySessions.find((h) => h.id === targetSessionId);

      const replayed = sessionState ? replaySessionState(sessionState.messages ?? [], sessionState.events ?? []) : { messages: [] as ChatMessage[] };
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
        workDir: restore.workDir ?? historyEntry?.workDir ?? projectPath,
        acpSessionId,
        useActiveTerminal: kind === 'resumable' ? shouldEnableTerminalForAgent(get()) : false,
        terminalId: kind === 'resumable' && shouldEnableTerminalForAgent(get()) ? get().activeTerminalId ?? undefined : undefined,
        useActiveBrowser: activeBrowserStateForWorkDir(get(), restore.workDir ?? historyEntry?.workDir ?? projectPath).enabled,
        browserSelection: activeBrowserStateForWorkDir(get(), restore.workDir ?? historyEntry?.workDir ?? projectPath).selection,
        browserSelectionMode: activeBrowserStateForWorkDir(get(), restore.workDir ?? historyEntry?.workDir ?? projectPath).selectionMode,
        browserSelectionCapture: activeBrowserStateForWorkDir(get(), restore.workDir ?? historyEntry?.workDir ?? projectPath).selectionCapture,
        commands: replayed.commands ?? snapshotCommands,
        configOptions: replayed.configOptions ?? snapshotConfig,
        contextWindow: replayed.contextWindow,
        contextUsed: replayed.contextUsed,
        costAmount: replayed.costAmount,
        costCurrency: replayed.costCurrency,
      };

      set((state) => {
        const idsToRemove = new Set([targetSessionId, liveSessionId]);
        const nextSessions = [...state.sessions.filter((s) => !idsToRemove.has(s.id) && !idsToRemove.has(s.recordId)), session];
        const nextState = { ...state, sessions: nextSessions };
        return {
          sessions: nextSessions,
          ...activateSessionState(nextState, session.id),
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

  toggleUseActiveBrowser: () =>
    set((state) => ({ useActiveBrowser: !state.useActiveBrowser })),

  setBrowserSelection: (selection) =>
    set((state) => ({
      browserSelection: selection,
      browserSelectionCapture: null,
      browserSelectionMode: selection ? state.browserSelectionMode : 'detail',
      sessions: state.activeSessionId
        ? updateSession(state.sessions, state.activeSessionId, (session) => ({
            ...session,
            browserSelection: selection,
            browserSelectionCapture: null,
            browserSelectionMode: selection ? (session.browserSelectionMode ?? state.browserSelectionMode) : 'detail',
          }))
        : state.sessions,
    })),

  setBrowserSelectionMode: (mode) =>
    set((state) => ({
      browserSelectionMode: mode,
      sessions: state.activeSessionId
        ? updateSession(state.sessions, state.activeSessionId, (session) => ({
            ...session,
            browserSelectionMode: mode,
          }))
        : state.sessions,
    })),

  setBrowserSelectionCapture: (capture) =>
    set((state) => ({
      browserSelectionCapture: capture,
      sessions: state.activeSessionId
        ? updateSession(state.sessions, state.activeSessionId, (session) => ({
            ...session,
            browserSelectionCapture: capture,
          }))
        : state.sessions,
    })),

  toggleUseActiveTerminal: () =>
    set((state) => ({ useActiveTerminal: !state.useActiveTerminal })),

  setUseActiveTerminal: (enabled) =>
    set({ useActiveTerminal: enabled }),

  restartActiveSessionForTerminal: async (enabled) => {
    const state = get();
    const sessionId = state.activeSessionId;
    const session = sessionId ? state.sessions.find((s) => s.id === sessionId) : undefined;
    if (!session || session.status !== 'idle' || session.pendingPermission) return false;
    if (!session.agentId || !session.workDir) return false;

    const previousEnabled = state.useActiveTerminal;
    const terminalId = enabled ? state.activeTerminalId ?? undefined : undefined;
    const browserEnabled = session.useActiveBrowser ?? state.useActiveBrowser;
    const browserTabId = browserEnabled ? activeBrowserTabForWorkDir(session.workDir) : undefined;
    const browserSelection = browserEnabled ? state.browserSelection : null;
    const browserSelectionMode = state.browserSelectionMode;
    const browserSelectionCapture = browserEnabled ? state.browserSelectionCapture : null;
    set({ useActiveTerminal: enabled });

    if (session.useActiveTerminal === enabled) return true;

    const recordId = session.recordId ?? session.id;
    const requestKey = `${recordId}\x00terminal:${enabled ? 'on' : 'off'}`;
    const existingRequest = resumeRequests.get(requestKey);
    if (existingRequest) return existingRequest;

    const request = (async () => {
      set((current) => ({
        sessions: updateSession(current.sessions, session.id, (s) => ({ ...s, status: 'connecting', kind: 'archived' })),
      }));

      const resumed = await resumeChatSession(recordId, session.agentId, session.workDir!, session.acpSessionId, enabled, enabled ? get().activeTerminalId : undefined, browserEnabled, browserTabId);
      if (!('id' in resumed)) {
        throw new Error(resumed.resumeError ?? 'Restart failed');
      }

      const liveSessionId = resumed.id;
      const acpSessionId = resumed.acpSessionId ?? session.acpSessionId;
      const nextWorkDir = resumed.workDir ?? session.workDir;
      set((current) => {
        const idsToRemove = new Set([session.id, liveSessionId]);
        const nextSession: ChatSessionInfo = {
          ...session,
          id: liveSessionId,
          recordId,
          status: 'connecting',
          kind: 'resumable',
          workDir: nextWorkDir,
          acpSessionId,
          useActiveTerminal: enabled,
          terminalId,
          useActiveBrowser: browserEnabled,
          browserSelection: browserSelection,
          browserSelectionMode: browserSelectionMode,
          browserSelectionCapture: browserSelectionCapture,
        };
        const nextSessions = [...current.sessions.filter((s) => !idsToRemove.has(s.id) && s.recordId !== recordId), nextSession];
        const nextState = { ...current, sessions: nextSessions };
        return {
          sessions: nextSessions,
          ...(current.activeSessionId === session.id ? activateSessionState(nextState, liveSessionId) : {}),
          lastRestoreError: null,
        };
      });
      return true;
    })().catch((err) => {
      set((current) => ({
        useActiveTerminal: previousEnabled,
        sessions: updateSession(current.sessions, session.id, (s) => ({ ...s, status: 'error' })),
        lastRestoreError: {
          sessionId: recordId,
          reason: err instanceof Error ? err.message : 'Restart request failed',
        },
      }));
      return false;
    }).finally(() => {
      resumeRequests.delete(requestKey);
    });

    resumeRequests.set(requestKey, request);
    return request;
  },

  restartActiveSessionForBrowser: async (enabled, force = false) => {
    const state = get();
    const sessionId = state.activeSessionId;
    const session = sessionId ? state.sessions.find((s) => s.id === sessionId) : undefined;
    if (!session) return false;
    if (!session.agentId || !session.workDir) return false;

    // If the session is busy (streaming/connecting) or waiting on a permission
    // prompt, we cannot hard-restart it right now. Instead of silently dropping
    // the toggle, queue the desired state on the session so it applies once the
    // session returns to idle (handled in the 'done' event handler). This keeps
    // the frontend toggle responsive and avoids lost user intent. A session in
    // 'error' state is not retried — the user must resolve the error first.
    const isBusy = session.status === 'connecting' || session.status === 'streaming' || !!session.pendingPermission;
    if (isBusy) {
      set((current) => ({
        useActiveBrowser: enabled,
        sessions: updateSession(current.sessions, session.id, (s) => ({ ...s, pendingBrowserToggle: enabled })),
      }));
      return true;
    }
    // status === 'error' (or any other non-idle, non-busy state) cannot be
    // restarted; surface the failure rather than silently swallowing the toggle.
    if (session.status !== 'idle') return false;

    const previousEnabled = state.useActiveBrowser;
    const browserState = activeBrowserStateForWorkDir(state, session.workDir);
    const browserEnabledForSession = enabled && !!browserState.tabId;
    const terminalEnabled = session.useActiveTerminal ?? shouldEnableTerminalForAgent(state);
    const terminalId = terminalEnabled ? (session.terminalId ?? state.activeTerminalId ?? undefined) : undefined;
    set({ useActiveBrowser: enabled });

    if (!force && !!session.useActiveBrowser === browserEnabledForSession) return true;

    const recordId = session.recordId ?? session.id;
    const requestKey = `${recordId}\x00browser:${browserEnabledForSession ? 'on' : 'off'}:${browserState.tabId ?? 'none'}`;
    const existingRequest = resumeRequests.get(requestKey);
    if (existingRequest) return existingRequest;

    const request = (async () => {
      set((current) => ({
        sessions: updateSession(current.sessions, session.id, (s) => ({ ...s, status: 'connecting', kind: 'archived' })),
      }));

      const resumed = await resumeChatSession(
        recordId,
        session.agentId,
        session.workDir!,
        session.acpSessionId,
        terminalEnabled,
        terminalId,
        browserEnabledForSession,
        browserEnabledForSession ? browserState.tabId : undefined,
      );
      if (!('id' in resumed)) {
        throw new Error(resumed.resumeError ?? 'Restart failed');
      }

      const liveSessionId = resumed.id;
      const acpSessionId = resumed.acpSessionId ?? session.acpSessionId;
      const nextWorkDir = resumed.workDir ?? session.workDir;
      const nextSelection = browserEnabledForSession ? browserState.selection : null;
      const nextSelectionCapture = browserEnabledForSession ? browserState.selectionCapture : null;
      set((current) => {
        const idsToRemove = new Set([session.id, liveSessionId]);
        const nextSession: ChatSessionInfo = {
          ...session,
          id: liveSessionId,
          recordId,
          status: 'connecting',
          kind: 'resumable',
          workDir: nextWorkDir,
          acpSessionId,
          useActiveTerminal: terminalEnabled,
          terminalId,
          useActiveBrowser: browserEnabledForSession,
          browserSelection: nextSelection,
          browserSelectionMode: browserState.selectionMode,
          browserSelectionCapture: nextSelectionCapture,
        };
        const nextSessions = [...current.sessions.filter((s) => !idsToRemove.has(s.id) && s.recordId !== recordId), nextSession];
        const nextState = { ...current, sessions: nextSessions };
        return {
          sessions: nextSessions,
          ...(current.activeSessionId === session.id ? activateSessionState(nextState, liveSessionId) : {}),
          lastRestoreError: null,
        };
      });
      return true;
    })().catch((err) => {
      set((current) => ({
        useActiveBrowser: previousEnabled,
        sessions: updateSession(current.sessions, session.id, (s) => ({ ...s, status: 'error' })),
        lastRestoreError: {
          sessionId: recordId,
          reason: err instanceof Error ? err.message : 'Restart request failed',
        },
      }));
      return false;
    }).finally(() => {
      resumeRequests.delete(requestKey);
    });

    resumeRequests.set(requestKey, request);
    return request;
  },

  // Unified browser-toggle entry point used by both the chat config bar
  // (AgentPicker) and the BrowserPanel / inspect-mode UI. It routes to the
  // hard-restart path (restartActiveSessionForBrowser) when an active chat
  // session exists that can be restarted or queued, and falls back to a
  // frontend-only soft toggle when there is no session or the session is in
  // a non-restartable state (e.g. 'error'). This keeps both UI surfaces
  // consistent and prevents the state desync that occurred when one path
  // hard-restarted while the other only flipped frontend state.
  setBrowserEnabled: async (enabled) => {
    const state = get();
    const sessionId = state.activeSessionId;
    const session = sessionId ? state.sessions.find((s) => s.id === sessionId) : undefined;
    if (session && (session.status === 'idle' || session.status === 'connecting' || session.status === 'streaming')) {
      const ok = await get().restartActiveSessionForBrowser(enabled);
      if (ok) return true;
      // Hard restart refused (e.g. session moved to an error state); fall
      // through to a soft toggle so the user's intent is still reflected.
    }
    set({ useActiveBrowser: enabled });
    return true;
  },

  setActiveTerminalId: (id) =>
    set({ activeTerminalId: id }),
}));
