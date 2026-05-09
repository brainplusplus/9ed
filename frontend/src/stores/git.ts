import { create } from 'zustand';
import type { GitBranch, GitCommit, GitFileStatus, GitStash, GutterChange } from '../types';
import { getGitBranches, getGitDiffLines, getGitLog, getGitStatus, gitStashAction } from '../api';

type GitState = {
  status: GitFileStatus[];
  branches: GitBranch[];
  commits: GitCommit[];
  stashes: GitStash[];
  gutterChanges: Record<string, GutterChange[]>;
  loading: boolean;
  error: string | null;
  isRepo: boolean;

  refresh: (projectPath: string) => Promise<void>;
  refreshGutter: (projectPath: string, filePath: string) => Promise<void>;
  loadMoreCommits: (projectPath: string) => Promise<void>;
  refreshStashes: (projectPath: string) => Promise<void>;
  clearError: () => void;
};

export const useGitStore = create<GitState>((set, get) => ({
  status: [],
  branches: [],
  commits: [],
  stashes: [],
  gutterChanges: {},
  loading: false,
  error: null,
  isRepo: true,

  refresh: async (projectPath) => {
    const prev = get();
    if (!prev.loading) {
      set({ error: null });
    }
    try {
      const { status, isRepo } = await getGitStatus(projectPath);

      if (!isRepo) {
        set({ status: [], branches: [], commits: [], stashes: [], loading: false, isRepo: false });
        return;
      }

      const [branches, commits] = await Promise.all([
        getGitBranches(projectPath),
        getGitLog(projectPath, 50, 0),
      ]);

      const statusChanged = JSON.stringify(status) !== JSON.stringify(prev.status);
      const branchesChanged = JSON.stringify(branches) !== JSON.stringify(prev.branches);
      const commitsChanged = JSON.stringify(commits) !== JSON.stringify(prev.commits);

      if (statusChanged || branchesChanged || commitsChanged) {
        set({
          status: statusChanged ? status : prev.status,
          branches: branchesChanged ? branches : prev.branches,
          commits: commitsChanged ? commits : prev.commits,
          loading: false,
        });
      } else if (prev.loading) {
        set({ loading: false });
      }
    } catch (e) {
      set({ loading: false, error: e instanceof Error ? e.message : 'Git refresh failed' });
    }
  },

  refreshGutter: async (projectPath, filePath) => {
    try {
      const changes = await getGitDiffLines(projectPath, filePath);
      set((state) => ({
        gutterChanges: { ...state.gutterChanges, [filePath]: changes },
      }));
    } catch {
      set((state) => ({
        gutterChanges: { ...state.gutterChanges, [filePath]: [] },
      }));
    }
  },

  loadMoreCommits: async (projectPath) => {
    const offset = get().commits.length;
    try {
      const more = await getGitLog(projectPath, 50, offset);
      set((state) => ({ commits: [...state.commits, ...more] }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Failed to load commits' });
    }
  },

  refreshStashes: async (projectPath) => {
    try {
      const result = await gitStashAction(projectPath, 'list');
      if (Array.isArray(result)) {
        set({ stashes: result });
      }
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Failed to load stashes' });
    }
  },

  clearError: () => set({ error: null }),
}));
