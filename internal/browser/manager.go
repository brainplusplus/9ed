package browser

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	camoufox "github.com/brainplusplus/go-camoufox"
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
	Provider       string `json:"provider"`
	Automation     Status `json:"automation"`
	Tabs           []Tab  `json:"tabs"`
	ActiveTabID    string `json:"activeTabId,omitempty"`
	LocalhostScope string `json:"localhostScope"`
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
	browser *camoufox.Browser
	context playwright.BrowserContext
	page    playwright.Page
	lastErr string
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
		Provider:       "go-camoufox",
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
		Provider:  "go-camoufox/playwright",
		Running:   a.page != nil,
		LastError: a.lastErr,
	}
}

func (a *automationRuntime) ensureLocked(ctx context.Context) error {
	if a.page != nil {
		return nil
	}

	headless := camoufox.HeadlessTrue
	iKnow := true
	blockWebRTC := true
	opts := &camoufox.LaunchOptions{
		Headless:         &headless,
		IKnowWhatImDoing: &iKnow,
		BlockWebRTC:      &blockWebRTC,
		OS:               camoufoxOS(),
		Window:           &[2]int{1280, 800},
	}

	browser, err := camoufox.New(ctx, opts)
	if err != nil {
		a.lastErr = err.Error()
		return err
	}

	fpCtx, err := browser.NewBrowserContext(ctx, &camoufox.ContextOptions{})
	if err != nil {
		_ = browser.Close(ctx)
		a.lastErr = err.Error()
		return err
	}

	page, err := fpCtx.Context.NewPage()
	if err != nil {
		_ = fpCtx.Context.Close()
		_ = browser.Close(ctx)
		a.lastErr = err.Error()
		return err
	}

	a.browser = browser
	a.context = fpCtx.Context
	a.page = page
	a.lastErr = ""
	return nil
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

func camoufoxOS() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"windows"}
	case "darwin":
		return []string{"macos"}
	case "linux":
		return []string{"linux"}
	default:
		return nil
	}
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
