import { useWorkspaceStore } from '../../stores/workspace';
import { useGitStore } from '../../stores/git';
import type { ActivePanel } from '../../types';

function ExplorerIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="2" y="3" width="7" height="9" rx="1" stroke="currentColor" strokeWidth="1.4" />
      <rect x="6" y="8" width="7" height="9" rx="1" stroke="currentColor" strokeWidth="1.4" />
      <rect x="11" y="3" width="7" height="14" rx="1" stroke="currentColor" strokeWidth="1.4" />
    </svg>
  );
}

function SearchIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="8.5" cy="8.5" r="5.5" stroke="currentColor" strokeWidth="1.5" />
      <line x1="12.5" y1="12.5" x2="17" y2="17" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function GitIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="6" cy="5" r="2" stroke="currentColor" strokeWidth="1.3" />
      <circle cx="6" cy="15" r="2" stroke="currentColor" strokeWidth="1.3" />
      <circle cx="14" cy="10" r="2" stroke="currentColor" strokeWidth="1.3" />
      <line x1="6" y1="7" x2="6" y2="13" stroke="currentColor" strokeWidth="1.3" />
      <path d="M6 7 C6 9, 12 8, 12 10" stroke="currentColor" strokeWidth="1.3" fill="none" />
    </svg>
  );
}

function ProjectsIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M2 5 L2 16 L18 16 L18 5 L11 5 L9.5 3 L2 3 Z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
    </svg>
  );
}

function TerminalIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="2" y="3" width="16" height="14" rx="2" stroke="currentColor" strokeWidth="1.3" />
      <polyline points="6,8 9,10.5 6,13" stroke="currentColor" strokeWidth="1.3" fill="none" strokeLinecap="round" strokeLinejoin="round" />
      <line x1="11" y1="13" x2="15" y2="13" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
    </svg>
  );
}

function ChatIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M3 4 h14 a1 1 0 0 1 1 1 v8 a1 1 0 0 1 -1 1 h-8 l-4 3.5 v-3.5 h-2 a1 1 0 0 1 -1 -1 v-8 a1 1 0 0 1 1 -1z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
      <circle cx="14" cy="5" r="2.5" fill="currentColor" opacity="0.3" />
      <path d="M13 4.5 l0.8 1.2 l1.7 -2" stroke="currentColor" strokeWidth="0.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function BrandIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 22 22" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="3" y="3" width="16" height="16" rx="4" fill="url(#brand-grad)" />
      <path d="M8 8.5 L11 11 L8 13.5" stroke="#EAF3FF" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 8.5 L11 11 L14 13.5" stroke="#EAF3FF" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <defs>
        <linearGradient id="brand-grad" x1="3" y1="3" x2="19" y2="19" gradientUnits="userSpaceOnUse">
          <stop stopColor="#2D8CFF" />
          <stop offset="1" stopColor="#155EEF" />
        </linearGradient>
      </defs>
    </svg>
  );
}

const panels: { id: ActivePanel; icon: React.ReactNode; label: string }[] = [
  { id: 'explorer', icon: <ExplorerIcon />, label: 'Explorer' },
  { id: 'search', icon: <SearchIcon />, label: 'Search' },
  { id: 'git', icon: <GitIcon />, label: 'Source Control' },
  { id: 'projects', icon: <ProjectsIcon />, label: 'Projects' },
  { id: 'terminal', icon: <TerminalIcon />, label: 'Terminal' },
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
      <div className="activity-brand" title="go-webttyd">
        <span className="activity-brand-icon"><BrandIcon /></span>
      </div>
      <div className="activity-bar-divider" />
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
        <span className="activity-icon"><ChatIcon /></span>
      </button>
    </nav>
  );
}
