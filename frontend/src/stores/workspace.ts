import { create } from 'zustand';
import type { ActivePanel, FileTab, Project } from '../types';
import { saveRecentProject } from '../api';

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
  restoreLastActiveProject: () => void;
  setActivePanel: (panel: ActivePanel) => void;
  toggleSidebar: () => void;
  toggleTerminal: () => void;
  toggleChat: () => void;
  setShowPicker: (show: boolean) => void;

  openFile: (projectId: string, file: FileTab) => void;
  closeFile: (projectId: string, fileId: string) => void;
  closeOtherFiles: (projectId: string, fileId: string) => void;
  closeFilesToLeft: (projectId: string, fileId: string) => void;
  closeFilesToRight: (projectId: string, fileId: string) => void;
  closeAllFiles: (projectId: string) => void;
  setActiveFile: (projectId: string, fileId: string) => void;
  updateFileContent: (projectId: string, fileId: string, content: string) => void;
  markFileSaved: (projectId: string, fileId: string) => void;
  renameOpenFile: (projectId: string, oldPath: string, newPath: string, newName: string) => void;
  closeFileByPath: (projectId: string, filePath: string) => void;
  markFileConflict: (projectId: string, fileId: string) => void;
  markFileDeleted: (projectId: string, filePath: string) => void;
  resolveConflict: (projectId: string, fileId: string, action: 'overwrite' | 'revert', newContent?: string) => void;

  addTerminalSession: (projectId: string, sessionId: string) => void;
  removeTerminalSession: (projectId: string, sessionId: string) => void;
};

type StoredActiveProject = {
  id: string;
  path: string;
  name: string;
};

const ACTIVE_PROJECT_STORAGE_KEY = '9ed.activeProject.v1';

function storage(): Storage | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
}

function generateId(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
}

function readStoredActiveProject(): StoredActiveProject | null {
  const store = storage();
  if (!store) return null;
  try {
    const raw = store.getItem(ACTIVE_PROJECT_STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<StoredActiveProject>;
    if (!parsed.path || !parsed.name) return null;
    return {
      id: parsed.id || generateId(),
      path: parsed.path,
      name: parsed.name,
    };
  } catch {
    return null;
  }
}

function writeStoredActiveProject(project: Pick<Project, 'id' | 'path' | 'name'> | null): void {
  const store = storage();
  if (!store) return;
  try {
    if (!project) {
      store.removeItem(ACTIVE_PROJECT_STORAGE_KEY);
      return;
    }
    store.setItem(ACTIVE_PROJECT_STORAGE_KEY, JSON.stringify({
      id: project.id,
      path: project.path,
      name: project.name,
    }));
  } catch {
  }
}

function updateProject(projects: Project[], projectId: string, updater: (p: Project) => Project): Project[] {
  return projects.map((p) => (p.id === projectId ? updater(p) : p));
}

const initialActiveProject = readStoredActiveProject();

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  projects: initialActiveProject ? [{
    id: initialActiveProject.id,
    path: initialActiveProject.path,
    name: initialActiveProject.name,
    openFiles: [],
    activeFileId: null,
    terminalSessions: [],
  }] : [],
  activeProjectId: initialActiveProject?.id ?? null,
  activePanel: 'explorer',
  sidebarVisible: true,
  terminalVisible: true,
  chatVisible: true,
  showPicker: false,

  addProject: (path, name) => {
    void saveRecentProject(path, name);
    set((state) => ({
      ...(() => {
        const existing = state.projects.find((p) => p.path === path);
        if (existing) {
          writeStoredActiveProject(existing);
          return {
            projects: state.projects,
            activeProjectId: existing.id,
          };
        }
        const id = generateId();
        const project = { id, path, name, openFiles: [], activeFileId: null, terminalSessions: [] };
        writeStoredActiveProject(project);
        return {
          projects: [...state.projects, project],
          activeProjectId: id,
        };
      })(),
      showPicker: false,
    }));
  },

  removeProject: (id) =>
    set((state) => {
      const next = state.projects.filter((p) => p.id !== id);
      const nextActiveId = state.activeProjectId === id ? (next[0]?.id ?? null) : state.activeProjectId;
      const nextActive = next.find((p) => p.id === nextActiveId) ?? null;
      writeStoredActiveProject(nextActive);
      return {
        projects: next,
        activeProjectId: nextActiveId,
      };
    }),

  setActiveProject: (id) => set((state) => {
    const project = state.projects.find((p) => p.id === id) ?? null;
    if (project) writeStoredActiveProject(project);
    return { activeProjectId: id };
  }),

  restoreLastActiveProject: () => set((state) => {
    if (state.activeProjectId) return state;
    const stored = readStoredActiveProject();
    if (!stored) return state;
    const existing = state.projects.find((p) => p.path === stored.path);
    if (existing) {
      return { activeProjectId: existing.id, showPicker: false };
    }
    return {
      projects: [...state.projects, {
        id: stored.id,
        path: stored.path,
        name: stored.name,
        openFiles: [],
        activeFileId: null,
        terminalSessions: [],
      }],
      activeProjectId: stored.id,
      showPicker: false,
    };
  }),

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

  closeOtherFiles: (projectId, fileId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => {
        const kept = p.openFiles.filter((f) => f.id === fileId);
        return { ...p, openFiles: kept, activeFileId: kept.length ? fileId : null };
      }),
    })),

  closeFilesToLeft: (projectId, fileId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => {
        const idx = p.openFiles.findIndex((f) => f.id === fileId);
        const kept = p.openFiles.slice(idx);
        const activeStillOpen = kept.some((f) => f.id === p.activeFileId);
        return { ...p, openFiles: kept, activeFileId: activeStillOpen ? p.activeFileId : fileId };
      }),
    })),

  closeFilesToRight: (projectId, fileId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => {
        const idx = p.openFiles.findIndex((f) => f.id === fileId);
        const kept = p.openFiles.slice(0, idx + 1);
        const activeStillOpen = kept.some((f) => f.id === p.activeFileId);
        return { ...p, openFiles: kept, activeFileId: activeStillOpen ? p.activeFileId : fileId };
      }),
    })),

  closeAllFiles: (projectId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        openFiles: [],
        activeFileId: null,
      })),
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

  markFileConflict: (projectId, fileId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        openFiles: p.openFiles.map((f) => (f.id === fileId ? { ...f, conflict: true } : f)),
      })),
    })),

  markFileDeleted: (projectId, filePath) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        openFiles: p.openFiles.map((f) => (f.path === filePath ? { ...f, deleted: true } : f)),
      })),
    })),

  resolveConflict: (projectId, fileId, action, newContent) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        openFiles: p.openFiles.map((f) => {
          if (f.id !== fileId) return f;
          if (action === 'revert' && newContent !== undefined) {
            return { ...f, content: newContent, modified: false, conflict: false };
          }
          return { ...f, conflict: false };
        }),
      })),
    })),

  renameOpenFile: (projectId, oldPath, newPath, newName) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        openFiles: p.openFiles.map((f) =>
          f.path === oldPath ? { ...f, path: newPath, id: newPath, name: newName } : f
        ),
        activeFileId: p.activeFileId === oldPath ? newPath : p.activeFileId,
      })),
    })),

  closeFileByPath: (projectId, filePath) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => {
        const file = p.openFiles.find((f) => f.path === filePath);
        if (!file) return p;
        const idx = p.openFiles.indexOf(file);
        const next = p.openFiles.filter((f) => f.path !== filePath);
        let nextActive = p.activeFileId;
        if (p.activeFileId === file.id) {
          const fallback = next[idx] ?? next[idx - 1] ?? null;
          nextActive = fallback?.id ?? null;
        }
        return { ...p, openFiles: next, activeFileId: nextActive };
      }),
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
