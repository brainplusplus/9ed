export type MobileView = 'explorer' | 'git' | 'editor' | 'terminal' | 'chat' | 'browser' | 'settings';

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

function SettingsIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="10" cy="10" r="2.2" stroke="currentColor" strokeWidth="1.3" />
      <path d="M10 3.2V5.1M10 14.9V16.8M16.8 10H14.9M5.1 10H3.2M14.95 5.05L13.6 6.4M6.4 13.6L5.05 14.95M14.95 14.95L13.6 13.6M6.4 6.4L5.05 5.05" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
    </svg>
  );
}

const NAV_ITEMS: { view: MobileView; icon: React.ReactNode; label: string }[] = [
  { view: 'explorer', icon: <ExplorerIcon />, label: 'Files' },
  { view: 'git', icon: <GitIcon />, label: 'Git' },
  { view: 'editor', icon: <EditorIcon />, label: 'Editor' },
  { view: 'terminal', icon: <TerminalIcon />, label: 'Term' },
  { view: 'chat', icon: <ChatIcon />, label: 'Chat' },
];

const BROWSER_ITEM: { view: MobileView; icon: React.ReactNode; label: string } = {
  view: 'browser',
  icon: <BrowserIcon />,
  label: 'Web',
};

const SETTINGS_ITEM: { view: MobileView; icon: React.ReactNode; label: string } = {
  view: 'settings',
  icon: <SettingsIcon />,
  label: 'Prefs',
};

export function BottomNav({ activeView, onViewChange }: BottomNavProps) {
  const items = [...NAV_ITEMS, BROWSER_ITEM, SETTINGS_ITEM];

  return (
    <nav className="bottom-nav">
      {items.map((item) => (
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
