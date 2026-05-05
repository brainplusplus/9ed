import { useEffect, useRef } from 'react';
import { useWorkspaceStore } from '../stores/workspace';
import { getWorkspaceState, saveWorkspaceState, getFileContent } from '../api';
import type { WorkspaceState } from '../api';
import type { FileTab } from '../types';

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
  };
  return map[ext] ?? 'plaintext';
}

export function useWorkspaceStatePersistence() {
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const restoringRef = useRef(false);

  useEffect(() => {
    const unsubscribe = useWorkspaceStore.subscribe((state, prevState) => {
      if (restoringRef.current) return;

      const activeProject = state.projects.find((p) => p.id === state.activeProjectId);
      if (!activeProject) return;

      const prevProject = prevState.projects.find((p) => p.id === prevState.activeProjectId);
      const changed =
        state.activePanel !== prevState.activePanel ||
        state.chatVisible !== prevState.chatVisible ||
        activeProject.openFiles !== prevProject?.openFiles ||
        activeProject.activeFileId !== prevProject?.activeFileId;

      if (!changed) return;

      if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
      saveTimerRef.current = setTimeout(() => {
        const wsState: WorkspaceState = {
          openFiles: activeProject.openFiles.map((f) => ({
            path: f.path,
            name: f.name,
            language: f.language,
          })),
          activeFilePath: activeProject.openFiles.find((f) => f.id === activeProject.activeFileId)?.path ?? null,
          sidebarPanel: state.activePanel,
          chatVisible: state.chatVisible,
        };
        void saveWorkspaceState(activeProject.path, wsState);
      }, 1000);
    });

    return () => {
      unsubscribe();
      if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
    };
  }, []);
}

export async function restoreWorkspaceState(projectPath: string, projectId: string): Promise<void> {
  const saved = await getWorkspaceState(projectPath);
  if (!saved || !saved.openFiles || saved.openFiles.length === 0) return;

  const store = useWorkspaceStore.getState();

  for (const fileInfo of saved.openFiles) {
    try {
      const content = await getFileContent(fileInfo.path);
      const fileTab: FileTab = {
        id: fileInfo.path,
        path: fileInfo.path,
        name: fileInfo.name,
        content: content.content,
        language: fileInfo.language || languageFromPath(fileInfo.path),
        modified: false,
      };
      store.openFile(projectId, fileTab);
    } catch {
    }
  }

  if (saved.activeFilePath) {
    const updatedStore = useWorkspaceStore.getState();
    const project = updatedStore.projects.find((p) => p.id === projectId);
    const activeFile = project?.openFiles.find((f) => f.path === saved.activeFilePath);
    if (activeFile) {
      store.setActiveFile(projectId, activeFile.id);
    }
  }

  if (saved.sidebarPanel) {
    store.setActivePanel(saved.sidebarPanel as import('../types').ActivePanel);
  }

  if (saved.chatVisible !== undefined) {
    const current = useWorkspaceStore.getState().chatVisible;
    if (current !== saved.chatVisible) {
      store.toggleChat();
    }
  }
}
