import { useCallback, useEffect, useState } from 'react';
import { useGitStore } from '../../stores/git';
import { gitStashAction } from '../../api';

type GitStashPanelProps = {
  projectPath: string;
};

export function GitStashPanel({ projectPath }: GitStashPanelProps) {
  const stashes = useGitStore((s) => s.stashes);
  const refreshStashes = useGitStore((s) => s.refreshStashes);
  const refresh = useGitStore((s) => s.refresh);
  const [collapsed, setCollapsed] = useState(true);

  useEffect(() => {
    refreshStashes(projectPath);
  }, [projectPath, refreshStashes]);

  const handlePop = useCallback(async (index: number) => {
    try {
      await gitStashAction(projectPath, 'pop', index);
      await refreshStashes(projectPath);
      await refresh(projectPath);
    } catch (err) {
      console.error('Stash pop failed:', err);
    }
  }, [projectPath, refreshStashes, refresh]);

  const handleDrop = useCallback(async (index: number) => {
    try {
      await gitStashAction(projectPath, 'drop', index);
      await refreshStashes(projectPath);
    } catch (err) {
      console.error('Stash drop failed:', err);
    }
  }, [projectPath, refreshStashes]);

  const handleSave = useCallback(async () => {
    try {
      await gitStashAction(projectPath, 'push', undefined, 'WIP');
      await refreshStashes(projectPath);
      await refresh(projectPath);
    } catch (err) {
      console.error('Stash save failed:', err);
    }
  }, [projectPath, refreshStashes, refresh]);

  return (
    <div className="git-section">
      <div className="git-section-header">
        <button
          type="button"
          style={{ background: 'none', border: 'none', color: 'inherit', cursor: 'pointer', padding: 0, font: 'inherit', letterSpacing: 'inherit', textTransform: 'inherit' as const }}
          onClick={() => setCollapsed(!collapsed)}
        >
          {collapsed ? '▸' : '▾'} STASHES ({stashes.length})
        </button>
        <button className="git-file-action" onClick={handleSave} type="button" title="Stash Save">
          💾
        </button>
      </div>
      {!collapsed && stashes.map((stash) => (
        <div key={stash.index} className="git-stash-item">
          <span className="git-file-path">{stash.message}</span>
          <button
            className="git-file-action"
            onClick={() => handlePop(stash.index)}
            type="button"
            title="Pop"
          >
            ↑
          </button>
          <button
            className="git-file-action"
            onClick={() => handleDrop(stash.index)}
            type="button"
            title="Drop"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
