import { useState, useCallback } from 'react';
import { ContextMenu } from '../shared/ContextMenu';
import type { ContextMenuItem } from '../shared/ContextMenu';
import type { FileTab } from '../../types';
import { useWorkspaceStore } from '../../stores/workspace';

type EditorTabsProps = {
  files: FileTab[];
  activeFileId: string | null;
  onSelect: (fileId: string) => void;
  onClose: (fileId: string) => void;
};

export function EditorTabs({ files, activeFileId, onSelect, onClose }: EditorTabsProps) {
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; fileId: string } | null>(null);
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const projects = useWorkspaceStore((s) => s.projects);
  const closeOtherFiles = useWorkspaceStore((s) => s.closeOtherFiles);
  const closeFilesToLeft = useWorkspaceStore((s) => s.closeFilesToLeft);
  const closeFilesToRight = useWorkspaceStore((s) => s.closeFilesToRight);
  const closeAllFiles = useWorkspaceStore((s) => s.closeAllFiles);

  const activeProject = projects.find((p) => p.id === activeProjectId) ?? null;

  const handleContextMenu = useCallback((e: React.MouseEvent, fileId: string) => {
    // Same mobile guard as FileTree: touch long-press must not open the
    // menu or trigger native selection. Only real right-click (or macOS
    // Ctrl/Cmd+click) shows the tab menu.
    const ne = e.nativeEvent;
    const isRightClick = e.button === 2;
    const isCtrlClick = e.ctrlKey || e.metaKey;
    const isTouch = (ne as PointerEvent).pointerType === 'touch' || (ne as PointerEvent).pointerType === 'pen';
    if (isTouch || (!isRightClick && !isCtrlClick)) {
      return;
    }
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY, fileId });
  }, []);

  const getMenuItems = useCallback((): ContextMenuItem[] => {
    if (!contextMenu || !activeProjectId) return [];
    const { fileId } = contextMenu;
    const idx = files.findIndex((f) => f.id === fileId);
    const file = files[idx];
    if (!file) return [];

    const projectPath = activeProject?.path ?? '';
    const filePath = file.path;

    const relativePath = filePath.startsWith(projectPath)
      ? filePath.slice(projectPath.length).replace(/^[/\\]/, '')
      : filePath;

    return [
      { type: 'item', label: 'Close', shortcut: 'Ctrl+W', onClick: () => onClose(fileId) },
      { type: 'item', label: 'Close Others', shortcut: 'Ctrl+Alt+Shift+T', disabled: files.length <= 1, onClick: () => closeOtherFiles(activeProjectId, fileId) },
      { type: 'item', label: 'Close Left', shortcut: 'Ctrl+K E', disabled: idx === 0, onClick: () => closeFilesToLeft(activeProjectId, fileId) },
      { type: 'item', label: 'Close Right', shortcut: 'Ctrl+K T', disabled: idx === files.length - 1, onClick: () => closeFilesToRight(activeProjectId, fileId) },
      { type: 'separator' },
      { type: 'item', label: 'Close All', shortcut: 'Ctrl+K W', onClick: () => closeAllFiles(activeProjectId) },
      { type: 'separator' },
      { type: 'item', label: 'Copy Path', shortcut: 'Alt+Shift+C', onClick: () => { void navigator.clipboard.writeText(filePath); } },
      { type: 'item', label: 'Copy Relative Path', shortcut: 'Ctrl+K Ctrl+Shift+C', onClick: () => { void navigator.clipboard.writeText(relativePath); } },
    ];
  }, [contextMenu, activeProjectId, activeProject, files, onClose, closeOtherFiles, closeFilesToLeft, closeFilesToRight, closeAllFiles]);

  return (
    <div className="editor-tabs" role="tablist">
      {files.map((file) => {
        const isActive = file.id === activeFileId;
        const tabClass = `editor-tab${isActive ? ' active' : ''}${file.conflict ? ' conflict' : ''}${file.deleted ? ' deleted' : ''}`;
        return (
          <div key={file.id} className={tabClass} onContextMenu={(e) => handleContextMenu(e, file.id)}>
            <button className="editor-tab-btn" onClick={() => onSelect(file.id)} role="tab" aria-selected={isActive} type="button">
              {file.modified && !file.conflict && !file.deleted && <span className="editor-tab-dot">●</span>}
              {file.conflict && <span className="editor-tab-dot conflict">⚠</span>}
              {file.deleted && <span className="editor-tab-dot deleted">⊘</span>}
              <span className={file.deleted ? 'editor-tab-name-deleted' : ''}>{file.name}</span>
            </button>
            <button className="editor-tab-close" onClick={() => onClose(file.id)} type="button" aria-label={`Close ${file.name}`}>
              ×
            </button>
          </div>
        );
      })}
      {contextMenu && (
        <ContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          items={getMenuItems()}
          onClose={() => setContextMenu(null)}
        />
      )}
    </div>
  );
}
