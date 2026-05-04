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
};

export type Project = {
  id: string;
  path: string;
  name: string;
  openFiles: FileTab[];
  activeFileId: string | null;
  terminalSessions: string[];
};

export type ActivePanel = 'explorer' | 'search' | 'projects' | 'terminal' | 'git';

export type AppConfig = {
  mode: 'simple' | 'full';
  workspaceRoot: string;
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
export type ChatAgent = {
  id: string;
  label: string;
  available: boolean;
};

export type CodeContext = {
  filePath: string;
  startLine: number;
  endLine: number;
  selectedCode: string;
  language: string;
};

export type ChatMessage = {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  context?: CodeContext;
  timestamp: number;
};

export type ChatSessionInfo = {
  id: string;
  agentId: string;
  title: string;
  messages: ChatMessage[];
  status: 'idle' | 'streaming' | 'error';
  createdAt: number;
};
