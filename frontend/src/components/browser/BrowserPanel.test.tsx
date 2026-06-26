import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { BrowserPanel } from './BrowserPanel';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

// jsdom does not implement ResizeObserver; BrowserPanel uses it to track the
// viewport stage size when a tab is active.
class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.assign(globalThis, { ResizeObserver: ResizeObserverMock });

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
const saveRecentProject = vi.fn();

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
  saveRecentProject: (...args: unknown[]) => saveRecentProject(...args),
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

describe('BrowserPanel browser toggle unified path', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    // Start with the browser disabled so the inspect-commit flow will attempt
    // to enable it via the unified setBrowserEnabled path.
    useChatStore.setState({
      sessions: [],
      activeSessionId: null,
      useActiveBrowser: false,
      browserSelection: null,
    });

    const webrtcTab = {
      id: 'webrtc-1',
      url: 'about:blank',
      title: '',
      transport: 'webrtc',
      canGoBack: false,
      canGoForward: false,
      proxyPath: '/browser/webrtc-1/',
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };
    getBrowserState.mockResolvedValue({ tabs: [webrtcTab], automation: null });
    getBrowserMCPDebugLog.mockResolvedValue({ enabled: false, entries: [] });
    inspectBrowserTabPoint.mockResolvedValue({
      tabId: 'webrtc-1',
      selector: 'div',
      tagName: 'DIV',
      attributes: {},
      text: '',
      boxModel: { content: { x: 0, y: 0, width: 10, height: 10 }, padding: { x: 0, y: 0, width: 10, height: 10 }, border: { x: 0, y: 0, width: 10, height: 10 }, margin: { x: 0, y: 0, width: 10, height: 10 } },
      viewport: { width: 1280, height: 720 },
    });

    // Set up a project that owns the WebRTC tab and has it active.
    useWorkspaceStore.setState({
      projects: [],
      activeProjectId: null,
      activePanel: 'explorer',
      sidebarVisible: true,
      terminalVisible: true,
      chatVisible: true,
      browserVisible: true,
      showPicker: false,
    });
    useWorkspaceStore.getState().addProject('/repo', 'repo');
    const project = useWorkspaceStore.getState().projects[0];
    useWorkspaceStore.getState().addBrowserTab(project.id, 'webrtc-1');

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

  it('enables the browser via the unified setBrowserEnabled path on inspect commit', async () => {
    const setBrowserEnabledSpy = vi.spyOn(useChatStore.getState(), 'setBrowserEnabled');

    await act(async () => {
      root.render(<BrowserPanel />);
      // Flush the initial getBrowserState + reconcile.
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });

    // Toggle inspect mode via the inspect button.
    const inspectBtn = container.querySelector('button.browser-icon-btn[title]') as HTMLButtonElement | null;
    // Fall back to the first icon button if the title selector is brittle.
    const inspectButton = (inspectBtn && inspectBtn.title.toLowerCase().includes('inspect'))
      ? inspectBtn
      : (Array.from(container.querySelectorAll('button.browser-icon-btn')) as HTMLButtonElement[]).find((b) => (b.title || '').toLowerCase().includes('inspect')) ?? null;
    expect(inspectButton).not.toBeNull();
    await act(async () => {
      inspectButton!.click();
      await Promise.resolve();
    });

    const surface = container.querySelector('.browser-webrtc-frame') as HTMLElement | null;
    expect(surface).not.toBeNull();
    const surfaceEl = surface!;

    // jsdom returns a zero-size rect by default; stub a non-zero geometry so
    // clientPointToViewport maps the click to viewport coordinates.
    surfaceEl.getBoundingClientRect = () => ({
      x: 0, y: 0, top: 0, left: 0, width: 1280, height: 720, right: 1280, bottom: 720, toJSON: () => ({}),
    });

    await act(async () => {
      // Click in the centre of the surface while inspect mode is active.
      surfaceEl.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: 640, clientY: 360 }));
      // Flush the async inspectBrowserTabPoint resolution.
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(setBrowserEnabledSpy).toHaveBeenCalledWith(true);
  });
});
