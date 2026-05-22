import { useEffect, useRef, useCallback } from 'react';
import { useGitStore } from '../stores/git';

const POLL_INTERVAL = 5000;
const DEBOUNCE_DELAY = 1000;
const INITIAL_REFRESH_DELAY = 1200;

export function useGitStatus(projectPath: string | null) {
  const refresh = useGitStore((s) => s.refresh);
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!projectPath) return;

    const initialTimer = setTimeout(() => {
      void refresh(projectPath);
    }, INITIAL_REFRESH_DELAY);

    const interval = setInterval(() => {
      void refresh(projectPath);
    }, POLL_INTERVAL + INITIAL_REFRESH_DELAY);

    return () => {
      clearTimeout(initialTimer);
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
