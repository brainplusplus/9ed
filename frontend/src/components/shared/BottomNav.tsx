export type MobileView = 'explorer' | 'git' | 'editor' | 'terminal' | 'chat';

type BottomNavProps = {
  activeView: MobileView;
  onViewChange: (view: MobileView) => void;
};

const NAV_ITEMS: { view: MobileView; icon: string; label: string }[] = [
  { view: 'explorer', icon: '📁', label: 'Files' },
  { view: 'git', icon: '🌿', label: 'Git' },
  { view: 'editor', icon: '📝', label: 'Editor' },
  { view: 'terminal', icon: '🖥', label: 'Term' },
  { view: 'chat', icon: '💬', label: 'Chat' },
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
