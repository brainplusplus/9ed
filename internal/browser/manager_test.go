package browser

import (
	"net/http"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
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
