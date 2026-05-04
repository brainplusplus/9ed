import { useCallback, useMemo, useState } from 'react';
import { useWorkspaceStore } from '../../stores/workspace';
import { useGitGutter } from '../../hooks/useGitGutter';
import { EditorTabs } from './EditorTabs';
import { MonacoEditor } from './MonacoEditor';
import { GitDiffView } from '../git/GitDiffView';
import { saveFileContent } from '../../api';

type DiffTab = {
  id: string;
  filePath: string;
  name: string;
  original: string;
  modified: string;
  language: string;
};

export function EditorArea() {
  const projects = useWorkspaceStore((s) => s.projects);
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const closeFile = useWorkspaceStore((s) => s.closeFile);
  const setActiveFile = useWorkspaceStore((s) => s.setActiveFile);
  const updateFileContent = useWorkspaceStore((s) => s.updateFileContent);
  const markFileSaved = useWorkspaceStore((s) => s.markFileSaved);

  const [diffTabs, setDiffTabs] = useState<DiffTab[]>([]);
  const [activeDiffId, setActiveDiffId] = useState<string | null>(null);

  const activeProject = useMemo(() => projects.find((p) => p.id === activeProjectId) ?? null, [projects, activeProjectId]);
  const activeFile = useMemo(
    () => activeProject?.openFiles.find((f) => f.id === activeProject.activeFileId) ?? null,
    [activeProject],
  );

  const activeDiff = useMemo(() => diffTabs.find((d) => d.id === activeDiffId) ?? null, [diffTabs, activeDiffId]);

  const gutterChanges = useGitGutter(
    activeProject?.path ?? null,
    activeDiffId ? null : (activeFile?.path ?? null),
  );

  const handleSave = useCallback(async () => {
    if (!activeProjectId || !activeFile) return;
    try {
      await saveFileContent(activeFile.path, activeFile.content);
      markFileSaved(activeProjectId, activeFile.id);
    } catch (err) {
      console.error('Save failed:', err);
    }
  }, [activeProjectId, activeFile, markFileSaved]);

  const handleContentChange = useCallback((value: string) => {
    if (!activeProjectId || !activeFile) return;
    updateFileContent(activeProjectId, activeFile.id, value);
  }, [activeProjectId, activeFile, updateFileContent]);

  const openDiffTab = useCallback((filePath: string, original: string, modified: string, language: string) => {
    const id = `diff:${filePath}`;
    setDiffTabs((prev) => {
      const exists = prev.find((d) => d.id === id);
      if (exists) return prev.map((d) => d.id === id ? { ...d, original, modified } : d);
      const name = filePath.split('/').pop() ?? filePath;
      return [...prev, { id, filePath, name, original, modified, language }];
    });
    setActiveDiffId(id);
    if (activeProjectId) {
      setActiveFile(activeProjectId, '');
    }
  }, [activeProjectId, setActiveFile]);

  const closeDiffTab = useCallback((id: string) => {
    setDiffTabs((prev) => prev.filter((d) => d.id !== id));
    if (activeDiffId === id) {
      setActiveDiffId(null);
    }
  }, [activeDiffId]);

  const allTabs = useMemo(() => {
    const fileTabs = (activeProject?.openFiles ?? []).map((f) => ({
      id: f.id,
      name: f.name,
      modified: f.modified,
      isDiff: false,
    }));
    const dTabs = diffTabs.map((d) => ({
      id: d.id,
      name: `↔ ${d.name}`,
      modified: false,
      isDiff: true,
    }));
    return [...fileTabs, ...dTabs];
  }, [activeProject?.openFiles, diffTabs]);

  const activeTabId = activeDiffId ?? activeProject?.activeFileId ?? null;

  const handleTabSelect = useCallback((tabId: string) => {
    if (tabId.startsWith('diff:')) {
      setActiveDiffId(tabId);
      if (activeProjectId) setActiveFile(activeProjectId, '');
    } else {
      setActiveDiffId(null);
      if (activeProjectId) setActiveFile(activeProjectId, tabId);
    }
  }, [activeProjectId, setActiveFile]);

  const handleTabClose = useCallback((tabId: string) => {
    if (tabId.startsWith('diff:')) {
      closeDiffTab(tabId);
    } else if (activeProjectId) {
      closeFile(activeProjectId, tabId);
    }
  }, [activeProjectId, closeFile, closeDiffTab]);

  if (!activeProject || (activeProject.openFiles.length === 0 && diffTabs.length === 0)) {
    return (
      <div className="editor-empty">
        <p>Open a file from the explorer to start editing.</p>
      </div>
    );
  }

  return (
    <div className="editor-area" data-has-diff-support="true" ref={(el) => {
      if (el) (el as HTMLDivElement & { openDiffTab?: typeof openDiffTab }).openDiffTab = openDiffTab;
    }}>
      <EditorTabs
        files={allTabs.map((t) => ({ id: t.id, path: t.id, name: t.name, content: '', language: '', modified: t.modified }))}
        activeFileId={activeTabId}
        onSelect={handleTabSelect}
        onClose={handleTabClose}
      />
      {activeDiff ? (
        <GitDiffView
          originalContent={activeDiff.original}
          modifiedContent={activeDiff.modified}
          language={activeDiff.language}
          filePath={activeDiff.filePath}
        />
      ) : activeFile ? (
        <MonacoEditor
          key={activeFile.id}
          value={activeFile.content}
          language={activeFile.language}
          onChange={handleContentChange}
          onSave={handleSave}
          gutterChanges={gutterChanges}
        />
      ) : null}
    </div>
  );
}

export type EditorAreaHandle = {
  openDiffTab: (filePath: string, original: string, modified: string, language: string) => void;
};
