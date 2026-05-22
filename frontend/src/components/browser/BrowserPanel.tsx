import { FormEvent, useEffect, useMemo, useState } from 'react';
import { createBrowserTab, deleteBrowserTab, getBrowserState, navigateBrowserTab } from '../../api';
import type { BrowserAutomationStatus, BrowserTab } from '../../types';

const DEFAULT_URL = 'localhost:3000';

function displayTitle(tab: BrowserTab): string {
  return tab.title || tab.url.replace(/^https?:\/\//, '');
}

export function BrowserPanel() {
  const [tabs, setTabs] = useState<BrowserTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [address, setAddress] = useState(DEFAULT_URL);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [automation, setAutomation] = useState<BrowserAutomationStatus | null>(null);
  const [reloadNonce, setReloadNonce] = useState(0);

  const activeTab = useMemo(
    () => tabs.find((tab) => tab.id === activeTabId) ?? tabs[0] ?? null,
    [tabs, activeTabId],
  );

  useEffect(() => {
    let alive = true;
    getBrowserState()
      .then((state) => {
        if (!alive) return;
        setTabs(state.tabs);
        setAutomation(state.automation);
        setActiveTabId(state.activeTabId || state.tabs[0]?.id || null);
        if (state.tabs.length === 0) {
          return createBrowserTab(DEFAULT_URL);
        }
        return null;
      })
      .then((tab) => {
        if (!alive || !tab) return;
        setTabs([tab]);
        setActiveTabId(tab.id);
      })
      .catch((err: Error) => {
        if (alive) setError(err.message);
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (activeTab) {
      setAddress(activeTab.url);
    }
  }, [activeTab?.id, activeTab?.url]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!address.trim()) return;
    setLoading(true);
    setError(null);
    try {
      const tab = activeTab
        ? await navigateBrowserTab(activeTab.id, address)
        : await createBrowserTab(address);
      setTabs((current) => {
        const exists = current.some((item) => item.id === tab.id);
        return exists ? current.map((item) => (item.id === tab.id ? tab : item)) : [...current, tab];
      });
      setActiveTabId(tab.id);
      setReloadNonce((value) => value + 1);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to navigate');
    } finally {
      setLoading(false);
    }
  }

  async function handleNewTab() {
    setLoading(true);
    setError(null);
    try {
      const tab = await createBrowserTab(DEFAULT_URL);
      setTabs((current) => [...current, tab]);
      setActiveTabId(tab.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create tab');
    } finally {
      setLoading(false);
    }
  }

  async function handleCloseTab(tabId: string) {
    const nextTabs = tabs.filter((tab) => tab.id !== tabId);
    setTabs(nextTabs);
    if (activeTabId === tabId) {
      setActiveTabId(nextTabs[0]?.id ?? null);
    }
    try {
      await deleteBrowserTab(tabId);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to close tab');
    }
  }

  function handleReload() {
    setReloadNonce((value) => value + 1);
  }

  return (
    <section className="browser-panel">
      <div className="browser-tab-strip">
        {tabs.map((tab) => (
          <div key={tab.id} className={`browser-tab-chip${tab.id === activeTab?.id ? ' active' : ''}`}>
            <button className="browser-tab-button" type="button" onClick={() => setActiveTabId(tab.id)} title={tab.url}>
              <span className="browser-tab-dot" />
              <span className="browser-tab-title">{displayTitle(tab)}</span>
            </button>
            <button className="browser-tab-close" type="button" onClick={() => handleCloseTab(tab.id)} aria-label={`Close ${displayTitle(tab)}`}>
              x
            </button>
          </div>
        ))}
        <button className="browser-icon-btn" type="button" onClick={handleNewTab} title="New tab">
          +
        </button>
      </div>

      <form className="browser-toolbar" onSubmit={handleSubmit}>
        <button className="browser-icon-btn" type="button" title="Back" disabled>
          &lt;
        </button>
        <button className="browser-icon-btn" type="button" title="Forward" disabled>
          &gt;
        </button>
        <button className="browser-icon-btn" type="button" title="Reload" onClick={handleReload} disabled={!activeTab}>
          R
        </button>
        <input
          className="browser-address"
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          spellCheck={false}
          placeholder="localhost:3000"
        />
        <button className="browser-go-btn" type="submit" disabled={loading}>
          {loading ? 'Opening' : 'Go'}
        </button>
      </form>

      {(error || automation?.lastError) && (
        <div className="browser-status-line">
          {error || automation?.lastError}
        </div>
      )}

      <div className="browser-frame-wrap">
        {activeTab ? (
          <iframe
            key={`${activeTab.id}-${reloadNonce}`}
            className="browser-frame"
            src={activeTab.proxyPath}
            title={displayTitle(activeTab)}
            sandbox="allow-downloads allow-forms allow-modals allow-popups allow-same-origin allow-scripts"
          />
        ) : (
          <div className="browser-empty">
            <button className="browser-go-btn" type="button" onClick={handleNewTab}>
              Open Browser
            </button>
          </div>
        )}
      </div>
    </section>
  );
}
