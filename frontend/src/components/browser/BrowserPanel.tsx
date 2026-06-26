import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { activateBrowserTab, browserTabScreenshotUrl, captureBrowserElementScreenshot, createBrowserTab, deleteBrowserTab, getBrowserMCPDebugLog, getBrowserState, goBackBrowserTab, goForwardBrowserTab, inspectBrowserTabNavigate, inspectBrowserTabPoint, mouseDownBrowserTab, mouseMoveBrowserTab, mouseUpBrowserTab, navigateBrowserTab, pasteBrowserTabClipboard, pressBrowserTabKey, reloadBrowserTab, scrollBrowserTab, setBrowserTabViewport, stopBrowserTab, syncBrowserTabState, typeBrowserTabText, uploadBrowserTabFiles } from '../../api';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';
import type { BrowserAutomationStatus, BrowserElementSelection, BrowserMCPDebugEntry, BrowserSelectionMode, BrowserTab, BrowserTransport } from '../../types';
import { useInspectMode } from './useInspectMode';
import { InspectOverlay, RemoteInspectOverlay, SelectedHighlight, InspectMiniPanel } from './InspectOverlay';
import { useVisualStream } from '../../hooks/useVisualStream';
import { useGestures } from '../../hooks/useGestures';

const DEFAULT_URL = 'about:blank';
const VIEWPORT_PRESETS = {
  responsive: { label: 'Auto', width: 0, height: 0 },
  desktop: { label: 'Desktop', width: 1440, height: 900 },
  tablet: { label: 'Tablet', width: 834, height: 1194 },
  mobile: { label: 'Mobile', width: 390, height: 844 },
  custom: { label: 'Custom', width: 1280, height: 720 },
} as const;

type ViewportMode = keyof typeof VIEWPORT_PRESETS;
const FIXED_VIEWPORT_STAGE_X_PADDING = 36;
const WEBRTC_FRAME_REFRESH_TIMEOUT_MS = 8000;

type BrowserProxyMessage = {
  __nineBrowser: true;
  type: 'open-tab' | 'close-tab' | 'focus-tab' | 'post-message' | 'location-change';
  tabId: string;
  url?: string;
  target?: string;
  title?: string;
  canGoBack?: boolean;
  canGoForward?: boolean;
};

type RemoteTooltip = {
  top: number;
  left: number;
  label: string;
  sublabel?: string;
};

function formatDebugTimestamp(timestamp: number): string {
  if (!Number.isFinite(timestamp) || timestamp <= 0) {
    return '--:--:--';
  }
  return new Date(timestamp).toLocaleTimeString([], {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function displayTitle(tab: BrowserTab): string {
  return tab.title || tab.url.replace(/^https?:\/\//, '');
}

function buildRemoteTooltip(selection: BrowserElementSelection, scaleX: number, scaleY: number): RemoteTooltip {
  const rect = selection.boundingRect;
  const box = selection.boxModel;
  const label = [
    selection.tagName.toLowerCase(),
    selection.attributes?.id ? `#${selection.attributes.id}` : '',
    selection.attributes?.class ? `.${selection.attributes.class.split(/\s+/).filter(Boolean).slice(0, 2).join('.')}` : '',
  ].join('');
  const dims = rect ? `${Math.round(rect.width)}x${Math.round(rect.height)}` : '';
  const subtitleParts = [dims];
  if (box) {
    subtitleParts.push(`margin ${Math.round(box.margin.top)} ${Math.round(box.margin.right)} ${Math.round(box.margin.bottom)} ${Math.round(box.margin.left)}`);
  }
  return {
    top: Math.max(4, ((rect?.y ?? 0) * scaleY) - 32),
    left: Math.max(4, ((rect?.x ?? 0) * scaleX) + 8),
    label,
    sublabel: subtitleParts.filter(Boolean).join(' | '),
  };
}

function isPrintableKey(event: React.KeyboardEvent<HTMLDivElement>): boolean {
  return event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey;
}

function toPlaywrightKey(event: React.KeyboardEvent<HTMLDivElement>): string | null {
  const modifiers: string[] = [];
  if (event.ctrlKey) modifiers.push('Control');
  if (event.metaKey) modifiers.push('Meta');
  if (event.altKey) modifiers.push('Alt');
  if (event.shiftKey && event.key.length > 1) modifiers.push('Shift');

  const keyMap: Record<string, string> = {
    ' ': 'Space',
    Escape: 'Escape',
    Enter: 'Enter',
    Tab: 'Tab',
    Backspace: 'Backspace',
    Delete: 'Delete',
    ArrowUp: 'ArrowUp',
    ArrowDown: 'ArrowDown',
    ArrowLeft: 'ArrowLeft',
    ArrowRight: 'ArrowRight',
    Home: 'Home',
    End: 'End',
    PageUp: 'PageUp',
    PageDown: 'PageDown',
  };
  const key = keyMap[event.key] ?? (event.key.length === 1 ? event.key.toUpperCase() : event.key);
  if (!key) return null;
  return modifiers.length > 0 ? `${modifiers.join('+')}+${key}` : key;
}

function isTransientWebRTCError(err: unknown): boolean {
  const message = (err instanceof Error ? err.message : String(err || '')).toLowerCase();
  return (
    message.includes('aborted without reason')
    || message.includes('signal is aborted')
    || message.includes('aborterror')
    || message.includes('canceled')
    || message.includes('broken pipe')
    || message.includes('epipe')
    || message.includes('pipe closed')
    || message.includes('connection closed')
    || message.includes('browser has been closed')
    || message.includes('target page, context or browser has been closed')
  );
}

function isRequestCanceledError(err: unknown): boolean {
  const message = (err instanceof Error ? err.message : String(err || '')).toLowerCase();
  return message.includes('request canceled') || message.includes('signal is aborted') || message.includes('aborterror');
}

function decodeWebRTCFrame(blob: Blob, signal: AbortSignal): Promise<{ objectUrl: string; width: number; height: number }> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new Error('signal is aborted'));
      return;
    }

    const objectUrl = URL.createObjectURL(blob);
    let settled = false;

    const cleanup = () => {
      if (settled) {
        return;
      }
      settled = true;
      signal.removeEventListener('abort', handleAbort);
    };

    const fail = (err: Error) => {
      cleanup();
      URL.revokeObjectURL(objectUrl);
      reject(err);
    };

    const handleAbort = () => {
      fail(new Error('signal is aborted'));
    };

    const image = new Image();
    image.onload = () => {
      if (settled) {
        URL.revokeObjectURL(objectUrl);
        return;
      }
      cleanup();
      resolve({
        objectUrl,
        width: image.naturalWidth || 0,
        height: image.naturalHeight || 0,
      });
    };
    image.onerror = () => {
      fail(new Error('Failed to decode WebRTC browser frame'));
    };
    signal.addEventListener('abort', handleAbort, { once: true });
    image.src = objectUrl;
  });
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

function IconUpload() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M8 10.75V2.5" />
      <path d="M4.75 5.75 8 2.5l3.25 3.25" />
      <path d="M2.5 10.75v1.75A1 1 0 0 0 3.5 13.5h9a1 1 0 0 0 1-1v-1.75" />
    </svg>
  );
}

function IconDebug() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5.25 5.75h5.5v4.5h-5.5z" />
      <path d="M6.5 2.5h3M8 2.5v1.25M3.5 6.25h1.75M10.75 6.25h1.75M4.5 12.25l1-1.5M11.5 12.25l-1-1.5" />
      <path d="M6.75 8h.01M9.25 8h.01" />
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
  const browserUploadInputRef = useRef<HTMLInputElement | null>(null);
  const [tabs, setTabs] = useState<BrowserTab[]>([]);
  const [address, setAddress] = useState(DEFAULT_URL);
  const [loading, setLoading] = useState(false);
  const [navigationBusy, setNavigationBusy] = useState(false);
  const [uploadBusy, setUploadBusy] = useState(false);
  const [initializing, setInitializing] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [automation, setAutomation] = useState<BrowserAutomationStatus | null>(null);
  const [browserMCPDebugEnabled, setBrowserMCPDebugEnabled] = useState(false);
  const [browserMCPEntries, setBrowserMCPEntries] = useState<BrowserMCPDebugEntry[]>([]);
  const [showBrowserMCPDebugPanel, setShowBrowserMCPDebugPanel] = useState(false);
  const [reloadNonce, setReloadNonce] = useState(0);
  const tabsRef = useRef<BrowserTab[]>([]);
  const creatingDefaultProjectTabRef = useRef<Set<string>>(new Set());
  const [viewportMode, setViewportMode] = useState<ViewportMode>('responsive');
  const [customWidth, setCustomWidth] = useState(1280);
  const [customHeight, setCustomHeight] = useState(720);
  const [stageSize, setStageSize] = useState({ width: 0, height: 0 });
  const [autoContentSize, setAutoContentSize] = useState({ width: 0, height: 0 });
  void autoContentSize;
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showMiniPanel, setShowMiniPanel] = useState(false);
  const [selectionCaptureBusy, setSelectionCaptureBusy] = useState(false);
  const [createMenuOpen, setCreateMenuOpen] = useState(false);
  const [webrtcImageSrc, setWebrtcImageSrc] = useState<string | null>(null);
  const [webrtcFrameLoading, setWebrtcFrameLoading] = useState(false);
  const [webrtcImageNaturalSize, setWebrtcImageNaturalSize] = useState({ width: 0, height: 0 });
  const [remoteHoverSelection, setRemoteHoverSelection] = useState<BrowserElementSelection | null>(null);
  const [remoteTooltip, setRemoteTooltip] = useState<RemoteTooltip | null>(null);
  const [remoteSelectedCandidate, setRemoteSelectedCandidate] = useState<BrowserElementSelection | null>(null);
  const addressEditingRef = useRef(false);
  const createMenuRef = useRef<HTMLDivElement | null>(null);
  const webrtcSurfaceRef = useRef<HTMLDivElement | null>(null);
  const webrtcCanvasRef = useRef<HTMLCanvasElement | null>(null);
  const webrtcObjectUrlRef = useRef<string | null>(null);
  const webrtcRefreshInFlightRef = useRef(false);
  const webrtcRefreshPendingRef = useRef(false);
  const webrtcRefreshNonceRef = useRef(0);
  const webrtcRefreshAbortRef = useRef<AbortController | null>(null);
  const browserNavigateAbortRef = useRef<AbortController | null>(null);
  const activeTabIdRef = useRef<string | null>(null);
  const webrtcImageSrcRef = useRef<string | null>(null);
  const remoteInspectSeqRef = useRef(0);
  const remoteInspectBusyRef = useRef(false);
  const remotePendingPointRef = useRef<{ x: number; y: number } | null>(null);
  const webrtcPointerDownRef = useRef(false);
  const webrtcPointerIdRef = useRef<number | null>(null);
  const webrtcDragMovePointRef = useRef<{ x: number; y: number } | null>(null);
  const webrtcDragMoveInFlightRef = useRef(false);
  const wheelDeltaRef = useRef({ x: 0, y: 0 });
  const wheelFlushTimerRef = useRef<number | null>(null);
  const interactionSeqRef = useRef(0);
  const browserSelection = useChatStore((s) => s.browserSelection);
  const browserSelectionMode = useChatStore((s) => s.browserSelectionMode);
  const browserSelectionCapture = useChatStore((s) => s.browserSelectionCapture);
  const setBrowserSelectionMode = useChatStore((s) => s.setBrowserSelectionMode);
  const setBrowserSelectionCapture = useChatStore((s) => s.setBrowserSelectionCapture);
  const setBrowserSelection = useChatStore((s) => s.setBrowserSelection);
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);
  const setBrowserEnabled = useChatStore((s) => s.setBrowserEnabled);
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const activeProject = useWorkspaceStore((s) => s.projects.find((p) => p.id === s.activeProjectId) ?? null);
  const addBrowserTab = useWorkspaceStore((s) => s.addBrowserTab);
  const removeBrowserTab = useWorkspaceStore((s) => s.removeBrowserTab);
  const setActiveBrowserTab = useWorkspaceStore((s) => s.setActiveBrowserTab);
  const reconcileBrowserTabs = useWorkspaceStore((s) => s.reconcileBrowserTabs);

  const projectTabs = useMemo(() => {
    const tabIds = new Set(activeProject?.browserTabIds ?? []);
    return tabs.filter((tab) => tabIds.has(tab.id));
  }, [activeProject?.browserTabIds, tabs]);
  const activeTab = useMemo(
    () => projectTabs.find((tab) => tab.id === activeProject?.activeBrowserTabId) ?? projectTabs[0] ?? null,
    [activeProject?.activeBrowserTabId, projectTabs],
  );
  const activeTabId = activeTab?.id ?? null;
  const activeTabIsWebRTC = activeTab?.transport === 'webrtc';

  // WebRTC visual streaming: when connected, live JPEG tiles are drawn to
  // the canvas via DataChannel (ADR-0001). Falls back to HTTP screenshot
  // polling when not connected or connecting.
  const visualStream = useVisualStream(
    activeTabIsWebRTC ? activeTabId : null,
    webrtcCanvasRef,
  );
  const webrtcStreamConnected = visualStream.connected;

  const resolveBrowserProjectId = useCallback((tabId?: string | null) => {
    const store = useWorkspaceStore.getState();
    if (tabId) {
      const owner = store.projects.find((project) => (project.browserTabIds ?? []).includes(tabId));
      if (owner) {
        return owner.id;
      }
    }
    return store.activeProjectId ?? activeProjectId ?? null;
  }, [activeProjectId]);

  const refreshBrowserState = useCallback(async () => {
    const state = await getBrowserState();
    setTabs(state.tabs);
    setAutomation(state.automation);
    reconcileBrowserTabs(state.tabs.map((tab) => tab.id));
  }, [reconcileBrowserTabs]);

  const refreshBrowserMCPDebugLog = useCallback(async () => {
    const state = await getBrowserMCPDebugLog(80);
    setBrowserMCPDebugEnabled(state.enabled);
    setBrowserMCPEntries(state.entries);
  }, []);

  const replaceWebRTCImage = useCallback((nextUrl: string | null) => {
    setWebrtcImageSrc(() => {
      const previous = webrtcObjectUrlRef.current;
      webrtcObjectUrlRef.current = nextUrl;
      webrtcImageSrcRef.current = nextUrl;
      if (previous && previous !== nextUrl) {
        URL.revokeObjectURL(previous);
      }
      return nextUrl;
    });
  }, []);

  const clearWebRTCImage = useCallback(() => {
    replaceWebRTCImage(null);
  }, [replaceWebRTCImage]);

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

  const handleClearSelection = useCallback(() => {
    clearSelection();
    setRemoteHoverSelection(null);
    setRemoteTooltip(null);
    setRemoteSelectedCandidate(null);
  }, [clearSelection]);

  const selectionKey = useMemo(() => {
    if (!browserSelection) return null;
    return `${browserSelection.tabId ?? activeTab?.id ?? ''}:${browserSelection.uniqueSelector ?? browserSelection.selector}:${browserSelection.url}`;
  }, [activeTab?.id, browserSelection]);
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

  // Touch gesture mapping for mobile/tablet: long press→right-click,
  // two-finger tap→right-click, two-finger drag→scroll, pinch→zoom.
  const gestures = useGestures({
    sendInput: visualStream.sendInput,
    viewportWidth: viewport.width,
    viewportHeight: viewport.height,
  });

  const viewportScale = useMemo(() => {
    if (stageSize.width === 0 || stageSize.height === 0) {
      return 1;
    }
    if (viewportMode === 'responsive') {
      return 1;
    }
    const availableWidth = Math.max(1, stageSize.width - FIXED_VIEWPORT_STAGE_X_PADDING);
    return Math.min(availableWidth / viewport.width, 1);
  }, [stageSize.height, stageSize.width, viewport.width, viewportMode]);
  const scaledViewportStyle = useMemo(() => {
    if (viewportMode === 'responsive') {
      if (stageSize.width === 0 || stageSize.height === 0) {
        return undefined;
      }
      return {
        width: `${Math.max(320, Math.round(stageSize.width))}px`,
        height: `${Math.max(1, stageSize.height)}px`,
      };
    }
    return {
      width: `${Math.max(1, Math.round(viewport.width * viewportScale))}px`,
      height: `${Math.max(1, Math.round(viewport.height * viewportScale))}px`,
    };
  }, [stageSize.height, stageSize.width, viewport.height, viewport.width, viewportMode, viewportScale]);
  const frameStyle = useMemo(() => {
    if (viewportMode === 'responsive') {
      if (stageSize.width === 0 || stageSize.height === 0) {
        return undefined;
      }
      return {
        width: `${Math.max(320, Math.round(stageSize.width))}px`,
        height: `${Math.max(320, Math.round(stageSize.height))}px`,
      };
    }
    return {
      width: `${viewport.width}px`,
      height: `${viewport.height}px`,
      transform: `scale(${viewportScale})`,
    };
  }, [stageSize.height, stageSize.width, viewport.height, viewport.width, viewportMode, viewportScale]);
  const remoteViewport = useMemo(() => {
    if (viewportMode === 'responsive') {
      return {
        width: Math.max(320, Math.round(stageSize.width || 320)),
        height: Math.max(320, Math.round(stageSize.height || 320)),
      };
    }
    return { width: viewport.width, height: viewport.height };
  }, [stageSize.height, stageSize.width, viewport.height, viewport.width, viewportMode]);
  const inspectViewport = useMemo(() => {
    if (webrtcImageNaturalSize.width > 0 && webrtcImageNaturalSize.height > 0) {
      return {
        width: webrtcImageNaturalSize.width,
        height: webrtcImageNaturalSize.height,
      };
    }
    return remoteViewport;
  }, [remoteViewport, webrtcImageNaturalSize.height, webrtcImageNaturalSize.width]);
  const remoteOverlayScale = useMemo(() => {
    const width = Math.max(1, inspectViewport.width);
    const height = Math.max(1, inspectViewport.height);
    return {
      x: remoteViewport.width / width,
      y: remoteViewport.height / height,
    };
  }, [inspectViewport.height, inspectViewport.width, remoteViewport.height, remoteViewport.width]);

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
    activeTabIdRef.current = activeTabId;
  }, [activeTabId]);

  useEffect(() => {
    return () => {
      if (wheelFlushTimerRef.current !== null) {
        window.clearTimeout(wheelFlushTimerRef.current);
        wheelFlushTimerRef.current = null;
      }
      browserNavigateAbortRef.current?.abort();
      browserNavigateAbortRef.current = null;
      webrtcRefreshAbortRef.current?.abort();
      webrtcRefreshAbortRef.current = null;
      if (webrtcObjectUrlRef.current) {
        URL.revokeObjectURL(webrtcObjectUrlRef.current);
        webrtcObjectUrlRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    browserNavigateAbortRef.current?.abort();
    browserNavigateAbortRef.current = null;
    webrtcRefreshAbortRef.current?.abort();
    webrtcRefreshAbortRef.current = null;
    webrtcPointerDownRef.current = false;
    webrtcPointerIdRef.current = null;
    webrtcDragMovePointRef.current = null;
    webrtcDragMoveInFlightRef.current = false;
    setNavigationBusy(false);
    setRemoteHoverSelection(null);
    setRemoteTooltip(null);
    setRemoteSelectedCandidate(null);
    webrtcRefreshPendingRef.current = false;
    webrtcRefreshInFlightRef.current = false;
    setWebrtcFrameLoading(false);
    setWebrtcImageNaturalSize({ width: 0, height: 0 });
    clearWebRTCImage();
  }, [activeTabId]);

  useEffect(() => {
    if (!createMenuOpen) return;
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (createMenuRef.current?.contains(target)) return;
      setCreateMenuOpen(false);
    };
    document.addEventListener('mousedown', handlePointerDown);
    return () => document.removeEventListener('mousedown', handlePointerDown);
  }, [createMenuOpen]);

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

  const requestWebRTCFrameRefresh = useCallback(() => {
    if (!activeTabIdRef.current || !activeTabIsWebRTC) {
      return;
    }
    if (webrtcRefreshInFlightRef.current) {
      webrtcRefreshPendingRef.current = true;
      return;
    }

    const tabId = activeTabIdRef.current;
    const requestNonce = ++webrtcRefreshNonceRef.current;
    const controller = new AbortController();
    let timedOut = false;
    webrtcRefreshAbortRef.current?.abort();
    webrtcRefreshAbortRef.current = controller;
    webrtcRefreshInFlightRef.current = true;
    webrtcRefreshPendingRef.current = false;
    if (!webrtcImageSrcRef.current) {
      setWebrtcFrameLoading(true);
    }

    const timeoutId = window.setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, WEBRTC_FRAME_REFRESH_TIMEOUT_MS);

    void fetch(browserTabScreenshotUrl(tabId, requestNonce), {
      credentials: 'include',
      cache: 'no-store',
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) {
          const message = await response.text();
          throw new Error(message || 'Failed to load WebRTC browser frame');
        }
        return response.blob();
      })
      .then((blob) => decodeWebRTCFrame(blob, controller.signal))
      .then(({ objectUrl, width, height }) => {
        if (requestNonce !== webrtcRefreshNonceRef.current || activeTabIdRef.current !== tabId) {
          URL.revokeObjectURL(objectUrl);
          return;
        }
        if (width > 0 && height > 0) {
          setWebrtcImageNaturalSize({ width, height });
        }
        replaceWebRTCImage(objectUrl);
        setError(null);
      })
      .catch((err) => {
        if (requestNonce !== webrtcRefreshNonceRef.current || activeTabIdRef.current !== tabId) {
          return;
        }
        if (timedOut) {
          if (!webrtcImageSrcRef.current) {
            setError('WebRTC browser frame timed out');
          }
          return;
        }
        if (isTransientWebRTCError(err)) {
          return;
        }
        if (!webrtcImageSrcRef.current) {
          setError(err instanceof Error ? err.message : 'Failed to load WebRTC browser frame');
        }
      })
      .finally(() => {
        window.clearTimeout(timeoutId);
        const isCurrentRequest = (
          webrtcRefreshAbortRef.current === controller
          && requestNonce === webrtcRefreshNonceRef.current
          && activeTabIdRef.current === tabId
        );
        if (webrtcRefreshAbortRef.current === controller) {
          webrtcRefreshAbortRef.current = null;
        }
        if (isCurrentRequest) {
          setWebrtcFrameLoading(false);
        }
        if (isCurrentRequest) {
          webrtcRefreshInFlightRef.current = false;
        }
        if (isCurrentRequest && webrtcRefreshPendingRef.current) {
          webrtcRefreshPendingRef.current = false;
          requestWebRTCFrameRefresh();
        }
      });
  }, [activeTabIsWebRTC, replaceWebRTCImage]);

  useEffect(() => {
    if (!activeTabIsWebRTC || !activeTabId) {
      return;
    }
    // When WebRTC DataChannel streaming is connected, live frames arrive via
    // the canvas — skip the HTTP screenshot polling to save bandwidth.
    // refreshBrowserState still runs to sync navigation/file-chooser state.
    if (!webrtcStreamConnected) {
      requestWebRTCFrameRefresh();
    }
    const interval = window.setInterval(() => {
      void refreshBrowserState().catch(() => {});
      if (!webrtcStreamConnected) {
        requestWebRTCFrameRefresh();
      }
    }, 1500);
    return () => window.clearInterval(interval);
  }, [activeTabId, activeTabIsWebRTC, refreshBrowserState, reloadNonce, requestWebRTCFrameRefresh, webrtcStreamConnected]);

  useEffect(() => {
    if (!activeTabIsWebRTC || !activeTabId) {
      setRemoteHoverSelection(null);
      setRemoteTooltip(null);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void setBrowserTabViewport(activeTabId, remoteViewport.width, remoteViewport.height)
        .then(() => {
          if (!cancelled) {
            requestWebRTCFrameRefresh();
          }
        })
        .catch((err) => {
          if (!cancelled) {
            setError(err instanceof Error ? err.message : 'Failed to sync WebRTC viewport');
          }
        });
    }, 120);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [activeTabId, activeTabIsWebRTC, remoteViewport.height, remoteViewport.width, requestWebRTCFrameRefresh]);

  useEffect(() => {
    function handleFullscreenChange() {
      setIsFullscreen(document.fullscreenElement === panelRef.current);
    }

    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange);
  }, []);

  useEffect(() => {
    let cancelled = false;
    async function ensureCapture() {
      if (!browserSelection || browserSelectionMode !== 'screenshot' || !selectionKey) return;
      if (browserSelectionCapture?.selectorKey === selectionKey) return;

      setSelectionCaptureBusy(true);
      try {
        const response = await captureBrowserElementScreenshot({
          url: browserSelection.url,
          tabId: activeTabIsWebRTC ? (browserSelection.tabId ?? activeTab?.id) : undefined,
          selectors: [browserSelection.uniqueSelector ?? '', browserSelection.selector].filter(Boolean),
          name: browserSelection.tagName.toLowerCase(),
        });
        if (cancelled) return;
        setBrowserSelectionCapture({
          path: response.path,
          dataUrl: response.dataUrl,
          mimeType: response.mimeType,
          name: `${browserSelection.tagName.toLowerCase()}-selection.png`,
          selectorKey: selectionKey,
          capturedAt: Date.now(),
        });
        setError(null);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to capture selected element');
        }
      } finally {
        if (!cancelled) {
          setSelectionCaptureBusy(false);
        }
      }
    }

    void ensureCapture();
    return () => { cancelled = true; };
  }, [activeTab?.id, activeTabIsWebRTC, browserSelection, browserSelectionCapture?.selectorKey, browserSelectionMode, selectionKey, setBrowserSelectionCapture]);

  useEffect(() => {
    let alive = true;
    refreshBrowserState()
      .then(() => {
        if (!alive) return;
        return null;
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
  }, [refreshBrowserState]);

  useEffect(() => {
    let alive = true;
    refreshBrowserMCPDebugLog()
      .catch(() => {
        if (!alive) return;
        setBrowserMCPDebugEnabled(false);
        setBrowserMCPEntries([]);
      });
    return () => {
      alive = false;
    };
  }, [refreshBrowserMCPDebugLog]);

  useEffect(() => {
    if (!browserMCPDebugEnabled) {
      return;
    }
    const interval = window.setInterval(() => {
      void refreshBrowserMCPDebugLog().catch(() => {});
    }, 1500);
    return () => window.clearInterval(interval);
  }, [browserMCPDebugEnabled, refreshBrowserMCPDebugLog]);

  useEffect(() => {
    if (initializing || !activeProjectId) return;
    if (projectTabs.length > 0) {
      if (!activeProject?.activeBrowserTabId || !projectTabs.some((tab) => tab.id === activeProject.activeBrowserTabId)) {
        setActiveBrowserTab(activeProjectId, projectTabs[0].id);
      }
      return;
    }
    if (creatingDefaultProjectTabRef.current.has(activeProjectId)) return;
    creatingDefaultProjectTabRef.current.add(activeProjectId);
    createBrowserTab(DEFAULT_URL, 'webrtc')
      .then((tab) => {
        setTabs((current) => [...current, tab]);
        addBrowserTab(activeProjectId, tab.id);
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => {
        creatingDefaultProjectTabRef.current.delete(activeProjectId);
      });
  }, [activeProject?.activeBrowserTabId, activeProjectId, addBrowserTab, initializing, projectTabs, setActiveBrowserTab]);

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
          const tab = await createBrowserTab(data.url, 'proxy');
          setTabs((current) => (current.some((item) => item.id === tab.id) ? current : [...current, tab]));
          const runtimeProjectId = resolveBrowserProjectId(data.tabId);
          if (runtimeProjectId) {
            addBrowserTab(runtimeProjectId, tab.id);
            setActiveBrowserTab(runtimeProjectId, tab.id);
          }
          await activateBrowserTab(tab.id);
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
        const projectId = resolveBrowserProjectId(data.tabId);
        const nextTabs = tabsRef.current.filter((tab) => tab.id !== data.tabId);
        setTabs(nextTabs);
        if (projectId) {
          removeBrowserTab(projectId, data.tabId);
        }
        try {
          await deleteBrowserTab(data.tabId);
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Failed to close browser tab');
        }
        return;
      }

      if (data.type === 'focus-tab') {
        const projectId = resolveBrowserProjectId(data.tabId);
        if (projectId) {
          setActiveBrowserTab(projectId, data.tabId);
        }
        try {
          await activateBrowserTab(data.tabId);
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Failed to activate browser tab');
        }
        return;
      }

      if (data.type === 'location-change' && data.url) {
        const title = data.title ?? '';
        const canGoBack = Boolean(data.canGoBack);
        const canGoForward = Boolean(data.canGoForward);
        setNavigationBusy(false);
        setTabs((current) => current.map((tab) => (
          tab.id === data.tabId
            ? { ...tab, url: data.url!, title: title || tab.title, canGoBack, canGoForward }
            : tab
        )));
        void syncBrowserTabState(data.tabId, {
          url: data.url,
          title,
          canGoBack,
          canGoForward,
        }).catch(() => {});
      }
    }

    window.addEventListener('message', handleProxyMessage);
    return () => {
      window.removeEventListener('message', handleProxyMessage);
    };
  }, [activeTabId, addBrowserTab, activeProjectId, removeBrowserTab, resolveBrowserProjectId, setActiveBrowserTab]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!address.trim()) return;
    const navigateController = new AbortController();
    browserNavigateAbortRef.current?.abort();
    browserNavigateAbortRef.current = navigateController;
    setLoading(true);
    setNavigationBusy(Boolean(activeTab));
    setError(null);
    try {
      const tab = activeTab
        ? await navigateBrowserTab(activeTab.id, address, navigateController.signal)
        : await createBrowserTab(address, 'webrtc');
      if (browserNavigateAbortRef.current === navigateController) {
        browserNavigateAbortRef.current = null;
      }
      setTabs((current) => {
        const exists = current.some((item) => item.id === tab.id);
        return exists ? current.map((item) => (item.id === tab.id ? tab : item)) : [...current, tab];
      });
      if (activeProjectId) addBrowserTab(activeProjectId, tab.id);
      setReloadNonce((value) => value + 1);
      if (!activeTab || activeTab.transport === 'webrtc') {
        setNavigationBusy(false);
      }
    } catch (err) {
      if (browserNavigateAbortRef.current === navigateController) {
        browserNavigateAbortRef.current = null;
      }
      if (activeTab) {
        addressEditingRef.current = false;
        setAddress(activeTab.url);
      }
      setNavigationBusy(false);
      if (!isRequestCanceledError(err)) {
        const message = err instanceof Error ? err.message : 'Failed to navigate';
        setError(message === 'Request timeout' ? 'Browser navigation timed out' : message);
      }
    } finally {
      setLoading(false);
    }
  }

  async function handleNewTab(transport: BrowserTransport = 'webrtc') {
    setLoading(true);
    setError(null);
    setCreateMenuOpen(false);
    try {
      const tab = await createBrowserTab(DEFAULT_URL, transport);
      setTabs((current) => (current.some((item) => item.id === tab.id) ? current : [...current, tab]));
      const runtimeProjectId = useWorkspaceStore.getState().activeProjectId ?? activeProjectId;
      if (runtimeProjectId) {
        addBrowserTab(runtimeProjectId, tab.id);
        setActiveBrowserTab(runtimeProjectId, tab.id);
      }
      await activateBrowserTab(tab.id);
      setAddress(tab.url);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create tab');
    } finally {
      setLoading(false);
    }
  }

  async function handleCloseTab(tabId: string, projectId = resolveBrowserProjectId(tabId)) {
    const nextTabs = tabsRef.current.filter((tab) => tab.id !== tabId);
    setTabs(nextTabs);
    if (projectId) removeBrowserTab(projectId, tabId);
    try {
      await deleteBrowserTab(tabId);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to close tab');
    }
  }

  const handleStopLoading = useCallback(async () => {
    browserNavigateAbortRef.current?.abort();
    browserNavigateAbortRef.current = null;
    setLoading(false);
    setNavigationBusy(false);
    if (!activeTab) {
      return;
    }
    if (activeTab.transport === 'webrtc') {
      void stopBrowserTab(activeTab.id)
        .then(() => {
          requestWebRTCFrameRefresh();
          void refreshBrowserState().catch(() => {});
        })
        .catch(() => {});
      return;
    }
    const frameWindow = iframeRef.current?.contentWindow;
    frameWindow?.stop?.();
  }, [activeTab, refreshBrowserState, requestWebRTCFrameRefresh]);

  function handleReload() {
    if (navigationBusy) {
      void handleStopLoading();
      return;
    }
    if (!activeTab) return;
    setAutoContentSize({ width: 0, height: 0 });
    setError(null);
    setNavigationBusy(true);
    if (activeTab.transport === 'webrtc') {
      const reloadController = new AbortController();
      browserNavigateAbortRef.current?.abort();
      browserNavigateAbortRef.current = reloadController;
      void reloadBrowserTab(activeTab.id, reloadController.signal)
        .then((tab) => {
          if (browserNavigateAbortRef.current === reloadController) {
            browserNavigateAbortRef.current = null;
          }
          setTabs((current) => current.map((entry) => (entry.id === tab.id ? tab : entry)));
          requestWebRTCFrameRefresh();
        })
        .catch((err) => {
          if (browserNavigateAbortRef.current === reloadController) {
            browserNavigateAbortRef.current = null;
          }
          if (!isRequestCanceledError(err)) {
            const message = err instanceof Error ? err.message : 'Failed to reload browser';
            setError(message === 'Request timeout' ? 'Browser reload timed out' : message);
          }
        })
        .finally(() => {
          setNavigationBusy(false);
        });
      return;
    }
    setReloadNonce((value) => value + 1);
  }

  async function handleBack() {
    if (!activeTab) return;
    if (navigationBusy) {
      return;
    }
    setError(null);
    setNavigationBusy(true);
    try {
      if (activeTab.transport === 'webrtc') {
        const tab = await goBackBrowserTab(activeTab.id);
        setTabs((current) => current.map((entry) => (entry.id === tab.id ? tab : entry)));
        setNavigationBusy(false);
        return;
      }
      const frameWindow = iframeRef.current?.contentWindow;
      frameWindow?.history.back();
    } catch (err) {
      setNavigationBusy(false);
      setError(err instanceof Error ? err.message : 'Failed to go back');
    }
  }

  async function handleForward() {
    if (!activeTab) return;
    if (navigationBusy) {
      return;
    }
    setError(null);
    setNavigationBusy(true);
    try {
      if (activeTab.transport === 'webrtc') {
        const tab = await goForwardBrowserTab(activeTab.id);
        setTabs((current) => current.map((entry) => (entry.id === tab.id ? tab : entry)));
        setNavigationBusy(false);
        return;
      }
      const frameWindow = iframeRef.current?.contentWindow;
      frameWindow?.history.forward();
    } catch (err) {
      setNavigationBusy(false);
      setError(err instanceof Error ? err.message : 'Failed to go forward');
    }
  }

  useEffect(() => {
    if (!inspectMode) {
      setRemoteHoverSelection(null);
      setRemoteTooltip(null);
      setRemoteSelectedCandidate(null);
    }
  }, [inspectMode]);

  const clientPointToViewport = useCallback((clientX: number, clientY: number) => {
    const surface = webrtcSurfaceRef.current;
    if (!surface) return null;
    const rect = surface.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return null;
    const relX = clientX - rect.left;
    const relY = clientY - rect.top;
    if (relX < 0 || relY < 0 || relX > rect.width || relY > rect.height) return null;
    return {
      x: (relX / rect.width) * inspectViewport.width,
      y: (relY / rect.height) * inspectViewport.height,
    };
  }, [inspectViewport.height, inspectViewport.width]);

  const bumpWebRTCFrame = useCallback((delays: number[] = [0, 120, 320]) => {
    const seq = ++interactionSeqRef.current;
    delays.forEach((delay) => {
      window.setTimeout(() => {
        if (seq === interactionSeqRef.current) {
          requestWebRTCFrameRefresh();
        }
      }, delay);
    });
  }, [requestWebRTCFrameRefresh]);

  const handleUploadFiles = useCallback(async (files: Iterable<File>) => {
    if (!activeTab || activeTab.transport !== 'webrtc') {
      return;
    }
    setUploadBusy(true);
    setError(null);
    try {
      const tab = await uploadBrowserTabFiles(activeTab.id, files);
      setTabs((current) => current.map((entry) => (entry.id === tab.id ? tab : entry)));
      bumpWebRTCFrame([0, 120, 280, 520]);
      void refreshBrowserState().catch(() => {});
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to upload files to WebRTC browser');
    } finally {
      setUploadBusy(false);
      if (browserUploadInputRef.current) {
        browserUploadInputRef.current.value = '';
      }
    }
  }, [activeTab, bumpWebRTCFrame, refreshBrowserState]);

  const handleUploadButtonClick = useCallback(() => {
    if (!activeTab || activeTab.transport !== 'webrtc' || uploadBusy) {
      return;
    }
    browserUploadInputRef.current?.click();
  }, [activeTab, uploadBusy]);

  const handleUploadInputChange = useCallback((event: React.ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files;
    if (!files || files.length === 0) {
      return;
    }
    void handleUploadFiles(files);
  }, [handleUploadFiles]);

  const runRemoteInspect = useCallback(async (point: { x: number; y: number }, commitSelection: boolean) => {
    if (!activeTabId) return;
    remoteInspectBusyRef.current = true;
    const seq = ++remoteInspectSeqRef.current;
    try {
      const selection = await inspectBrowserTabPoint(activeTabId, point.x, point.y);
      if (seq !== remoteInspectSeqRef.current) {
        return;
      }
      setRemoteHoverSelection(selection);
      setRemoteTooltip(buildRemoteTooltip(selection, remoteOverlayScale.x, remoteOverlayScale.y));
      setRemoteSelectedCandidate(selection);
      if (commitSelection) {
        setBrowserSelection(selection);
        if (!useActiveBrowser) {
          void setBrowserEnabled(true);
        }
        if (inspectMode) {
          toggleInspectMode();
        }
      }
    } catch (err) {
      if (commitSelection) {
        setError(err instanceof Error ? err.message : 'Failed to inspect browser element');
      }
    } finally {
      remoteInspectBusyRef.current = false;
      const pending = remotePendingPointRef.current;
      remotePendingPointRef.current = null;
      if (pending && !commitSelection) {
        void runRemoteInspect(pending, false);
      }
    }
  }, [activeTabId, inspectMode, remoteOverlayScale.x, remoteOverlayScale.y, setBrowserSelection, setBrowserEnabled, toggleInspectMode, useActiveBrowser]);

  const queueRemoteInspect = useCallback((point: { x: number; y: number }, commitSelection = false) => {
    if (commitSelection) {
      remotePendingPointRef.current = null;
      void runRemoteInspect(point, true);
      return;
    }
    if (remoteInspectBusyRef.current) {
      remotePendingPointRef.current = point;
      return;
    }
    void runRemoteInspect(point, false);
  }, [runRemoteInspect]);

  useEffect(() => {
    if (!inspectMode || !activeTabIsWebRTC) {
      return;
    }
    webrtcSurfaceRef.current?.focus();
    if (remoteHoverSelection || remoteSelectedCandidate) {
      return;
    }
    queueRemoteInspect({
      x: inspectViewport.width / 2,
      y: inspectViewport.height / 2,
    }, false);
  }, [activeTabIsWebRTC, inspectMode, inspectViewport.height, inspectViewport.width, queueRemoteInspect, remoteHoverSelection, remoteSelectedCandidate]);

  const flushWebRTCDragMove = useCallback(() => {
    if (!activeTabId || !webrtcPointerDownRef.current) {
      webrtcDragMovePointRef.current = null;
      return;
    }
    const point = webrtcDragMovePointRef.current;
    if (!point) {
      return;
    }
    webrtcDragMovePointRef.current = null;
    webrtcDragMoveInFlightRef.current = true;
    // Prefer DataChannel input when WebRTC stream is connected.
    if (webrtcStreamConnected) {
      visualStream.sendInput({ type: 'mouse_move', x: point.x, y: point.y });
      webrtcDragMoveInFlightRef.current = false;
      if (webrtcPointerDownRef.current && webrtcDragMovePointRef.current) {
        flushWebRTCDragMove();
      }
      return;
    }
    void mouseMoveBrowserTab(activeTabId, point.x, point.y)
      .then(() => {
        requestWebRTCFrameRefresh();
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Failed to move in WebRTC browser');
      })
      .finally(() => {
        webrtcDragMoveInFlightRef.current = false;
        if (webrtcPointerDownRef.current && webrtcDragMovePointRef.current) {
          flushWebRTCDragMove();
        }
      });
  }, [activeTabId, requestWebRTCFrameRefresh, visualStream, webrtcStreamConnected]);

  const handleWebRTCPointerMove = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const point = clientPointToViewport(event.clientX, event.clientY);
    if (inspectMode) {
      if (!point) {
        setRemoteHoverSelection(null);
        setRemoteTooltip(null);
        return;
      }
      queueRemoteInspect(point, false);
      return;
    }
    if (!webrtcPointerDownRef.current || !point || !activeTabId) {
      return;
    }
    webrtcDragMovePointRef.current = point;
    if (!webrtcDragMoveInFlightRef.current) {
      flushWebRTCDragMove();
    }
  }, [activeTabId, clientPointToViewport, flushWebRTCDragMove, inspectMode, queueRemoteInspect]);

  const handleWebRTCPointerDown = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (inspectMode || !activeTabId || event.button !== 0) {
      return;
    }
    const point = clientPointToViewport(event.clientX, event.clientY);
    if (!point) {
      return;
    }
    webrtcSurfaceRef.current?.focus();
    webrtcPointerDownRef.current = true;
    webrtcPointerIdRef.current = event.pointerId;
    event.currentTarget.setPointerCapture(event.pointerId);
    event.preventDefault();
    event.stopPropagation();
    // Prefer DataChannel input when WebRTC stream is connected.
    if (webrtcStreamConnected) {
      visualStream.sendInput({ type: 'mouse_down', x: point.x, y: point.y, button: 0 });
      return;
    }
    void mouseDownBrowserTab(activeTabId, point.x, point.y)
      .then(() => {
        requestWebRTCFrameRefresh();
      })
      .catch((err) => {
        webrtcPointerDownRef.current = false;
        webrtcPointerIdRef.current = null;
        setError(err instanceof Error ? err.message : 'Failed to press mouse in WebRTC browser');
      });
  }, [activeTabId, clientPointToViewport, inspectMode, requestWebRTCFrameRefresh, visualStream, webrtcStreamConnected]);

  const handleWebRTCClick = useCallback((event: React.MouseEvent<HTMLDivElement>) => {
    const point = clientPointToViewport(event.clientX, event.clientY);
    if (!point) return;
    if (!inspectMode) return;
    event.preventDefault();
    event.stopPropagation();
    queueRemoteInspect(point, true);
  }, [clientPointToViewport, inspectMode, queueRemoteInspect]);

  const handleWebRTCPointerUp = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (inspectMode || !activeTabId || !webrtcPointerDownRef.current) {
      return;
    }
    const point = clientPointToViewport(event.clientX, event.clientY);
    webrtcPointerDownRef.current = false;
    webrtcPointerIdRef.current = null;
    webrtcDragMovePointRef.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    if (!point) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    // Prefer DataChannel input when WebRTC stream is connected.
    if (webrtcStreamConnected) {
      visualStream.sendInput({ type: 'mouse_up', x: point.x, y: point.y, button: 0 });
      return;
    }
    void mouseUpBrowserTab(activeTabId, point.x, point.y)
      .then(() => {
        bumpWebRTCFrame([0, 80, 220]);
        void refreshBrowserState().catch(() => {});
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Failed to release mouse in WebRTC browser');
      });
  }, [activeTabId, bumpWebRTCFrame, clientPointToViewport, inspectMode, refreshBrowserState, visualStream, webrtcStreamConnected]);

  const handleWebRTCPointerCancel = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    const point = clientPointToViewport(event.clientX, event.clientY);
    if (!activeTabId || !webrtcPointerDownRef.current) {
      webrtcPointerDownRef.current = false;
      webrtcPointerIdRef.current = null;
      webrtcDragMovePointRef.current = null;
      return;
    }
    webrtcPointerDownRef.current = false;
    webrtcPointerIdRef.current = null;
    webrtcDragMovePointRef.current = null;
    if (!point) {
      return;
    }
    // Prefer DataChannel input when WebRTC stream is connected.
    if (webrtcStreamConnected) {
      visualStream.sendInput({ type: 'mouse_up', x: point.x, y: point.y, button: 0 });
      return;
    }
    void mouseUpBrowserTab(activeTabId, point.x, point.y).catch(() => {});
  }, [activeTabId, clientPointToViewport, visualStream, webrtcStreamConnected]);

  const runRemoteNavigate = useCallback(async (direction: 'up' | 'down' | 'left' | 'right') => {
    if (!activeTabId) return;
    const tabSelection = browserSelection?.tabId === activeTabId ? browserSelection : null;
    const target = remoteSelectedCandidate ?? remoteHoverSelection ?? tabSelection;
    const selector = target?.uniqueSelector ?? target?.selector;
    if (!selector) return;
    try {
      const selection = await inspectBrowserTabNavigate(activeTabId, selector, direction);
      setRemoteHoverSelection(selection);
      setRemoteSelectedCandidate(selection);
      setRemoteTooltip(buildRemoteTooltip(selection, remoteOverlayScale.x, remoteOverlayScale.y));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to navigate inspected element');
    }
  }, [activeTabId, browserSelection, remoteHoverSelection, remoteOverlayScale.x, remoteOverlayScale.y, remoteSelectedCandidate]);

  const handleWebRTCKeyDown = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    if (!inspectMode) {
      if (!activeTabId) return;
      if (isPrintableKey(event)) {
        event.preventDefault();
        // Prefer DataChannel input when WebRTC stream is connected.
        if (webrtcStreamConnected) {
          visualStream.sendInput({ type: 'text', text: event.key });
          return;
        }
        void typeBrowserTabText(activeTabId, event.key)
          .then(() => {
            bumpWebRTCFrame();
            void refreshBrowserState().catch(() => {});
          })
          .catch((err) => {
            setError(err instanceof Error ? err.message : 'Failed to type in WebRTC browser');
          });
        return;
      }
      const key = toPlaywrightKey(event);
      if (!key) return;
      event.preventDefault();
      // Prefer DataChannel input when WebRTC stream is connected.
      if (webrtcStreamConnected) {
        visualStream.sendInput({ type: 'key_down', key });
        visualStream.sendInput({ type: 'key_up', key });
        return;
      }
      void pressBrowserTabKey(activeTabId, key)
        .then(() => {
          bumpWebRTCFrame();
          void refreshBrowserState().catch(() => {});
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : 'Failed to send key to WebRTC browser');
        });
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      toggleInspectMode();
      return;
    }
    if (event.key === 'Enter') {
      const selection = remoteSelectedCandidate ?? remoteHoverSelection;
      if (!selection) return;
      event.preventDefault();
      setBrowserSelection(selection);
      if (!useActiveBrowser) {
        void setBrowserEnabled(true);
      }
      toggleInspectMode();
      return;
    }
    const directionMap: Record<string, 'up' | 'down' | 'left' | 'right'> = {
      ArrowUp: 'up',
      ArrowDown: 'down',
      ArrowLeft: 'left',
      ArrowRight: 'right',
    };
    const direction = directionMap[event.key];
    if (!direction) return;
    event.preventDefault();
    void runRemoteNavigate(direction);
  }, [activeTabId, bumpWebRTCFrame, inspectMode, remoteHoverSelection, remoteSelectedCandidate, runRemoteNavigate, setBrowserEnabled, setBrowserSelection, toggleInspectMode, useActiveBrowser]);

  const handleWebRTCPaste = useCallback((event: React.ClipboardEvent<HTMLDivElement>) => {
    if (inspectMode || !activeTabId) return;
    const text = event.clipboardData.getData('text/plain');
    const files = Array.from(event.clipboardData.items)
      .filter((item) => item.kind === 'file')
      .map((item) => item.getAsFile())
      .filter((file): file is File => Boolean(file));
    if (files.length === 0 && !text) return;
    event.preventDefault();
    if (files.length > 0) {
      void pasteBrowserTabClipboard(activeTabId, { text, files })
        .then(() => {
          bumpWebRTCFrame([0, 150, 400, 650]);
          void refreshBrowserState().catch(() => {});
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : 'Failed to paste clipboard into WebRTC browser');
        });
      return;
    }
    void typeBrowserTabText(activeTabId, text)
      .then(() => {
        bumpWebRTCFrame([0, 150, 400]);
        void refreshBrowserState().catch(() => {});
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Failed to paste into WebRTC browser');
      });
  }, [activeTabId, bumpWebRTCFrame, inspectMode, refreshBrowserState]);

  const flushWebRTCWheel = useCallback(() => {
    if (!activeTabId) return;
    const { x, y } = wheelDeltaRef.current;
    wheelDeltaRef.current = { x: 0, y: 0 };
    wheelFlushTimerRef.current = null;
    if (x === 0 && y === 0) return;
    // Prefer DataChannel input when WebRTC stream is connected.
    if (webrtcStreamConnected) {
      visualStream.sendInput({ type: 'scroll', deltaX: x, deltaY: y });
      return;
    }
    void scrollBrowserTab(activeTabId, x, y)
      .then(() => {
        bumpWebRTCFrame();
        void refreshBrowserState().catch(() => {});
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Failed to scroll WebRTC browser');
      });
  }, [activeTabId, bumpWebRTCFrame, visualStream, webrtcStreamConnected]);

  const handleWebRTCWheel = useCallback((event: WheelEvent) => {
    if (inspectMode || !activeTabId) return;
    event.preventDefault();
    wheelDeltaRef.current.x += event.deltaX;
    wheelDeltaRef.current.y += event.deltaY;
    if (wheelFlushTimerRef.current !== null) return;
    wheelFlushTimerRef.current = window.setTimeout(flushWebRTCWheel, 40);
  }, [activeTabId, flushWebRTCWheel, inspectMode]);

  useEffect(() => {
    const surface = webrtcSurfaceRef.current;
    if (!surface || !activeTabIsWebRTC) {
      return;
    }
    surface.addEventListener('wheel', handleWebRTCWheel, { passive: false });
    return () => {
      surface.removeEventListener('wheel', handleWebRTCWheel);
    };
  }, [activeTabIsWebRTC, handleWebRTCWheel]);

  async function handleSelectTab(tabId: string, projectId = resolveBrowserProjectId(tabId)) {
    if (projectId) setActiveBrowserTab(projectId, tabId);
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
        <div className="browser-tab-rail">
          {projectTabs.map((tab) => (
            <div key={tab.id} className={`browser-tab-chip${tab.id === activeTab?.id ? ' active' : ''}`}>
            <button className="browser-tab-button" type="button" onClick={() => void handleSelectTab(tab.id)} title={tab.url}>
              <span className="browser-tab-dot" />
              <span className="browser-tab-copy">
                <span className="browser-tab-title">{displayTitle(tab)}</span>
                <span className={`browser-tab-transport browser-tab-transport-${tab.transport}`}>
                  {tab.transport === 'webrtc' ? 'WebRTC' : 'Proxy'}
                </span>
              </span>
            </button>
              <button className="browser-tab-close" type="button" onClick={() => handleCloseTab(tab.id)} aria-label={`Close ${displayTitle(tab)}`}>
                <IconClose />
              </button>
            </div>
          ))}
        </div>
        <div className="browser-tab-actions">
          <div ref={createMenuRef} className={`browser-newtab-wrap${createMenuOpen ? ' open' : ''}`}>
            <button className="browser-icon-btn" type="button" onClick={() => setCreateMenuOpen((value) => !value)} title="New browser tab">
              <IconPlus />
            </button>
            {createMenuOpen && (
              <div className="browser-newtab-menu">
                <button type="button" onClick={() => void handleNewTab('webrtc')}>
                  WebRTC Tab
                </button>
                <button type="button" onClick={() => void handleNewTab('proxy')}>
                  Proxy Tab
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      <input
        ref={browserUploadInputRef}
        className="browser-hidden-upload"
        type="file"
        multiple={activeTab?.fileChooserMultiple ?? true}
        tabIndex={-1}
        onChange={handleUploadInputChange}
      />

      <form className="browser-toolbar" onSubmit={handleSubmit}>
        <button className="browser-icon-btn" type="button" title="Back" onClick={() => void handleBack()} disabled={!activeTab?.canGoBack}>
          <IconChevronLeft />
        </button>
        <button className="browser-icon-btn" type="button" title="Forward" onClick={() => void handleForward()} disabled={!activeTab?.canGoForward}>
          <IconChevronRight />
        </button>
        <button className="browser-icon-btn" type="button" title={navigationBusy ? 'Cancel loading' : 'Reload'} onClick={handleReload} disabled={!activeTab}>
          {navigationBusy ? <IconClose /> : <IconReload />}
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
        <button
          className={`browser-icon-btn${activeTab?.fileChooserPending ? ' active' : ''}`}
          type="button"
          title={activeTab?.transport === 'webrtc'
            ? (activeTab.fileChooserPending ? 'Choose files for the pending upload dialog' : 'Upload files to the active WebRTC page')
            : 'File upload is handled natively in proxy tabs'}
          onClick={handleUploadButtonClick}
          disabled={!activeTab || activeTab.transport !== 'webrtc' || uploadBusy}
        >
          <IconUpload />
        </button>
        {browserMCPDebugEnabled && (
          <button
            className={`browser-icon-btn${showBrowserMCPDebugPanel ? ' active' : ''}`}
            type="button"
            title={showBrowserMCPDebugPanel ? 'Hide browser MCP debug panel' : 'Show browser MCP debug panel'}
            onClick={() => setShowBrowserMCPDebugPanel((value) => !value)}
          >
            <IconDebug />
          </button>
        )}
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
      {browserMCPDebugEnabled && showBrowserMCPDebugPanel && (
        <div className="browser-debug-log" aria-live="polite">
          <div className="browser-debug-log-header">
            <span>Browser MCP Debug</span>
            <span>{browserMCPEntries.length > 0 ? `${browserMCPEntries.length} entries` : 'Waiting for activity'}</span>
          </div>
          <div className="browser-debug-log-list">
            {browserMCPEntries.length === 0 ? (
              <div className="browser-debug-log-empty">No browser MCP activity yet.</div>
            ) : browserMCPEntries.map((entry, index) => (
              <div key={`${entry.timestamp}-${entry.source}-${index}`} className={`browser-debug-log-entry level-${entry.level || 'info'}`}>
                <span className="browser-debug-log-time">{formatDebugTimestamp(entry.timestamp)}</span>
                <span className={`browser-debug-log-source source-${entry.source || 'server'}`}>{entry.source || 'server'}</span>
                <span className="browser-debug-log-message">{entry.message}</span>
              </div>
            ))}
          </div>
        </div>
      )}
      {inspectMode && (
        <div className="browser-inspect-tip">
          <span>
            {activeTabIsWebRTC
              ? 'Inspect mode — move over the WebRTC surface, use arrow keys to navigate, Enter to select, and Esc to cancel.'
              : 'Inspect mode — hover and click to select for AI chat. Press Esc to cancel.'}
          </span>
          <span className="kbd-hint">
            <kbd>↑↓</kbd>
            <kbd>Enter</kbd>
            <kbd>Esc</kbd> cancel
          </span>
        </div>
      )}
      {browserSelection && (
        <BrowserSelectionBar
          selection={browserSelection}
          mode={browserSelectionMode}
          captureReady={browserSelectionCapture?.selectorKey === selectionKey}
          captureBusy={selectionCaptureBusy}
          onClear={handleClearSelection}
          onReselect={toggleInspectMode}
          onTogglePanel={() => setShowMiniPanel((v) => !v)}
          onChangeMode={(mode) => setBrowserSelectionMode(mode)}
          showPanel={showMiniPanel}
        />
      )}

      <div ref={stageRef} className={`browser-frame-wrap${viewportMode === 'responsive' ? ' responsive' : ''}`}>
        {activeTab ? (
          <div className="browser-viewport-stage">
            <div className={`browser-viewport-shell${viewportMode === 'responsive' ? ' responsive' : ''}`} style={scaledViewportStyle}>
              {activeTabIsWebRTC ? (
                <div
                  ref={webrtcSurfaceRef}
                  className={`browser-webrtc-frame${viewportMode === 'responsive' ? ' responsive' : ''}${inspectMode ? ' inspect' : ''}`}
                  style={frameStyle}
                  tabIndex={0}
                  onPointerMove={handleWebRTCPointerMove}
                  onPointerDown={handleWebRTCPointerDown}
                  onPointerUp={handleWebRTCPointerUp}
                  onPointerCancel={handleWebRTCPointerCancel}
                  onTouchStart={gestures.onTouchStart}
                  onTouchMove={gestures.onTouchMove}
                  onTouchEnd={gestures.onTouchEnd}
                  onMouseLeave={() => {
                    if (inspectMode) {
                      setRemoteHoverSelection(null);
                      setRemoteTooltip(null);
                    }
                  }}
                  onClick={handleWebRTCClick}
                  onKeyDown={handleWebRTCKeyDown}
                  onPaste={handleWebRTCPaste}
                >
                  {webrtcStreamConnected ? (
                    <canvas
                      ref={webrtcCanvasRef}
                      className="browser-webrtc-image"
                      style={{ width: '100%', height: '100%' }}
                    />
                  ) : (
                    <img
                      className="browser-webrtc-image"
                      src={webrtcImageSrc ?? undefined}
                      alt={displayTitle(activeTab)}
                      draggable={false}
                    />
                  )}
                  {visualStream.connecting && !webrtcStreamConnected && (
                    <div className="browser-webrtc-loading">
                      <div className="browser-loading-spinner" />
                      <span className="browser-loading-text">Establishing WebRTC stream…</span>
                    </div>
                  )}
                  {webrtcFrameLoading && !webrtcImageSrc && !error && !webrtcStreamConnected && (
                    <div className="browser-webrtc-loading">
                      <div className="browser-loading-spinner" />
                      <span className="browser-loading-text">Loading browser frame…</span>
                    </div>
                  )}
                  <RemoteInspectOverlay
                    hoverSelection={remoteHoverSelection}
                    selection={browserSelection?.tabId === activeTab.id ? browserSelection : null}
                    tooltip={remoteTooltip}
                    inspectMode={inspectMode}
                    scaleX={remoteOverlayScale.x}
                    scaleY={remoteOverlayScale.y}
                  />
                  <InspectMiniPanel
                    selection={browserSelection?.tabId === activeTab.id ? browserSelection : null}
                    visible={showMiniPanel}
                    onClose={() => setShowMiniPanel(false)}
                  />
                </div>
              ) : (
                <>
                  <iframe
                    ref={iframeRef}
                    key={`${activeTab.id}-${reloadNonce}`}
                    className={`browser-frame${viewportMode === 'responsive' ? ' responsive' : ''}`}
                    style={{ ...frameStyle, pointerEvents: inspectMode ? 'none' : 'auto' }}
                    src={activeTab.proxyPath}
                    title={displayTitle(activeTab)}
                    onLoad={() => {
                      setNavigationBusy(false);
                      updateAutoContentSize();
                      window.setTimeout(updateAutoContentSize, 250);
                      window.setTimeout(updateAutoContentSize, 1000);
                    }}
                    sandbox="allow-downloads allow-forms allow-modals allow-popups allow-same-origin allow-scripts"
                  />
                  {inspectMode && (
                    <div className="browser-inspect-hitlayer" />
                  )}
                  <InspectOverlay
                    boxModel={inspectState.hoveredBoxModel}
                    outerRect={inspectState.hoveredRect}
                    tooltip={inspectState.tooltip}
                    iframeRef={iframeRef}
                    inspectMode={inspectMode}
                    selection={browserSelection}
                  />
                  {!inspectMode && browserSelection && (
                    <SelectedHighlight
                      selection={browserSelection}
                      iframeRef={iframeRef}
                    />
                  )}
                  <InspectMiniPanel
                    selection={browserSelection}
                    visible={showMiniPanel}
                    onClose={() => setShowMiniPanel(false)}
                  />
                </>
              )}
            </div>
          </div>
        ) : (
          <div className="browser-empty">
            <IconGlobe />
            <button className="browser-go-btn" type="button" onClick={() => void handleNewTab('webrtc')}>
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
  mode,
  captureReady,
  captureBusy,
  onClear,
  onReselect,
  onTogglePanel,
  onChangeMode,
  showPanel,
}: {
  selection: { selector: string };
  mode: BrowserSelectionMode;
  captureReady: boolean;
  captureBusy: boolean;
  onClear: () => void;
  onReselect: () => void;
  onTogglePanel: () => void;
  onChangeMode: (mode: BrowserSelectionMode) => void;
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
        <button
          type="button"
          className={`browser-selection-mode${mode === 'detail' ? ' active' : ''}`}
          onClick={() => onChangeMode('detail')}
        >
          Detail
        </button>
        <button
          type="button"
          className={`browser-selection-mode${mode === 'screenshot' ? ' active' : ''}`}
          onClick={() => onChangeMode('screenshot')}
          title={captureBusy ? 'Capturing element...' : (captureReady ? 'Send element screenshot' : 'Capture element screenshot')}
        >
          {captureBusy && mode === 'screenshot' ? 'Shot...' : 'Shot'}
        </button>
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
