import { useCallback, useEffect, useMemo } from 'react';
import { useGitStore } from '../../stores/git';
import { getGitFileAtHEAD, getFileContent } from '../../api';
import { GitBranchPicker } from './GitBranchPicker';
import { GitCommitBox } from './GitCommitBox';
import { GitStatusList } from './GitStatusList';
import { GitStashPanel } from './GitStashPanel';
import { GitLogView } from './GitLogView';
import type { GitFileStatus } from '../../types';

type GitPanelProps = {
  projectPath: string;
  onOpenDiff: (filePath: string, original: string, modified: string, language: string) => void;
};

function languageFromPath(filePath: string): string {
  const ext = filePath.split('.').pop()?.toLowerCase() ?? '';
  const map: Record<string, string> = {
    ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript',
    go: 'go', py: 'python', rs: 'rust', java: 'java', c: 'c', cpp: 'cpp',
    h: 'c', hpp: 'cpp', cs: 'csharp', rb: 'ruby', php: 'php',
    html: 'html', css: 'css', scss: 'scss', less: 'less',
    json: 'json', yaml: 'yaml', yml: 'yaml', toml: 'toml',
    md: 'markdown', sql: 'sql', sh: 'shell', bash: 'shell',
    xml: 'xml', svg: 'xml', dockerfile: 'dockerfile',
    vue: 'vue', svelte: 'svelte',
  };
  return map[ext] ?? 'plaintext';
}

export function GitPanel({ projectPath, onOpenDiff }: GitPanelProps) {
  const status = useGitStore((s) => s.status);
  const refresh = useGitStore((s) => s.refresh);
  const loading = useGitStore((s) => s.loading);
  const error = useGitStore((s) => s.error);
  const clearError = useGitStore((s) => s.clearError);
  const isRepo = useGitStore((s) => s.isRepo);

  useEffect(() => {
    refresh(projectPath);
  }, [projectPath, refresh]);

  if (!isRepo) {
    return (
      <div className="git-panel">
        <div className="git-empty-state" style={{ padding: '24px 16px', textAlign: 'center' }}>
          <div style={{ marginBottom: '8px', opacity: 0.5 }}>
            <svg width="32" height="32" viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
              <circle cx="10" cy="8" r="3" stroke="currentColor" strokeWidth="1.5" />
              <circle cx="10" cy="24" r="3" stroke="currentColor" strokeWidth="1.5" />
              <circle cx="22" cy="16" r="3" stroke="currentColor" strokeWidth="1.5" />
              <line x1="10" y1="11" x2="10" y2="21" stroke="currentColor" strokeWidth="1.3" />
              <path d="M10 11 C10 14, 20 13, 20 16" stroke="currentColor" strokeWidth="1.3" fill="none" />
              <line x1="22" y1="13" x2="22" y2="6" stroke="currentColor" strokeWidth="1.3" />
              <line x1="3" y1="6" x2="29" y2="26" stroke="currentColor" strokeWidth="1.5" opacity="0.4" />
            </svg>
          </div>
          <div style={{ color: 'var(--text-muted)', fontSize: '13px' }}>Not a Git repository</div>
          <div style={{ color: 'var(--text-faint)', fontSize: '11px', marginTop: '4px' }}>
            Initialize with <code style={{ background: 'var(--bg-hover)', padding: '1px 4px', borderRadius: '3px' }}>git init</code> to enable source control
          </div>
        </div>
      </div>
    );
  }

  const stagedFiles = useMemo(() => status.filter((f) => f.staged), [status]);
  const unstagedFiles = useMemo(() => status.filter((f) => !f.staged), [status]);

  const handleFileClick = useCallback(async (file: GitFileStatus) => {
    try {
      const sep = projectPath.includes('\\') ? '\\' : '/';
      const fullPath = projectPath + sep + file.path.replace(/\//g, sep);
      const [headContent, workingContent] = await Promise.all([
        getGitFileAtHEAD(projectPath, file.path).catch(() => ''),
        getFileContent(fullPath).then((fc) => fc.content).catch(() => ''),
      ]);
      const lang = languageFromPath(file.path);
      onOpenDiff(file.path, headContent, workingContent, lang);
    } catch (err) {
      console.error('Failed to open diff:', err);
    }
  }, [projectPath, onOpenDiff]);

  return (
    <div className="git-panel">
      {error && (
        <div className="status-banner error" style={{ margin: '8px 12px' }}>
          {error}
          <button className="git-file-action" onClick={clearError} type="button" style={{ marginLeft: '8px' }}>×</button>
        </div>
      )}
      {loading && <div className="tree-loading">Refreshing...</div>}
      <GitBranchPicker projectPath={projectPath} />
      <GitCommitBox projectPath={projectPath} />
      <GitStatusList
        files={stagedFiles}
        title="STAGED CHANGES"
        staged={true}
        projectPath={projectPath}
        onFileClick={handleFileClick}
      />
      <GitStatusList
        files={unstagedFiles}
        title="CHANGES"
        staged={false}
        projectPath={projectPath}
        onFileClick={handleFileClick}
      />
      <GitStashPanel projectPath={projectPath} />
      <GitLogView projectPath={projectPath} />
    </div>
  );
}
