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
  chatVisible: true,
  showPicker: false,

  addProject: (path, name) => {
    const id = generateId();
    void saveRecentProject(path, name);
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
