package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	playwright "github.com/playwright-community/playwright-go"
)

type Tab struct {
	ID                  string `json:"id"`
	URL                 string `json:"url"`
	Title               string `json:"title"`
	Transport           string `json:"transport"`
	CanGoBack           bool   `json:"canGoBack"`
	CanGoForward        bool   `json:"canGoForward"`
	FileChooserPending  bool   `json:"fileChooserPending,omitempty"`
	FileChooserMultiple bool   `json:"fileChooserMultiple,omitempty"`
	ProxyPath           string `json:"proxyPath"`
	CreatedAt           int64  `json:"createdAt"`
	UpdatedAt           int64  `json:"updatedAt"`
}

type State struct {
	Provider       string        `json:"provider"`
	Transport      TransportType `json:"transport"`
	Automation     Status        `json:"automation"`
	Tabs           []Tab         `json:"tabs"`
	ActiveTabID    string        `json:"activeTabId,omitempty"`
	LocalhostScope string        `json:"localhostScope"`
}

type Status struct {
	Provider  string `json:"provider"`
	Running   bool   `json:"running"`
	LastError string `json:"lastError,omitempty"`
}

type InspectResult struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	TextBytes int    `json:"textBytes"`
}

type ConsoleEntry struct {
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	URL       string `json:"url,omitempty"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
}

type NetworkEntry struct {
	Timestamp    int64  `json:"timestamp"`
	Phase        string `json:"phase"`
	Method       string `json:"method"`
	URL          string `json:"url"`
	ResourceType string `json:"resourceType,omitempty"`
	Status       int    `json:"status,omitempty"`
	StatusText   string `json:"statusText,omitempty"`
	OK           bool   `json:"ok,omitempty"`
	Error        string `json:"error,omitempty"`
}

type Manager struct {
	mu           sync.Mutex
	playwrightMu sync.Mutex
	tabs         map[string]Tab
	activeTabID  string
	automation   *automationRuntime
	cookies      map[string]map[string]map[string]*http.Cookie
}

type automationRuntime struct {
	pw       *playwright.Playwright
	browser  playwright.Browser
	context  playwright.BrowserContext
	page     playwright.Page
	tabPages map[string]*automationTab
	lastErr  string
	execPath string
}

type automationTab struct {
	context                    playwright.BrowserContext
	page                       playwright.Page
	pendingFileChooser         playwright.FileChooser
	pendingFileChooserMultiple bool
	lastErr                    string
	history                    []string
	index                      int
	consoleLogs                []ConsoleEntry
	networkLogs                []NetworkEntry
}

type UploadedFile struct {
	Name     string
	MimeType string
	Buffer   []byte
}

const (
	telemetryBufferSize                  = 400
	defaultTelemetryReadLimit            = 60
	maxTelemetryReadLimit                = 400
	automationDefaultTimeoutMs           = 15000.0
	automationDefaultNavigationTimeoutMs = 20000.0
	automationScreenshotTimeoutMs        = 8000.0
	selectorClickPrimaryTimeoutMs        = 9000.0
	selectorClickFallbackWaitMs          = 2200.0
	selectorClickFallbackTimeoutMs       = 5000.0
)

const telemetryInitScript = `(function () {
  const root = globalThis;
  const cap = 400;
  if (!root.__nineTelemetry) {
    root.__nineTelemetry = { console: [], network: [] };
  }
  const telemetry = root.__nineTelemetry;
  const push = function (key, entry) {
    const list = telemetry[key];
    if (!Array.isArray(list)) return;
    list.push(entry);
    if (list.length > cap) {
      list.splice(0, list.length - cap);
    }
  };
  const stringify = function (value) {
    try {
      if (typeof value === "string") return value;
      if (value instanceof Error) return value.stack || value.message || String(value);
      return JSON.stringify(value);
    } catch (_) {
      return String(value);
    }
  };
  if (!root.__nineConsolePatched) {
    root.__nineConsolePatched = true;
    ["log", "info", "warn", "error", "debug"].forEach(function (type) {
      const original = console[type];
      if (typeof original !== "function") return;
      console[type] = function () {
        const args = Array.prototype.slice.call(arguments);
        push("console", {
          timestamp: Date.now(),
          type: type,
          text: args.map(stringify).join(" ")
        });
        return original.apply(console, args);
      };
    });
  }
  if (!root.__nineFetchPatched && typeof root.fetch === "function") {
    root.__nineFetchPatched = true;
    const originalFetch = root.fetch.bind(root);
    root.fetch = async function () {
      const args = Array.prototype.slice.call(arguments);
      let method = "GET";
      let url = "";
      try {
        const input = args[0];
        const init = args[1] || {};
        method = String((init && init.method) || (input && input.method) || "GET").toUpperCase();
        url = String((input && input.url) || input || "");
      } catch (_) {}
      push("network", { timestamp: Date.now(), phase: "request", method: method, url: url, resourceType: "fetch" });
      try {
        const response = await originalFetch.apply(root, args);
        push("network", {
          timestamp: Date.now(),
          phase: "response",
          method: method,
          url: response.url || url,
          resourceType: "fetch",
          status: response.status,
          statusText: response.statusText,
          ok: !!response.ok
        });
        return response;
      } catch (error) {
        push("network", {
          timestamp: Date.now(),
          phase: "failed",
          method: method,
          url: url,
          resourceType: "fetch",
          error: stringify(error)
        });
        throw error;
      }
    };
  }
  if (!root.__nineXHRPatched && typeof root.XMLHttpRequest === "function") {
    root.__nineXHRPatched = true;
    const originalOpen = root.XMLHttpRequest.prototype.open;
    const originalSend = root.XMLHttpRequest.prototype.send;
    root.XMLHttpRequest.prototype.open = function (method, url) {
      this.__nineMethod = String(method || "GET").toUpperCase();
      this.__nineURL = String(url || "");
      return originalOpen.apply(this, arguments);
    };
    root.XMLHttpRequest.prototype.send = function () {
      const method = this.__nineMethod || "GET";
      const url = this.__nineURL || "";
      push("network", { timestamp: Date.now(), phase: "request", method: method, url: url, resourceType: "xhr" });
      this.addEventListener("load", () => {
        push("network", {
          timestamp: Date.now(),
          phase: "response",
          method: method,
          url: this.responseURL || url,
          resourceType: "xhr",
          status: this.status,
          statusText: this.statusText || "",
          ok: this.status >= 200 && this.status < 400
        });
      });
      this.addEventListener("error", () => {
        push("network", { timestamp: Date.now(), phase: "failed", method: method, url: url, resourceType: "xhr", error: "network error" });
      });
      this.addEventListener("abort", () => {
        push("network", { timestamp: Date.now(), phase: "failed", method: method, url: url, resourceType: "xhr", error: "aborted" });
      });
      return originalSend.apply(this, arguments);
    };
  }
})();`

func NewManager() *Manager {
	return &Manager{
		tabs:    make(map[string]Tab),
		cookies: make(map[string]map[string]map[string]*http.Cookie),
		automation: &automationRuntime{
			lastErr:  "",
			tabPages: make(map[string]*automationTab),
		},
	}
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()

	return State{
		Provider:       "playwright",
		Transport:      TransportProxy,
		Automation:     m.automation.statusLocked(),
		Tabs:           m.tabsLocked(),
		ActiveTabID:    m.activeTabID,
		LocalhostScope: "server",
	}
}

func (m *Manager) ListTabs() []Tab {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tabsLocked()
}

func (m *Manager) CreateTab(rawURL string) (Tab, error) {
	return m.CreateTabWithTransport(rawURL, TransportProxy)
}

func (m *Manager) CreateTabWithTransport(rawURL string, transport TransportType) (Tab, error) {
	target, err := NormalizeURL(rawURL)
	if err != nil {
		return Tab{}, err
	}
	transport, err = NormalizeTransport(transport)
	if err != nil {
		return Tab{}, err
	}

	now := time.Now().UnixMilli()
	id := "browser-" + uuid.NewString()
	tab := Tab{
		ID:           id,
		URL:          target.String(),
		Title:        target.Host,
		Transport:    string(transport),
		CanGoBack:    false,
		CanGoForward: false,
		ProxyPath:    "/browser/" + id + "/",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	m.mu.Lock()
	m.tabs[id] = tab
	m.activeTabID = id
	m.mu.Unlock()
	if transport != TransportWebRTC {
		return tab, nil
	}
	m.playwrightMu.Lock()
	_, err = m.ensureTabPageLocked(context.Background(), id, target.String())
	m.playwrightMu.Unlock()
	if err != nil {
		m.mu.Lock()
		delete(m.tabs, id)
		if m.activeTabID == id {
			m.activeTabID = ""
		}
		m.mu.Unlock()
		return Tab{}, err
	}
	return tab, nil
}

func (m *Manager) NavigateTab(id string, rawURL string) (Tab, error) {
	target, err := NormalizeURL(rawURL)
	if err != nil {
		return Tab{}, err
	}

	m.mu.Lock()
	tab, ok := m.tabs[id]
	if !ok {
		m.mu.Unlock()
		return Tab{}, fmt.Errorf("browser tab %q not found", id)
	}
	tab.URL = target.String()
	tab.Title = target.Host
	tab.UpdatedAt = time.Now().UnixMilli()
	tab.CanGoForward = false
	m.tabs[id] = tab
	m.activeTabID = id
	m.mu.Unlock()
	if tab.Transport == string(TransportWebRTC) {
		m.playwrightMu.Lock()
		page, err := m.ensureTabPageLocked(context.Background(), id, target.String())
		if err != nil {
			m.playwrightMu.Unlock()
			return Tab{}, err
		}
		m.mu.Lock()
		m.syncTabFromRuntimeLocked(id, page, true)
		m.mu.Unlock()
		m.playwrightMu.Unlock()
	}
	return tab, nil
}

func (m *Manager) DeleteTab(id string) error {
	m.playwrightMu.Lock()
	defer m.playwrightMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tabs[id]; !ok {
		return fmt.Errorf("browser tab %q not found", id)
	}
	sortedTabs := m.tabsLocked()
	var fallbackActive string
	if m.activeTabID == id {
		for idx, tab := range sortedTabs {
			if tab.ID != id {
				continue
			}
			if idx+1 < len(sortedTabs) {
				fallbackActive = sortedTabs[idx+1].ID
			} else if idx-1 >= 0 {
				fallbackActive = sortedTabs[idx-1].ID
			}
			break
		}
	}
	delete(m.tabs, id)
	delete(m.cookies, id)
	m.automation.closeTabLocked(id)
	if m.activeTabID == id {
		m.activeTabID = fallbackActive
	}
	return nil
}

func (m *Manager) ActivateTab(id string) (Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tab, ok := m.tabs[id]
	if !ok {
		return Tab{}, fmt.Errorf("browser tab %q not found", id)
	}
	m.activeTabID = id
	return tab, nil
}

func (m *Manager) ProxyTarget(id string, requestPath string, rawQuery string) (*url.URL, error) {
	m.mu.Lock()
	tab, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("browser tab %q not found", id)
	}

	base, err := NormalizeURL(tab.URL)
	if err != nil {
		return nil, err
	}
	target := *base
	if requestPath == "" || requestPath == "/" {
		target.Path = joinURLPath(base.Path, requestPath)
	} else {
		target.Path = requestPath
	}
	target.RawPath = ""
	if rawQuery != "" {
		target.RawQuery = rawQuery
	}
	return &target, nil
}

func (m *Manager) ProxyExternalTarget(id string, scheme string, host string, requestPath string, rawQuery string) (*url.URL, error) {
	m.mu.Lock()
	_, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("browser tab %q not found", id)
	}

	scheme = strings.ToLower(strings.TrimSpace(scheme))
	host = strings.TrimSpace(host)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported external proxy scheme %q", scheme)
	}
	if host == "" {
		return nil, fmt.Errorf("external proxy host is required")
	}
	if requestPath == "" {
		requestPath = "/"
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	target := &url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     requestPath,
		RawQuery: rawQuery,
	}
	return target, nil
}

func (m *Manager) CookieHeader(id string, host string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	hostCookies := m.cookies[id][strings.ToLower(host)]
	if len(hostCookies) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(hostCookies))
	for _, cookie := range hostCookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		pairs = append(pairs, (&http.Cookie{Name: cookie.Name, Value: cookie.Value}).String())
	}
	return strings.Join(pairs, "; ")
}

func (m *Manager) StoreCookies(id string, host string, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	tabCookies := m.cookies[id]
	if tabCookies == nil {
		tabCookies = make(map[string]map[string]*http.Cookie)
		m.cookies[id] = tabCookies
	}
	hostKey := strings.ToLower(host)
	hostCookies := tabCookies[hostKey]
	if hostCookies == nil {
		hostCookies = make(map[string]*http.Cookie)
		tabCookies[hostKey] = hostCookies
	}
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		if cookie.MaxAge < 0 {
			delete(hostCookies, cookie.Name)
			continue
		}
		copied := *cookie
		hostCookies[cookie.Name] = &copied
	}
}

func NormalizeTransport(transport TransportType) (TransportType, error) {
	value := strings.ToLower(strings.TrimSpace(string(transport)))
	switch value {
	case "", string(TransportProxy), "iframe":
		return TransportProxy, nil
	case string(TransportWebRTC):
		return TransportWebRTC, nil
	default:
		return "", fmt.Errorf("unsupported browser transport %q", transport)
	}
}

func (m *Manager) Tab(id string) (Tab, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tab, ok := m.tabs[id]
	return tab, ok
}

func (m *Manager) SyncTabState(id string, url string, title string, canGoBack bool, canGoForward bool) (Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tab, ok := m.tabs[id]
	if !ok {
		return Tab{}, fmt.Errorf("browser tab %q not found", id)
	}
	if normalized, err := NormalizeURL(url); err == nil {
		tab.URL = normalized.String()
	}
	if strings.TrimSpace(title) != "" {
		tab.Title = strings.TrimSpace(title)
	}
	tab.CanGoBack = canGoBack
	tab.CanGoForward = canGoForward
	tab.UpdatedAt = time.Now().UnixMilli()
	m.tabs[id] = tab
	return tab, nil
}

func (m *Manager) ActiveTabID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeTabID
}

func (m *Manager) TabInspect(ctx context.Context, id string) (InspectResult, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return InspectResult{}, err
	}
	defer release()
	return inspectPage(page)
}

func (m *Manager) TabNavigate(ctx context.Context, id string, rawURL string) (InspectResult, error) {
	target, err := NormalizeURL(rawURL)
	if err != nil {
		return InspectResult{}, err
	}
	m.mu.Lock()
	tab, ok := m.tabs[id]
	if !ok {
		m.mu.Unlock()
		return InspectResult{}, fmt.Errorf("browser tab %q not found", id)
	}
	tab.URL = target.String()
	tab.Title = target.Host
	tab.UpdatedAt = time.Now().UnixMilli()
	m.tabs[id] = tab
	m.activeTabID = id
	m.mu.Unlock()
	m.playwrightMu.Lock()
	page, err := m.ensureTabPageLocked(ctx, id, target.String())
	if err != nil {
		m.playwrightMu.Unlock()
		return InspectResult{}, err
	}
	if page.URL() != target.String() {
		timeout := 15000.0
		if _, err := page.Goto(target.String(), playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   &timeout,
		}); err != nil {
			if isNonFatalNavigationError(err) {
				// Keep the tab alive for unreachable targets; user still needs
				// independent tab state and can retry/navigation later.
			} else {
				currentURL := strings.TrimSpace(page.URL())
				if currentURL == "" || currentURL == "about:blank" || currentURL == target.String() {
					m.playwrightMu.Unlock()
					m.recoverBrowserRuntime(id, err)
					return InspectResult{}, err
				}
			}
		}
	}
	result, inspectErr := inspectPage(page)
	_, syncErr := m.syncTabFromPageResultLocked(id, page, true)
	m.playwrightMu.Unlock()
	if syncErr != nil {
		return InspectResult{}, syncErr
	}
	return result, inspectErr
}

func (m *Manager) TabReload(ctx context.Context, id string) (Tab, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return Tab{}, err
	}
	defer release()
	timeout := automationDefaultNavigationTimeoutMs
	if _, err := page.Reload(playwright.PageReloadOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   &timeout,
	}); err != nil {
		if !isNonFatalNavigationError(err) {
			m.recoverBrowserRuntimeLocked(id, err)
			return Tab{}, err
		}
	}
	return m.syncTabFromPageResultLocked(id, page, true)
}

func (m *Manager) TabStop(ctx context.Context, id string) error {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if _, err := page.Evaluate(`() => window.stop()`); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabUploadFilePaths(ctx context.Context, id string, paths []string) (Tab, error) {
	if len(paths) == 0 {
		return Tab{}, fmt.Errorf("at least one file is required")
	}
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return Tab{}, err
	}
	defer release()

	var chooser playwright.FileChooser
	m.mu.Lock()
	chooser = m.consumePendingFileChooserLocked(id)
	m.mu.Unlock()

	timeout := automationDefaultTimeoutMs
	if chooser != nil {
		if err := chooser.SetFiles(paths, playwright.FileChooserSetFilesOptions{Timeout: &timeout}); err == nil {
			return m.syncTabFromPageResultLocked(id, page, false)
		}
	}

	marked, err := markUploadTarget(page)
	if err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return Tab{}, err
	}
	if !marked {
		return Tab{}, fmt.Errorf("no pending file chooser or file input found in the active browser tab")
	}
	defer cleanupUploadMarker(page)

	if err := page.Locator(`input[type="file"][data-nine-upload-target="1"]`).SetInputFiles(paths, playwright.LocatorSetInputFilesOptions{
		Timeout: &timeout,
	}); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return Tab{}, err
	}
	return m.syncTabFromPageResultLocked(id, page, false)
}

func (m *Manager) TabPasteClipboard(ctx context.Context, id string, text string, files []UploadedFile) error {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	type clipboardFile struct {
		Name     string `json:"name"`
		MimeType string `json:"mimeType"`
		Base64   string `json:"base64"`
	}
	payload := struct {
		Text  string          `json:"text"`
		Files []clipboardFile `json:"files"`
	}{
		Text: text,
	}
	for _, file := range files {
		payload.Files = append(payload.Files, clipboardFile{
			Name:     file.Name,
			MimeType: file.MimeType,
			Base64:   base64.StdEncoding.EncodeToString(file.Buffer),
		})
	}
	if payload.Text == "" && len(payload.Files) == 0 {
		return fmt.Errorf("clipboard payload is empty")
	}
	_, err = page.Evaluate(`async (payload) => {
		const target = document.activeElement || document.body;
		const transfer = new DataTransfer();
		if (payload.text) {
			try { transfer.setData('text/plain', payload.text); } catch {}
		}
		for (const file of payload.files || []) {
			const binary = atob(file.base64 || '');
			const bytes = new Uint8Array(binary.length);
			for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
			const blob = new Blob([bytes], { type: file.mimeType || 'application/octet-stream' });
			const clipboardFile = new File([blob], file.name || 'clipboard.bin', { type: file.mimeType || 'application/octet-stream' });
			transfer.items.add(clipboardFile);
		}
		let event;
		try {
			event = new ClipboardEvent('paste', { bubbles: true, cancelable: true });
		} catch {
			event = new Event('paste', { bubbles: true, cancelable: true });
		}
		try { Object.defineProperty(event, 'clipboardData', { value: transfer }); } catch {}
		try { Object.defineProperty(event, 'dataTransfer', { value: transfer }); } catch {}
		target.dispatchEvent(event);
		if (payload.text && target instanceof HTMLInputElement) {
			const start = target.selectionStart ?? target.value.length;
			const end = target.selectionEnd ?? target.value.length;
			target.setRangeText(payload.text, start, end, 'end');
			target.dispatchEvent(new Event('input', { bubbles: true }));
			target.dispatchEvent(new Event('change', { bubbles: true }));
		} else if (payload.text && target instanceof HTMLTextAreaElement) {
			const start = target.selectionStart ?? target.value.length;
			const end = target.selectionEnd ?? target.value.length;
			target.setRangeText(payload.text, start, end, 'end');
			target.dispatchEvent(new Event('input', { bubbles: true }));
			target.dispatchEvent(new Event('change', { bubbles: true }));
		}
		return true;
	}`, payload)
	if err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabClick(ctx context.Context, id string, selector string, x, y *float64) error {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if strings.TrimSpace(selector) != "" {
		if err := clickSelectorWithFallback(page, selector); err != nil {
			m.recoverBrowserRuntimeLocked(id, err)
			return err
		}
		_, err = m.syncTabFromPageResultLocked(id, page, false)
		return err
	}
	if x == nil || y == nil {
		return fmt.Errorf("selector or x/y coordinates are required")
	}
	if err := page.Mouse().Click(*x, *y); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func clickSelectorWithFallback(page playwright.Page, selector string) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return fmt.Errorf("selector is required")
	}
	primaryTimeout := selectorClickPrimaryTimeoutMs
	if err := page.Click(selector, playwright.PageClickOptions{Timeout: &primaryTimeout}); err == nil {
		return nil
	} else {
		candidates := selectorFallbackCandidates(selector, page.URL())
		failures := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate == "" || candidate == selector {
				continue
			}
			if candidateErr := clickFirstVisibleLocator(page, candidate); candidateErr == nil {
				return nil
			} else if len(failures) < 4 {
				failures = append(failures, candidate+" -> "+candidateErr.Error())
			}
		}
		if len(failures) == 0 {
			return err
		}
		return fmt.Errorf("%w (fallback tried: %s)", err, strings.Join(failures, " | "))
	}
}

func clickFirstVisibleLocator(page playwright.Page, selector string) error {
	waitTimeout := selectorClickFallbackWaitMs
	clickTimeout := selectorClickFallbackTimeoutMs
	locator := page.Locator(selector).First()
	if err := locator.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: &waitTimeout,
	}); err != nil {
		return err
	}
	return locator.Click(playwright.LocatorClickOptions{Timeout: &clickTimeout})
}

func selectorFallbackCandidates(rawSelector string, pageURL string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 16)
	add := func(candidate string) {
		candidate = normalizeSelectorCandidate(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}

	add(rawSelector)
	for _, part := range strings.FieldsFunc(rawSelector, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';' || r == '|'
	}) {
		part = normalizeSelectorCandidate(part)
		if part == "" {
			continue
		}
		add(part)
		if strings.Contains(part, `href^="/`) {
			add(strings.ReplaceAll(part, `href^="/`, `href*="/`))
		}
	}

	if strings.Contains(strings.ToLower(pageURL), "detik.com") {
		add(`a[href*="news.detik.com/berita/"]`)
		add(`a[href*="/berita/d-"]`)
		add(`article h2 a[href]`)
		add(`article h3 a[href]`)
		add(`.media__link[href]`)
		add(`.media_text a[href]`)
		add(`.list-content__item a[href]`)
	}

	return out
}

func normalizeSelectorCandidate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Trim(value, "`")
	value = strings.Trim(value, `"'`)
	for strings.HasPrefix(value, "_a[") {
		value = strings.TrimPrefix(value, "_")
	}
	return strings.TrimSpace(value)
}

func (m *Manager) TabMouseDown(ctx context.Context, id string, x, y float64) error {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if err := page.Mouse().Move(x, y); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	if err := page.Mouse().Down(); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabMouseMove(ctx context.Context, id string, x, y float64) error {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if err := page.Mouse().Move(x, y); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	return nil
}

func (m *Manager) TabMouseUp(ctx context.Context, id string, x, y float64) error {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if err := page.Mouse().Move(x, y); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	if err := page.Mouse().Up(); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabType(ctx context.Context, id string, selector string, text string) error {
	if text == "" {
		return fmt.Errorf("text is required")
	}
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if strings.TrimSpace(selector) != "" {
		if err := page.Fill(selector, text); err != nil {
			m.recoverBrowserRuntimeLocked(id, err)
			return err
		}
		_, err = m.syncTabFromPageResultLocked(id, page, false)
		return err
	}
	if err := page.Keyboard().Type(text); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabPress(ctx context.Context, id string, key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("key is required")
	}
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if err := page.Keyboard().Press(key); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabSetViewport(ctx context.Context, id string, width int, height int) error {
	if width < 320 || height < 320 {
		return fmt.Errorf("viewport must be at least 320x320")
	}
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if err := page.SetViewportSize(width, height); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabScroll(ctx context.Context, id string, deltaX, deltaY float64) error {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	_, err = page.Evaluate(`([dx, dy]) => window.scrollBy(dx, dy)`, []float64{deltaX, deltaY})
	if err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return err
	}
	_, err = m.syncTabFromPageResultLocked(id, page, false)
	return err
}

func (m *Manager) TabScreenshot(ctx context.Context, id string) ([]byte, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	_, _ = m.syncTabFromPageResultLocked(id, page, false)
	timeout := automationScreenshotTimeoutMs
	data, err := page.Screenshot(playwright.PageScreenshotOptions{
		Scale:   playwright.ScreenshotScaleCss,
		Timeout: &timeout,
	})
	if err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return nil, err
	}
	return data, nil
}

func (m *Manager) TabConsoleLogs(id string, limit int) ([]ConsoleEntry, error) {
	resolvedLimit := resolveTelemetryLimit(limit)
	page, release, err := m.acquireTabPage(context.Background(), id)
	if err != nil {
		return nil, err
	}
	defer release()

	entries, err := readConsoleTelemetry(page, resolvedLimit)
	if err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return nil, err
	}
	return copyConsoleLogs(entries, resolvedLimit), nil
}

func (m *Manager) TabNetworkRequests(id string, limit int) ([]NetworkEntry, error) {
	resolvedLimit := resolveTelemetryLimit(limit)
	page, release, err := m.acquireTabPage(context.Background(), id)
	if err != nil {
		return nil, err
	}
	defer release()

	entries, err := readNetworkTelemetry(page, resolvedLimit)
	if err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return nil, err
	}
	return copyNetworkLogs(entries, resolvedLimit), nil
}

func (m *Manager) TabGoBack(ctx context.Context, id string) (Tab, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return Tab{}, err
	}
	defer release()
	if _, err := page.GoBack(); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return Tab{}, err
	}
	return m.syncTabFromPageResultLocked(id, page, false)
}

func (m *Manager) TabGoForward(ctx context.Context, id string) (Tab, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return Tab{}, err
	}
	defer release()
	if _, err := page.GoForward(); err != nil {
		m.recoverBrowserRuntimeLocked(id, err)
		return Tab{}, err
	}
	return m.syncTabFromPageResultLocked(id, page, false)
}

func (m *Manager) TabInspectAtPoint(ctx context.Context, id string, x, y float64) (ElementSelection, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return ElementSelection{}, err
	}
	defer release()
	tab, ok := m.Tab(id)
	if !ok {
		return ElementSelection{}, fmt.Errorf("browser tab %q not found", id)
	}
	return inspectSelectionAtPoint(page, tab, x, y)
}

func (m *Manager) TabInspectNavigate(ctx context.Context, id string, selector string, direction string) (ElementSelection, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return ElementSelection{}, err
	}
	defer release()
	tab, ok := m.Tab(id)
	if !ok {
		return ElementSelection{}, fmt.Errorf("browser tab %q not found", id)
	}
	return inspectSelectionByDirection(page, tab, selector, direction)
}

func (m *Manager) TabElementScreenshot(ctx context.Context, id string, selectors ...string) ([]byte, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	timeout := automationScreenshotTimeoutMs
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		shot, err := page.Locator(selector).Screenshot(playwright.LocatorScreenshotOptions{
			Scale:   playwright.ScreenshotScaleCss,
			Timeout: &timeout,
		})
		if err == nil {
			return shot, nil
		}
		m.recoverBrowserRuntimeLocked(id, err)
	}
	return nil, fmt.Errorf("failed to capture selected element screenshot")
}

func (m *Manager) StartAutomation(ctx context.Context) (Status, error) {
	m.playwrightMu.Lock()
	defer m.playwrightMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.automation.ensureLocked(ctx)
	return m.automation.statusLocked(), err
}

func (m *Manager) AutomationStatus() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.automation.statusLocked()
}

func (m *Manager) AutomationNavigate(ctx context.Context, rawURL string) (InspectResult, error) {
	target, err := NormalizeURL(rawURL)
	if err != nil {
		return InspectResult{}, err
	}
	page, release, err := m.acquireAutomationPage(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	defer release()
	if _, err := page.Goto(target.String()); err != nil {
		m.recoverBrowserRuntimeLocked("", err)
		return InspectResult{}, err
	}
	return inspectPage(page)
}

func (m *Manager) AutomationClick(ctx context.Context, selector string) error {
	if strings.TrimSpace(selector) == "" {
		return fmt.Errorf("selector is required")
	}
	page, release, err := m.acquireAutomationPage(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := clickSelectorWithFallback(page, selector); err != nil {
		m.recoverBrowserRuntimeLocked("", err)
		return err
	}
	return nil
}

func (m *Manager) AutomationType(ctx context.Context, selector string, text string) error {
	if strings.TrimSpace(selector) == "" {
		return fmt.Errorf("selector is required")
	}
	page, release, err := m.acquireAutomationPage(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := page.Fill(selector, text); err != nil {
		m.recoverBrowserRuntimeLocked("", err)
		return err
	}
	return nil
}

func (m *Manager) AutomationEvaluate(ctx context.Context, expression string) (any, error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("expression is required")
	}
	page, release, err := m.acquireAutomationPage(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return page.Evaluate(expression)
}

func (m *Manager) AutomationInspect(ctx context.Context) (InspectResult, error) {
	page, release, err := m.acquireAutomationPage(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	defer release()
	return inspectPage(page)
}

func (m *Manager) AutomationScreenshot(ctx context.Context) ([]byte, error) {
	page, release, err := m.acquireAutomationPage(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	fullPage := true
	data, err := page.Screenshot(playwright.PageScreenshotOptions{FullPage: &fullPage})
	if err != nil {
		m.recoverBrowserRuntimeLocked("", err)
		return nil, err
	}
	return data, nil
}

func (m *Manager) AutomationElementScreenshot(ctx context.Context, rawURL string, selectors ...string) ([]byte, error) {
	page, release, err := m.acquireAutomationPage(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if strings.TrimSpace(rawURL) != "" {
		target, err := NormalizeURL(rawURL)
		if err != nil {
			return nil, err
		}
		if page.URL() != target.String() {
			if _, err := page.Goto(target.String()); err != nil {
				m.recoverBrowserRuntimeLocked("", err)
				return nil, err
			}
		}
	}

	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		shot, err := page.Locator(selector).Screenshot(playwright.LocatorScreenshotOptions{})
		if err == nil {
			return shot, nil
		}
	}

	return nil, fmt.Errorf("failed to capture selected element screenshot")
}

func (m *Manager) Close() {
	m.playwrightMu.Lock()
	defer m.playwrightMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.automation.closeLocked()
}

func (m *Manager) acquireAutomationPage(ctx context.Context) (playwright.Page, func(), error) {
	m.playwrightMu.Lock()
	page, err := m.automationPageLocked(ctx)
	if err != nil {
		m.playwrightMu.Unlock()
		return nil, nil, err
	}
	return page, func() {
		m.playwrightMu.Unlock()
	}, nil
}

func (m *Manager) automationPageLocked(ctx context.Context) (playwright.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.automation.ensureLocked(ctx); err != nil {
		return nil, err
	}
	return m.automation.page, nil
}

func (m *Manager) acquireTabPage(ctx context.Context, id string) (playwright.Page, func(), error) {
	m.playwrightMu.Lock()
	page, err := m.tabPageLocked(ctx, id)
	if err != nil {
		m.playwrightMu.Unlock()
		return nil, nil, err
	}
	return page, func() {
		m.playwrightMu.Unlock()
	}, nil
}

func (m *Manager) tabPageLocked(ctx context.Context, id string) (playwright.Page, error) {
	m.mu.Lock()
	tab, ok := m.tabs[id]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("browser tab %q not found", id)
	}
	if tab.Transport != string(TransportWebRTC) {
		m.mu.Unlock()
		return nil, fmt.Errorf("browser control requires a WebRTC tab; %s uses %s transport", id, tab.Transport)
	}
	rawURL := tab.URL
	m.mu.Unlock()
	return m.ensureTabPageLocked(ctx, id, rawURL)
}

func (m *Manager) ensureTabPageLocked(ctx context.Context, id string, rawURL string) (playwright.Page, error) {
	m.mu.Lock()
	if runtime := m.automation.tabPages[id]; runtime != nil && runtime.page != nil {
		page := runtime.page
		m.mu.Unlock()
		return page, nil
	}
	m.mu.Unlock()

	if err := m.automation.ensureBrowserLocked(ctx); err != nil {
		return nil, err
	}

	bCtx, err := m.automation.browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 800},
	})
	if err != nil {
		m.automation.lastErr = fmt.Sprintf("browser context failed: %v", err)
		return nil, fmt.Errorf("%s", m.automation.lastErr)
	}
	page, err := bCtx.NewPage()
	if err != nil {
		_ = bCtx.Close()
		m.automation.lastErr = fmt.Sprintf("browser page failed: %v", err)
		return nil, fmt.Errorf("%s", m.automation.lastErr)
	}
	bCtx.SetDefaultTimeout(automationDefaultTimeoutMs)
	bCtx.SetDefaultNavigationTimeout(automationDefaultNavigationTimeoutMs)
	page.SetDefaultTimeout(automationDefaultTimeoutMs)
	page.SetDefaultNavigationTimeout(automationDefaultNavigationTimeoutMs)
	if err := installTelemetry(page, bCtx); err != nil {
		m.automation.lastErr = fmt.Sprintf("browser telemetry unavailable: %v", err)
	}
	if strings.TrimSpace(rawURL) != "" {
		timeout := 15000.0
		if _, err := page.Goto(rawURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   &timeout,
		}); err != nil {
			if !isNonFatalNavigationError(err) {
				_ = bCtx.Close()
				m.automation.lastErr = fmt.Sprintf("browser navigation failed: %v", err)
				return nil, fmt.Errorf("%s", m.automation.lastErr)
			}
		}
	}
	runtime := &automationTab{
		context: bCtx,
		page:    page,
		history: []string{},
		index:   -1,
	}
	page.OnFrameNavigated(func(frame playwright.Frame) {
		if frame.ParentFrame() != nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if runtime := m.automation.tabPages[id]; runtime != nil {
			runtime.pendingFileChooser = nil
			runtime.pendingFileChooserMultiple = false
		}
		m.syncTabFromEventLocked(id, frame.URL())
	})
	page.OnFileChooser(func(chooser playwright.FileChooser) {
		m.mu.Lock()
		defer m.mu.Unlock()
		runtime := m.automation.tabPages[id]
		if runtime == nil {
			return
		}
		runtime.pendingFileChooser = chooser
		runtime.pendingFileChooserMultiple = chooser.IsMultiple()
		tab, ok := m.tabs[id]
		if !ok {
			return
		}
		tab.FileChooserPending = true
		tab.FileChooserMultiple = chooser.IsMultiple()
		tab.UpdatedAt = time.Now().UnixMilli()
		m.tabs[id] = tab
	})
	m.mu.Lock()
	m.automation.tabPages[id] = runtime
	m.syncHistoryLocked(id, page.URL())
	m.syncTabFromRuntimeLocked(id, page, true)
	m.mu.Unlock()
	m.automation.lastErr = ""
	return page, nil
}

func (m *Manager) syncTabFromPage(ctx context.Context, id string, resetForward bool) error {
	_, err := m.syncTabFromPageResult(ctx, id, resetForward)
	return err
}

func (m *Manager) syncTabFromPageResult(ctx context.Context, id string, resetForward bool) (Tab, error) {
	page, release, err := m.acquireTabPage(ctx, id)
	if err != nil {
		return Tab{}, err
	}
	defer release()
	return m.syncTabFromPageResultLocked(id, page, resetForward)
}

func (m *Manager) syncTabFromPageResultLocked(id string, page playwright.Page, resetForward bool) (Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncTabFromRuntimeLocked(id, page, resetForward)
	tab, ok := m.tabs[id]
	if !ok {
		return Tab{}, fmt.Errorf("browser tab %q not found", id)
	}
	return tab, nil
}

func (m *Manager) syncTabFromEventLocked(id string, currentURL string) {
	tab, ok := m.tabs[id]
	if !ok {
		return
	}
	if currentURL = strings.TrimSpace(currentURL); currentURL != "" {
		tab.URL = currentURL
		m.syncHistoryLocked(id, currentURL)
		if runtime := m.automation.tabPages[id]; runtime != nil {
			tab.CanGoBack = runtime.index > 0
			tab.CanGoForward = runtime.index >= 0 && runtime.index < len(runtime.history)-1
			tab.FileChooserPending = runtime.pendingFileChooser != nil
			tab.FileChooserMultiple = runtime.pendingFileChooserMultiple
		}
	}
	tab.UpdatedAt = time.Now().UnixMilli()
	m.tabs[id] = tab
}

func (m *Manager) syncHistoryLocked(id string, currentURL string) {
	runtime := m.automation.tabPages[id]
	if runtime == nil || strings.TrimSpace(currentURL) == "" {
		return
	}
	if runtime.index >= 0 && runtime.index < len(runtime.history) && runtime.history[runtime.index] == currentURL {
		return
	}
	if runtime.index+1 < len(runtime.history) && runtime.history[runtime.index+1] == currentURL {
		runtime.index++
		return
	}
	if runtime.index-1 >= 0 && runtime.history[runtime.index-1] == currentURL {
		runtime.index--
		return
	}
	if runtime.index >= 0 && runtime.index+1 < len(runtime.history) {
		runtime.history = append([]string{}, runtime.history[:runtime.index+1]...)
	}
	runtime.history = append(runtime.history, currentURL)
	runtime.index = len(runtime.history) - 1
}

func (m *Manager) consumePendingFileChooserLocked(id string) playwright.FileChooser {
	runtime := m.automation.tabPages[id]
	if runtime == nil || runtime.pendingFileChooser == nil {
		return nil
	}
	chooser := runtime.pendingFileChooser
	runtime.pendingFileChooser = nil
	runtime.pendingFileChooserMultiple = false
	if tab, ok := m.tabs[id]; ok {
		tab.FileChooserPending = false
		tab.FileChooserMultiple = false
		tab.UpdatedAt = time.Now().UnixMilli()
		m.tabs[id] = tab
	}
	return chooser
}

func cleanupUploadMarker(page playwright.Page) {
	_, _ = page.Evaluate(`() => {
		for (const el of Array.from(document.querySelectorAll('[data-nine-upload-target="1"]'))) {
			el.removeAttribute('data-nine-upload-target');
		}
	}`)
}

func markUploadTarget(page playwright.Page) (bool, error) {
	raw, err := page.Evaluate(`() => {
		for (const el of Array.from(document.querySelectorAll('[data-nine-upload-target="1"]'))) {
			el.removeAttribute('data-nine-upload-target');
		}
		const active = document.activeElement;
		if (active instanceof HTMLInputElement && active.type === 'file' && !active.disabled) {
			active.setAttribute('data-nine-upload-target', '1');
			return true;
		}
		const inputs = Array.from(document.querySelectorAll('input[type="file"]'));
		for (const input of inputs) {
			if (!(input instanceof HTMLInputElement) || input.disabled) continue;
			input.setAttribute('data-nine-upload-target', '1');
			return true;
		}
		return false;
	}`)
	if err != nil {
		return false, err
	}
	ok, _ := raw.(bool)
	return ok, nil
}

func (m *Manager) syncTabFromRuntimeLocked(id string, page playwright.Page, resetForward bool) {
	tab, ok := m.tabs[id]
	if !ok {
		return
	}
	runtime := m.automation.tabPages[id]
	title, _ := page.Title()
	if url := effectiveBrowserURL(page.URL(), tab.URL); url != "" {
		tab.URL = url
		if runtime != nil {
			if resetForward {
				if runtime.index >= 0 && runtime.index+1 < len(runtime.history) {
					runtime.history = append([]string{}, runtime.history[:runtime.index+1]...)
				}
			}
			m.syncHistoryLocked(id, url)
			tab.CanGoBack = runtime.index > 0
			tab.CanGoForward = runtime.index >= 0 && runtime.index < len(runtime.history)-1
		}
	}
	if strings.TrimSpace(title) != "" {
		tab.Title = strings.TrimSpace(title)
	}
	if runtime != nil {
		tab.FileChooserPending = runtime.pendingFileChooser != nil
		tab.FileChooserMultiple = runtime.pendingFileChooserMultiple
	}
	tab.UpdatedAt = time.Now().UnixMilli()
	m.tabs[id] = tab
}

func (m *Manager) tabsLocked() []Tab {
	out := make([]Tab, 0, len(m.tabs))
	for _, tab := range m.tabs {
		out = append(out, tab)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func (m *Manager) recoverBrowserRuntime(id string, err error) {
	if err == nil || !isRecoverableBrowserError(err) {
		return
	}
	m.playwrightMu.Lock()
	defer m.playwrightMu.Unlock()
	m.recoverBrowserRuntimeLocked(id, err)
}

func (m *Manager) recoverBrowserRuntimeLocked(id string, err error) {
	if err == nil || !isRecoverableBrowserError(err) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != "" {
		m.automation.closeTabLocked(id)
	} else {
		m.automation.closeLocked()
	}
	m.automation.lastErr = fmt.Sprintf("browser runtime reset after failure: %v", err)
}

func isRecoverableBrowserError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"target page, context or browser has been closed",
		"browser has been closed",
		"connection closed",
		"broken pipe",
		"epipe",
		"eof",
		"pipe closed",
		"signal is aborted without reason",
		"aborted without reason",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func isNonFatalNavigationError(err error) bool {
	if err == nil {
		return false
	}
	if isRecoverableBrowserError(err) {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"net::err_",
		"err_connection_refused",
		"err_name_not_resolved",
		"err_address_unreachable",
		"err_internet_disconnected",
		"err_connection_timed_out",
		"err_timed_out",
		"connection refused",
		"connection timed out",
		"address unreachable",
		"name not resolved",
		"failed to fetch",
		"timeout",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	if strings.Contains(message, "frame.goto") || strings.Contains(message, "navigation") {
		return strings.Contains(message, "playwright:") || strings.Contains(message, "net::err_")
	}
	return false
}

func (a *automationRuntime) statusLocked() Status {
	return Status{
		Provider:  "playwright",
		Running:   a.page != nil,
		LastError: a.lastErr,
	}
}

func (a *automationRuntime) ensureLocked(ctx context.Context) error {
	if a.page != nil {
		return nil
	}
	if err := a.ensureBrowserLocked(ctx); err != nil {
		return err
	}

	// Create context and page.
	bCtx, err := a.browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 800},
	})
	if err != nil {
		a.lastErr = fmt.Sprintf("browser context failed: %v", err)
		return fmt.Errorf("%s", a.lastErr)
	}

	page, err := bCtx.NewPage()
	if err != nil {
		a.lastErr = fmt.Sprintf("browser page failed: %v", err)
		_ = bCtx.Close()
		return fmt.Errorf("%s", a.lastErr)
	}
	bCtx.SetDefaultTimeout(automationDefaultTimeoutMs)
	bCtx.SetDefaultNavigationTimeout(automationDefaultNavigationTimeoutMs)
	page.SetDefaultTimeout(automationDefaultTimeoutMs)
	page.SetDefaultNavigationTimeout(automationDefaultNavigationTimeoutMs)

	a.context = bCtx
	a.page = page
	a.lastErr = ""
	return nil
}

func (a *automationRuntime) ensureBrowserLocked(_ context.Context) error {
	if a.browser != nil {
		return nil
	}
	// Discover local browser binary.
	execPath := FindBrowser()
	if execPath == "" {
		a.lastErr = "no local browser found; install Chrome, Edge, or Chromium"
		return fmt.Errorf("%s", a.lastErr)
	}
	a.execPath = execPath

	// Start playwright driver.
	pw, err := playwright.Run()
	if err != nil {
		a.lastErr = fmt.Sprintf("playwright driver failed: %v", err)
		return fmt.Errorf("%s", a.lastErr)
	}
	a.pw = pw

	// Launch browser using the local binary.
	headless := true
	// Keep the transport conservative: some modern sites become unstable in
	// headless Chromium when HTTP/2 or QUIC are negotiated.
	launchArgs := []string{
		"--disable-http2",
		"--disable-quic",
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		ExecutablePath: &execPath,
		Headless:       &headless,
		Args:           launchArgs,
	})
	if err != nil {
		a.lastErr = fmt.Sprintf("browser launch failed: %v", err)
		_ = pw.Stop()
		a.pw = nil
		return fmt.Errorf("%s", a.lastErr)
	}

	a.browser = browser
	a.lastErr = ""
	return nil
}

func (a *automationRuntime) closeTabLocked(id string) {
	runtime := a.tabPages[id]
	if runtime == nil {
		return
	}
	if runtime.page != nil {
		_ = runtime.page.Close()
	}
	if runtime.context != nil {
		_ = runtime.context.Close()
	}
	delete(a.tabPages, id)
}

func (a *automationRuntime) closeLocked() {
	if a.page != nil {
		_ = a.context.Close()
		a.page = nil
		a.context = nil
	}
	for id := range a.tabPages {
		a.closeTabLocked(id)
	}
	if a.browser != nil {
		_ = a.browser.Close()
		a.browser = nil
	}
	if a.pw != nil {
		_ = a.pw.Stop()
		a.pw = nil
	}
}

func appendConsoleLog(current []ConsoleEntry, entry ConsoleEntry) []ConsoleEntry {
	current = append(current, entry)
	if len(current) <= telemetryBufferSize {
		return current
	}
	return append([]ConsoleEntry(nil), current[len(current)-telemetryBufferSize:]...)
}

func appendNetworkLog(current []NetworkEntry, entry NetworkEntry) []NetworkEntry {
	current = append(current, entry)
	if len(current) <= telemetryBufferSize {
		return current
	}
	return append([]NetworkEntry(nil), current[len(current)-telemetryBufferSize:]...)
}

func installTelemetry(page playwright.Page, bCtx playwright.BrowserContext) error {
	content := telemetryInitScript
	if err := bCtx.AddInitScript(playwright.Script{Content: &content}); err != nil {
		return err
	}
	_, err := page.Evaluate(telemetryInitScript)
	return err
}

func readConsoleTelemetry(page playwright.Page, limit int) ([]ConsoleEntry, error) {
	raw, err := page.Evaluate(`(limit) => {
		const root = globalThis;
		const telemetry = root && root.__nineTelemetry ? root.__nineTelemetry : {};
		const entries = Array.isArray(telemetry.console) ? telemetry.console : [];
		const take = Math.max(0, Math.min(Number(limit) || 0, 400));
		if (take <= 0 || entries.length <= take) return entries.slice();
		return entries.slice(entries.length - take);
	}`, limit)
	if err != nil {
		return nil, err
	}
	return decodeConsoleEntries(raw)
}

func readNetworkTelemetry(page playwright.Page, limit int) ([]NetworkEntry, error) {
	raw, err := page.Evaluate(`(limit) => {
		const root = globalThis;
		const telemetry = root && root.__nineTelemetry ? root.__nineTelemetry : {};
		const entries = Array.isArray(telemetry.network) ? telemetry.network : [];
		const take = Math.max(0, Math.min(Number(limit) || 0, 400));
		if (take <= 0 || entries.length <= take) return entries.slice();
		return entries.slice(entries.length - take);
	}`, limit)
	if err != nil {
		return nil, err
	}
	return decodeNetworkEntries(raw)
}

func decodeConsoleEntries(raw any) ([]ConsoleEntry, error) {
	if raw == nil {
		return []ConsoleEntry{}, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var entries []ConsoleEntry
	if err := json.Unmarshal(payload, &entries); err != nil {
		return nil, err
	}
	for idx := range entries {
		entries[idx].Type = strings.TrimSpace(entries[idx].Type)
		if entries[idx].Type == "" {
			entries[idx].Type = "log"
		}
		entries[idx].Text = strings.TrimSpace(entries[idx].Text)
		entries[idx].URL = strings.TrimSpace(entries[idx].URL)
	}
	return entries, nil
}

func decodeNetworkEntries(raw any) ([]NetworkEntry, error) {
	if raw == nil {
		return []NetworkEntry{}, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var entries []NetworkEntry
	if err := json.Unmarshal(payload, &entries); err != nil {
		return nil, err
	}
	for idx := range entries {
		entries[idx].Phase = strings.TrimSpace(entries[idx].Phase)
		entries[idx].Method = strings.ToUpper(strings.TrimSpace(entries[idx].Method))
		if entries[idx].Method == "" {
			entries[idx].Method = "GET"
		}
		entries[idx].URL = strings.TrimSpace(entries[idx].URL)
		entries[idx].ResourceType = strings.TrimSpace(entries[idx].ResourceType)
		entries[idx].StatusText = strings.TrimSpace(entries[idx].StatusText)
		entries[idx].Error = strings.TrimSpace(entries[idx].Error)
	}
	return entries, nil
}

func resolveTelemetryLimit(limit int) int {
	if limit <= 0 {
		return defaultTelemetryReadLimit
	}
	if limit > maxTelemetryReadLimit {
		return maxTelemetryReadLimit
	}
	return limit
}

func effectiveBrowserURL(current string, fallback string) string {
	current = strings.TrimSpace(current)
	fallback = strings.TrimSpace(fallback)
	switch {
	case current == "":
		return fallback
	case current == "about:blank":
		return fallback
	case strings.HasPrefix(current, "chrome-error://chromewebdata"):
		return fallback
	default:
		return current
	}
}

func copyConsoleLogs(entries []ConsoleEntry, limit int) []ConsoleEntry {
	if len(entries) == 0 {
		return []ConsoleEntry{}
	}
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	start := len(entries) - limit
	out := make([]ConsoleEntry, limit)
	copy(out, entries[start:])
	return out
}

func copyNetworkLogs(entries []NetworkEntry, limit int) []NetworkEntry {
	if len(entries) == 0 {
		return []NetworkEntry{}
	}
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	start := len(entries) - limit
	out := make([]NetworkEntry, limit)
	copy(out, entries[start:])
	return out
}

func inspectPage(page playwright.Page) (InspectResult, error) {
	title, _ := page.Title()
	text, _ := page.TextContent("body")
	text = strings.TrimSpace(text)
	bytes := len([]byte(text))
	if len(text) > 12000 {
		text = text[:12000]
	}
	return InspectResult{
		URL:       page.URL(),
		Title:     title,
		Text:      text,
		TextBytes: bytes,
	}, nil
}

func joinURLPath(basePath string, requestPath string) string {
	if requestPath == "" || requestPath == "/" {
		if basePath == "" {
			return "/"
		}
		return basePath
	}
	if strings.HasSuffix(basePath, "/") {
		return path.Join(basePath, requestPath)
	}
	return path.Join(basePath, requestPath)
}
