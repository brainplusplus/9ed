import { create } from 'zustand';
import type { ActivePanel, FileTab, Project, SessionTab } from '../types';
import { saveRecentProject } from '../api';

type WorkspaceState = {
  projects: Project[];
  activeProjectId: string | null;
  activePanel: ActivePanel;
  sidebarVisible: boolean;
  terminalVisible: boolean;
  chatVisible: boolean;
  browserVisible: boolean;
  showPicker: boolean;

  addProject: (path: string, name: string) => void;
  removeProject: (id: string) => void;
  setActiveProject: (id: string) => void;
  restoreLastActiveProject: () => void;
  setActivePanel: (panel: ActivePanel) => void;
  toggleSidebar: () => void;
  toggleTerminal: () => void;
  showTerminal: () => void;
  toggleChat: () => void;
  toggleBrowser: () => void;
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
  addTerminalTab: (projectId: string, tab: SessionTab) => void;
  updateTerminalTab: (projectId: string, sessionId: string, patch: Partial<SessionTab>) => void;
  setActiveTerminalTab: (projectId: string, sessionId: string | null) => void;
  removeTerminalTab: (projectId: string, sessionId: string) => void;
  addBrowserTab: (projectId: string, tabId: string) => void;
  removeBrowserTab: (projectId: string, tabId: string) => void;
  setActiveBrowserTab: (projectId: string, tabId: string | null) => void;
  reconcileBrowserTabs: (liveTabIds: string[]) => void;
};

type StoredActiveProject = {
  id: string;
  path: string;
  name: string;
  browserTabIds?: string[];
  activeBrowserTabId?: string | null;
};

type StoredProject = StoredActiveProject;

type StoredWorkspace = {
  projects: StoredProject[];
  activeProjectId: string | null;
};

const ACTIVE_PROJECT_STORAGE_KEY = '9ed.activeProject.v1';
const OPEN_PROJECTS_STORAGE_KEY = '9ed.openProjects.v1';

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

function emptyProject(project: StoredProject): Project {
  const browserTabs = normalizeBrowserTabs(project.browserTabIds, project.activeBrowserTabId);
  return {
    id: project.id,
    path: project.path,
    name: project.name,
    openFiles: [],
    activeFileId: null,
    terminalTabs: [],
    activeTerminalTabId: null,
    terminalSessions: [],
    browserTabIds: browserTabs.browserTabIds,
    activeBrowserTabId: browserTabs.activeBrowserTabId,
  };
}

function normalizeBrowserTabs(browserTabIds?: string[], activeBrowserTabId?: string | null): { browserTabIds: string[]; activeBrowserTabId: string | null } {
  const browserTabs = Array.from(new Set((browserTabIds ?? []).map((id) => id.trim()).filter(Boolean)));
  const active = activeBrowserTabId && browserTabs.includes(activeBrowserTabId) ? activeBrowserTabId : (browserTabs[0] ?? null);
  return {
    browserTabIds: browserTabs,
    activeBrowserTabId: active,
  };
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
      browserTabIds: normalizeBrowserTabs(parsed.browserTabIds, parsed.activeBrowserTabId).browserTabIds,
      activeBrowserTabId: normalizeBrowserTabs(parsed.browserTabIds, parsed.activeBrowserTabId).activeBrowserTabId,
    };
  } catch {
    return null;
  }
}

function normalizeStoredProject(project: Partial<StoredProject>): StoredProject | null {
  if (!project.path || !project.name) return null;
  const browserTabs = normalizeBrowserTabs(project.browserTabIds, project.activeBrowserTabId);
  return {
    id: project.id || generateId(),
    path: project.path,
    name: project.name,
    browserTabIds: browserTabs.browserTabIds,
    activeBrowserTabId: browserTabs.activeBrowserTabId,
  };
}

function readStoredWorkspace(): StoredWorkspace {
  const store = storage();
  if (!store) return { projects: [], activeProjectId: null };

  try {
    const raw = store.getItem(OPEN_PROJECTS_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<StoredWorkspace>;
      const seen = new Set<string>();
      const projects = (parsed.projects ?? []).reduce<StoredProject[]>((acc, project) => {
        const normalized = normalizeStoredProject(project);
        if (!normalized || seen.has(normalized.path)) return acc;
        seen.add(normalized.path);
        acc.push(normalized);
        return acc;
      }, []);
      const activeProjectId = projects.some((project) => project.id === parsed.activeProjectId)
        ? parsed.activeProjectId ?? null
        : (projects[0]?.id ?? null);
      return { projects, activeProjectId };
    }
  } catch {
  }

  const activeProject = readStoredActiveProject();
  return {
    projects: activeProject ? [activeProject] : [],
    activeProjectId: activeProject?.id ?? null,
  };
}

function writeStoredActiveProject(project: Pick<Project, 'id' | 'path' | 'name' | 'browserTabIds' | 'activeBrowserTabId'> | null): void {
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
      browserTabIds: normalizeBrowserTabs(project.browserTabIds, project.activeBrowserTabId).browserTabIds,
      activeBrowserTabId: normalizeBrowserTabs(project.browserTabIds, project.activeBrowserTabId).activeBrowserTabId,
    }));
  } catch {
  }
}

function writeStoredWorkspace(projects: Pick<Project, 'id' | 'path' | 'name' | 'browserTabIds' | 'activeBrowserTabId'>[], activeProjectId: string | null): void {
  const store = storage();
  if (!store) return;
  try {
    if (projects.length === 0) {
      store.removeItem(OPEN_PROJECTS_STORAGE_KEY);
      writeStoredActiveProject(null);
      return;
    }

    const activeProject = projects.find((project) => project.id === activeProjectId) ?? projects[0] ?? null;
    store.setItem(OPEN_PROJECTS_STORAGE_KEY, JSON.stringify({
      projects: projects.map((project) => ({
        id: project.id,
        path: project.path,
        name: project.name,
        browserTabIds: normalizeBrowserTabs(project.browserTabIds, project.activeBrowserTabId).browserTabIds,
        activeBrowserTabId: normalizeBrowserTabs(project.browserTabIds, project.activeBrowserTabId).activeBrowserTabId,
      })),
      activeProjectId: activeProject?.id ?? null,
    }));
    writeStoredActiveProject(activeProject);
  } catch {
  }
}

function updateProject(projects: Project[], projectId: string, updater: (p: Project) => Project): Project[] {
  return projects.map((p) => (p.id === projectId ? updater(p) : p));
}

const initialWorkspace = readStoredWorkspace();

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  projects: initialWorkspace.projects.map(emptyProject),
  activeProjectId: initialWorkspace.activeProjectId,
  activePanel: 'explorer',
  sidebarVisible: true,
  terminalVisible: true,
  chatVisible: true,
  browserVisible: false,
  showPicker: false,

  addProject: (path, name) => {
    void saveRecentProject(path, name);
    set((state) => ({
      ...(() => {
        const existing = state.projects.find((p) => p.path === path);
        if (existing) {
          writeStoredWorkspace(state.projects, existing.id);
          return {
            projects: state.projects,
            activeProjectId: existing.id,
          };
        }
        const id = generateId();
        const project = emptyProject({ id, path, name });
        const projects = [...state.projects, project];
        writeStoredWorkspace(projects, id);
        return {
          projects,
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
      writeStoredWorkspace(next, nextActiveId);
      return {
        projects: next,
        activeProjectId: nextActiveId,
      };
    }),

  setActiveProject: (id) => set((state) => {
    const project = state.projects.find((p) => p.id === id) ?? null;
    if (project) writeStoredWorkspace(state.projects, project.id);
    return { activeProjectId: id };
  }),

  restoreLastActiveProject: () => set((state) => {
    if (state.activeProjectId) return state;
    const stored = readStoredWorkspace();
    if (stored.projects.length === 0) return state;
    const restoredProjects = stored.projects
      .filter((storedProject) => !state.projects.some((project) => project.path === storedProject.path))
      .map(emptyProject);
    const projects = [...state.projects, ...restoredProjects];
    const activeProject = projects.find((p) => p.id === stored.activeProjectId) ?? projects[0] ?? null;
    if (!activeProject) return state;
    const existing = state.projects.find((p) => p.path === activeProject.path);
    if (existing) {
      return { activeProjectId: existing.id, showPicker: false };
    }
    return {
      projects,
      activeProjectId: activeProject.id,
      showPicker: false,
    };
  }),

  setActivePanel: (panel) => set({ activePanel: panel }),

  toggleSidebar: () => set((state) => ({ sidebarVisible: !state.sidebarVisible })),

  toggleTerminal: () => set((state) => ({ terminalVisible: !state.terminalVisible })),

  showTerminal: () => set({ terminalVisible: true }),

  toggleChat: () => set((state) => ({ chatVisible: !state.chatVisible })),

  toggleBrowser: () => set((state) => ({ browserVisible: !state.browserVisible })),

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

  addTerminalTab: (projectId, tab) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        terminalTabs: [...p.terminalTabs, tab],
        activeTerminalTabId: tab.id,
      })),
    })),

  updateTerminalTab: (projectId, sessionId, patch) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        terminalTabs: p.terminalTabs.map((tab) => (tab.id === sessionId ? { ...tab, ...patch } : tab)),
      })),
    })),

  setActiveTerminalTab: (projectId, sessionId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => ({
        ...p,
        activeTerminalTabId: sessionId,
      })),
    })),

  removeTerminalTab: (projectId, sessionId) =>
    set((state) => ({
      projects: updateProject(state.projects, projectId, (p) => {
        const idx = p.terminalTabs.findIndex((tab) => tab.id === sessionId);
        const nextTabs = p.terminalTabs.filter((tab) => tab.id !== sessionId);
        let nextActiveId = p.activeTerminalTabId;
        if (p.activeTerminalTabId === sessionId) {
          const fallback = nextTabs[idx] ?? nextTabs[idx - 1] ?? null;
          nextActiveId = fallback?.id ?? null;
        }
        return {
          ...p,
          terminalTabs: nextTabs,
          activeTerminalTabId: nextActiveId,
          terminalSessions: p.terminalSessions.filter((s) => s !== sessionId),
        };
      }),
    })),

  addBrowserTab: (projectId, tabId) =>
    set((state) => {
      const projects = updateProject(state.projects, projectId, (p) => {
        const browserTabs = normalizeBrowserTabs(p.browserTabIds, p.activeBrowserTabId);
        return {
          ...p,
          browserTabIds: browserTabs.browserTabIds.includes(tabId) ? browserTabs.browserTabIds : [...browserTabs.browserTabIds, tabId],
          activeBrowserTabId: tabId,
        };
      });
      writeStoredWorkspace(projects, state.activeProjectId);
      return { projects };
    }),

  removeBrowserTab: (projectId, tabId) =>
    set((state) => {
      const projects = updateProject(state.projects, projectId, (p) => {
        const browserTabs = normalizeBrowserTabs(p.browserTabIds, p.activeBrowserTabId);
        const browserTabIds = browserTabs.browserTabIds.filter((id) => id !== tabId);
        const activeBrowserTabId = browserTabs.activeBrowserTabId === tabId ? (browserTabIds[0] ?? null) : browserTabs.activeBrowserTabId;
        return { ...p, browserTabIds, activeBrowserTabId };
      });
      writeStoredWorkspace(projects, state.activeProjectId);
      return { projects };
    }),

  setActiveBrowserTab: (projectId, tabId) =>
    set((state) => {
      const projects = updateProject(state.projects, projectId, (p) => {
        const browserTabs = normalizeBrowserTabs(p.browserTabIds, p.activeBrowserTabId);
        if (tabId && !browserTabs.browserTabIds.includes(tabId)) {
          return { ...p, activeBrowserTabId: browserTabs.activeBrowserTabId };
        }
        return { ...p, activeBrowserTabId: tabId };
      });
      writeStoredWorkspace(projects, state.activeProjectId);
      return { projects };
    }),

  reconcileBrowserTabs: (liveTabIds) =>
    set((state) => {
      const live = new Set(liveTabIds.map((id) => id.trim()).filter(Boolean));
      const projects = state.projects.map((project) => {
        const browserTabIds = (project.browserTabIds ?? []).filter((id) => live.has(id));
        const activeBrowserTabId = project.activeBrowserTabId && live.has(project.activeBrowserTabId)
          ? project.activeBrowserTabId
          : (browserTabIds[0] ?? null);
        return {
          ...project,
          browserTabIds,
          activeBrowserTabId,
        };
      });
      writeStoredWorkspace(projects, state.activeProjectId);
      return { projects };
    }),
}));
