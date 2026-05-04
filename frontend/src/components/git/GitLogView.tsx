import { useCallback } from 'react';
import { useGitStore } from '../../stores/git';

type GitLogViewProps = {
  projectPath: string;
};

export function GitLogView({ projectPath }: GitLogViewProps) {
  const commits = useGitStore((s) => s.commits);
  const loadMoreCommits = useGitStore((s) => s.loadMoreCommits);

  const handleLoadMore = useCallback(() => {
    loadMoreCommits(projectPath);
  }, [projectPath, loadMoreCommits]);

  if (commits.length === 0) return null;

  return (
    <div className="git-section">
      <div className="git-section-header">
        <span>COMMITS</span>
      </div>
      <div className="git-log-list">
        {commits.map((commit) => (
          <div key={commit.hash} className="git-log-item">
            <span className="git-log-hash">{commit.shortHash}</span>
            <span className="git-log-message" title={commit.message}>{commit.message}</span>
            <span className="git-log-date">{commit.relativeDate}</span>
          </div>
        ))}
        <button className="git-load-more" onClick={handleLoadMore} type="button">
          Load more...
        </button>
      </div>
    </div>
  );
}
