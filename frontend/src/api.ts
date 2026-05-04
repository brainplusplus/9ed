import type { AppConfig, ChatAgent, DirEntry, FileContent, GitBranch, GitCommit, GitFileStatus, GitStash, GutterChange, SearchResult, SessionResponse, ShellProfile } from './types';

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || `Request failed with status ${response.status}`);
  }

  return response.json() as Promise<T>;
}

export async function getShells(): Promise<ShellProfile[]> {
  const response = await fetch('/api/shells', {
    credentials: 'include',
  });

  return parseResponse<ShellProfile[]>(response);
}

export async function createSession(shellId: string, cwd?: string): Promise<SessionResponse> {
  const response = await fetch('/api/sessions', {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ shellId, cwd }),
  });

  return parseResponse<SessionResponse>(response);
}

export async function deleteSession(sessionId: string): Promise<void> {
  const response = await fetch(`/api/sessions/${sessionId}`, {
    method: 'DELETE',
    credentials: 'include',
  });

  if (!response.ok && response.status !== 404) {
    const message = await response.text();
    throw new Error(message || `Failed to close session ${sessionId}`);
  }
}

export function createSessionWebSocket(sessionId: string): WebSocket {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return new WebSocket(`${protocol}//${window.location.host}/ws/sessions/${sessionId}`);
}

export async function getConfig(): Promise<AppConfig> {
  const response = await fetch('/api/config', { credentials: 'include' });
  return parseResponse<AppConfig>(response);
}

export async function getDrives(): Promise<string[]> {
  const response = await fetch('/api/files/drives', { credentials: 'include' });
  return parseResponse<string[]>(response);
}

export async function getFileTree(path: string): Promise<DirEntry[]> {
  const response = await fetch(`/api/files/tree?path=${encodeURIComponent(path)}`, { credentials: 'include' });
  return parseResponse<DirEntry[]>(response);
}

export async function getFileContent(path: string): Promise<FileContent> {
  const response = await fetch(`/api/files/content?path=${encodeURIComponent(path)}`, { credentials: 'include' });
  return parseResponse<FileContent>(response);
}

export async function saveFileContent(path: string, content: string): Promise<void> {
  const response = await fetch(`/api/files/content?path=${encodeURIComponent(path)}`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to save file');
  }
}

export async function createEntry(path: string, type: 'file' | 'dir'): Promise<void> {
  const response = await fetch('/api/files/create', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, type }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to create entry');
  }
}

export async function renameEntry(oldPath: string, newPath: string): Promise<void> {
  const response = await fetch('/api/files/rename', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ oldPath, newPath }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to rename');
  }
}

export async function deleteEntry(path: string): Promise<void> {
  const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (!response.ok && response.status !== 404) {
    const message = await response.text();
    throw new Error(message || 'Failed to delete');
  }
}

export async function copyEntry(sourcePath: string, destPath: string): Promise<void> {
  const response = await fetch('/api/files/copy', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sourcePath, destPath }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to copy');
  }
}

export async function moveEntry(sourcePath: string, destPath: string): Promise<void> {
  const response = await fetch('/api/files/move', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sourcePath, destPath }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to move');
  }
}

export async function searchFiles(root: string, query: string, regex: boolean, maxResults: number): Promise<SearchResult[]> {
  const params = new URLSearchParams({ root, query, regex: String(regex), maxResults: String(maxResults) });
  const response = await fetch(`/api/files/search?${params}`, { credentials: 'include' });
  return parseResponse<SearchResult[]>(response);
}

export function downloadFile(path: string): void {
  const a = document.createElement('a');
  a.href = `/api/files/download?path=${encodeURIComponent(path)}`;
  a.download = '';
  a.click();
}

export async function uploadFiles(targetPath: string, files: FileList): Promise<void> {
  const formData = new FormData();
  for (let i = 0; i < files.length; i++) {
    formData.append('files', files[i]);
  }
  const response = await fetch(`/api/files/upload?path=${encodeURIComponent(targetPath)}`, {
    method: 'POST',
    credentials: 'include',
    body: formData,
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to upload');
  }
}

export async function getGitStatus(project: string): Promise<GitFileStatus[]> {
  const response = await fetch(`/api/git/status?project=${encodeURIComponent(project)}`, { credentials: 'include' });
  return parseResponse<GitFileStatus[]>(response);
}

export async function getGitLog(project: string, limit = 50, offset = 0): Promise<GitCommit[]> {
  const params = new URLSearchParams({ project, limit: String(limit), offset: String(offset) });
  const response = await fetch(`/api/git/log?${params}`, { credentials: 'include' });
  return parseResponse<GitCommit[]>(response);
}

export async function getGitBranches(project: string): Promise<GitBranch[]> {
  const response = await fetch(`/api/git/branches?project=${encodeURIComponent(project)}`, { credentials: 'include' });
  return parseResponse<GitBranch[]>(response);
}

export async function gitStage(project: string, paths: string[]): Promise<void> {
  const response = await fetch('/api/git/stage', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, paths }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to stage files');
  }
}

export async function gitUnstage(project: string, paths: string[]): Promise<void> {
  const response = await fetch('/api/git/unstage', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, paths }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to unstage files');
  }
}

export async function gitCommit(project: string, message: string, amend = false): Promise<void> {
  const response = await fetch('/api/git/commit', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, message, amend }),
  });
  if (!response.ok) {
    const msg = await response.text();
    throw new Error(msg || 'Failed to commit');
  }
}

export async function gitPush(project: string, remote?: string, branch?: string): Promise<string> {
  const response = await fetch('/api/git/push', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, remote, branch }),
  });
  return parseResponse<string>(response);
}

export async function gitPull(project: string, remote?: string, branch?: string): Promise<string> {
  const response = await fetch('/api/git/pull', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, remote, branch }),
  });
  return parseResponse<string>(response);
}

export async function gitBranchAction(project: string, action: 'create' | 'delete' | 'checkout', name: string): Promise<void> {
  const response = await fetch('/api/git/branch', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, action, name }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Branch action failed');
  }
}

export async function gitMerge(project: string, branch: string): Promise<void> {
  const response = await fetch('/api/git/merge', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, branch }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Merge failed');
  }
}

export async function gitStashAction(project: string, action: 'list' | 'push' | 'pop' | 'drop', index?: number, message?: string): Promise<GitStash[] | void> {
  const response = await fetch('/api/git/stash', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, action, index, message }),
  });
  if (action === 'list') {
    return parseResponse<GitStash[]>(response);
  }
  if (!response.ok) {
    const msg = await response.text();
    throw new Error(msg || 'Stash action failed');
  }
}

export async function getGitDiff(project: string, path: string): Promise<string> {
  const params = new URLSearchParams({ project, path });
  const response = await fetch(`/api/git/diff?${params}`, { credentials: 'include' });
  const data = await parseResponse<{ diff: string }>(response);
  return data.diff;
}

export async function getGitDiffLines(project: string, path: string): Promise<GutterChange[]> {
  const params = new URLSearchParams({ project, path });
  const response = await fetch(`/api/git/diff-lines?${params}`, { credentials: 'include' });
  return parseResponse<GutterChange[]>(response);
}

export async function getGitFileAtHEAD(project: string, path: string): Promise<string> {
  const params = new URLSearchParams({ project, path });
  const response = await fetch(`/api/git/file-at-head?${params}`, { credentials: 'include' });
  const data = await parseResponse<{ content: string }>(response);
  return data.content;
}

export async function gitDiscard(project: string, paths: string[]): Promise<void> {
  const response = await fetch('/api/git/discard', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, paths }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || 'Failed to discard changes');
  }
}

export async function getChatAgents(): Promise<ChatAgent[]> {
  const response = await fetch('/api/chat/agents', { credentials: 'include' });
  return parseResponse<ChatAgent[]>(response);
}

export async function createChatSession(agentId: string): Promise<{ id: string }> {
  const response = await fetch('/api/chat/sessions', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ agentId }),
  });
  return parseResponse<{ id: string }>(response);
}

export async function deleteChatSession(id: string): Promise<void> {
  const response = await fetch(`/api/chat/sessions/${id}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (!response.ok && response.status !== 404) {
    const message = await response.text();
    throw new Error(message || 'Failed to delete chat session');
  }
}

export function createChatWebSocket(sessionId: string): WebSocket {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return new WebSocket(`${protocol}//${window.location.host}/ws/chat/${sessionId}`);
}
