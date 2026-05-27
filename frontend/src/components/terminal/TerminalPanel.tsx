import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createSession, deleteSession, getShells } from '../../api';
import { disposeTerminalConnection } from '../../terminalConnection';
import { useWorkspaceStore } from '../../stores/workspace';
import { useChatStore } from '../../stores/chat';
import { TerminalTabs } from '../TerminalTabs';
import { TerminalView } from '../TerminalView';
import type { SessionTab, ShellProfile, TerminalAction } from '../../types';

const TERMINAL_SCROLLBACK_MAX_BYTES = 200_000;

function ClearTerminalIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M6 3H13.5V13H2.5V6.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M2.5 2.5V6.5H6.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M2.5 6.5L6.5 2.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function TerminalPanel() {
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const projects = useWorkspaceStore((s) => s.projects);
  const addTerminalSession = useWorkspaceStore((s) => s.addTerminalSession);
  const addTerminalTab = useWorkspaceStore((s) => s.addTerminalTab);
  const updateTerminalTab = useWorkspaceStore((s) => s.updateTerminalTab);
  const setActiveTerminalTab = useWorkspaceStore((s) => s.setActiveTerminalTab);
  const removeTerminalTab = useWorkspaceStore((s) => s.removeTerminalTab);
  const setActiveTerminalId = useChatStore((s) => s.setActiveTerminalId);

  const activeProject = useMemo(() => projects.find((p) => p.id === activeProjectId) ?? null, [projects, activeProjectId]);

  const [shells, setShells] = useState<ShellProfile[]>([]);
  const [selectedShellId, setSelectedShellId] = useState('');
  const [creating, setCreating] = useState(false);
  const [terminalAction, setTerminalAction] = useState<TerminalAction | null>(null);
  const [shellMenuOpen, setShellMenuOpen] = useState(false);
  const defaultCreatedProjects = useRef(new Set<string>());
  const shellMenuRef = useRef<HTMLDivElement>(null);

  const tabs = activeProject?.terminalTabs ?? [];
  const activeTabId = activeProject?.activeTerminalTabId ?? null;

  // Load available shells
  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const available = await getShells();
        if (!cancelled) {
          setShells(available);
          setSelectedShellId(available[0]?.id ?? '');
        }
      } catch {}
    }
    void load();
    return () => { cancelled = true; };
  }, []);

  const handleCreateTab = useCallback(async (shellId = selectedShellId) => {
    if (!shellId || !activeProjectId || !activeProject) return;
    setSelectedShellId(shellId);
    setShellMenuOpen(false);
    setCreating(true);
    try {
      const session = await createSession(shellId, activeProject.path);
      const newTab: SessionTab = { id: session.id, profile: session.profile, status: 'connecting' };
      addTerminalTab(activeProjectId, newTab);
      addTerminalSession(activeProjectId, session.id);
    } catch (err) {
      console.error('Failed to create terminal:', err);
    } finally {
      setCreating(false);
    }
  }, [selectedShellId, activeProjectId, activeProject, addTerminalSession, addTerminalTab]);

  useEffect(() => {
    if (!shellMenuOpen) return;

    function handlePointerDown(event: PointerEvent) {
      if (!shellMenuRef.current?.contains(event.target as Node)) {
        setShellMenuOpen(false);
      }
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setShellMenuOpen(false);
      }
    }

    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [shellMenuOpen]);

  // Auto-create default terminal when project opens
  useEffect(() => {
    if (!activeProjectId || !activeProject || shells.length === 0 || tabs.length > 0) return;
    if (defaultCreatedProjects.current.has(activeProjectId)) return;
    defaultCreatedProjects.current.add(activeProjectId);

    const shellId = shells[0]?.id;
    if (!shellId) return;

    setCreating(true);
    createSession(shellId, activeProject.path)
      .then((session) => {
        const newTab: SessionTab = { id: session.id, profile: session.profile, status: 'connecting' };
        addTerminalTab(activeProjectId, newTab);
        addTerminalSession(activeProjectId, session.id);
      })
      .catch((err) => {
        console.error('Failed to create default terminal:', err);
        defaultCreatedProjects.current.delete(activeProjectId);
      })
      .finally(() => setCreating(false));
  }, [activeProjectId, activeProject, shells, tabs.length, addTerminalSession, addTerminalTab]);

  const handleCloseTab = useCallback(async (sessionId: string) => {
    if (activeProjectId) removeTerminalTab(activeProjectId, sessionId);
    disposeTerminalConnection(sessionId);
    try { await deleteSession(sessionId); } catch {}
  }, [activeProjectId, removeTerminalTab]);

  function updateTabStatus(sessionId: string, status: SessionTab['status'], error?: string) {
    if (!activeProjectId) return;
    updateTerminalTab(activeProjectId, sessionId, { status, errorMessage: error });
  }

  const updateTabScrollback = useCallback((sessionId: string, scrollback: string) => {
    if (!activeProjectId) return;
    updateTerminalTab(activeProjectId, sessionId, { scrollback });
  }, [activeProjectId, updateTerminalTab]);

  const appendTabOutput = useCallback((sessionId: string, data: string) => {
    if (!activeProjectId) return;
    const project = useWorkspaceStore.getState().projects.find((p) => p.id === activeProjectId);
    const tab = project?.terminalTabs.find((t) => t.id === sessionId);
    const nextScrollback = `${tab?.scrollback ?? ''}${data}`;
    updateTerminalTab(activeProjectId, sessionId, {
      scrollback: nextScrollback.slice(-TERMINAL_SCROLLBACK_MAX_BYTES),
    });
  }, [activeProjectId, updateTerminalTab]);

  // Sync active terminal ID to chat store
  useEffect(() => {
    setActiveTerminalId(activeTabId);
  }, [activeTabId, setActiveTerminalId]);

  const activeTab = useMemo(() => tabs.find((t) => t.id === activeTabId) ?? null, [tabs, activeTabId]);

  const handleSelectTab = useCallback((sessionId: string) => {
    if (!activeProjectId) return;
    setActiveTerminalTab(activeProjectId, sessionId);
  }, [activeProjectId, setActiveTerminalTab]);

  const dispatchTerminalAction = useCallback((kind: TerminalAction['kind']) => {
    if (!activeTabId) return;
    setTerminalAction({ targetTabId: activeTabId, kind, nonce: Date.now() });
  }, [activeTabId]);

  return (
    <div className="terminal-panel-ide">
      <div className="terminal-panel-header">
        <div className="terminal-panel-session-area">
          <TerminalTabs tabs={tabs} activeTabId={activeTabId} onSelectTab={handleSelectTab} onCloseTab={(id) => void handleCloseTab(id)} />
        </div>
        <div className="terminal-panel-controls">
          <button
            className="terminal-clear-btn"
            onClick={() => dispatchTerminalAction('clear-terminal')}
            type="button"
            aria-label="Clear terminal"
            title="Clear Terminal — clear terminal output, keep active prompt"
            disabled={!activeTabId}
          >
            <span className="terminal-clear-btn-icon" aria-hidden="true"><ClearTerminalIcon /></span>
          </button>
          <div className={`terminal-new-menu-wrap${shellMenuOpen ? ' open' : ''}`} ref={shellMenuRef}>
            <button
              className={`terminal-new-btn${shellMenuOpen ? ' active' : ''}`}
              onPointerDown={(event) => {
                event.stopPropagation();
                setShellMenuOpen((open) => !open);
              }}
              disabled={creating || shells.length === 0}
              type="button"
              aria-label="New terminal"
              aria-haspopup="menu"
              aria-expanded={shellMenuOpen}
              title="New terminal"
            >
              +
            </button>
            {shells.length > 0 && (
              <div className="terminal-profile-menu" role="menu" aria-label="Terminal profiles">
                {shells.map((shell) => (
                  <button
                    key={shell.id}
                    className={`terminal-profile-menu-item${shell.id === selectedShellId ? ' active' : ''}`}
                    type="button"
                    role="menuitem"
                    onClick={() => void handleCreateTab(shell.id)}
                  >
                    {shell.label}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
      <div className="terminal-panel-body">
        {tabs.map((tab) => (
          <TerminalView
            key={tab.id}
            tab={tab}
            active={tab.id === activeTab?.id}
            action={terminalAction}
            cwd={activeProject?.path}
            onStatusChange={updateTabStatus}
            onScrollbackSnapshot={updateTabScrollback}
            onTerminalOutput={appendTabOutput}
          />
        ))}
        {tabs.length === 0 && <div className="terminal-empty">No terminal sessions.</div>}
      </div>
    </div>
  );
}
