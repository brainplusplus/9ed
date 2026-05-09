import { useCallback, useEffect, useRef, useState } from 'react';
import { copyEntry, createEntry, deleteEntry, getFileTree, moveEntry, renameEntry } from '../../api';
import { useGitStore } from '../../stores/git';
import { useWorkspaceStore } from '../../stores/workspace';
import type { DirEntry, GitFileStatus } from '../../types';
import { ContextMenu, type ContextMenuItem } from '../shared/ContextMenu';

type TreeNode = DirEntry & { fullPath: string; children?: TreeNode[]; expanded?: boolean; ignored?: boolean };

const FILE_COLORS: Record<string, string> = {
  ts: '#3178c6', tsx: '#3178c6',
  js: '#f0db4f', jsx: '#f0db4f', mjs: '#f0db4f',
  go: '#00add8',
  py: '#3572a5',
  rs: '#dea584',
  md: '#cdd6f4',
  json: '#f0db4f',
  yaml: '#e44d26', yml: '#e44d26',
  css: '#a855f7', scss: '#a855f7', less: '#a855f7',
  html: '#e44d26',
  vue: '#42b883',
  svelte: '#ff3e00',
  sh: '#89e051', bash: '#89e051',
  sql: '#e38c00',
  toml: '#9c4221',
  xml: '#e44d26',
  svg: '#f9e2af',
  java: '#b07219',
  c: '#555555', h: '#555555',
  cpp: '#f34b7d', hpp: '#f34b7d',
  rb: '#701516',
  php: '#4f5d95',
  cs: '#178600',
  env: '#89e051',
  lock: '#6c7086',
  mod: '#00add8', sum: '#00add8',
};

function getFileColor(name: string): string {
  const dotIdx = name.lastIndexOf('.');
  if (dotIdx < 0) return '#6c7086';
  const ext = name.slice(dotIdx + 1).toLowerCase();
  if (FILE_COLORS[ext]) return FILE_COLORS[ext];
  if (name === '.gitignore') return '#f14e32';
  if (name.startsWith('.env')) return '#89e051';
  if (name === 'dockerfile' || name === 'Dockerfile') return '#2496ed';
  if (name === 'Makefile') return '#6c7086';
  return '#6c7086';
}

function getFileExt(name: string): string {
  if (name === '.gitignore') return 'gi';
  if (name.startsWith('.env')) return 'env';
  const dotIdx = name.lastIndexOf('.');
  if (dotIdx < 0) return name.slice(0, 2);
  const ext = name.slice(dotIdx + 1).toLowerCase();
  if (ext.length > 3) return ext.slice(0, 3);
  return ext;
}

function FileIcon({ name }: { name: string }) {
  const color = getFileColor(name);
  const ext = getFileExt(name);
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" className="tree-file-icon">
      <path d="M2 1.5 a1 1 0 0 1 1 -1 h6 l4 4 v9.5 a1 1 0 0 1 -1 1 h-9 a1 1 0 0 1 -1 -1z" fill={color} opacity="0.2" stroke={color} strokeWidth="0.8" />
      <path d="M9 0.5 v3.5 a0.5 0.5 0 0 0 0.5 0.5 h3.5" stroke={color} strokeWidth="0.7" fill="none" />
      <text x="8" y="12" textAnchor="middle" fill={color} fontSize="5.5" fontFamily="IBM Plex Sans, Segoe UI, sans-serif" fontWeight="700">{ext}</text>
    </svg>
  );
}

function FolderOpenIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" className="tree-folder-icon">
      <path d="M1.5 3.5 a1 1 0 0 1 1 -1 h3 l1.5 1.5 h5.5 a1 1 0 0 1 1 1 v1 h-12z" fill="#eab308" opacity="0.25" stroke="#eab308" strokeWidth="0.7" />
      <path d="M1 6.5 h13 l-1.5 6.5 a0.8 0.8 0 0 1 -0.8 0.5 h-8.4 a0.8 0.8 0 0 1 -0.8 -0.5z" fill="#eab308" opacity="0.2" stroke="#eab308" strokeWidth="0.7" />
    </svg>
  );
}

function FolderClosedIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" className="tree-folder-icon">
      <path d="M1.5 3.5 a1 1 0 0 1 1 -1 h3 l1.5 1.5 h5.5 a1 1 0 0 1 1 1 v8 a1 1 0 0 1 -1 1 h-10 a1 1 0 0 1 -1 -1z" fill="#eab308" opacity="0.2" stroke="#eab308" strokeWidth="0.7" />
    </svg>
  );
}

function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg width="10" height="10" viewBox="0 0 10 10" fill="none" xmlns="http://www.w3.org/2000/svg" className="tree-chevron" style={{ transform: open ? 'rotate(90deg)' : 'none' }}>
      <path d="M3 1.5 L7.5 5 L3 8.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

type FileTreeProps = {
  rootPath: string;
  onFileSelect: (filePath: string, fileName: string) => void;
  refreshKey?: number;
};

let clipboardPath: string | null = null;
let clipboardAction: 'cut' | 'copy' | null = null;

type InlineInputState = {
  parentPath: string;
  type: 'file' | 'dir';
  depth: number;
  insertAfterPath?: string;
} | null;

type RenameState = {
  fullPath: string;
  currentName: string;
} | null;

type ContextMenuState = {
  x: number;
  y: number;
  node: TreeNode | null;
} | null;

export function FileTree({ rootPath, onFileSelect, refreshKey }: FileTreeProps) {
  const [nodes, setNodes] = useState<TreeNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [localRefresh, setLocalRefresh] = useState(0);
  const [contextMenu, setContextMenu] = useState<ContextMenuState>(null);
  const [inlineInput, setInlineInput] = useState<InlineInputState>(null);
  const [renameState, setRenameState] = useState<RenameState>(null);
  const gitStatus = useGitStore((s) => s.status);
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const renameOpenFile = useWorkspaceStore((s) => s.renameOpenFile);
  const closeFileByPath = useWorkspaceStore((s) => s.closeFileByPath);

  const triggerRefresh = useCallback(() => {
    setLocalRefresh((n) => n + 1);
  }, []);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const entries = await getFileTree(rootPath);
        if (!cancelled) {
          setNodes(toTreeNodes(rootPath, entries));
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => { cancelled = true; };
  }, [rootPath, refreshKey, localRefresh]);

  const handleToggle = useCallback(async (node: TreeNode, path: number[]) => {
    if (node.type === 'file') {
      onFileSelect(node.fullPath, node.name);
      return;
    }

    if (node.expanded) {
      setNodes((prev) => updateAtPath(prev, path, { ...node, expanded: false }));
      return;
    }

    try {
      const entries = await getFileTree(node.fullPath);
      setNodes((prev) => updateAtPath(prev, path, { ...node, expanded: true, children: toTreeNodes(node.fullPath, entries) }));
    } catch {
      setNodes((prev) => updateAtPath(prev, path, { ...node, expanded: true, children: [] }));
    }
  }, [onFileSelect]);

  const handleContextMenu = useCallback((e: React.MouseEvent, node: TreeNode | null) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({ x: e.clientX, y: e.clientY, node });
  }, []);

  const getSep = useCallback(() => rootPath.includes('\\') ? '\\' : '/', [rootPath]);

  const handleNewFile = useCallback((parentPath: string, depth: number) => {
    setContextMenu(null);
    setInlineInput({ parentPath, type: 'file', depth });
  }, []);

  const handleNewFolder = useCallback((parentPath: string, depth: number) => {
    setContextMenu(null);
    setInlineInput({ parentPath, type: 'dir', depth });
  }, []);

  const handleCut = useCallback((fullPath: string) => {
    clipboardPath = fullPath;
    clipboardAction = 'cut';
    setContextMenu(null);
  }, []);

  const handleCopy = useCallback((fullPath: string) => {
    clipboardPath = fullPath;
    clipboardAction = 'copy';
    setContextMenu(null);
  }, []);

  const handlePaste = useCallback(async (targetDir: string) => {
    setContextMenu(null);
    if (!clipboardPath || !clipboardAction) return;
    const sep = getSep();
    const name = clipboardPath.split(/[/\\]/).pop() || '';
    let dest = targetDir + sep + name;
    try {
      if (clipboardAction === 'copy') {
        const exists = nodes.some((n) => n.fullPath === dest) ||
          (await getFileTree(targetDir).then((entries) => entries.some((e) => e.name === name)).catch(() => false));
        if (exists) {
          const dotIdx = name.lastIndexOf('.');
          const safeName = dotIdx > 0
            ? name.slice(0, dotIdx) + ' copy' + name.slice(dotIdx)
            : name + ' copy';
          dest = targetDir + sep + safeName;
        }
        await copyEntry(clipboardPath, dest);
      } else {
        await moveEntry(clipboardPath, dest);
        if (activeProjectId) renameOpenFile(activeProjectId, clipboardPath, dest, name);
        clipboardPath = null;
        clipboardAction = null;
      }
      triggerRefresh();
    } catch (err) {
      console.error(err);
    }
  }, [getSep, triggerRefresh, nodes, activeProjectId, renameOpenFile]);

  const handleDuplicate = useCallback(async (fullPath: string) => {
    setContextMenu(null);
    const sep = getSep();
    const parts = fullPath.split(/[/\\]/);
    const name = parts.pop() || '';
    const dir = parts.join(sep);
    const dotIdx = name.lastIndexOf('.');
    const newName = dotIdx > 0
      ? name.slice(0, dotIdx) + ' copy' + name.slice(dotIdx)
      : name + ' copy';
    const dest = dir + sep + newName;
    try {
      await copyEntry(fullPath, dest);
      triggerRefresh();
    } catch (err) {
      console.error(err);
    }
  }, [getSep, triggerRefresh]);

  const handleCopyPath = useCallback(async (fullPath: string) => {
    setContextMenu(null);
    await navigator.clipboard.writeText(fullPath);
  }, []);

  const handleCopyRelativePath = useCallback(async (fullPath: string) => {
    setContextMenu(null);
    const sep = getSep();
    let relative = fullPath;
    if (fullPath.startsWith(rootPath)) {
      relative = fullPath.slice(rootPath.length);
      if (relative.startsWith(sep)) relative = relative.slice(1);
    }
    await navigator.clipboard.writeText(relative);
  }, [rootPath, getSep]);

  const handleRename = useCallback((fullPath: string, currentName: string) => {
    setContextMenu(null);
    setRenameState({ fullPath, currentName });
  }, []);

  const handleDelete = useCallback(async (fullPath: string, name: string) => {
    setContextMenu(null);
    if (!window.confirm(`Delete "${name}"?`)) return;
    try {
      await deleteEntry(fullPath);
      if (activeProjectId) closeFileByPath(activeProjectId, fullPath);
      triggerRefresh();
    } catch (err) {
      console.error(err);
    }
  }, [triggerRefresh, activeProjectId, closeFileByPath]);

  const handleCollapseAll = useCallback(() => {
    setContextMenu(null);
    setNodes((prev) => collapseAll(prev));
  }, []);

  const handleInlineSubmit = useCallback(async (name: string) => {
    if (!inlineInput || !name.trim()) {
      setInlineInput(null);
      return;
    }
    const sep = getSep();
    const fullPath = inlineInput.parentPath + sep + name.trim();
    try {
      await createEntry(fullPath, inlineInput.type);
      triggerRefresh();
    } catch (err) {
      console.error(err);
    }
    setInlineInput(null);
  }, [inlineInput, getSep, triggerRefresh]);

  const handleRenameSubmit = useCallback(async (newName: string) => {
    if (!renameState || !newName.trim() || newName.trim() === renameState.currentName) {
      setRenameState(null);
      return;
    }
    const sep = getSep();
    const parts = renameState.fullPath.split(/[/\\]/);
    parts.pop();
    const newPath = parts.join(sep) + sep + newName.trim();
    try {
      await renameEntry(renameState.fullPath, newPath);
      if (activeProjectId) renameOpenFile(activeProjectId, renameState.fullPath, newPath, newName.trim());
      triggerRefresh();
    } catch (err) {
      console.error(err);
    }
    setRenameState(null);
  }, [renameState, getSep, triggerRefresh, activeProjectId, renameOpenFile]);

  const buildMenuItems = useCallback((node: TreeNode | null): ContextMenuItem[] => {
    const items: ContextMenuItem[] = [];
    const isDir = !node || node.type === 'dir';
    const targetDir = node ? (isDir ? node.fullPath : node.fullPath.split(/[/\\]/).slice(0, -1).join(getSep())) : rootPath;
    const depth = node ? (isDir ? (node.fullPath.split(/[/\\]/).length - rootPath.split(/[/\\]/).length) : (node.fullPath.split(/[/\\]/).length - rootPath.split(/[/\\]/).length - 1)) : 0;

    items.push({ type: 'item', label: 'New File', shortcut: 'Ctrl+N', onClick: () => handleNewFile(targetDir, depth + 1) });
    items.push({ type: 'item', label: 'New Folder', shortcut: 'Alt+N', onClick: () => handleNewFolder(targetDir, depth + 1) });
    items.push({ type: 'separator' });

    if (node) {
      items.push({ type: 'item', label: 'Cut', shortcut: 'Ctrl+X', onClick: () => handleCut(node.fullPath) });
      items.push({ type: 'item', label: 'Copy', shortcut: 'Ctrl+C', onClick: () => handleCopy(node.fullPath) });
    }
    items.push({ type: 'item', label: 'Paste', shortcut: 'Ctrl+V', disabled: !clipboardPath, onClick: () => handlePaste(targetDir) });
    if (node) {
      items.push({ type: 'item', label: 'Duplicate', onClick: () => handleDuplicate(node.fullPath) });
    }
    items.push({ type: 'separator' });

    if (node) {
      items.push({ type: 'item', label: 'Copy Path', shortcut: 'Alt+Shift+C', onClick: () => handleCopyPath(node.fullPath) });
      items.push({ type: 'item', label: 'Copy Relative Path', shortcut: 'Ctrl+K Ctrl+Shift+C', onClick: () => handleCopyRelativePath(node.fullPath) });
      items.push({ type: 'separator' });
      items.push({ type: 'item', label: 'Rename', shortcut: 'F2', onClick: () => handleRename(node.fullPath, node.name) });
      items.push({ type: 'item', label: 'Delete', shortcut: 'Delete', onClick: () => handleDelete(node.fullPath, node.name) });
    }

    if (isDir || !node) {
      if (node) items.push({ type: 'separator' });
      items.push({ type: 'item', label: 'Collapse All', onClick: handleCollapseAll });
    }

    return items;
  }, [rootPath, getSep, handleNewFile, handleNewFolder, handleCut, handleCopy, handlePaste, handleDuplicate, handleCopyPath, handleCopyRelativePath, handleRename, handleDelete, handleCollapseAll]);

  if (loading) return <div className="tree-loading">Loading…</div>;

  return (
    <div className="file-tree" onContextMenu={(e) => handleContextMenu(e, null)}>
      {inlineInput && inlineInput.parentPath === rootPath && (
        <InlineInput depth={0} onSubmit={handleInlineSubmit} onCancel={() => setInlineInput(null)} />
      )}
      {nodes.map((node, i) => (
        <FileTreeNode
          key={node.name}
          node={node}
          path={[i]}
          depth={0}
          onToggle={handleToggle}
          rootPath={rootPath}
          gitStatus={gitStatus}
          onContextMenu={handleContextMenu}
          renameState={renameState}
          onRenameSubmit={handleRenameSubmit}
          onRenameCancel={() => setRenameState(null)}
          inlineInput={inlineInput}
          onInlineSubmit={handleInlineSubmit}
          onInlineCancel={() => setInlineInput(null)}
        />
      ))}
      {contextMenu && (
        <ContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          items={buildMenuItems(contextMenu.node)}
          onClose={() => setContextMenu(null)}
        />
      )}
    </div>
  );
}

type FileTreeNodeProps = {
  node: TreeNode;
  path: number[];
  depth: number;
  onToggle: (node: TreeNode, path: number[]) => void;
  rootPath: string;
  gitStatus: GitFileStatus[];
  onContextMenu: (e: React.MouseEvent, node: TreeNode) => void;
  renameState: RenameState;
  onRenameSubmit: (newName: string) => void;
  onRenameCancel: () => void;
  inlineInput: InlineInputState;
  onInlineSubmit: (name: string) => void;
  onInlineCancel: () => void;
};

function getGitColor(fullPath: string, rootPath: string, gitStatus: GitFileStatus[]): string | undefined {
  const relative = fullPath.startsWith(rootPath)
    ? fullPath.slice(rootPath.length).replace(/^[/\\]/, '').replace(/\\/g, '/')
    : '';
  if (!relative) return undefined;

  const match = gitStatus.find((f) => f.path === relative || relative.startsWith(f.path + '/'));
  if (!match) return undefined;

  switch (match.status) {
    case 'untracked':
    case 'added': return '#22c55e';
    case 'modified': return '#eab308';
    case 'deleted': return '#ef4444';
    case 'renamed': return '#3b82f6';
    default: return undefined;
  }
}

function FileTreeNode({ node, path, depth, onToggle, rootPath, gitStatus, onContextMenu, renameState, onRenameSubmit, onRenameCancel, inlineInput, onInlineSubmit, onInlineCancel }: FileTreeNodeProps) {
  const isDir = node.type === 'dir';
  const color = getGitColor(node.fullPath, rootPath, gitStatus);
  const isIgnored = node.ignored === true;
  const isRenaming = renameState?.fullPath === node.fullPath;

  const nameStyle: React.CSSProperties = {};
  if (color && !isIgnored) {
    nameStyle.color = color;
  }
  if (isIgnored) {
    nameStyle.opacity = 0.45;
    nameStyle.fontStyle = 'italic';
  }

  return (
    <>
      <button
        className={`tree-row${isDir ? ' tree-dir' : ' tree-file'}${isIgnored ? ' tree-ignored' : ''}`}
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
        onClick={() => onToggle(node, path)}
        onContextMenu={(e) => onContextMenu(e, node)}
        type="button"
      >
        {isDir && <span className="tree-arrow"><ChevronIcon open={!!node.expanded} /></span>}
        <span className="tree-icon" style={isIgnored ? { opacity: 0.45 } : undefined}>
          {isDir ? (node.expanded ? <FolderOpenIcon /> : <FolderClosedIcon />) : <FileIcon name={node.name} />}
        </span>
        {isRenaming ? (
          <InlineInput
            depth={0}
            defaultValue={node.name}
            onSubmit={onRenameSubmit}
            onCancel={onRenameCancel}
          />
        ) : (
          <span className="tree-name" style={nameStyle}>{node.name}</span>
        )}
      </button>
      {isDir && inlineInput && inlineInput.parentPath === node.fullPath && !node.expanded && (
        <InlineInput depth={depth + 1} onSubmit={onInlineSubmit} onCancel={onInlineCancel} />
      )}
      {isDir && node.expanded && (
        <>
          {inlineInput && inlineInput.parentPath === node.fullPath && (
            <InlineInput depth={depth + 1} onSubmit={onInlineSubmit} onCancel={onInlineCancel} />
          )}
          {node.children?.map((child, i) => (
            <FileTreeNode
              key={child.name}
              node={child}
              path={[...path, i]}
              depth={depth + 1}
              onToggle={onToggle}
              rootPath={rootPath}
              gitStatus={gitStatus}
              onContextMenu={onContextMenu}
              renameState={renameState}
              onRenameSubmit={onRenameSubmit}
              onRenameCancel={onRenameCancel}
              inlineInput={inlineInput}
              onInlineSubmit={onInlineSubmit}
              onInlineCancel={onInlineCancel}
            />
          ))}
        </>
      )}
    </>
  );
}

type InlineInputProps = {
  depth: number;
  defaultValue?: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
};

function InlineInput({ depth, defaultValue = '', onSubmit, onCancel }: InlineInputProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const timer = setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
        if (defaultValue) {
          const dotIdx = defaultValue.lastIndexOf('.');
          inputRef.current.setSelectionRange(0, dotIdx > 0 ? dotIdx : defaultValue.length);
        }
      }
    }, 50);
    return () => clearTimeout(timer);
  }, [defaultValue]);

  return (
    <div style={{ paddingLeft: `${depth * 16 + 28}px`, padding: '2px 8px 2px 0' }}>
      <input
        ref={inputRef}
        className="tree-inline-input"
        defaultValue={defaultValue}
        autoFocus
        placeholder={defaultValue ? undefined : 'Enter name...'}
        style={{ marginLeft: `${depth * 16 + 28}px` }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            onSubmit((e.target as HTMLInputElement).value);
          } else if (e.key === 'Escape') {
            onCancel();
          }
        }}
        onBlur={(e) => {
          const val = e.target.value.trim();
          if (val && val !== defaultValue) {
            onSubmit(val);
          } else {
            onCancel();
          }
        }}
      />
    </div>
  );
}

function toTreeNodes(parentPath: string, entries: DirEntry[]): TreeNode[] {
  const sep = parentPath.includes('\\') ? '\\' : '/';
  return entries
    .sort((a, b) => {
      if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
      return a.name.localeCompare(b.name);
    })
    .map((e) => ({
      ...e,
      fullPath: parentPath.endsWith(sep) ? parentPath + e.name : parentPath + sep + e.name,
      ignored: e.ignored,
    }));
}

function updateAtPath(nodes: TreeNode[], path: number[], updated: TreeNode): TreeNode[] {
  if (path.length === 1) {
    return nodes.map((n, i) => (i === path[0] ? updated : n));
  }
  return nodes.map((n, i) => {
    if (i === path[0] && n.children) {
      return { ...n, children: updateAtPath(n.children, path.slice(1), updated) };
    }
    return n;
  });
}

function collapseAll(nodes: TreeNode[]): TreeNode[] {
  return nodes.map((n) => {
    if (n.type === 'dir') {
      return { ...n, expanded: false, children: n.children ? collapseAll(n.children) : undefined };
    }
    return n;
  });
}
