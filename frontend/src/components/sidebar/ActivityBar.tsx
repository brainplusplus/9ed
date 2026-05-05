import { useWorkspaceStore } from '../../stores/workspace';
import { useGitStore } from '../../stores/git';
import type { ActivePanel } from '../../types';

const panels: { id: ActivePanel; icon: string; label: string }[] = [
  { id: 'explorer', icon: '📁', label: 'Explorer' },
  { id: 'search', icon: '🔍', label: 'Search' },
  { id: 'git', icon: '🌿', label: 'Source Control' },
  { id: 'projects', icon: '📂', label: 'Projects' },
  { id: 'terminal', icon: '🖥', label: 'Terminal' },
];

export function ActivityBar() {
  const activePanel = useWorkspaceStore((s) => s.activePanel);
  const setActivePanel = useWorkspaceStore((s) => s.setActivePanel);
  const toggleTerminal = useWorkspaceStore((s) => s.toggleTerminal);
  const chatVisible = useWorkspaceStore((s) => s.chatVisible);
  const toggleChat = useWorkspaceStore((s) => s.toggleChat);
  const gitChangeCount = useGitStore((s) => s.status.length);

  return (
    <nav className="activity-bar" aria-label="Activity Bar">
      {panels.map((p) => (
        <button
          key={p.id}
          className={`activity-btn${activePanel === p.id ? ' active' : ''}`}
          onClick={() => {
            if (p.id === 'terminal') {
              toggleTerminal();
            }
            setActivePanel(p.id);
          }}
          title={p.label}
          type="button"
        >
          <span className="activity-icon">{p.icon}</span>
          {p.id === 'git' && gitChangeCount > 0 && (
            <span className="activity-badge">{gitChangeCount}</span>
          )}
        </button>
      ))}
      <button
        className={`activity-btn${chatVisible ? ' active' : ''}`}
        onClick={toggleChat}
        title="Chat (Ctrl+Shift+L)"
        type="button"
      >
        <span className="activity-icon">💬</span>
      </button>
    </nav>
  );
}
