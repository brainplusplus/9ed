import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { BrowserPanel } from './BrowserPanel';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

// Mock API functions
const getBrowserState = vi.fn();
const getBrowserMCPDebugLog = vi.fn();
const createBrowserTab = vi.fn();
const navigateBrowserTab = vi.fn();
const activateBrowserTab = vi.fn();
const deleteBrowserTab = vi.fn();
const stopBrowserTab = vi.fn();
const reloadBrowserTab = vi.fn();
const goBackBrowserTab = vi.fn();
const goForwardBrowserTab = vi.fn();
const setBrowserTabViewport = vi.fn();
const scrollBrowserTab = vi.fn();
const mouseDownBrowserTab = vi.fn();
const mouseMoveBrowserTab = vi.fn();
const mouseUpBrowserTab = vi.fn();
const typeBrowserTabText = vi.fn();
const pressBrowserTabKey = vi.fn();
const pasteBrowserTabClipboard = vi.fn();
const uploadBrowserTabFiles = vi.fn();
const syncBrowserTabState = vi.fn();
const captureBrowserElementScreenshot = vi.fn();
const inspectBrowserTabPoint = vi.fn();
const inspectBrowserTabNavigate = vi.fn();
const browserTabScreenshotUrl = vi.fn();

vi.mock('../../api', () => ({
  getBrowserState: (...args: unknown[]) => getBrowserState(...args),
  getBrowserMCPDebugLog: (...args: unknown[]) => getBrowserMCPDebugLog(...args),
  createBrowserTab: (...args: unknown[]) => createBrowserTab(...args),
  navigateBrowserTab: (...args: unknown[]) => navigateBrowserTab(...args),
  activateBrowserTab: (...args: unknown[]) => activateBrowserTab(...args),
  deleteBrowserTab: (...args: unknown[]) => deleteBrowserTab(...args),
  stopBrowserTab: (...args: unknown[]) => stopBrowserTab(...args),
  reloadBrowserTab: (...args: unknown[]) => reloadBrowserTab(...args),
  goBackBrowserTab: (...args: unknown[]) => goBackBrowserTab(...args),
  goForwardBrowserTab: (...args: unknown[]) => goForwardBrowserTab(...args),
  setBrowserTabViewport: (...args: unknown[]) => setBrowserTabViewport(...args),
  scrollBrowserTab: (...args: unknown[]) => scrollBrowserTab(...args),
  mouseDownBrowserTab: (...args: unknown[]) => mouseDownBrowserTab(...args),
  mouseMoveBrowserTab: (...args: unknown[]) => mouseMoveBrowserTab(...args),
  mouseUpBrowserTab: (...args: unknown[]) => mouseUpBrowserTab(...args),
  typeBrowserTabText: (...args: unknown[]) => typeBrowserTabText(...args),
  pressBrowserTabKey: (...args: unknown[]) => pressBrowserTabKey(...args),
  pasteBrowserTabClipboard: (...args: unknown[]) => pasteBrowserTabClipboard(...args),
  uploadBrowserTabFiles: (...args: unknown[]) => uploadBrowserTabFiles(...args),
  syncBrowserTabState: (...args: unknown[]) => syncBrowserTabState(...args),
  captureBrowserElementScreenshot: (...args: unknown[]) => captureBrowserElementScreenshot(...args),
  inspectBrowserTabPoint: (...args: unknown[]) => inspectBrowserTabPoint(...args),
  inspectBrowserTabNavigate: (...args: unknown[]) => inspectBrowserTabNavigate(...args),
  browserTabScreenshotUrl: (...args: unknown[]) => browserTabScreenshotUrl(...args),
}));

vi.mock('./InspectOverlay', () => ({
  InspectOverlay: () => null,
  RemoteInspectOverlay: () => null,
  SelectedHighlight: () => null,
  InspectMiniPanel: () => null,
}));

vi.mock('../../hooks/useVisualStream', () => ({
  useVisualStream: () => ({
    connected: false,
    connecting: false,
    sendInput: vi.fn(),
  }),
}));

vi.mock('../../hooks/useGestures', () => ({
  useGestures: () => ({
    onTouchStart: vi.fn(),
    onTouchMove: vi.fn(),
    onTouchEnd: vi.fn(),
  }),
}));

describe('BrowserPanel default URL', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    getBrowserState.mockResolvedValue({ tabs: [], automation: null });
    getBrowserMCPDebugLog.mockResolvedValue({ enabled: false, entries: [] });
    createBrowserTab.mockResolvedValue({
      id: 'tab-1',
      url: 'about:blank',
      title: '',
      transport: 'webrtc',
      canGoBack: false,
      canGoForward: false,
      proxyPath: '/browser/tab-1/',
      createdAt: Date.now(),
      updatedAt: Date.now(),
    });
    setBrowserTabViewport.mockResolvedValue(undefined);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  it('initializes address bar with about:blank instead of localhost:3000', async () => {
    await act(async () => {
      root.render(<BrowserPanel />);
      await Promise.resolve();
      await Promise.resolve();
    });

    const addressInput = container.querySelector('input.browser-address') as HTMLInputElement | null;
    expect(addressInput).not.toBeNull();
    expect(addressInput!.value).toBe('about:blank');
    expect(addressInput!.value).not.toBe('localhost:3000');
  });
});
