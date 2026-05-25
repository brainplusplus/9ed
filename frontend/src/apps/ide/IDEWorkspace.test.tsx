import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { IDEWorkspace } from './IDEWorkspace';
import { useWorkspaceStore } from '../../stores/workspace';
import type { LayoutMode } from '../../hooks/useLayoutMode';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

let layoutMode: LayoutMode = 'desktop';
const mounts = {
  editor: 0,
  terminal: 0,
  chat: 0,
};
const unmounts = {
  editor: 0,
  terminal: 0,
  chat: 0,
};

vi.mock('../../hooks/useLayoutMode', () => ({
  useLayoutMode: () => layoutMode,
}));

vi.mock('../../hooks/useFileWatcher', () => ({
  useFileWatcher: () => undefined,
}));

vi.mock('../../hooks/useGitStatus', () => ({
  useGitStatus: () => undefined,
}));

vi.mock('../../hooks/useWorkspaceStatePersistence', () => ({
  useWorkspaceStatePersistence: () => undefined,
  restoreWorkspaceState: vi.fn(),
}));

vi.mock('../../api', () => ({
  getConfig: vi.fn().mockResolvedValue({ useBrowser: true }),
  getFileContent: vi.fn(),
  saveRecentProject: vi.fn(),
}));

vi.mock('../../components/sidebar/ActivityBar', () => ({
  ActivityBar: () => <div data-testid="activity" />,
}));

vi.mock('../../components/sidebar/FileTree', () => ({
  FileTree: () => <div data-testid="file-tree" />,
}));

vi.mock('../../components/sidebar/SearchPanel', () => ({
  SearchPanel: () => <div data-testid="search" />,
}));

vi.mock('../../components/sidebar/ProjectList', () => ({
  ProjectList: () => <div data-testid="projects" />,
}));

vi.mock('../../components/git/GitPanel', () => ({
  GitPanel: () => <div data-testid="git" />,
}));

vi.mock('../../components/editor/EditorArea', async () => {
  const React = await import('react');
  return {
    EditorArea: () => {
      React.useEffect(() => {
        mounts.editor += 1;
        return () => {
          unmounts.editor += 1;
        };
      }, []);
      return <div data-testid="editor" />;
    },
  };
});

vi.mock('../../components/terminal/TerminalPanel', async () => {
  const React = await import('react');
  return {
    TerminalPanel: () => {
      React.useEffect(() => {
        mounts.terminal += 1;
        return () => {
          unmounts.terminal += 1;
        };
      }, []);
      return <div data-testid="terminal" />;
    },
  };
});

vi.mock('../../components/chat/ChatPanel', async () => {
  const React = await import('react');
  return {
    ChatPanel: () => {
      React.useEffect(() => {
        mounts.chat += 1;
        return () => {
          unmounts.chat += 1;
        };
      }, []);
      return <div data-testid="chat" />;
    },
  };
});

vi.mock('../../components/browser/BrowserPanel', () => ({
  BrowserPanel: () => <div data-testid="browser" />,
}));

vi.mock('../../components/shared/BottomNav', () => ({
  BottomNav: () => <div data-testid="bottom-nav" />,
}));

vi.mock('../../components/shared/ShortcutsHelp', () => ({
  ShortcutsHelp: () => <div data-testid="help" />,
}));

function resetWorkspaceStore() {
  useWorkspaceStore.setState({
    projects: [{
      id: 'project-1',
      path: '/repo',
      name: 'repo',
      openFiles: [],
      activeFileId: null,
      terminalTabs: [],
      activeTerminalTabId: null,
      terminalSessions: [],
    }],
    activeProjectId: 'project-1',
    activePanel: 'explorer',
    sidebarVisible: true,
    terminalVisible: true,
    chatVisible: true,
    browserVisible: false,
    browserEnabled: true,
    showPicker: false,
  });
}

describe('IDEWorkspace responsive lifecycle', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    layoutMode = 'desktop';
    mounts.editor = 0;
    mounts.terminal = 0;
    mounts.chat = 0;
    unmounts.editor = 0;
    unmounts.terminal = 0;
    unmounts.chat = 0;
    resetWorkspaceStore();
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

  it('keeps core panels mounted when the responsive layout mode changes', () => {
    act(() => {
      root.render(<IDEWorkspace />);
    });

    expect(mounts).toEqual({ editor: 1, terminal: 1, chat: 1 });
    expect(unmounts).toEqual({ editor: 0, terminal: 0, chat: 0 });

    layoutMode = 'mobile';
    act(() => {
      root.render(<IDEWorkspace />);
    });

    layoutMode = 'tablet';
    act(() => {
      root.render(<IDEWorkspace />);
    });

    expect(mounts).toEqual({ editor: 1, terminal: 1, chat: 1 });
    expect(unmounts).toEqual({ editor: 0, terminal: 0, chat: 0 });
  });
});
