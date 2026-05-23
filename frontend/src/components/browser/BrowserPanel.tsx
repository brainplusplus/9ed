import { FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import { activateBrowserTab, createBrowserTab, deleteBrowserTab, getBrowserState, navigateBrowserTab } from '../../api';
import { useChatStore } from '../../stores/chat';
import type { BrowserAutomationStatus, BrowserElementSelection, BrowserTab } from '../../types';

const DEFAULT_URL = 'localhost:3000';
const VIEWPORT_PRESETS = {
  responsive: { label: 'Auto', width: 0, height: 0 },
  desktop: { label: 'Desktop', width: 1440, height: 900 },
  tablet: { label: 'Tablet', width: 834, height: 1194 },
  mobile: { label: 'Mobile', width: 390, height: 844 },
  custom: { label: 'Custom', width: 1280, height: 720 },
} as const;

type ViewportMode = keyof typeof VIEWPORT_PRESETS;

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

export function BrowserPanel() {
  const panelRef = useRef<HTMLElement | null>(null);
  const stageRef = useRef<HTMLDivElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const cleanupInspectRef = useRef<(() => void) | null>(null);
  const [tabs, setTabs] = useState<BrowserTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [address, setAddress] = useState(DEFAULT_URL);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [automation, setAutomation] = useState<BrowserAutomationStatus | null>(null);
  const [reloadNonce, setReloadNonce] = useState(0);
  const tabsRef = useRef<BrowserTab[]>([]);
  const [viewportMode, setViewportMode] = useState<ViewportMode>('responsive');
  const [customWidth, setCustomWidth] = useState(1280);
  const [customHeight, setCustomHeight] = useState(720);
  const [stageSize, setStageSize] = useState({ width: 0, height: 0 });
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [inspectMode, setInspectMode] = useState(false);
  const [inspectRect, setInspectRect] = useState<{ top: number; left: number; width: number; height: number } | null>(null);
  const browserSelection = useChatStore((s) => s.browserSelection);
  const setBrowserSelection = useChatStore((s) => s.setBrowserSelection);
  const toggleUseActiveBrowser = useChatStore((s) => s.toggleUseActiveBrowser);
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);

  const activeTab = useMemo(
    () => tabs.find((tab) => tab.id === activeTabId) ?? tabs[0] ?? null,
    [tabs, activeTabId],
  );
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
    if (viewportMode === 'responsive' || stageSize.width === 0 || stageSize.height === 0) {
      return 1;
    }
    return Math.min(stageSize.width / viewport.width, stageSize.height / viewport.height, 1);
  }, [stageSize.height, stageSize.width, viewport.height, viewport.width, viewportMode]);
  const scaledViewportStyle = useMemo(() => {
    if (viewportMode === 'responsive') {
      return undefined;
    }
    return {
      width: `${Math.max(1, Math.round(viewport.width * viewportScale))}px`,
      height: `${Math.max(1, Math.round(viewport.height * viewportScale))}px`,
    };
  }, [viewport.height, viewport.width, viewportMode, viewportScale]);
  const frameStyle = useMemo(() => {
    if (viewportMode === 'responsive') {
      return undefined;
    }
    return {
      width: `${viewport.width}px`,
      height: `${viewport.height}px`,
      transform: `scale(${viewportScale})`,
    };
  }, [viewport.height, viewport.width, viewportMode, viewportScale]);

  useEffect(() => {
    tabsRef.current = tabs;
  }, [tabs]);

  useEffect(() => {
    const stage = stageRef.current;
    if (!stage) {
      return;
    }

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
    return () => observer.disconnect();
  }, []);

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

  useEffect(() => {
    if (!inspectMode) {
      cleanupInspectRef.current?.();
      cleanupInspectRef.current = null;
      setInspectRect(null);
      return;
    }

    const iframe = iframeRef.current;
    const stage = stageRef.current;
    if (!iframe || !stage) {
      return;
    }

    const bindInspect = () => {
      try {
        const frameWindow = iframe.contentWindow;
        const doc = iframe.contentDocument;
        if (!frameWindow || !doc) {
          return;
        }

        const updateRect = (element: Element | null) => {
          if (!element) {
            setInspectRect(null);
            return;
          }
          const elementRect = element.getBoundingClientRect();
          const iframeRect = iframe.getBoundingClientRect();
          const stageRect = stage.getBoundingClientRect();
          const scaleX = iframeRect.width / Math.max(1, iframe.clientWidth);
          const scaleY = iframeRect.height / Math.max(1, iframe.clientHeight);
          setInspectRect({
            left: iframeRect.left - stageRect.left + elementRect.left * scaleX,
            top: iframeRect.top - stageRect.top + elementRect.top * scaleY,
            width: Math.max(1, elementRect.width * scaleX),
            height: Math.max(1, elementRect.height * scaleY),
          });
        };

        const handleMove = (event: MouseEvent) => {
          updateRect(event.target instanceof Element ? event.target : null);
        };

        const handleLeave = () => {
          setInspectRect(null);
        };

        const handleClick = (event: MouseEvent) => {
          const element = event.target instanceof Element ? event.target : null;
          if (!element || !activeTab) {
            return;
          }
          event.preventDefault();
          event.stopPropagation();
          const selection = createBrowserSelection(activeTab, element);
          setBrowserSelection(selection);
          if (!useActiveBrowser) {
            toggleUseActiveBrowser();
          }
          updateRect(element);
          setInspectMode(false);
        };

        doc.body.style.cursor = 'crosshair';
        doc.addEventListener('mousemove', handleMove, true);
        doc.addEventListener('mouseleave', handleLeave, true);
        doc.addEventListener('click', handleClick, true);
        cleanupInspectRef.current = () => {
          doc.body.style.cursor = '';
          doc.removeEventListener('mousemove', handleMove, true);
          doc.removeEventListener('mouseleave', handleLeave, true);
          doc.removeEventListener('click', handleClick, true);
        };
      } catch {
        setError('Inspect mode is unavailable for this page');
        setInspectMode(false);
      }
    };

    if (iframe.contentDocument?.readyState === 'complete') {
      bindInspect();
    } else {
      const handleLoad = () => {
        bindInspect();
      };
      iframe.addEventListener('load', handleLoad, { once: true });
      cleanupInspectRef.current = () => iframe.removeEventListener('load', handleLoad);
    }

    return () => {
      cleanupInspectRef.current?.();
      cleanupInspectRef.current = null;
    };
  }, [activeTab, inspectMode, setBrowserSelection, toggleUseActiveBrowser, useActiveBrowser]);

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
        <button
          className={`browser-icon-btn${inspectMode ? ' active' : ''}`}
          type="button"
          title="Inspect element"
          onClick={() => setInspectMode((value) => !value)}
          disabled={!activeTab}
        >
          I
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

      <div className="browser-viewport-toolbar">
        <div className="browser-viewport-presets" role="tablist" aria-label="Viewport mode">
          {(Object.keys(VIEWPORT_PRESETS) as ViewportMode[]).map((mode) => (
            <button
              key={mode}
              className={`browser-viewport-chip${viewportMode === mode ? ' active' : ''}`}
              type="button"
              onClick={() => setViewportMode(mode)}
            >
              {VIEWPORT_PRESETS[mode].label}
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
          <button className="browser-viewport-fullscreen" type="button" onClick={handleFullscreenToggle}>
            {isFullscreen ? 'Exit Full' : 'Full'}
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
          Inspect mode active. Hover an element inside the page, then click to select it for chat context.
        </div>
      )}
      {browserSelection && (
        <div className="browser-selection-bar">
          <div className="browser-selection-copy">
            <strong>Selection</strong>
            <span>{browserSelection.selector}</span>
          </div>
          <button type="button" className="browser-selection-clear" onClick={() => setBrowserSelection(null)}>Clear</button>
        </div>
      )}

      <div ref={stageRef} className={`browser-frame-wrap${viewportMode === 'responsive' ? ' responsive' : ''}`}>
        {activeTab ? (
          <div className="browser-viewport-stage">
            <div className={`browser-viewport-shell${viewportMode === 'responsive' ? ' responsive' : ''}`} style={scaledViewportStyle}>
              <iframe
                ref={iframeRef}
                key={`${activeTab.id}-${reloadNonce}`}
                className={`browser-frame${viewportMode === 'responsive' ? ' responsive' : ''}`}
                style={frameStyle}
                src={activeTab.proxyPath}
                title={displayTitle(activeTab)}
                sandbox="allow-downloads allow-forms allow-modals allow-popups allow-same-origin allow-scripts"
              />
              {inspectRect && <div className="browser-inspect-highlight" style={inspectRect} />}
            </div>
          </div>
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

function createBrowserSelection(tab: BrowserTab, element: Element): BrowserElementSelection {
  const text = element.textContent?.trim().replace(/\s+/g, ' ') ?? '';
  return {
    url: tab.url,
    title: tab.title,
    tagName: element.tagName,
    role: element.getAttribute('role') ?? undefined,
    text: text.slice(0, 160) || undefined,
    selector: buildElementSelector(element),
    outerHTML: element.outerHTML.slice(0, 3000),
  };
}

function buildElementSelector(element: Element): string {
  const parts: string[] = [];
  let current: Element | null = element;
  while (current && parts.length < 4) {
    let part = current.tagName.toLowerCase();
    if (current.id) {
      part += `#${current.id}`;
      parts.unshift(part);
      break;
    }
    const className = (current.getAttribute('class') ?? '').trim().split(/\s+/).filter(Boolean).slice(0, 2).join('.');
    if (className) {
      part += `.${className}`;
    }
    parts.unshift(part);
    current = current.parentElement;
  }
  return parts.join(' > ');
}
