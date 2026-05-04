import { useCallback } from 'react';
import { useGitStore } from '../../stores/git';
import { gitStage, gitUnstage, gitDiscard } from '../../api';
import type { GitFileStatus } from '../../types';

type GitStatusListProps = {
  files: GitFileStatus[];
  title: string;
  staged: boolean;
  projectPath: string;
  onFileClick: (file: GitFileStatus) => void;
};

function statusIcon(status: GitFileStatus['status']): string {
  switch (status) {
    case 'modified': return 'M';
    case 'added': return 'A';
    case 'deleted': return 'D';
    case 'renamed': return 'R';
    case 'untracked': return '?';
  }
}

function statusClass(status: GitFileStatus['status']): string {
  switch (status) {
    case 'modified': return 'modified';
    case 'added': return 'added';
    case 'deleted': return 'deleted';
    case 'renamed': return 'added';
    case 'untracked': return 'untracked';
  }
}

export function GitStatusList({ files, title, staged, projectPath, onFileClick }: GitStatusListProps) {
  const refresh = useGitStore((s) => s.refresh);

  const handleStageAll = useCallback(async () => {
    const paths = files.map((f) => f.path);
    if (paths.length === 0) return;
    try {
      await gitStage(projectPath, paths);
      await refresh(projectPath);
    } catch (err) {
      console.error('Stage all failed:', err);
    }
  }, [files, projectPath, refresh]);

  const handleUnstageAll = useCallback(async () => {
    const paths = files.map((f) => f.path);
    if (paths.length === 0) return;
    try {
      await gitUnstage(projectPath, paths);
      await refresh(projectPath);
    } catch (err) {
      console.error('Unstage all failed:', err);
    }
  }, [files, projectPath, refresh]);

  const handleAction = useCallback(async (file: GitFileStatus) => {
    try {
      if (staged) {
        await gitUnstage(projectPath, [file.path]);
      } else {
        await gitStage(projectPath, [file.path]);
      }
      await refresh(projectPath);
    } catch (err) {
      console.error('Stage/unstage failed:', err);
    }
  }, [staged, projectPath, refresh]);

  const handleDiscard = useCallback(async (file: GitFileStatus) => {
    try {
      await gitDiscard(projectPath, [file.path]);
      await refresh(projectPath);
    } catch (err) {
      console.error('Discard failed:', err);
    }
  }, [projectPath, refresh]);

  if (files.length === 0) return null;

  return (
    <div className="git-section">
      <div className="git-section-header">
        <span>{title} <span className="git-section-count">{files.length}</span></span>
        <div style={{ display: 'flex', gap: '4px' }}>
          {!staged && (
            <button className="git-file-action" onClick={handleStageAll} type="button" title="Stage All">
              +
            </button>
          )}
          {staged && (
            <button className="git-file-action" onClick={handleUnstageAll} type="button" title="Unstage All">
              −
            </button>
          )}
        </div>
      </div>
      {files.map((file) => (
        <div key={file.path} className="git-file-row" onClick={() => onFileClick(file)}>
          <span className={`git-file-status ${statusClass(file.status)}`}>
            {statusIcon(file.status)}
          </span>
          <span className="git-file-path" title={file.path}>
            <span className="git-file-name">{file.path.split(/[/\\]/).pop() ?? file.path}</span>
            <span className="git-file-dir">{file.path.split(/[/\\]/).slice(0, -1).join('/')}</span>
          </span>
          {!staged && (
            <button
              className="git-file-action"
              onClick={(e) => { e.stopPropagation(); handleDiscard(file); }}
              type="button"
              title="Discard Changes"
            >
              ↺
            </button>
          )}
          <button
            className="git-file-action"
            onClick={(e) => { e.stopPropagation(); handleAction(file); }}
            type="button"
            title={staged ? 'Unstage' : 'Stage'}
          >
            {staged ? '−' : '+'}
          </button>
        </div>
      ))}
    </div>
  );
}
