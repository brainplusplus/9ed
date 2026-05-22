export type MobileView = 'explorer' | 'git' | 'editor' | 'terminal' | 'chat' | 'browser';

type BottomNavProps = {
  activeView: MobileView;
  onViewChange: (view: MobileView) => void;
};

function ExplorerIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="2" y="3" width="7" height="9" rx="1" stroke="currentColor" strokeWidth="1.4" />
      <rect x="6" y="8" width="7" height="9" rx="1" stroke="currentColor" strokeWidth="1.4" />
      <rect x="11" y="3" width="7" height="14" rx="1" stroke="currentColor" strokeWidth="1.4" />
    </svg>
  );
}

function GitIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="6" cy="5" r="2" stroke="currentColor" strokeWidth="1.3" />
      <circle cx="6" cy="15" r="2" stroke="currentColor" strokeWidth="1.3" />
      <circle cx="14" cy="10" r="2" stroke="currentColor" strokeWidth="1.3" />
      <line x1="6" y1="7" x2="6" y2="13" stroke="currentColor" strokeWidth="1.3" />
      <path d="M6 7 C6 9, 12 8, 12 10" stroke="currentColor" strokeWidth="1.3" fill="none" />
    </svg>
  );
}

function EditorIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="2" y="2" width="16" height="16" rx="2" stroke="currentColor" strokeWidth="1.3" />
      <line x1="6" y1="7" x2="14" y2="7" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
      <line x1="6" y1="10" x2="12" y2="10" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
      <line x1="6" y1="13" x2="10" y2="13" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  );
}

function TerminalIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="2" y="3" width="16" height="14" rx="2" stroke="currentColor" strokeWidth="1.3" />
      <polyline points="6,8 9,10.5 6,13" stroke="currentColor" strokeWidth="1.3" fill="none" strokeLinecap="round" strokeLinejoin="round" />
      <line x1="11" y1="13" x2="15" y2="13" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
    </svg>
  );
}

function ChatIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M3 4 h14 a1 1 0 0 1 1 1 v8 a1 1 0 0 1 -1 1 h-8 l-4 3.5 v-3.5 h-2 a1 1 0 0 1 -1 -1 v-8 a1 1 0 0 1 1 -1z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
    </svg>
  );
}

function BrowserIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="10" cy="10" r="7" stroke="currentColor" strokeWidth="1.3" />
      <path d="M3.5 8 h13" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
      <path d="M3.5 12 h13" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
      <path d="M10 3 c2 2 3 4.3 3 7 s-1 5 -3 7" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
      <path d="M10 3 c-2 2 -3 4.3 -3 7 s1 5 3 7" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  );
}

const NAV_ITEMS: { view: MobileView; icon: React.ReactNode; label: string }[] = [
  { view: 'explorer', icon: <ExplorerIcon />, label: 'Files' },
  { view: 'git', icon: <GitIcon />, label: 'Git' },
  { view: 'editor', icon: <EditorIcon />, label: 'Editor' },
  { view: 'terminal', icon: <TerminalIcon />, label: 'Term' },
  { view: 'chat', icon: <ChatIcon />, label: 'Chat' },
  { view: 'browser', icon: <BrowserIcon />, label: 'Web' },
];

export function BottomNav({ activeView, onViewChange }: BottomNavProps) {
  return (
    <nav className="bottom-nav">
      {NAV_ITEMS.map((item) => (
        <button
          key={item.view}
          className={`bottom-nav-btn${activeView === item.view ? ' active' : ''}`}
          onClick={() => onViewChange(item.view)}
          aria-label={item.label}
        >
          <span>{item.icon}</span>
          <span className="bottom-nav-label">{item.label}</span>
        </button>
      ))}
    </nav>
  );
}
