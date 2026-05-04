import { useEffect } from 'react';
import { useGitStore } from '../stores/git';
import type { GutterChange } from '../types';

export function useGitGutter(projectPath: string | null, filePath: string | null): GutterChange[] {
  const refreshGutter = useGitStore((s) => s.refreshGutter);
  const gutterChanges = useGitStore((s) => s.gutterChanges);

  useEffect(() => {
    if (!projectPath || !filePath) return;
    refreshGutter(projectPath, filePath);
  }, [projectPath, filePath, refreshGutter]);

  if (!filePath) return [];
  return gutterChanges[filePath] ?? [];
}
