package browser

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "about:blank", raw: "about:blank", want: "about:blank"},
		{name: "about:blank case insensitive", raw: "ABOUT:BLANK", want: "about:blank"},
		{name: "localhost port", raw: "localhost:3000", want: "http://localhost:3000"},
		{name: "loopback port", raw: "127.0.0.1:5173", want: "http://127.0.0.1:5173"},
		{name: "host port", raw: "example.com:8080", want: "http://example.com:8080"},
		{name: "http", raw: "http://127.0.0.1:5173/app", want: "http://127.0.0.1:5173/app"},
		{name: "bare host", raw: "example.com", want: "https://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeURL(tt.raw)
			if err != nil {
				t.Fatalf("NormalizeURL() error = %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("NormalizeURL() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestNormalizeURLRejectsUnsupportedScheme(t *testing.T) {
	if _, err := NormalizeURL("file:///tmp/secret.txt"); err == nil {
		t.Fatal("NormalizeURL() expected unsupported scheme error")
	}
}

func TestCreateTabAboutBlank(t *testing.T) {
	manager := NewManager()
	tab, err := manager.CreateTab("about:blank")
	if err != nil {
		t.Fatalf("CreateTab(about:blank) error = %v", err)
	}
	if tab.URL != "about:blank" {
		t.Fatalf("CreateTab(about:blank) URL = %q, want %q", tab.URL, "about:blank")
	}
}

func TestNormalizeTransport(t *testing.T) {
	tests := []struct {
		name      string
		input     TransportType
		want      TransportType
		wantError bool
	}{
		{name: "default", input: "", want: TransportProxy},
		{name: "proxy", input: TransportProxy, want: TransportProxy},
		{name: "iframe alias", input: "iframe", want: TransportProxy},
		{name: "webrtc", input: TransportWebRTC, want: TransportWebRTC},
		{name: "invalid", input: "socket", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeTransport(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("NormalizeTransport(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeTransport(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeTransport(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeSelectorCandidate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trim quoted selector", input: ` "a[href^="/berita/"]" `, want: `a[href^="/berita/"]`},
		{name: "strip markdown underscore artifact", input: `_a[href*="news.detik.com"]`, want: `a[href*="news.detik.com"]`},
		{name: "trim backticks", input: "`article h2 a`", want: "article h2 a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSelectorCandidate(tt.input); got != tt.want {
				t.Fatalf("normalizeSelectorCandidate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSelectorFallbackCandidatesDetikExpansion(t *testing.T) {
	raw := `a[href^="/berita/"] | .media_text h2 a, article a`
	candidates := selectorFallbackCandidates(raw, "https://www.detik.com")
	mustContain := []string{
		`a[href^="/berita/"]`,
		`a[href*="/berita/"]`,
		`.media_text h2 a`,
		`article a`,
		`a[href*="news.detik.com/berita/"]`,
		`a[href*="/berita/d-"]`,
	}
	for _, want := range mustContain {
		if !stringSliceContains(candidates, want) {
			t.Fatalf("selectorFallbackCandidates missing %q in %#v", want, candidates)
		}
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestManagerTabsAndProxyTarget(t *testing.T) {
	manager := NewManager()
	tab, err := manager.CreateTab("localhost:3000")
	if err != nil {
		t.Fatalf("CreateTab() error = %v", err)
	}

	target, err := manager.ProxyTarget(tab.ID, "/dashboard", "x=1")
	if err != nil {
		t.Fatalf("ProxyTarget() error = %v", err)
	}
	if target.String() != "http://localhost:3000/dashboard?x=1" {
		t.Fatalf("ProxyTarget() = %q", target.String())
	}

	if _, err := manager.NavigateTab(tab.ID, "https://example.com/docs"); err != nil {
		t.Fatalf("NavigateTab() error = %v", err)
	}
	if len(manager.ListTabs()) != 1 {
		t.Fatalf("ListTabs() length = %d", len(manager.ListTabs()))
	}
	if err := manager.DeleteTab(tab.ID); err != nil {
		t.Fatalf("DeleteTab() error = %v", err)
	}
}

func TestManagerCreateTabWithTransportPersistsTransport(t *testing.T) {
	manager := NewManager()
	tab, err := manager.CreateTabWithTransport("https://example.com", TransportProxy)
	if err != nil {
		t.Fatalf("CreateTabWithTransport() error = %v", err)
	}
	if tab.Transport != string(TransportProxy) {
		t.Fatalf("tab.Transport = %q, want %q", tab.Transport, TransportProxy)
	}
}

func TestProxyTargetMapsInitialDocumentAndAbsoluteResources(t *testing.T) {
	manager := NewManager()
	tab, err := manager.CreateTab("https://example.com/docs/page")
	if err != nil {
		t.Fatalf("CreateTab() error = %v", err)
	}

	document, err := manager.ProxyTarget(tab.ID, "/", "")
	if err != nil {
		t.Fatalf("ProxyTarget(document) error = %v", err)
	}
	if document.String() != "https://example.com/docs/page" {
		t.Fatalf("expected initial proxy request to load tab URL, got %q", document.String())
	}

	asset, err := manager.ProxyTarget(tab.ID, "/assets/app.js", "v=1")
	if err != nil {
		t.Fatalf("ProxyTarget(asset) error = %v", err)
	}
	if asset.String() != "https://example.com/assets/app.js?v=1" {
		t.Fatalf("expected absolute asset path to stay root-relative, got %q", asset.String())
	}
}

func TestProxyExternalTargetRequiresKnownTabAndBuildsTargetURL(t *testing.T) {
	manager := NewManager()
	tab, err := manager.CreateTab("https://example.com")
	if err != nil {
		t.Fatalf("CreateTab() error = %v", err)
	}

	target, err := manager.ProxyExternalTarget(tab.ID, "https", "play.google.com", "/log", "format=json")
	if err != nil {
		t.Fatalf("ProxyExternalTarget() error = %v", err)
	}
	if target.String() != "https://play.google.com/log?format=json" {
		t.Fatalf("ProxyExternalTarget() = %q", target.String())
	}

	if _, err := manager.ProxyExternalTarget("missing", "https", "play.google.com", "/log", ""); err == nil {
		t.Fatal("expected missing tab to fail")
	}
}

func TestManagerStoresCookiesPerTabAndHost(t *testing.T) {
	manager := NewManager()
	tab, err := manager.CreateTab("https://example.com")
	if err != nil {
		t.Fatalf("CreateTab() error = %v", err)
	}

	manager.StoreCookies(tab.ID, "play.google.com", []*http.Cookie{{Name: "sid", Value: "abc"}})
	manager.StoreCookies(tab.ID, "accounts.google.com", []*http.Cookie{{Name: "other", Value: "def"}})

	if got := manager.CookieHeader(tab.ID, "play.google.com"); got != "sid=abc" {
		t.Fatalf("CookieHeader(play.google.com) = %q", got)
	}
	if got := manager.CookieHeader(tab.ID, "accounts.google.com"); got != "other=def" {
		t.Fatalf("CookieHeader(accounts.google.com) = %q", got)
	}
}

func TestTelemetryHelpersKeepMostRecentEntries(t *testing.T) {
	console := make([]ConsoleEntry, 0, telemetryBufferSize+8)
	network := make([]NetworkEntry, 0, telemetryBufferSize+8)
	for i := 0; i < telemetryBufferSize+12; i++ {
		console = appendConsoleLog(console, ConsoleEntry{
			Timestamp: time.Now().UnixMilli(),
			Type:      "log",
			Text:      "entry",
			Line:      i + 1,
		})
		network = appendNetworkLog(network, NetworkEntry{
			Timestamp: time.Now().UnixMilli(),
			Phase:     "request",
			Method:    "GET",
			URL:       "https://example.com",
			Status:    i + 1,
		})
	}
	if len(console) != telemetryBufferSize {
		t.Fatalf("expected console buffer size %d, got %d", telemetryBufferSize, len(console))
	}
	if len(network) != telemetryBufferSize {
		t.Fatalf("expected network buffer size %d, got %d", telemetryBufferSize, len(network))
	}
	if console[0].Line != 13 {
		t.Fatalf("expected oldest console entry to be trimmed to line 13, got %d", console[0].Line)
	}
	if network[0].Status != 13 {
		t.Fatalf("expected oldest network status to be trimmed to 13, got %d", network[0].Status)
	}
}

func TestCopyTelemetryLogsHonorsLimit(t *testing.T) {
	console := []ConsoleEntry{
		{Type: "log", Text: "a", Line: 1},
		{Type: "log", Text: "b", Line: 2},
		{Type: "log", Text: "c", Line: 3},
	}
	network := []NetworkEntry{
		{Phase: "request", URL: "https://a"},
		{Phase: "response", URL: "https://b"},
		{Phase: "failed", URL: "https://c"},
	}
	if got := copyConsoleLogs(console, 2); len(got) != 2 || got[0].Line != 2 || got[1].Line != 3 {
		t.Fatalf("unexpected console copy result: %#v", got)
	}
	if got := copyNetworkLogs(network, 1); len(got) != 1 || got[0].URL != "https://c" {
		t.Fatalf("unexpected network copy result: %#v", got)
	}
}

func TestResolvePageSourceLimit(t *testing.T) {
	if got := resolvePageSourceLimit(0); got != defaultPageSourceMaxBytes {
		t.Fatalf("resolvePageSourceLimit(0) = %d, want %d", got, defaultPageSourceMaxBytes)
	}
	if got := resolvePageSourceLimit(-1); got != defaultPageSourceMaxBytes {
		t.Fatalf("resolvePageSourceLimit(-1) = %d, want %d", got, defaultPageSourceMaxBytes)
	}
	if got := resolvePageSourceLimit(maxPageSourceMaxBytes + 1); got != maxPageSourceMaxBytes {
		t.Fatalf("resolvePageSourceLimit(max+1) = %d, want %d", got, maxPageSourceMaxBytes)
	}
	if got := resolvePageSourceLimit(12345); got != 12345 {
		t.Fatalf("resolvePageSourceLimit(12345) = %d, want 12345", got)
	}
}

func TestTruncateUTF8ByBytes(t *testing.T) {
	source := "halo🙂dunia"
	truncated, clipped := truncateUTF8ByBytes(source, 7)
	if !clipped {
		t.Fatal("expected clipped=true")
	}
	if truncated != "halo" {
		t.Fatalf("truncateUTF8ByBytes produced invalid boundary result %q", truncated)
	}
	full, clippedFull := truncateUTF8ByBytes(source, 100)
	if clippedFull || full != source {
		t.Fatalf("expected full source unchanged, got %q clipped=%v", full, clippedFull)
	}
}

func TestResolveTelemetryLimit(t *testing.T) {
	if got := resolveTelemetryLimit(0); got != defaultTelemetryReadLimit {
		t.Fatalf("resolveTelemetryLimit(0) = %d, want %d", got, defaultTelemetryReadLimit)
	}
	if got := resolveTelemetryLimit(-5); got != defaultTelemetryReadLimit {
		t.Fatalf("resolveTelemetryLimit(-5) = %d, want %d", got, defaultTelemetryReadLimit)
	}
	if got := resolveTelemetryLimit(maxTelemetryReadLimit + 1); got != maxTelemetryReadLimit {
		t.Fatalf("resolveTelemetryLimit(max+1) = %d, want %d", got, maxTelemetryReadLimit)
	}
	if got := resolveTelemetryLimit(25); got != 25 {
		t.Fatalf("resolveTelemetryLimit(25) = %d, want 25", got)
	}
}

func TestIsNonFatalNavigationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "connection refused", err: fmt.Errorf("Frame.Goto http://localhost:3000: playwright: net::ERR_CONNECTION_REFUSED at http://localhost:3000/"), want: true},
		{name: "timeout", err: fmt.Errorf("navigation timeout after 15000ms"), want: true},
		{name: "recoverable pipe", err: fmt.Errorf("broken pipe"), want: false},
		{name: "other error", err: fmt.Errorf("selector not found"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNonFatalNavigationError(tt.err); got != tt.want {
				t.Fatalf("isNonFatalNavigationError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListTabsSortedByCreatedAt(t *testing.T) {
	manager := NewManager()
	manager.tabs = map[string]Tab{
		"b": {ID: "b", CreatedAt: 200},
		"a": {ID: "a", CreatedAt: 100},
		"c": {ID: "c", CreatedAt: 100},
	}

	got := manager.ListTabs()
	if len(got) != 3 {
		t.Fatalf("ListTabs() length = %d, want 3", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" || got[2].ID != "b" {
		t.Fatalf("unexpected tab order: %#v", got)
	}
}

func TestDeleteTabKeepsDeterministicActiveFallback(t *testing.T) {
	manager := NewManager()
	manager.tabs = map[string]Tab{
		"a": {ID: "a", CreatedAt: 100},
		"b": {ID: "b", CreatedAt: 200},
		"c": {ID: "c", CreatedAt: 300},
	}
	manager.activeTabID = "b"

	if err := manager.DeleteTab("b"); err != nil {
		t.Fatalf("DeleteTab() error = %v", err)
	}
	if got := manager.ActiveTabID(); got != "c" {
		t.Fatalf("ActiveTabID() = %q, want %q", got, "c")
	}
}
