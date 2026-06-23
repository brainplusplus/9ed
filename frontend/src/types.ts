export type ShellProfile = {
  id: string;
  label: string;
  command: string;
  args: string[];
};

export type SessionResponse = {
  id: string;
  profile: ShellProfile;
};

export type SessionTab = {
  id: string;
  profile: ShellProfile;
  status: 'connecting' | 'ready' | 'disconnected' | 'error';
  errorMessage?: string;
  scrollback?: string;
};

export type TerminalAction = {
  targetTabId: string;
  kind: 'clear-terminal';
  nonce: number;
};

export type WebSocketIncomingMessage =
  | { type: 'output'; data: string }
  | { type: 'error'; data: string };

export type WebSocketOutgoingMessage =
  | { type: 'input'; data: string }
  | { type: 'resize'; cols: number; rows: number };

export type DirEntry = {
  name: string;
  type: 'file' | 'dir';
  size: number;
  modified: number;
  ignored?: boolean;
};

export type FileContent = {
  content: string;
  encoding: string;
  size: number;
};

export type SearchResult = {
  path: string;
  line: number;
  column: number;
  preview: string;
};

export type FileTab = {
  id: string;
  path: string;
  name: string;
  content: string;
  language: string;
  modified: boolean;
  conflict?: boolean;
  deleted?: boolean;
};

export type Project = {
  id: string;
  path: string;
  name: string;
  openFiles: FileTab[];
  activeFileId: string | null;
  terminalTabs: SessionTab[];
  activeTerminalTabId: string | null;
  terminalSessions: string[];
  browserTabIds?: string[];
  activeBrowserTabId?: string | null;
};

export type ActivePanel = 'explorer' | 'search' | 'projects' | 'terminal' | 'git' | 'browser' | 'settings';

export type AppConfig = {
  workspaceRoot: string;
  terminalAiMaxLines: number;
};

export type BrowserTransport = 'proxy' | 'webrtc';

export type BrowserTab = {
  id: string;
  url: string;
  title: string;
  transport: BrowserTransport;
  canGoBack: boolean;
  canGoForward: boolean;
  fileChooserPending?: boolean;
  fileChooserMultiple?: boolean;
  proxyPath: string;
  createdAt: number;
  updatedAt: number;
};

export type BrowserAutomationStatus = {
  provider: string;
  running: boolean;
  lastError?: string;
};

export type BrowserMCPDebugEntry = {
  timestamp: number;
  source: string;
  level: string;
  message: string;
};

export type BrowserMCPDebugLog = {
  enabled: boolean;
  entries: BrowserMCPDebugEntry[];
};

export type BrowserInspectResult = {
  url: string;
  title: string;
  text: string;
  textBytes: number;
};

export type BoxModelEdges = {
  top: number;
  right: number;
  bottom: number;
  left: number;
};

export type BoxModelLayers = {
  margin: BoxModelEdges;
  border: BoxModelEdges;
  padding: BoxModelEdges;
  contentRect: { x: number; y: number; width: number; height: number };
};

export type ParentChainItem = {
  tagName: string;
  id?: string;
  classes: string[];
};

export type BrowserElementSelection = {
  tabId?: string;
  url: string;
  title: string;
  tagName: string;
  role?: string;
  text?: string;
  selector: string;
  uniqueSelector?: string;
  outerHTML: string;
  attributes?: Record<string, string>;
  computedStyle?: {
    display: string;
    position: string;
    width: string;
    height: string;
    color: string;
    backgroundColor: string;
    fontSize: string;
    fontFamily: string;
    fontWeight: string;
    lineHeight: string;
    textAlign: string;
    margin: string;
    padding: string;
    border: string;
    borderRadius: string;
    overflow: string;
    opacity: string;
    visibility: string;
    zIndex: string;
    flex?: string;
    grid?: string;
    gap?: string;
    top?: string;
    left?: string;
    right?: string;
    bottom?: string;
  };
  boundingRect?: {
    width: number;
    height: number;
    x: number;
    y: number;
  };
  boxModel?: BoxModelLayers;
  parentChain?: ParentChainItem[];
  accessibilityInfo?: {
    role?: string;
    label?: string;
    tabIndex?: number;
    focusable: boolean;
    contrastRatio?: string;
  };
  eventListeners?: Array<{ type: string; handlerBody: string }>;
};

export type BrowserSelectionMode = 'detail' | 'screenshot';

export type BrowserElementCapture = {
  path: string;
  dataUrl: string;
  mimeType: string;
  name: string;
  selectorKey: string;
  capturedAt: number;
};

/** Terminal context sent to AI chat when terminal integration is active. */
export type TerminalContext = {
  sessionId: string;
  cwd: string;
  shellType: string;
  scrollback: string;
};

export type BrowserState = {
  provider: string;
  transport: BrowserTransport;
  automation: BrowserAutomationStatus;
  tabs: BrowserTab[];
  activeTabId?: string;
  localhostScope: 'server';
};

export type SettingsTunnel = {
  id: string;
  name: string;
  localPort: string;
  engine: 'bore' | 'cloudflare';
  enabled: boolean;
  status: 'starting' | 'started' | 'stopped';
  url?: string;
  lastError?: string;
  createdAt: number;
  updatedAt: number;
};

export type SettingsAboutInfo = {
  name: string;
  version: string;
  description: string;
  defaultTunnelEngine: 'bore' | 'cloudflare';
  appPort: string;
  appTunnelEnabled: boolean;
};

// Git types
export type GitFileStatus = {
  path: string;
  status: 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked';
  staged: boolean;
};

export type GitBranch = {
  name: string;
  current: boolean;
  remote?: string;
  ahead: number;
  behind: number;
};

export type GitCommit = {
  hash: string;
  shortHash: string;
  message: string;
  author: string;
  date: string;
  relativeDate: string;
};

export type GitStash = {
  index: number;
  message: string;
};

export type GutterChange = {
  startLine: number;
  endLine: number;
  type: 'added' | 'modified' | 'deleted';
};

// Chat types
export type AgentModel = {
  id: string;
  name: string;
  provider: string;
  contextWindow?: number;
  maxTokens?: number;
  canReason?: boolean;
};

export type AgentProvider = {
  id: string;
  name: string;
  baseUrl?: string;
  hasKey: boolean;
};

export type ChatAgent = {
  id: string;
  label: string;
  available: boolean;
  configFound: boolean;
  activeModel: string;
  models: AgentModel[];
  providers: AgentProvider[];
  agentName?: string;
};

export type CodeContext = {
  filePath: string;
  startLine: number;
  endLine: number;
  selectedCode: string;
  language: string;
};

export type ToolCallLocation = {
  path: string;
  line?: number;
};

export type ToolCallInfo = {
  toolCallId: string;
  title: string;
  kind: string;
  status: string;
  content?: string;
  locations?: ToolCallLocation[];
  rawInput?: string;
};

export type DiffInfo = {
  path: string;
  oldText: string;
  newText: string;
};

export type PlanEntryInfo = {
  content: string;
  priority?: string;
  status?: string;
};

export type ChatMessage = {
  id: string;
  role: 'user' | 'assistant' | 'tool_call' | 'plan';
  content: string;
  context?: CodeContext;
  timestamp: number;
  thinking?: string;
  toolCall?: ToolCallInfo;
  diffs?: DiffInfo[];
  plan?: PlanEntryInfo[];
};

export type SlashCommandInfo = {
  name: string;
  description: string;
  inputHint?: string;
};

export type ConfigOptionInfo = {
  id: string;
  name: string;
  description?: string;
  category?: string;
  type: string;
  currentValue: string;
  options: ConfigValueInfo[];
};

export type ConfigValueInfo = {
  value: string;
  name: string;
  description?: string;
};

export type PermissionOptionInfo = {
  optionId: string;
  name: string;
  kind: string;
};

export type ChatEvent = {
  type: string;
  text?: string;
  thinking?: string;
  toolCallId?: string;
  toolTitle?: string;
  toolKind?: string;
  toolStatus?: string;
  toolContent?: string;
  toolLocations?: ToolCallLocation[];
  toolRawInput?: string;
  terminalCommand?: string;
  diffPath?: string;
  diffOldText?: string;
  diffNewText?: string;
  planEntries?: PlanEntryInfo[];
  commands?: SlashCommandInfo[];
  configOptions?: ConfigOptionInfo[];
  title?: string;
  stopReason?: string;
  error?: string;
  contextWindow?: number;
  contextUsed?: number;
  costAmount?: number;
  costCurrency?: string;
  permissionId?: string;
  permissionTitle?: string;
  permissionOptions?: PermissionOptionInfo[];
  // ADR-0002: cursor tracking for gap/stale detection.
  seq?: number;
  epoch?: string;
};

export type PendingPermission = {
  permissionId: string;
  title: string;
  toolCallId?: string;
  toolKind?: string;
  options: PermissionOptionInfo[];
};

export type ChatDebugEntry = {
  timestamp: number;
  source: 'client' | 'ws' | 'session' | 'tool';
  level: 'info' | 'warn' | 'error';
  message: string;
};

export type ChatSessionKind = 'live' | 'resumable' | 'archived';

export type ChatSessionInfo = {
  id: string;
  recordId: string;
  agentId: string;
  title: string;
  messages: ChatMessage[];
  status: 'connecting' | 'idle' | 'streaming' | 'error';
  createdAt: number;
  lastEventAt?: number;
  stalled?: boolean;
  kind: ChatSessionKind;
  workDir?: string;
  acpSessionId?: string;
  useActiveTerminal?: boolean;
  terminalId?: string;
  useActiveBrowser?: boolean;
  browserSelection?: BrowserElementSelection | null;
  browserSelectionMode?: BrowserSelectionMode;
  browserSelectionCapture?: BrowserElementCapture | null;
  commands?: SlashCommandInfo[];
  configOptions?: ConfigOptionInfo[];
  pendingPermission?: PendingPermission;
  contextWindow?: number;
  contextUsed?: number;
  costAmount?: number;
  costCurrency?: string;
  debugEntries?: ChatDebugEntry[];
};

export type HistorySessionRecord = {
  id: string;
  agentId: string;
  title: string;
  workDir?: string;
  acpSessionId?: string;
  status?: string;
  createdAt: number;
  updatedAt: number;
};

export type HistoryMessageRecord = {
  id: string;
  sessionId: string;
  role: 'user' | 'assistant';
  content: string;
  contextFile?: string;
  contextStartLine?: number;
  contextEndLine?: number;
  contextCode?: string;
  contextLanguage?: string;
  timestamp: number;
};

export type TranscriptEventRecord = {
  id: string;
  sessionId: string;
  kind: string;
  payloadJson: string;
  seq: number;
  timestamp: number;
};

export type TranscriptSnapshotRecord = {
  sessionId: string;
  commandsJson: string;
  configOptsJson: string;
  updatedAt: number;
};

export type TranscriptResponse = {
  events: TranscriptEventRecord[];
  snapshot: TranscriptSnapshotRecord;
};

// ADR-0002: timeline catch-up response from fetch_timeline RPC.
export type TimelineEvent = {
  type: string;
  seq: number;
  epoch?: string;
  event: ChatEvent;
};

// ADR-0002: seq window metadata describing the event range for the session.
export type SeqWindow = {
  minSeq: number;
  maxSeq: number;
  nextSeq: number;
};

// ADR-0002: full timeline response shape. The client uses reset/staleCursor/gap
// to decide whether to reset its cursor and re-fetch the timeline tail.
export type TimelineResponse = {
  type: 'timeline';
  events: TimelineEvent[];
  epoch: string;
  reset: boolean;
  staleCursor: boolean;
  gap: boolean;
  window: SeqWindow;
  hasOlder: boolean;
  hasNewer: boolean;
  endCursor: number;
  // Legacy hint preserved for backward compatibility.
  hasMore: boolean;
};

// ADR-0002: replay-on-subscribe metadata envelope. Sent immediately after the
// replay events so the client can initialize its cursor with the current
// window and epoch.
export type ReplayMeta = {
  type: 'replay_meta';
  epoch: string;
  window: SeqWindow;
};

// ADR-0001: WebRTC visual signaling messages.
export type VisualSignalingMessage = {
  type: 'offer' | 'answer' | 'ice-candidate' | 'error';
  sessionId: string;
  sdp?: string;
  ice?: RTCIceCandidateInit;
  iceServers?: RTCIceServer[];
};
