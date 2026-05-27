import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ProjectPicker } from './ProjectPicker';
import { useWorkspaceStore } from '../../stores/workspace';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const getConfig = vi.fn();
const getDrives = vi.fn();
const getFileTree = vi.fn();
const getRecentProjects = vi.fn();
const removeRecentProject = vi.fn();
const saveRecentProject = vi.fn();

vi.mock('../../api', () => ({
  getConfig: () => getConfig(),
  getDrives: () => getDrives(),
  getFileTree: (path: string) => getFileTree(path),
  getRecentProjects: () => getRecentProjects(),
  removeRecentProject: (path: string) => removeRecentProject(path),
  saveRecentProject: (path: string, name: string) => saveRecentProject(path, name),
}));

function resetWorkspaceStore() {
  window.sessionStorage.clear();
  useWorkspaceStore.setState({
    projects: [],
    activeProjectId: null,
    activePanel: 'explorer',
    sidebarVisible: true,
    terminalVisible: true,
    chatVisible: true,
    showPicker: false,
  });
}

describe('ProjectPicker', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    resetWorkspaceStore();
    getRecentProjects.mockResolvedValue([]);
    removeRecentProject.mockResolvedValue(undefined);
    saveRecentProject.mockResolvedValue(undefined);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  it('loads workspace root tree without waiting for drive enumeration', async () => {
    getConfig.mockResolvedValue({ workspaceRoot: '/repo' });
    getDrives.mockReturnValue(new Promise(() => {}));
    getFileTree.mockResolvedValue([
      { name: 'src', type: 'dir', size: 0, modified: 1 },
      { name: 'README.md', type: 'file', size: 1, modified: 1 },
    ]);

    await act(async () => {
      root.render(<ProjectPicker />);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(getFileTree).toHaveBeenCalledWith('/repo');
    expect(container.textContent).toContain('src');
    expect(container.textContent).not.toContain('Loading directories');
  });
});
