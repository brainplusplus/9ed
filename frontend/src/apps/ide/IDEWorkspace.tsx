import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Group, Panel, Separator } from 'react-resizable-panels';

import { useWorkspaceStore } from '../../stores/workspace';
import { useFileWatcher } from '../../hooks/useFileWatcher';
import { useGitStatus } from '../../hooks/useGitStatus';
import { useLayoutMode } from '../../hooks/useLayoutMode';
import { useWorkspaceStatePersistence, restoreWorkspaceState } from '../../hooks/useWorkspaceStatePersistence';
import { ActivityBar } from '../../components/sidebar/ActivityBar';
import { FileTree } from '../../components/sidebar/FileTree';
import { SearchPanel } from '../../components/sidebar/SearchPanel';
import { ProjectList } from '../../components/sidebar/ProjectList';
import { GitPanel } from '../../components/git/GitPanel';
import { EditorArea } from '../../components/editor/EditorArea';
import { TerminalPanel } from '../../components/terminal/TerminalPanel';
import { ChatPanel } from '../../components/chat/ChatPanel';
import { BrowserPanel } from '../../components/browser/BrowserPanel';
import { BottomNav, type MobileView } from '../../components/shared/BottomNav';
import { ShortcutsHelp } from '../../components/shared/ShortcutsHelp';
import { getFileContent } from '../../api';
import type { FileTab } from '../../types';

export const recentSaveTimestamps = new Map<string, number>();

setInterval(() => {
  const now = Date.now();
  for (const [key, ts] of recentSaveTimestamps) {
    if (now - ts > 5000) recentSaveTimestamps.delete(key);
  }
}, 10000);

function languageFromPath(filePath: string): string {
  const ext = filePath.split('.').pop()?.toLowerCase() ?? '';
  const map: Record<string, string> = {
    ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript',
    go: 'go', py: 'python', rs: 'rust', java: 'java', c: 'c', cpp: 'cpp',
    h: 'c', hpp: 'cpp', cs: 'csharp', rb: 'ruby', php: 'php',
    html: 'html', css: 'css', scss: 'scss', less: 'less',
    json: 'json', yaml: 'yaml', yml: 'yaml', toml: 'toml',
    md: 'markdown', sql: 'sql', sh: 'shell', bash: 'shell',
    xml: 'xml', svg: 'xml', dockerfile: 'dockerfile',
    vue: 'vue', svelte: 'svelte',
    kt: 'kotlin', swift: 'swift', dart: 'dart', lua: 'lua',
    r: 'r', pl: 'perl', ex: 'elixir', exs: 'elixir',
    zig: 'zig', nim: 'nim', haskell: 'haskell', hs: 'haskell',
    clj: 'clojure', scala: 'scala', groovy: 'groovy',
    tf: 'hcl', hcl: 'hcl', proto: 'protobuf',
    graphql: 'graphql', gql: 'graphql',
  };
  return map[ext] ?? 'plaintext';
}

export function IDEWorkspace() {
  const layoutMode = useLayoutMode();
  const activePanel = useWorkspaceStore((s) => s.activePanel);
  const sidebarVisible = useWorkspaceStore((s) => s.sidebarVisible);
  const terminalVisible = useWorkspaceStore((s) => s.terminalVisible);
  const chatVisible = useWorkspaceStore((s) => s.chatVisible);
  const browserVisible = useWorkspaceStore((s) => s.browserVisible);
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const projects = useWorkspaceStore((s) => s.projects);
  const openFile = useWorkspaceStore((s) => s.openFile);
  const toggleSidebar = useWorkspaceStore((s) => s.toggleSidebar);
  const toggleTerminal = useWorkspaceStore((s) => s.toggleTerminal);
  const toggleChat = useWorkspaceStore((s) => s.toggleChat);
  const toggleBrowser = useWorkspaceStore((s) => s.toggleBrowser);
  const setActivePanel = useWorkspaceStore((s) => s.setActivePanel);

  const activeProject = useMemo(() => projects.find((p) => p.id === activeProjectId) ?? null, [projects, activeProjectId]);

  useWorkspaceStatePersistence();
  useGitStatus(activeProject?.path ?? null);

  const restoredRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    if (!activeProject || restoredRef.current.has(activeProject.path)) return;
    restoredRef.current.add(activeProject.path);
    void restoreWorkspaceState(activeProject.path, activeProject.id);
  }, [activeProject?.id, activeProject?.path]);

  const editorAreaRef = useRef<HTMLDivElement>(null);
  const [treeRefreshKey, setTreeRefreshKey] = useState(0);

  const [tabletSidebarOpen, setTabletSidebarOpen] = useState(false);
  const [tabletChatOpen, setTabletChatOpen] = useState(false);
  const [tabletBrowserOpen, setTabletBrowserOpen] = useState(false);
  const [mobileView, setMobileView] = useState<MobileView>('editor');
  const [showHelp, setShowHelp] = useState(false);
  const pendingDiffRef = useRef<{ filePath: string; original: string; modified: string; language: string } | null>(null);

  useEffect(() => {
    if (layoutMode === 'tablet') {
      setTabletSidebarOpen(sidebarVisible);
    }
  }, [layoutMode, sidebarVisible]);

  useEffect(() => {
    if (layoutMode === 'tablet') {
      setTabletChatOpen(chatVisible);
    }
  }, [layoutMode, chatVisible]);

  useEffect(() => {
    if (layoutMode === 'tablet') {
      setTabletBrowserOpen(browserVisible);
    }
  }, [layoutMode, browserVisible]);

  const updateFileContent = useWorkspaceStore((s) => s.updateFileContent);
  const markFileSaved = useWorkspaceStore((s) => s.markFileSaved);
  const markFileConflict = useWorkspaceStore((s) => s.markFileConflict);
  const markFileDeleted = useWorkspaceStore((s) => s.markFileDeleted);

  const recentSaves = useRef(recentSaveTimestamps);

  useFileWatcher({
    root: activeProject?.path ?? null,
    onFileChange: useCallback((event) => {
      if (event.type === 'create' || event.type === 'rename') {
        setTreeRefreshKey((k) => k + 1);
      }

      if (event.type === 'delete') {
        setTreeRefreshKey((k) => k + 1);
        if (activeProjectId) {
          const normalizedPath = event.path.replace(/\\/g, '/');
          const project = useWorkspaceStore.getState().projects.find((p) => p.id === activeProjectId);
          const openTab = project?.openFiles.find((f) => {
            const normalizedTabPath = f.path.replace(/\\/g, '/');
            return normalizedTabPath === normalizedPath || normalizedPath.endsWith(normalizedTabPath);
          });
          if (openTab) {
            markFileDeleted(activeProjectId, openTab.path);
          }
        }
      }

      if (event.type === 'modify' && activeProjectId) {
        const normalizedPath = event.path.replace(/\\/g, '/');

        const savedAt = recentSaves.current.get(normalizedPath);
        if (savedAt && Date.now() - savedAt < 3000) {
          return;
        }

        const project = useWorkspaceStore.getState().projects.find((p) => p.id === activeProjectId);
        const openTab = project?.openFiles.find((f) => {
          const normalizedTabPath = f.path.replace(/\\/g, '/');
          return normalizedTabPath === normalizedPath || normalizedPath.endsWith(normalizedTabPath);
        });

        if (openTab && !openTab.modified) {
          getFileContent(openTab.path).then((fc) => {
            if (fc.content !== openTab.content) {
              updateFileContent(activeProjectId, openTab.id, fc.content);
              markFileSaved(activeProjectId, openTab.id);
            }
          }).catch(() => {});
        } else if (openTab && openTab.modified) {
          markFileConflict(activeProjectId, openTab.id);
        } else if (!openTab) {
          setTreeRefreshKey((k) => k + 1);
        }
      }
    }, [activeProjectId, updateFileContent, markFileSaved, markFileConflict, markFileDeleted]),
  });

  const handleFileSelect = useCallback(async (filePath: string, fileName: string) => {
    if (!activeProjectId) return;
    try {
      const fc = await getFileContent(filePath);
      const tab: FileTab = {
        id: filePath,
        path: filePath,
        name: fileName,
        content: fc.content,
        language: languageFromPath(filePath),
        modified: false,
      };
      openFile(activeProjectId, tab);
      if (layoutMode !== 'desktop') {
        setMobileView('editor');
        setTabletSidebarOpen(false);
      }
    } catch (err) {
      console.error('Failed to open file:', err);
    }
  }, [activeProjectId, openFile, layoutMode]);

  const handleOpenDiff = useCallback((filePath: string, original: string, modified: string, language: string) => {
    const el = editorAreaRef.current?.querySelector('[data-has-diff-support]') as
      (HTMLDivElement & { openDiffTab?: (fp: string, o: string, m: string, l: string) => void }) | null;
    if (el?.openDiffTab) {
      el.openDiffTab(filePath, original, modified, language);
    } else {
      pendingDiffRef.current = { filePath, original, modified, language };
    }
    if (layoutMode !== 'desktop') {
      setMobileView('editor');
      setTabletSidebarOpen(false);
    }
  }, [layoutMode]);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key === 'b') {
        e.preventDefault();
        toggleSidebar();
      }
      if ((e.ctrlKey || e.metaKey) && e.key === '`') {
        e.preventDefault();
        toggleTerminal();
      }
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'g' || e.key === 'G')) {
        e.preventDefault();
        setActivePanel('git');
        if (!sidebarVisible) toggleSidebar();
      }
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'l' || e.key === 'L')) {
        e.preventDefault();
        toggleChat();
      }
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'b' || e.key === 'B')) {
        e.preventDefault();
        toggleBrowser();
      }
      if (e.key === 'F1') {
        e.preventDefault();
        setShowHelp((v) => !v);
      }
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [toggleSidebar, toggleTerminal, toggleChat, toggleBrowser, setActivePanel, sidebarVisible]);

  const sidebarContent = (
    <>
      <div className="sidebar-header">
        <strong>{activeProject?.name ?? 'No project'}</strong>
      </div>
      {activePanel === 'explorer' && activeProject && (
        <FileTree rootPath={activeProject.path} onFileSelect={handleFileSelect} refreshKey={treeRefreshKey} />
      )}
      {activePanel === 'search' && activeProject && (
        <SearchPanel rootPath={activeProject.path} onResultClick={handleFileSelect} />
      )}
      {activePanel === 'git' && activeProject && (
        <GitPanel projectPath={activeProject.path} onOpenDiff={handleOpenDiff} />
      )}
      {activePanel === 'projects' && <ProjectList />}
    </>
  );

  useEffect(() => {
    if (mobileView !== 'editor' || !pendingDiffRef.current) return;
    const timer = setTimeout(() => {
      const el = editorAreaRef.current?.querySelector('[data-has-diff-support]') as
        (HTMLDivElement & { openDiffTab?: (fp: string, o: string, m: string, l: string) => void }) | null;
      if (el?.openDiffTab && pendingDiffRef.current) {
        el.openDiffTab(pendingDiffRef.current.filePath, pendingDiffRef.current.original, pendingDiffRef.current.modified, pendingDiffRef.current.language);
        pendingDiffRef.current = null;
      }
    }, 100);
    return () => clearTimeout(timer);
  }, [mobileView]);

  const helpOverlay = showHelp ? <ShortcutsHelp onClose={() => setShowHelp(false)} /> : null;

  if (layoutMode === 'mobile') {
    return (
      <div className="mobile-shell">
        <div className="mobile-panel">
          {mobileView === 'explorer' && activeProject && (
            <div className="ide-sidebar" style={{ height: '100%' }}>
              {sidebarContent}
            </div>
          )}
          {mobileView === 'git' && activeProject && (
            <div className="ide-sidebar" style={{ height: '100%' }}>
              <div className="sidebar-header"><strong>Git</strong></div>
              <GitPanel projectPath={activeProject.path} onOpenDiff={handleOpenDiff} />
            </div>
          )}
          {mobileView === 'editor' && (
            <div ref={editorAreaRef} style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
              <EditorArea />
            </div>
          )}
          {mobileView === 'terminal' && (
            <div style={{ height: '100%' }}>
              <TerminalPanel />
            </div>
          )}
          {mobileView === 'chat' && (
            <div style={{ height: '100%' }}>
              <ChatPanel />
            </div>
          )}
          {mobileView === 'browser' && (
            <div style={{ height: '100%' }}>
              <BrowserPanel />
            </div>
          )}
        </div>
        <BottomNav activeView={mobileView} onViewChange={(view) => {
          setMobileView(view);
          if (view === 'explorer') setActivePanel('explorer');
          if (view === 'git') setActivePanel('git');
        }} />
        {helpOverlay}
      </div>
    );
  }

  if (layoutMode === 'tablet') {
    return (
      <div className="tablet-shell">
        <ActivityBar />
        <div className="tablet-main">
          <Group orientation="vertical" style={{ height: '100%' }}>
            <Panel minSize="15%" className="ide-editor-area">
              <div ref={editorAreaRef} style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
                <EditorArea />
              </div>
            </Panel>
            {terminalVisible && (
              <>
                <Separator className="resize-handle-v" style={{ cursor: 'row-resize' }} />
                <Panel defaultSize="38%" minSize="10%" maxSize="70%" className="ide-terminal-area">
                  <TerminalPanel />
                </Panel>
              </>
            )}
          </Group>
        </div>

        {tabletSidebarOpen && (
          <>
            <div className="overlay-backdrop" onClick={() => setTabletSidebarOpen(false)} />
            <div className="sidebar-overlay open">
              {sidebarContent}
            </div>
          </>
        )}

        {tabletChatOpen && (
          <>
            <div className="overlay-backdrop" onClick={() => setTabletChatOpen(false)} />
            <div className="chat-overlay open">
              <ChatPanel />
            </div>
          </>
        )}
        {tabletBrowserOpen && (
          <>
            <div className="overlay-backdrop" onClick={() => setTabletBrowserOpen(false)} />
            <div className="browser-overlay open">
              <BrowserPanel />
            </div>
          </>
        )}
        {helpOverlay}
      </div>
    );
  }

  return (
    <div className="ide-shell">
      <ActivityBar />
      <Group orientation="horizontal" className="ide-main">
        {sidebarVisible && (
          <>
            <Panel defaultSize="28%" minSize="15%" maxSize="50%" className="ide-sidebar">
              {sidebarContent}
            </Panel>
            <Separator className="resize-handle-h" style={{ cursor: 'col-resize' }} />
          </>
        )}
        <Panel minSize="20%" className="ide-content">
          <Group orientation="vertical" style={{ height: '100%' }}>
            <Panel minSize="15%" className="ide-editor-area">
              <div ref={editorAreaRef} style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
                <EditorArea />
              </div>
            </Panel>
            {terminalVisible && (
              <>
                <Separator className="resize-handle-v" style={{ cursor: 'row-resize' }} />
                <Panel defaultSize="38%" minSize="10%" maxSize="70%" className="ide-terminal-area">
                  <TerminalPanel />
                </Panel>
              </>
            )}
          </Group>
        </Panel>
        {chatVisible && (
          <>
            <Separator className="resize-handle-h" style={{ cursor: 'col-resize' }} />
            <Panel defaultSize="25%" minSize="15%" maxSize="40%" className="ide-chat-area">
              <ChatPanel />
            </Panel>
          </>
        )}
        {browserVisible && (
          <>
            <Separator className="resize-handle-h" style={{ cursor: 'col-resize' }} />
            <Panel defaultSize="34%" minSize="20%" maxSize="55%" className="ide-browser-area">
              <BrowserPanel />
            </Panel>
          </>
        )}
      </Group>
      {helpOverlay}
    </div>
  );
}
