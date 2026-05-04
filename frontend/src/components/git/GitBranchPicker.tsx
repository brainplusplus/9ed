import { useCallback, useMemo, useState } from 'react';
import { useGitStore } from '../../stores/git';
import { gitBranchAction } from '../../api';

type GitBranchPickerProps = {
  projectPath: string;
};

export function GitBranchPicker({ projectPath }: GitBranchPickerProps) {
  const branches = useGitStore((s) => s.branches);
  const refresh = useGitStore((s) => s.refresh);
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newBranchName, setNewBranchName] = useState('');

  const currentBranch = useMemo(() => branches.find((b) => b.current), [branches]);

  const handleSwitch = useCallback(async (name: string) => {
    try {
      await gitBranchAction(projectPath, 'checkout', name);
      await refresh(projectPath);
    } catch (err) {
      console.error('Branch switch failed:', err);
    }
    setDropdownOpen(false);
  }, [projectPath, refresh]);

  const handleCreate = useCallback(async () => {
    if (!newBranchName.trim()) return;
    try {
      await gitBranchAction(projectPath, 'create', newBranchName.trim());
      await refresh(projectPath);
      setNewBranchName('');
      setCreating(false);
    } catch (err) {
      console.error('Branch create failed:', err);
    }
  }, [projectPath, newBranchName, refresh]);

  const handleDelete = useCallback(async (name: string) => {
    try {
      await gitBranchAction(projectPath, 'delete', name);
      await refresh(projectPath);
    } catch (err) {
      console.error('Branch delete failed:', err);
    }
  }, [projectPath, refresh]);

  return (
    <div className="git-branch-picker">
      <button
        className="git-branch-name"
        onClick={() => setDropdownOpen(!dropdownOpen)}
        type="button"
        title="Switch branch"
      >
        🌿 {currentBranch?.name ?? 'detached'}
      </button>
      {currentBranch && (currentBranch.ahead > 0 || currentBranch.behind > 0) && (
        <span className="git-branch-tracking">
          {currentBranch.ahead > 0 && `↑${currentBranch.ahead}`}
          {currentBranch.behind > 0 && ` ↓${currentBranch.behind}`}
        </span>
      )}
      {dropdownOpen && (
        <div className="git-branch-dropdown">
          {branches.filter((b) => !b.current).map((b) => (
            <div key={b.name} className="git-branch-dropdown-item">
              <button
                className="git-branch-dropdown-btn"
                onClick={() => handleSwitch(b.name)}
                type="button"
              >
                {b.name}
              </button>
              <button
                className="git-file-action"
                onClick={() => handleDelete(b.name)}
                type="button"
                title="Delete branch"
              >
                🗑
              </button>
            </div>
          ))}
          {creating ? (
            <div className="git-branch-create-row">
              <input
                className="git-commit-input"
                style={{ minHeight: 'auto', padding: '4px 8px' }}
                value={newBranchName}
                onChange={(e) => setNewBranchName(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') handleCreate(); }}
                placeholder="new-branch-name"
                autoFocus
              />
              <button className="git-file-action" onClick={handleCreate} type="button">✓</button>
            </div>
          ) : (
            <button
              className="git-branch-dropdown-btn"
              onClick={() => setCreating(true)}
              type="button"
            >
              + New branch
            </button>
          )}
        </div>
      )}
    </div>
  );
}
