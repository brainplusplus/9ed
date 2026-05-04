import { useCallback, useState } from 'react';
import { useGitStore } from '../../stores/git';
import { gitCommit } from '../../api';

type GitCommitBoxProps = {
  projectPath: string;
};

export function GitCommitBox({ projectPath }: GitCommitBoxProps) {
  const status = useGitStore((s) => s.status);
  const refresh = useGitStore((s) => s.refresh);
  const [message, setMessage] = useState('');
  const [committing, setCommitting] = useState(false);

  const stagedCount = status.filter((f) => f.staged).length;
  const canCommit = message.trim().length > 0 && stagedCount > 0 && !committing;

  const handleCommit = useCallback(async () => {
    if (!canCommit) return;
    setCommitting(true);
    try {
      await gitCommit(projectPath, message.trim());
      setMessage('');
      await refresh(projectPath);
    } catch (err) {
      console.error('Commit failed:', err);
    } finally {
      setCommitting(false);
    }
  }, [canCommit, projectPath, message, refresh]);

  return (
    <div className="git-commit-box">
      <textarea
        className="git-commit-input"
        value={message}
        onChange={(e) => setMessage(e.target.value)}
        placeholder="Commit message..."
        rows={3}
        onKeyDown={(e) => {
          if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            handleCommit();
          }
        }}
      />
      <div className="git-commit-actions">
        <button
          className="git-commit-btn primary"
          disabled={!canCommit}
          onClick={handleCommit}
          type="button"
        >
          ✓ Commit
        </button>
        <button className="git-commit-btn" type="button" disabled>
          ▾ More
        </button>
      </div>
    </div>
  );
}
