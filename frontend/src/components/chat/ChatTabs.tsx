import { useCallback, useEffect, useRef, useState } from 'react';
import type { ChatSessionInfo, ChatAgent } from '../../types';

type ChatTabsProps = {
  tabs: ChatSessionInfo[];
  activeTabId: string | null;
  agents: ChatAgent[];
  onSelectTab: (sessionId: string) => void;
  onCloseTab: (sessionId: string) => void;
};

const statusLabel: Record<ChatSessionInfo['status'], string> = {
  connecting: 'Connecting',
  idle: 'Ready',
  streaming: 'Working',
  error: 'Error',
};

function agentIcon(agentId: string): string {
  const icons: Record<string, string> = {
    opencode: '⚡',
    claude: '🤖',
    codex: '🔷',
    gemini: '✦',
    pi: 'π',
    amp: '⚡',
    copilot: '✈',
  };
  return icons[agentId] ?? '💬';
}

function tabTitle(tab: ChatSessionInfo): string {
  if (tab.title && tab.title !== tab.agentId) return tab.title;
  return 'New Chat';
}

function tabStatusText(tab: ChatSessionInfo): string {
  if (tab.kind === 'archived') {
    if (tab.status === 'connecting') return 'Reconnecting';
    if (tab.status === 'error') return 'Resume failed';
    if (tab.agentId && tab.workDir) return 'Resumable';
    return 'History';
  }
  return statusLabel[tab.status];
}

export function ChatTabs({ tabs, activeTabId, agents, onSelectTab, onCloseTab }: ChatTabsProps) {
  const stripRef = useRef<HTMLDivElement>(null);
  const [canScrollLeft, setCanScrollLeft] = useState(false);
  const [canScrollRight, setCanScrollRight] = useState(false);

  const updateScrollState = useCallback(() => {
    const el = stripRef.current;
    if (!el) return;
    const threshold = 2;
    setCanScrollLeft(el.scrollLeft > threshold);
    setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - threshold);
  }, []);

  useEffect(() => {
    const el = stripRef.current;
    if (!el) return;

    updateScrollState();

    el.addEventListener('scroll', updateScrollState, { passive: true });

    const observer = new ResizeObserver(updateScrollState);
    observer.observe(el);

    return () => {
      el.removeEventListener('scroll', updateScrollState);
      observer.disconnect();
    };
  }, [tabs, activeTabId, updateScrollState]);

  useEffect(() => {
    if (!activeTabId || !stripRef.current) return;
    const activeTab = stripRef.current.querySelector('.chat-tab-chip.active');
    if (activeTab) {
      activeTab.scrollIntoView({ behavior: 'smooth', inline: 'nearest', block: 'nearest' });
    }
  }, [activeTabId]);

  const scrollPrev = useCallback(() => {
    stripRef.current?.scrollBy({ left: -200, behavior: 'smooth' });
  }, []);

  const scrollNext = useCallback(() => {
    stripRef.current?.scrollBy({ left: 200, behavior: 'smooth' });
  }, []);

  if (tabs.length === 0) return null;

  return (
    <div className="chat-tab-strip-container">
      {canScrollLeft && (
        <button className="chat-tab-nav-btn chat-tab-nav-prev" onClick={scrollPrev} type="button" aria-label="Previous tabs">
          <span aria-hidden="true">‹</span>
        </button>
      )}
      <div className="chat-tab-strip" ref={stripRef} role="tablist" aria-label="Chat sessions">
        {tabs.map((tab) => {
          const isActive = tab.id === activeTabId;
          const agent = agents.find((a) => a.id === tab.agentId);

          return (
            <div key={tab.id} className={`chat-tab-chip chat-tab-chip-${tab.status}${isActive ? ' active' : ''}${tab.kind === 'archived' ? ' chat-tab-chip-archived' : ''}`}>
              <button
                aria-selected={isActive}
                className="chat-tab-button"
                onClick={() => onSelectTab(tab.id)}
                role="tab"
                type="button"
              >
                <span className="chat-tab-chip-icon" aria-hidden="true">{agentIcon(tab.agentId)}</span>
                <span className="chat-tab-chip-copy">
                  <span className="chat-tab-chip-title">{tabTitle(tab)}</span>
                  <small>{agent?.label ?? tab.agentId} · {tabStatusText(tab)}</small>
                </span>
              </button>
              <button className="chat-tab-close" onClick={() => onCloseTab(tab.id)} type="button" aria-label={`Close ${tabTitle(tab)}`}>
                ×
              </button>
            </div>
          );
        })}
      </div>
      {canScrollRight && (
        <button className="chat-tab-nav-btn chat-tab-nav-next" onClick={scrollNext} type="button" aria-label="Next tabs">
          <span aria-hidden="true">›</span>
        </button>
      )}
    </div>
  );
}
