import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Group, Panel, Separator } from 'react-resizable-panels';

import { useWorkspaceStore } from '../../stores/workspace';
import { useFileWatcher } from '../../hooks/useFileWatcher';
import { useGitStatus } from '../../hooks/useGitStatus';
import { ActivityBar } from '../../components/sidebar/ActivityBar';
import { FileTree } from '../../components/sidebar/FileTree';
import { SearchPanel } from '../../components/sidebar/SearchPanel';
import { ProjectList } from '../../components/sidebar/ProjectList';
import { GitPanel } from '../../components/git/GitPanel';
import { EditorArea } from '../../components/editor/EditorArea';
import { TerminalPanel } from '../../components/terminal/TerminalPanel';
import { getFileContent } from '../../api';
import type { FileTab } from '../../types';

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
  };
  return map[ext] ?? 'plaintext';
}

export function IDEWorkspace() {
  const activePanel = useWorkspaceStore((s) => s.activePanel);
  const sidebarVisible = useWorkspaceStore((s) => s.sidebarVisible);
  const terminalVisible = useWorkspaceStore((s) => s.terminalVisible);
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const projects = useWorkspaceStore((s) => s.projects);
  const openFile = useWorkspaceStore((s) => s.openFile);
  const toggleSidebar = useWorkspaceStore((s) => s.toggleSidebar);
  const toggleTerminal = useWorkspaceStore((s) => s.toggleTerminal);
  const setActivePanel = useWorkspaceStore((s) => s.setActivePanel);

  const activeProject = useMemo(() => projects.find((p) => p.id === activeProjectId) ?? null, [projects, activeProjectId]);

  useGitStatus(activeProject?.path ?? null);

  const editorAreaRef = useRef<HTMLDivElement>(null);
  const [treeRefreshKey, setTreeRefreshKey] = useState(0);

  const updateFileContent = useWorkspaceStore((s) => s.updateFileContent);
  const markFileSaved = useWorkspaceStore((s) => s.markFileSaved);

  useFileWatcher({
    root: activeProject?.path ?? null,
    onFileChange: useCallback((event) => {
      if (event.type === 'create' || event.type === 'delete' || event.type === 'rename') {
        setTreeRefreshKey((k) => k + 1);
      }

      if (event.type === 'modify' && activeProjectId) {
        const normalizedPath = event.path.replace(/\\/g, '/');
        const project = useWorkspaceStore.getState().projects.find((p) => p.id === activeProjectId);
        const openTab = project?.openFiles.find((f) => {
          const normalizedTabPath = f.path.replace(/\\/g, '/');
          return normalizedTabPath === normalizedPath || normalizedPath.endsWith(normalizedTabPath);
        });

        if (openTab && !openTab.modified) {
          getFileContent(openTab.path).then((fc) => {
            updateFileContent(activeProjectId, openTab.id, fc.content);
            markFileSaved(activeProjectId, openTab.id);
          }).catch(() => {});
        }
      }
    }, [activeProjectId, updateFileContent, markFileSaved]),
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
    } catch (err) {
      console.error('Failed to open file:', err);
    }
  }, [activeProjectId, openFile]);

  const handleOpenDiff = useCallback((filePath: string, original: string, modified: string, language: string) => {
    const el = editorAreaRef.current?.querySelector('[data-has-diff-support]') as
      (HTMLDivElement & { openDiffTab?: (fp: string, o: string, m: string, l: string) => void }) | null;
    el?.openDiffTab?.(filePath, original, modified, language);
  }, []);

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
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [toggleSidebar, toggleTerminal, setActivePanel, sidebarVisible]);

  return (
    <div className="ide-shell">
      <ActivityBar />
      <Group orientation="horizontal" className="ide-main">
        {sidebarVisible && (
          <>
            <Panel defaultSize="28%" minSize="15%" maxSize="50%" className="ide-sidebar">
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
      </Group>
    </div>
  );
}
