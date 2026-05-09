import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createSession, deleteSession, getShells } from '../../api';
import { useWorkspaceStore } from '../../stores/workspace';
import { TerminalTabs } from '../TerminalTabs';
import { TerminalView } from '../TerminalView';
import type { SessionTab, ShellProfile, TerminalAction } from '../../types';

export function TerminalPanel() {
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const projects = useWorkspaceStore((s) => s.projects);
  const addTerminalSession = useWorkspaceStore((s) => s.addTerminalSession);
  const removeTerminalSession = useWorkspaceStore((s) => s.removeTerminalSession);

  const activeProject = useMemo(() => projects.find((p) => p.id === activeProjectId) ?? null, [projects, activeProjectId]);

  const [shells, setShells] = useState<ShellProfile[]>([]);
  const [selectedShellId, setSelectedShellId] = useState('');
  const [tabs, setTabs] = useState<SessionTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [terminalAction, setTerminalAction] = useState<TerminalAction | null>(null);
  const [actionMenuOpen, setActionMenuOpen] = useState(false);
  const actionMenuRef = useRef<HTMLDivElement | null>(null);

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

  const handleCreateTab = useCallback(async () => {
    if (!selectedShellId || !activeProjectId || !activeProject) return;
    setCreating(true);
    try {
      const session = await createSession(selectedShellId, activeProject.path);
      const newTab: SessionTab = { id: session.id, profile: session.profile, status: 'connecting' };
      setTabs((prev) => [...prev, newTab]);
      setActiveTabId(session.id);
      addTerminalSession(activeProjectId, session.id);
    } catch (err) {
      console.error('Failed to create terminal:', err);
    } finally {
      setCreating(false);
    }
  }, [selectedShellId, activeProjectId, activeProject, addTerminalSession]);

  const handleCloseTab = useCallback(async (sessionId: string) => {
    setTabs((prev) => {
      const idx = prev.findIndex((t) => t.id === sessionId);
      const next = prev.filter((t) => t.id !== sessionId);
      if (activeTabId === sessionId) {
        const fallback = next[idx] ?? next[idx - 1] ?? null;
        setActiveTabId(fallback?.id ?? null);
      }
      return next;
    });
    if (activeProjectId) removeTerminalSession(activeProjectId, sessionId);
    try { await deleteSession(sessionId); } catch {}
  }, [activeTabId, activeProjectId, removeTerminalSession]);

  function updateTabStatus(sessionId: string, status: SessionTab['status'], error?: string) {
    setTabs((prev) => prev.map((t) => (t.id === sessionId ? { ...t, status, errorMessage: error } : t)));
  }

  useEffect(() => {
    if (shells.length > 0 && tabs.length === 0 && !creating && activeProject) {
      void handleCreateTab();
    }
  }, [shells.length, tabs.length, creating, activeProject, handleCreateTab]);

  const activeTab = useMemo(() => tabs.find((t) => t.id === activeTabId) ?? null, [tabs, activeTabId]);

  const dispatchTerminalAction = useCallback((kind: TerminalAction['kind']) => {
    if (!activeTabId) return;
    setTerminalAction({ targetTabId: activeTabId, kind, nonce: Date.now() });
    setActionMenuOpen(false);
  }, [activeTabId]);

  useEffect(() => {
    if (!actionMenuOpen) return;

    const handlePointerDown = (event: MouseEvent) => {
      if (!actionMenuRef.current?.contains(event.target as Node)) {
        setActionMenuOpen(false);
      }
    };

    document.addEventListener('mousedown', handlePointerDown);
    return () => document.removeEventListener('mousedown', handlePointerDown);
  }, [actionMenuOpen]);

  return (
    <div className="terminal-panel-ide">
      <div className="terminal-panel-header">
        <div className="terminal-panel-session-area">
          <TerminalTabs tabs={tabs} activeTabId={activeTabId} onSelectTab={setActiveTabId} onCloseTab={(id) => void handleCloseTab(id)} />
        </div>
        <div className="terminal-panel-controls">
          <select value={selectedShellId} onChange={(e) => setSelectedShellId(e.target.value)} className="terminal-shell-select">
            {shells.map((s) => <option key={s.id} value={s.id}>{s.label}</option>)}
          </select>
          <div className="terminal-action-group">
            <button
              className="terminal-clear-btn"
              onClick={() => dispatchTerminalAction('clear-view')}
              type="button"
              title="Clear View — clear visible terminal output only"
              disabled={!activeTabId}
            >
              <span className="terminal-clear-btn-icon" aria-hidden="true">⌫</span>
              <span>Clear View</span>
            </button>
            <div className="terminal-action-menu-wrap" ref={actionMenuRef}>
              <button
                className="terminal-menu-btn"
                onClick={() => setActionMenuOpen((open) => !open)}
                type="button"
                title="More terminal actions"
                disabled={!activeTabId}
                aria-expanded={actionMenuOpen}
              >
                <span aria-hidden="true">▾</span>
              </button>
              {actionMenuOpen && (
                <div className="terminal-action-menu">
                  <button
                    className="terminal-action-menu-item"
                    onClick={() => dispatchTerminalAction('send-clear-command')}
                    type="button"
                    title="Send Clear Command — run clear or cls in active shell"
                  >
                    <span className="terminal-action-menu-icon" aria-hidden="true">›_</span>
                    <span>Send Clear Command</span>
                  </button>
                </div>
              )}
            </div>
          </div>
          <button className="terminal-new-btn" onClick={() => void handleCreateTab()} disabled={creating} type="button">+</button>
        </div>
      </div>
      <div className="terminal-panel-body">
        {tabs.map((tab) => (
          <TerminalView key={tab.id} tab={tab} active={tab.id === activeTab?.id} action={terminalAction} onStatusChange={updateTabStatus} />
        ))}
        {tabs.length === 0 && <div className="terminal-empty">No terminal sessions.</div>}
      </div>
    </div>
  );
}
