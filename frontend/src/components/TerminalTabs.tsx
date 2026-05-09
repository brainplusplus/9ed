import type { SessionTab } from '../types';

type TerminalTabsProps = {
  tabs: SessionTab[];
  activeTabId: string | null;
  onSelectTab: (sessionId: string) => void;
  onCloseTab: (sessionId: string) => void;
};

export function TerminalTabs(props: TerminalTabsProps) {
  const { tabs, activeTabId, onSelectTab, onCloseTab } = props;

  const statusLabel: Record<SessionTab['status'], string> = {
    connecting: 'Connecting',
    ready: 'Ready',
    disconnected: 'Disconnected',
    error: 'Error',
  };

  return (
    <div className="tab-strip" role="tablist" aria-label="Terminal sessions">
      {tabs.map((tab) => {
        const isActive = tab.id === activeTabId;

        return (
          <div key={tab.id} className={`tab-chip tab-chip-${tab.status}${isActive ? ' active' : ''}`}>
            <button
              aria-selected={isActive}
              className="tab-button"
              onClick={() => onSelectTab(tab.id)}
              role="tab"
              type="button"
            >
              <span className="tab-chip-icon" aria-hidden="true">›_</span>
              <span className="tab-chip-copy">
                <span className="tab-chip-title">{tab.profile.label}</span>
                <small>{statusLabel[tab.status]}</small>
              </span>
            </button>
            <button className="tab-close" onClick={() => onCloseTab(tab.id)} type="button" aria-label={`Close ${tab.profile.label}`}>
              ×
            </button>
          </div>
        );
      })}
    </div>
  );
}
