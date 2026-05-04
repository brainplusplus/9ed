import { useCallback, useEffect, useState } from 'react';
import { getConfig, getDrives, getFileTree, getRecentProjects, removeRecentProject, type RecentProject } from '../../api';
import { useWorkspaceStore } from '../../stores/workspace';
import type { DirEntry } from '../../types';

type TreeNode = DirEntry & { children?: TreeNode[]; expanded?: boolean; fullPath: string };

export function ProjectPicker() {
  const addProject = useWorkspaceStore((s) => s.addProject);
  const projects = useWorkspaceStore((s) => s.projects);
  const setActiveProject = useWorkspaceStore((s) => s.setActiveProject);
  const setShowPicker = useWorkspaceStore((s) => s.setShowPicker);

  const [recentProjects, setRecentProjects] = useState<RecentProject[]>([]);
  const [drives, setDrives] = useState<string[]>([]);
  const [rootPath, setRootPath] = useState('');
  const [nodes, setNodes] = useState<TreeNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void getRecentProjects().then(setRecentProjects).catch(() => {});
  }, []);

  const loadTree = useCallback(async (path: string) => {
    setLoading(true);
    setError(null);
    setRootPath(path);
    try {
      const entries = await getFileTree(path);
      setNodes(
        entries
          .filter((e) => e.type === 'dir')
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((e) => ({ ...e, fullPath: joinPath(path, e.name) })),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
      setNodes([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    async function init() {
      try {
        const [config, availableDrives] = await Promise.all([getConfig(), getDrives()]);
        if (cancelled) return;

        setDrives(availableDrives);

        const root = config.workspaceRoot || availableDrives[0] || '/';
        await loadTree(root);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load');
          setLoading(false);
        }
      }
    }
    void init();
    return () => { cancelled = true; };
  }, [loadTree]);

  const handleDriveChange = useCallback((drive: string) => {
    void loadTree(drive);
  }, [loadTree]);

  const toggleExpand = useCallback(async (node: TreeNode, path: number[]) => {
    if (node.expanded) {
      setNodes((prev) => updateNodeAtPath(prev, path, { ...node, expanded: false }));
      return;
    }

    try {
      const entries = await getFileTree(node.fullPath);
      const children = entries
        .filter((e) => e.type === 'dir')
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((e) => ({ ...e, fullPath: joinPath(node.fullPath, e.name) }));
      setNodes((prev) => updateNodeAtPath(prev, path, { ...node, expanded: true, children }));
    } catch {
      setNodes((prev) => updateNodeAtPath(prev, path, { ...node, expanded: true, children: [] }));
    }
  }, []);

  const handleOpen = useCallback((node: TreeNode) => {
    addProject(node.fullPath, node.name);
  }, [addProject]);

  const handleSelectExisting = useCallback((projectId: string) => {
    setActiveProject(projectId);
    setShowPicker(false);
  }, [setActiveProject, setShowPicker]);

  return (
    <main className="picker-shell">
      <div className="picker-card">
        <header className="picker-header">
          <p className="eyebrow">Web IDE</p>
          <h1>Open Project</h1>
        </header>

        {recentProjects.length > 0 && (
          <div className="picker-section">
            <h3 className="picker-section-title">Recent Projects</h3>
            <div className="picker-projects">
              {recentProjects.map((rp) => (
                <div key={rp.path} className="picker-project-row">
                  <button
                    className="picker-project-btn"
                    onClick={() => addProject(rp.path, rp.name)}
                    type="button"
                  >
                    <span className="picker-project-name">{rp.name}</span>
                    <span className="picker-project-path">{rp.path}</span>
                  </button>
                  <button
                    className="picker-project-remove"
                    onClick={(e) => {
                      e.stopPropagation();
                      void removeRecentProject(rp.path).then(() => {
                        setRecentProjects((prev) => prev.filter((p) => p.path !== rp.path));
                      });
                    }}
                    type="button"
                    title="Remove from recent"
                  >
                    ×
                  </button>
                </div>
              ))}
            </div>
            <div className="picker-divider" />
          </div>
        )}

        {projects.length > 0 && (
          <div className="picker-section">
            <h3 className="picker-section-title">Opened Projects</h3>
            <div className="picker-projects">
              {projects.map((p) => (
                <button
                  key={p.id}
                  className="picker-project-btn"
                  onClick={() => handleSelectExisting(p.id)}
                  type="button"
                >
                  <span className="picker-project-name">{p.name}</span>
                  <span className="picker-project-path">{p.path}</span>
                </button>
              ))}
            </div>
            <div className="picker-divider" />
          </div>
        )}

        <h3 className="picker-section-title">Open New Folder</h3>

        {drives.length > 1 && (
          <div className="drive-picker">
            <label className="drive-label">Drive</label>
            <div className="drive-list">
              {drives.map((drive) => (
                <button
                  key={drive}
                  className={`drive-btn${drive === rootPath ? ' active' : ''}`}
                  onClick={() => handleDriveChange(drive)}
                  type="button"
                >
                  {drive}
                </button>
              ))}
            </div>
          </div>
        )}

        <p className="picker-root">Browsing: {rootPath}</p>

        {error && <div className="status-banner error">{error}</div>}
        {loading && <div className="status-banner">Loading directories…</div>}

        <div className="picker-tree">
          {nodes.map((node, i) => (
            <FolderNode key={node.name} node={node} path={[i]} depth={0} onToggle={toggleExpand} onOpen={handleOpen} />
          ))}
          {!loading && nodes.length === 0 && <p className="picker-empty">No directories found.</p>}
        </div>
      </div>
    </main>
  );
}

type FolderNodeProps = {
  node: TreeNode;
  path: number[];
  depth: number;
  onToggle: (node: TreeNode, path: number[]) => void;
  onOpen: (node: TreeNode) => void;
};

function FolderNode({ node, path, depth, onToggle, onOpen }: FolderNodeProps) {
  return (
    <div className="folder-node">
      <div className="folder-row" style={{ paddingLeft: `${depth * 20 + 8}px` }}>
        <button className="folder-toggle" onClick={() => onToggle(node, path)} type="button">
          {node.expanded ? '▼' : '▶'}
        </button>
        <span className="folder-name">{node.name}</span>
        <button className="folder-open-btn" onClick={() => onOpen(node)} type="button">
          Open
        </button>
      </div>
      {node.expanded && node.children?.map((child, i) => (
        <FolderNode key={child.name} node={child} path={[...path, i]} depth={depth + 1} onToggle={onToggle} onOpen={onOpen} />
      ))}
    </div>
  );
}

function joinPath(base: string, name: string): string {
  if (base.includes('\\')) {
    return base.endsWith('\\') ? base + name : base + '\\' + name;
  }
  return base.endsWith('/') ? base + name : base + '/' + name;
}

function updateNodeAtPath(nodes: TreeNode[], path: number[], updated: TreeNode): TreeNode[] {
  if (path.length === 1) {
    return nodes.map((n, i) => (i === path[0] ? updated : n));
  }
  return nodes.map((n, i) => {
    if (i === path[0] && n.children) {
      return { ...n, children: updateNodeAtPath(n.children, path.slice(1), updated) };
    }
    return n;
  });
}
