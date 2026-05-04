import { create } from 'zustand';
import type { ActivePanel, FileTab, Project } from '../types';

const STORAGE_KEY = 'webide-recent-projects';

type RecentProject = { path: string; name: string; lastOpened: number };

function loadRecentProjects(): RecentProject[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch { return []; }
}

function saveRecentProject(path: string, name: string) {
  const recent = loadRecentProjects().filter((p) => p.path !== path);
  recent.unshift({ path, name, lastOpened: Date.now() });
  localStorage.setItem(STORAGE_KEY, JSON.stringify(recent.slice(0, 20)));
}

export { loadRecentProjects, type RecentProject };

type WorkspaceState = {
  projects: Project[];
  activeProjectId: string | null;
  activePanel: ActivePanel;
  sidebarVisible: boolean;
  terminalVisible: boolean;
  chatVisible: boolean;
  showPicker: boolean;

  addProject: (path: string, name: string) => void;
  removeProject: (id: string) => void;
  setActiveProject: (id: string) => void;
  setActivePanel: (panel: ActivePanel) => void;
  toggleSidebar: () => void;
  toggleTerminal: () => void;
  toggleChat: () => void;
  setShowPicker: (show: boolean) => void;

  openFile: (projectId: string, file: FileTab) => void;
  closeFile: (projectId: string, fileId: string) => void;
  setActiveFile: (projectId: string, fileId: string) => void;
  updateFileContent: (projectId: string, fileId: string, content: string) => void;
  markFileSaved: (projectId: string, fileId: string) => void;

  addTerminalSession: (projectId: string, sessionId: string) => void;
  removeTerminalSession: (projectId: string, sessionId: string) => void;
};

function generateId(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
}

function updateProject(projects: Project[], projectId: string, updater: (p: Project) => Project): Project[] {
  return projects.map((p) => (p.id === projectId ? updater(p) : p));
}

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  projects: [],
  activeProjectId: null,
  activePanel: 'explorer',
  sidebarVisible: true,
  terminalVisible: true,
  chatVisible: false,
  showPicker: false,

  addProject: (path, name) => {
    const id = generateId();
    saveRecentProject(path, name);
    set((state) => ({
      projects: [...state.projects, { id, path, name, openFiles: [], activeFileId: null, terminalSessions: [] }],
      activeProjectId: id,
      showPicker: false,
    }));
  },

  removeProject: (id) =>
    set((state) => {
      const next = state.projects.filter((p) => p.id !== id);
      return {
        projects: next,
        activeProjectId: state.activeProjectId === id ? (next[0]?.id ?? null) : state.activeProjectId,
      };
    }),

  setActiveProject: (id) => set({ activeProjectId: id }),

  setActivePanel: (panel) => set({ activePanel: panel }),

  toggleSidebar: () => set((state) => ({ sidebarVisible: !state.sidebarVisible })),

  toggleTerminal: () => set((state) => ({ terminalVisible: !state.terminalVisible })),

  toggleChat: () => set((state) => ({ chatVisible: !state.chatVisible })),

  setShowPicker: (show) => set({ showPicker: show }),

  openFile: (projectId, file) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => {
        const exists = p.openFiles.find((f) => f.path === file.path);
        if (exists) {
          return { ...p, activeFileId: exists.id };
        }
        return { ...p, openFiles: [...p.openFiles, file], activeFileId: file.id };
      }),
    })),

  closeFile: (projectId, fileId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => {
        const idx = p.openFiles.findIndex((f) => f.id === fileId);
        const next = p.openFiles.filter((f) => f.id !== fileId);
        let nextActive = p.activeFileId;
        if (p.activeFileId === fileId) {
          const fallback = next[idx] ?? next[idx - 1] ?? null;
          nextActive = fallback?.id ?? null;
        }
        return { ...p, openFiles: next, activeFileId: nextActive };
      }),
    })),

  setActiveFile: (projectId, fileId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({ ...p, activeFileId: fileId })),
    })),

  updateFileContent: (projectId, fileId, content) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        openFiles: p.openFiles.map((f) => (f.id === fileId ? { ...f, content, modified: true } : f)),
      })),
    })),

  markFileSaved: (projectId, fileId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        openFiles: p.openFiles.map((f) => (f.id === fileId ? { ...f, modified: false } : f)),
      })),
    })),

  addTerminalSession: (projectId, sessionId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        terminalSessions: [...p.terminalSessions, sessionId],
      })),
    })),

  removeTerminalSession: (projectId, sessionId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        terminalSessions: p.terminalSessions.filter((s) => s !== sessionId),
      })),
    })),
}));
