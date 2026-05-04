import { useEffect, useRef, useCallback } from 'react';
import { useGitStore } from '../stores/git';

const POLL_INTERVAL = 5000;
const DEBOUNCE_DELAY = 1000;

export function useGitStatus(projectPath: string | null) {
  const refresh = useGitStore((s) => s.refresh);
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!projectPath) return;

    refresh(projectPath);

    const interval = setInterval(() => {
      refresh(projectPath);
    }, POLL_INTERVAL);

    return () => {
      clearInterval(interval);
    };
  }, [projectPath, refresh]);

  const triggerRefresh = useCallback(() => {
    if (!projectPath) return;

    if (debounceTimer.current) {
      clearTimeout(debounceTimer.current);
    }

    debounceTimer.current = setTimeout(() => {
      refresh(projectPath);
      debounceTimer.current = null;
    }, DEBOUNCE_DELAY);
  }, [projectPath, refresh]);

  useEffect(() => {
    return () => {
      if (debounceTimer.current) {
        clearTimeout(debounceTimer.current);
      }
    };
  }, []);

  return { triggerRefresh };
}
