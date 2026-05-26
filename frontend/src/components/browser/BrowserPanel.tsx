import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { activateBrowserTab, createBrowserTab, deleteBrowserTab, getBrowserState, navigateBrowserTab } from '../../api';
import { useChatStore } from '../../stores/chat';
import type { BrowserAutomationStatus, BrowserTab } from '../../types';
import { useInspectMode } from './useInspectMode';
import { InspectOverlay, SelectedHighlight, InspectMiniPanel } from './InspectOverlay';

const DEFAULT_URL = 'localhost:3000';
const VIEWPORT_PRESETS = {
  responsive: { label: 'Auto', width: 0, height: 0 },
  desktop: { label: 'Desktop', width: 1440, height: 900 },
  tablet: { label: 'Tablet', width: 834, height: 1194 },
  mobile: { label: 'Mobile', width: 390, height: 844 },
  custom: { label: 'Custom', width: 1280, height: 720 },
} as const;

type ViewportMode = keyof typeof VIEWPORT_PRESETS;
const FIXED_VIEWPORT_STAGE_X_PADDING = 36;
const RESPONSIVE_FIT_MIN_WIDTH = 1024;

type BrowserProxyMessage = {
  __nineBrowser: true;
  type: 'open-tab' | 'close-tab' | 'focus-tab' | 'post-message';
  tabId: string;
  url?: string;
  target?: string;
};

function displayTitle(tab: BrowserTab): string {
  return tab.title || tab.url.replace(/^https?:\/\//, '');
}

/* ── Inline SVG icon components ── */

function IconChevronLeft() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M10 3L5 8l5 5" />
    </svg>
  );
}

function IconChevronRight() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M6 3l5 5-5 5" />
    </svg>
  );
}

function IconReload() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M2.5 8a5.5 5.5 0 0 1 9.3-3.95M13.5 8a5.5 5.5 0 0 1-9.3 3.95" />
      <path d="M12 1.5v2.55h-2.55M4 14.5v-2.55h2.55" />
    </svg>
  );
}

function IconInspect() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d="M6.5 2a4.5 4.5 0 0 1 3.4 7.45l3.55 3.55-1.05 1.05-3.55-3.55A4.5 4.5 0 1 1 6.5 2z" />
      <circle cx="6.5" cy="6.5" r="2" />
    </svg>
  );
}

function IconPlus() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
      <path d="M8 3v10M3 8h10" />
    </svg>
  );
}

function IconClose() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
      <path d="M4 4l8 8M12 4l-8 8" />
    </svg>
  );
}

function IconExpand() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M2 10v4h4M14 6V2h-4M2 14l5-5M14 2l-5 5" />
    </svg>
  );
}

function IconShrink() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M6 2v4H2M10 14v-4h4M6 6L2 2M10 10l4 4" />
    </svg>
  );
}

function IconGlobe() {
  return (
    <svg aria-hidden="true" className="browser-empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="10" />
      <path d="M2 12h20M12 2a15 15 0 0 1 4 10 15 15 0 0 1-4 10 15 15 0 0 1-4-10A15 15 0 0 1 12 2z" />
    </svg>
  );
}

function ViewportModeIcon({ mode }: { mode: ViewportMode }) {
  if (mode === 'responsive') {
    return (
      <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
        <path d="M3 8h10" />
        <path d="M5.5 5.5 3 8l2.5 2.5M10.5 5.5 13 8l-2.5 2.5" />
        <rect x="1.75" y="3" width="12.5" height="10" rx="1.5" />
      </svg>
    );
  }

  if (mode === 'desktop') {
    return (
      <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
        <rect x="1.75" y="2.5" width="12.5" height="8.5" rx="1.3" />
        <path d="M6.25 13.5h3.5M8 11v2.5" />
      </svg>
    );
  }

  if (mode === 'tablet') {
    return (
      <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
        <rect x="3.5" y="1.5" width="9" height="13" rx="1.5" />
        <path d="M7.25 12.5h1.5" />
      </svg>
    );
  }

  if (mode === 'mobile') {
    return (
      <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
        <rect x="5" y="1.5" width="6" height="13" rx="1.4" />
        <path d="M7.35 12.25h1.3" />
      </svg>
    );
  }

  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M3 3h10v10H3z" />
      <path d="M5 1.75h6M5 14.25h6M1.75 5v6M14.25 5v6" />
    </svg>
  );
}

export function BrowserPanel() {
  const panelRef = useRef<HTMLElement | null>(null);
  const stageRef = useRef<HTMLDivElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const [tabs, setTabs] = useState<BrowserTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [address, setAddress] = useState(DEFAULT_URL);
  const [loading, setLoading] = useState(false);
  const [initializing, setInitializing] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [automation, setAutomation] = useState<BrowserAutomationStatus | null>(null);
  const [reloadNonce, setReloadNonce] = useState(0);
  const tabsRef = useRef<BrowserTab[]>([]);
  const [viewportMode, setViewportMode] = useState<ViewportMode>('responsive');
  const [customWidth, setCustomWidth] = useState(1280);
  const [customHeight, setCustomHeight] = useState(720);
  const [stageSize, setStageSize] = useState({ width: 0, height: 0 });
  const [autoContentSize, setAutoContentSize] = useState({ width: 0, height: 0 });
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showMiniPanel, setShowMiniPanel] = useState(false);
  const addressEditingRef = useRef(false);
  const browserSelection = useChatStore((s) => s.browserSelection);

  const activeTab = useMemo(
    () => tabs.find((tab) => tab.id === activeTabId) ?? tabs[0] ?? null,
    [tabs, activeTabId],
  );

  const updateStageSize = useCallback(() => {
    const stage = stageRef.current;
    if (!stage) {
      return;
    }
    const rect = stage.getBoundingClientRect();
    const width = Math.floor(rect.width);
    const height = Math.floor(rect.height);
    if (width <= 0 || height <= 0) {
      return;
    }
    setStageSize((current) => (
      current.width === width && current.height === height ? current : { width, height }
    ));
  }, []);

  // Inspect mode hook (Tier 1-4)
  const {
    inspectState,
    inspectMode,
    toggleInspectMode,
    clearSelection,
  } = useInspectMode(iframeRef, stageRef, activeTab);
  const viewport = useMemo(() => {
    if (viewportMode === 'custom') {
      return {
        label: VIEWPORT_PRESETS.custom.label,
        width: Math.max(320, customWidth),
        height: Math.max(320, customHeight),
      };
    }
    return VIEWPORT_PRESETS[viewportMode];
  }, [customHeight, customWidth, viewportMode]);
  const viewportScale = useMemo(() => {
    if (stageSize.width === 0 || stageSize.height === 0) {
      return 1;
    }
    if (viewportMode === 'responsive') {
      const contentWidth = Math.max(stageSize.width, autoContentSize.width, RESPONSIVE_FIT_MIN_WIDTH);
      return Math.min(stageSize.width / contentWidth, 1);
    }
    const availableWidth = Math.max(1, stageSize.width - FIXED_VIEWPORT_STAGE_X_PADDING);
    return Math.min(availableWidth / viewport.width, 1);
  }, [autoContentSize.width, stageSize.height, stageSize.width, viewport.width, viewportMode]);
  const scaledViewportStyle = useMemo(() => {
    if (viewportMode === 'responsive') {
      if (stageSize.width === 0 || stageSize.height === 0) {
        return undefined;
      }
      const contentWidth = Math.max(stageSize.width, autoContentSize.width, RESPONSIVE_FIT_MIN_WIDTH);
      return {
        width: `${Math.max(1, Math.round(contentWidth * viewportScale))}px`,
        height: `${Math.max(1, stageSize.height)}px`,
      };
    }
    return {
      width: `${Math.max(1, Math.round(viewport.width * viewportScale))}px`,
      height: `${Math.max(1, Math.round(viewport.height * viewportScale))}px`,
    };
  }, [autoContentSize.width, stageSize.height, stageSize.width, viewport.height, viewport.width, viewportMode, viewportScale]);
  const frameStyle = useMemo(() => {
    if (viewportMode === 'responsive') {
      if (stageSize.width === 0 || stageSize.height === 0) {
        return undefined;
      }
      const contentWidth = Math.max(stageSize.width, autoContentSize.width, RESPONSIVE_FIT_MIN_WIDTH);
      return {
        width: `${Math.max(1, Math.round(contentWidth))}px`,
        height: `${Math.max(1, Math.round(stageSize.height / viewportScale))}px`,
        transform: `scale(${viewportScale})`,
      };
    }
    return {
      width: `${viewport.width}px`,
      height: `${viewport.height}px`,
      transform: `scale(${viewportScale})`,
    };
  }, [autoContentSize.width, stageSize.height, stageSize.width, viewport.height, viewport.width, viewportMode, viewportScale]);

  const updateAutoContentSize = useCallback(() => {
    if (viewportMode !== 'responsive') {
      return;
    }
    try {
      const doc = iframeRef.current?.contentDocument;
      if (!doc) {
        return;
      }
      const body = doc.body;
      const root = doc.documentElement;
      const width = Math.ceil(Math.max(
        root?.scrollWidth ?? 0,
        root?.offsetWidth ?? 0,
        body?.scrollWidth ?? 0,
        body?.offsetWidth ?? 0,
      ));
      const height = Math.ceil(Math.max(
        root?.scrollHeight ?? 0,
        root?.offsetHeight ?? 0,
        body?.scrollHeight ?? 0,
        body?.offsetHeight ?? 0,
      ));
      setAutoContentSize((current) => (
        current.width === width && current.height === height ? current : { width, height }
      ));
    } catch {
      setAutoContentSize({ width: 0, height: 0 });
    }
  }, [viewportMode]);

  useEffect(() => {
    tabsRef.current = tabs;
  }, [tabs]);

  useEffect(() => {
    const stage = stageRef.current;
    if (!stage) {
      return;
    }

    updateStageSize();
    const frame = window.requestAnimationFrame(updateStageSize);
    const interval = window.setInterval(updateStageSize, 500);
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) {
        return;
      }
      setStageSize({
        width: Math.floor(entry.contentRect.width),
        height: Math.floor(entry.contentRect.height),
      });
    });
    observer.observe(stage);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearInterval(interval);
      observer.disconnect();
    };
  }, [activeTab, updateStageSize, viewportMode]);

  useEffect(() => {
    if (viewportMode !== 'responsive' || !activeTab) {
      return;
    }

    updateAutoContentSize();
    const interval = window.setInterval(updateAutoContentSize, 700);
    return () => window.clearInterval(interval);
  }, [activeTab, reloadNonce, updateAutoContentSize, viewportMode]);

  useEffect(() => {
    function handleFullscreenChange() {
      setIsFullscreen(document.fullscreenElement === panelRef.current);
    }

    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange);
  }, []);

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
      })
      .finally(() => {
        if (alive) setInitializing(false);
      });
    return () => {
      alive = false;
    };
  }, []);

  // Sync address from active tab — skip while user is editing
  useEffect(() => {
    if (activeTab && !addressEditingRef.current) {
      setAddress(activeTab.url);
    }
  }, [activeTab]);

  useEffect(() => {
    async function handleProxyMessage(event: MessageEvent<BrowserProxyMessage>) {
      const data = event.data;
      if (!data || data.__nineBrowser !== true || !data.tabId) {
        return;
      }

      if (data.type === 'open-tab' && data.url) {
        try {
          setError(null);
          const tab = await createBrowserTab(data.url);
          setTabs((current) => [...current, tab]);
          setActiveTabId(tab.id);
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Failed to open browser tab');
        }
        return;
      }

      if (data.type === 'close-tab') {
        const existing = tabsRef.current.find((tab) => tab.id === data.tabId);
        if (!existing) {
          return;
        }
        await handleCloseTab(data.tabId);
        return;
      }

      if (data.type === 'focus-tab') {
        await handleSelectTab(data.tabId);
      }
    }

    window.addEventListener('message', handleProxyMessage);
    return () => {
      window.removeEventListener('message', handleProxyMessage);
    };
  }, [activeTabId]);

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
    const nextTabs = tabsRef.current.filter((tab) => tab.id !== tabId);
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
    setAutoContentSize({ width: 0, height: 0 });
    setReloadNonce((value) => value + 1);
  }

  async function handleSelectTab(tabId: string) {
    setActiveTabId(tabId);
    try {
      await activateBrowserTab(tabId);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to activate browser tab');
    }
  }

  async function handleFullscreenToggle() {
    const panel = panelRef.current;
    if (!panel) {
      return;
    }

    try {
      if (document.fullscreenElement === panel) {
        await document.exitFullscreen();
        return;
      }
      await panel.requestFullscreen();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to toggle fullscreen');
    }
  }

  const handleAddressFocus = useCallback(() => {
    addressEditingRef.current = true;
  }, []);

  const handleAddressBlur = useCallback(() => {
    addressEditingRef.current = false;
  }, []);

  // ── Loading state ──
  if (initializing) {
    return (
      <section className="browser-panel">
        <div className="browser-loading">
          <div className="browser-loading-spinner" />
          <span className="browser-loading-text">Loading browser…</span>
        </div>
      </section>
    );
  }

  return (
    <section ref={panelRef} className={`browser-panel${isFullscreen ? ' fullscreen' : ''}`}>
      <div className="browser-tab-strip">
        {tabs.map((tab) => (
          <div key={tab.id} className={`browser-tab-chip${tab.id === activeTab?.id ? ' active' : ''}`}>
            <button className="browser-tab-button" type="button" onClick={() => void handleSelectTab(tab.id)} title={tab.url}>
              <span className="browser-tab-dot" />
              <span className="browser-tab-title">{displayTitle(tab)}</span>
            </button>
            <button className="browser-tab-close" type="button" onClick={() => handleCloseTab(tab.id)} aria-label={`Close ${displayTitle(tab)}`}>
              <IconClose />
            </button>
          </div>
        ))}
        <button className="browser-icon-btn" type="button" onClick={handleNewTab} title="New tab">
          <IconPlus />
        </button>
      </div>

      <form className="browser-toolbar" onSubmit={handleSubmit}>
        <button className="browser-icon-btn" type="button" title="Back" disabled>
          <IconChevronLeft />
        </button>
        <button className="browser-icon-btn" type="button" title="Forward" disabled>
          <IconChevronRight />
        </button>
        <button className="browser-icon-btn" type="button" title="Reload" onClick={handleReload} disabled={!activeTab}>
          <IconReload />
        </button>
        <button
          className={`browser-icon-btn${inspectMode ? ' active' : ''}`}
          type="button"
          title="Inspect element"
          onClick={() => toggleInspectMode()}
          disabled={!activeTab}
        >
          <IconInspect />
        </button>
        <input
          className="browser-address"
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          onFocus={handleAddressFocus}
          onBlur={handleAddressBlur}
          spellCheck={false}
          placeholder="Enter URL…"
        />
        <button className="browser-go-btn" type="submit" disabled={loading}>
          {loading ? '…' : 'Go'}
        </button>
      </form>

      <div className="browser-viewport-toolbar">
        <div className="browser-viewport-presets" role="tablist" aria-label="Viewport mode">
          {(Object.keys(VIEWPORT_PRESETS) as ViewportMode[]).map((mode) => (
            <button
              key={mode}
              className={`browser-viewport-chip${viewportMode === mode ? ' active' : ''}`}
              type="button"
              role="tab"
              aria-label={`${VIEWPORT_PRESETS[mode].label} viewport`}
              aria-selected={viewportMode === mode}
              title={VIEWPORT_PRESETS[mode].label}
              onClick={() => setViewportMode(mode)}
            >
              <ViewportModeIcon mode={mode} />
            </button>
          ))}
        </div>
        <div className="browser-viewport-fields">
          <label className="browser-viewport-field">
            <span>W</span>
            <input
              type="number"
              min={320}
              max={4096}
              step={1}
              value={viewportMode === 'responsive' ? stageSize.width || '' : viewport.width}
              onChange={(event) => {
                const value = Number(event.target.value);
                if (Number.isFinite(value)) {
                  setViewportMode('custom');
                  setCustomWidth(value);
                }
              }}
              disabled={viewportMode === 'responsive'}
            />
          </label>
          <label className="browser-viewport-field">
            <span>H</span>
            <input
              type="number"
              min={320}
              max={4096}
              step={1}
              value={viewportMode === 'responsive' ? stageSize.height || '' : viewport.height}
              onChange={(event) => {
                const value = Number(event.target.value);
                if (Number.isFinite(value)) {
                  setViewportMode('custom');
                  setCustomHeight(value);
                }
              }}
              disabled={viewportMode === 'responsive'}
            />
          </label>
          <button
            className="browser-viewport-fullscreen"
            type="button"
            onClick={handleFullscreenToggle}
            aria-label={isFullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}
            title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? <IconShrink /> : <IconExpand />}
          </button>
        </div>
      </div>

      {(error || automation?.lastError) && (
        <div className="browser-status-line">
          {error || automation?.lastError}
        </div>
      )}
      {inspectMode && (
        <div className="browser-inspect-tip">
          <span>Inspect mode — hover and click to select for AI chat. Press Esc to cancel.</span>
          <span className="kbd-hint">
            <kbd>↑↓</kbd> navigate
            <kbd>Enter</kbd> select
            <kbd>Esc</kbd> cancel
          </span>
        </div>
      )}
      {browserSelection && (
        <BrowserSelectionBar
          selection={browserSelection}
          onClear={clearSelection}
          onReselect={toggleInspectMode}
          onTogglePanel={() => setShowMiniPanel((v) => !v)}
          showPanel={showMiniPanel}
        />
      )}

      <div ref={stageRef} className={`browser-frame-wrap${viewportMode === 'responsive' ? ' responsive' : ''}`}>
        {activeTab ? (
          <div className="browser-viewport-stage">
            <div className={`browser-viewport-shell${viewportMode === 'responsive' ? ' responsive' : ''}`} style={scaledViewportStyle}>
              <iframe
                ref={iframeRef}
                key={`${activeTab.id}-${reloadNonce}`}
                className={`browser-frame${viewportMode === 'responsive' ? ' responsive' : ''}`}
                style={{ ...frameStyle, pointerEvents: inspectMode ? 'none' : 'auto' }}
                src={activeTab.proxyPath}
                title={displayTitle(activeTab)}
                onLoad={() => {
                  updateAutoContentSize();
                  window.setTimeout(updateAutoContentSize, 250);
                  window.setTimeout(updateAutoContentSize, 1000);
                }}
                sandbox="allow-downloads allow-forms allow-modals allow-popups allow-same-origin allow-scripts"
              />
              {/* Event-capture overlay — catches mouse events when iframe has pointer-events:none */}
              {inspectMode && (
                <div className="browser-inspect-hitlayer" />
              )}
              {/* Canvas overlay for inspect highlight (Tier 1-2) */}
              <InspectOverlay
                boxModel={inspectState.hoveredBoxModel}
                outerRect={inspectState.hoveredRect}
                tooltip={inspectState.tooltip}
                iframeRef={iframeRef}
                inspectMode={inspectMode}
                selection={browserSelection}
              />
              {/* Persistent selected element highlight */}
              {!inspectMode && browserSelection && (
                <SelectedHighlight
                  selection={browserSelection}
                  iframeRef={iframeRef}
                />
              )}
              {/* Mini panel for detailed inspect (Tier 4) */}
              <InspectMiniPanel
                selection={browserSelection}
                visible={showMiniPanel}
                onClose={() => setShowMiniPanel(false)}
              />
            </div>
          </div>
        ) : (
          <div className="browser-empty">
            <IconGlobe />
            <button className="browser-go-btn" type="button" onClick={handleNewTab}>
              Open Browser
            </button>
          </div>
        )}
      </div>
    </section>
  );
}

/* ── Selection bar with chat indicator (Tier 3) ── */

function BrowserSelectionBar({
  selection,
  onClear,
  onReselect,
  onTogglePanel,
  showPanel,
}: {
  selection: { selector: string };
  onClear: () => void;
  onReselect: () => void;
  onTogglePanel: () => void;
  showPanel: boolean;
}) {
  const linked = useChatStore((s) => s.useActiveBrowser);

  return (
    <div className={`browser-selection-bar${linked ? ' linked' : ''}`}>
      <div className="browser-selection-copy">
        <strong>
          Selection
          {linked && <span className="chat-linked-badge">→ linked to chat</span>}
        </strong>
        <span>{selection.selector}</span>
      </div>
      <div className="browser-selection-actions">
        <button type="button" className="browser-selection-reselect" onClick={onReselect}>
          Re-select
        </button>
        <button
          type="button"
          className={`browser-selection-panel${showPanel ? ' active' : ''}`}
          onClick={onTogglePanel}
        >
          Inspect
        </button>
        <button type="button" className="browser-selection-clear" onClick={onClear}>
          Clear
        </button>
      </div>
    </div>
  );
}
