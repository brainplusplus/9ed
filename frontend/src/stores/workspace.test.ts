import { beforeEach, describe, expect, it, vi } from 'vitest';

const saveRecentProject = vi.fn();

vi.mock('../api', () => ({
  saveRecentProject: (path: string, name: string) => saveRecentProject(path, name),
}));

import { useWorkspaceStore } from './workspace';

function resetWorkspaceStore() {
  window.sessionStorage.clear();
  useWorkspaceStore.setState({
    projects: [],
    activeProjectId: null,
    activePanel: 'explorer',
    sidebarVisible: true,
    terminalVisible: true,
    chatVisible: true,
    browserVisible: false,
    browserEnabled: false,
    showPicker: false,
  });
}

describe('useWorkspaceStore active project restore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetWorkspaceStore();
  });

  it('persists active project in session storage and restores it after reload state reset', () => {
    useWorkspaceStore.getState().addProject('/repo', 'repo');

    const opened = useWorkspaceStore.getState().projects[0];
    expect(opened.path).toBe('/repo');
    expect(useWorkspaceStore.getState().activeProjectId).toBe(opened.id);
    expect(saveRecentProject).toHaveBeenCalledWith('/repo', 'repo');

    useWorkspaceStore.setState({
      projects: [],
      activeProjectId: null,
      showPicker: false,
    });

    useWorkspaceStore.getState().restoreLastActiveProject();

    const restored = useWorkspaceStore.getState().projects[0];
    expect(restored.path).toBe('/repo');
    expect(restored.name).toBe('repo');
    expect(useWorkspaceStore.getState().activeProjectId).toBe(restored.id);
  });

  it('activates an existing project instead of duplicating the same path', () => {
    useWorkspaceStore.getState().addProject('/repo', 'repo');
    useWorkspaceStore.getState().addProject('/repo', 'repo');

    expect(useWorkspaceStore.getState().projects).toHaveLength(1);
  });
});
