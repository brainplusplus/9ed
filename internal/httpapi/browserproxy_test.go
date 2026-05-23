package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brainplusplus/9ed/internal/browser"
)

func TestRewriteProxyHTMLRewritesRootRelativeAssetsAndInjectsRuntime(t *testing.T) {
	input := []byte(`<!doctype html><html><head><script src="/assets/app.js"></script></head><body><img src="/hero.png"><a href="/docs">Docs</a></body></html>`)

	output, err := rewriteProxyHTML(input, "/api/browser/proxy/browser-1/", "/", "browser-1")
	if err != nil {
		t.Fatalf("rewriteProxyHTML() error = %v", err)
	}
	html := string(output)

	if !strings.Contains(html, `<base href="/api/browser/proxy/browser-1/"/>`) {
		t.Fatalf("expected base href to be injected, got %q", html)
	}
	if !strings.Contains(html, `src="/api/browser/proxy/browser-1/assets/app.js"`) {
		t.Fatalf("expected script src to be rewritten, got %q", html)
	}
	if !strings.Contains(html, `href="/api/browser/proxy/browser-1/docs"`) {
		t.Fatalf("expected anchor href to be rewritten, got %q", html)
	}
	if !strings.Contains(html, `data-nine-proxy-runtime="true"`) {
		t.Fatalf("expected runtime patch to be injected, got %q", html)
	}
	if !strings.Contains(html, `var remotePath="/"`) {
		t.Fatalf("expected runtime patch to receive remote path, got %q", html)
	}
	if !strings.Contains(html, `var tabId="browser-1"`) {
		t.Fatalf("expected runtime patch to receive tab id, got %q", html)
	}
}

func TestRewriteProxyLocationRewritesAbsoluteAndRootRelativeLocations(t *testing.T) {
	prefix := "/api/browser/proxy/browser-1/"

	if got := rewriteProxyLocation("/login", prefix); got != "/api/browser/proxy/browser-1/login" {
		t.Fatalf("rewriteProxyLocation(root-relative) = %q", got)
	}
	if got := rewriteProxyLocation("https://example.com/dashboard?x=1", prefix); got != "/api/browser/proxy/browser-1/dashboard?x=1" {
		t.Fatalf("rewriteProxyLocation(absolute) = %q", got)
	}
	if got := rewriteProxyLocation("settings", prefix); got != "settings" {
		t.Fatalf("rewriteProxyLocation(relative) = %q", got)
	}
}

func TestHandleBrowserProxyRewritesHTMLResponses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head></head><body><script src="/assets/app.js"></script></body></html>`))
	}))
	defer upstream.Close()

	manager := browser.NewManager()
	tab, err := manager.CreateTab(upstream.URL)
	if err != nil {
		t.Fatalf("CreateTab() error = %v", err)
	}

	api := New(Dependencies{Mode: "full", UseBrowser: true, Browser: manager})
	req := httptest.NewRequest(http.MethodGet, "/api/browser/proxy/"+tab.ID+"/", nil)
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `src="/api/browser/proxy/`+tab.ID+`/assets/app.js"`) {
		t.Fatalf("expected proxied asset path, got %q", body)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("expected X-Frame-Options to be removed, got %q", got)
	}
}

func TestRewriteProxyCSSRewritesRootRelativeURLs(t *testing.T) {
	input := []byte(`@font-face{src:url('/cf-fonts/v/inter/normal.woff2')} .hero{background-image:url(/images/hero.png)}`)

	output := string(rewriteProxyCSS(input, "/api/browser/proxy/browser-1/"))

	if !strings.Contains(output, `url('/api/browser/proxy/browser-1/cf-fonts/v/inter/normal.woff2')`) {
		t.Fatalf("expected font URL to be rewritten, got %q", output)
	}
	if !strings.Contains(output, `url(/api/browser/proxy/browser-1/images/hero.png)`) {
		t.Fatalf("expected background URL to be rewritten, got %q", output)
	}
}
