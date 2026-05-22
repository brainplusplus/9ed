package browser

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	playwright "github.com/playwright-community/playwright-go"
)

type Tab struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	ProxyPath string `json:"proxyPath"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
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

type Manager struct {
	mu          sync.Mutex
	tabs        map[string]Tab
	activeTabID string
	automation  *automationRuntime
}

type automationRuntime struct {
	pw       *playwright.Playwright
	browser  playwright.Browser
	context  playwright.BrowserContext
	page     playwright.Page
	lastErr  string
	execPath string
}

func NewManager() *Manager {
	return &Manager{
		tabs: make(map[string]Tab),
		automation: &automationRuntime{
			lastErr: "",
		},
	}
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()

	return State{
		Provider:       "playwright",
		Transport:      TransportIframe,
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
	target, err := NormalizeURL(rawURL)
	if err != nil {
		return Tab{}, err
	}

	now := time.Now().UnixMilli()
	id := "browser-" + uuid.NewString()
	tab := Tab{
		ID:        id,
		URL:       target.String(),
		Title:     target.Host,
		ProxyPath: "/api/browser/proxy/" + id + "/",
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.tabs[id] = tab
	m.activeTabID = id
	return tab, nil
}

func (m *Manager) NavigateTab(id string, rawURL string) (Tab, error) {
	target, err := NormalizeURL(rawURL)
	if err != nil {
		return Tab{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	tab, ok := m.tabs[id]
	if !ok {
		return Tab{}, fmt.Errorf("browser tab %q not found", id)
	}
	tab.URL = target.String()
	tab.Title = target.Host
	tab.UpdatedAt = time.Now().UnixMilli()
	m.tabs[id] = tab
	m.activeTabID = id
	return tab, nil
}

func (m *Manager) DeleteTab(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tabs[id]; !ok {
		return fmt.Errorf("browser tab %q not found", id)
	}
	delete(m.tabs, id)
	if m.activeTabID == id {
		m.activeTabID = ""
		for nextID := range m.tabs {
			m.activeTabID = nextID
			break
		}
	}
	return nil
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
	target.Path = joinURLPath(base.Path, requestPath)
	target.RawPath = ""
	if rawQuery != "" {
		target.RawQuery = rawQuery
	}
	return &target, nil
}

func (m *Manager) StartAutomation(ctx context.Context) (Status, error) {
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
	page, err := m.automationPage(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	if _, err := page.Goto(target.String()); err != nil {
		return InspectResult{}, err
	}
	return inspectPage(page)
}

func (m *Manager) AutomationClick(ctx context.Context, selector string) error {
	if strings.TrimSpace(selector) == "" {
		return fmt.Errorf("selector is required")
	}
	page, err := m.automationPage(ctx)
	if err != nil {
		return err
	}
	return page.Click(selector)
}

func (m *Manager) AutomationType(ctx context.Context, selector string, text string) error {
	if strings.TrimSpace(selector) == "" {
		return fmt.Errorf("selector is required")
	}
	page, err := m.automationPage(ctx)
	if err != nil {
		return err
	}
	return page.Fill(selector, text)
}

func (m *Manager) AutomationEvaluate(ctx context.Context, expression string) (any, error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("expression is required")
	}
	page, err := m.automationPage(ctx)
	if err != nil {
		return nil, err
	}
	return page.Evaluate(expression)
}

func (m *Manager) AutomationInspect(ctx context.Context) (InspectResult, error) {
	page, err := m.automationPage(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	return inspectPage(page)
}

func (m *Manager) AutomationScreenshot(ctx context.Context) ([]byte, error) {
	page, err := m.automationPage(ctx)
	if err != nil {
		return nil, err
	}
	fullPage := true
	return page.Screenshot(playwright.PageScreenshotOptions{FullPage: &fullPage})
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.automation.closeLocked()
}

func (m *Manager) automationPage(ctx context.Context) (playwright.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.automation.ensureLocked(ctx); err != nil {
		return nil, err
	}
	return m.automation.page, nil
}

func (m *Manager) tabsLocked() []Tab {
	out := make([]Tab, 0, len(m.tabs))
	for _, tab := range m.tabs {
		out = append(out, tab)
	}
	return out
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
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		ExecutablePath: &execPath,
		Headless:       &headless,
	})
	if err != nil {
		a.lastErr = fmt.Sprintf("browser launch failed: %v", err)
		_ = pw.Stop()
		a.pw = nil
		return fmt.Errorf("%s", a.lastErr)
	}

	// Create context and page.
	bCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 800},
	})
	if err != nil {
		a.lastErr = fmt.Sprintf("browser context failed: %v", err)
		_ = browser.Close()
		_ = pw.Stop()
		a.pw = nil
		return fmt.Errorf("%s", a.lastErr)
	}

	page, err := bCtx.NewPage()
	if err != nil {
		a.lastErr = fmt.Sprintf("browser page failed: %v", err)
		_ = bCtx.Close()
		_ = browser.Close()
		_ = pw.Stop()
		a.pw = nil
		return fmt.Errorf("%s", a.lastErr)
	}

	a.browser = browser
	a.context = bCtx
	a.page = page
	a.lastErr = ""
	return nil
}

func (a *automationRuntime) closeLocked() {
	if a.page != nil {
		a.context.Close()
		a.browser.Close()
		a.page = nil
		a.context = nil
		a.browser = nil
	}
	if a.pw != nil {
		_ = a.pw.Stop()
		a.pw = nil
	}
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
