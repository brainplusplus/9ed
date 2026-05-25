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

	output, err := rewriteProxyHTML(input, "/browser/browser-1/", "/", "browser-1")
	if err != nil {
		t.Fatalf("rewriteProxyHTML() error = %v", err)
	}
	html := string(output)

	if !strings.Contains(html, `<base href="/browser/browser-1/"/>`) {
		t.Fatalf("expected base href to be injected, got %q", html)
	}
	if !strings.Contains(html, `src="/browser/browser-1/assets/app.js"`) {
		t.Fatalf("expected script src to be rewritten, got %q", html)
	}
	if !strings.Contains(html, `href="/browser/browser-1/docs"`) {
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
	if strings.Contains(html, `history.replaceState`) {
		t.Fatalf("proxy runtime must not move the iframe out of the proxy URL, got %q", html)
	}
	if !strings.Contains(html, `window.location.origin+"/"`) {
		t.Fatalf("expected runtime patch to normalize same-origin absolute URLs, got %q", html)
	}
}

func TestRewriteProxyHTMLUsesRemoteDocumentDirectoryForRelativeAssets(t *testing.T) {
	input := []byte(`<!doctype html><html><head><script src="chunk.js"></script></head><body></body></html>`)

	output, err := rewriteProxyHTML(input, "/browser/browser-1/", "/docs/page?x=1", "browser-1")
	if err != nil {
		t.Fatalf("rewriteProxyHTML() error = %v", err)
	}
	html := string(output)

	if !strings.Contains(html, `<base href="/browser/browser-1/docs/"/>`) {
		t.Fatalf("expected base href to use remote directory, got %q", html)
	}
	if !strings.Contains(html, `src="chunk.js"`) {
		t.Fatalf("expected relative asset to remain relative to base, got %q", html)
	}
}

func TestRewriteProxyAttributeLeavesSchemeRelativeURLsAlone(t *testing.T) {
	got := rewriteProxyAttribute("//www.gstatic.com/assets/app.js", "/browser/browser-1/")
	if got != "//www.gstatic.com/assets/app.js" {
		t.Fatalf("expected scheme-relative URL to be preserved, got %q", got)
	}
}

func TestRewriteProxyLocationRewritesAbsoluteAndRootRelativeLocations(t *testing.T) {
	prefix := "/browser/browser-1/"

	if got := rewriteProxyLocation("/login", prefix); got != "/browser/browser-1/login" {
		t.Fatalf("rewriteProxyLocation(root-relative) = %q", got)
	}
	if got := rewriteProxyLocation("https://example.com/dashboard?x=1", prefix); got != "/browser/browser-1/dashboard?x=1" {
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
	req := httptest.NewRequest(http.MethodGet, "/browser/"+tab.ID+"/", nil)
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `src="/browser/`+tab.ID+`/assets/app.js"`) {
		t.Fatalf("expected proxied asset path, got %q", body)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("expected X-Frame-Options to be removed, got %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected proxied browser responses to disable cache, got %q", got)
	}
}

func TestRewriteProxyCSSRewritesRootRelativeURLs(t *testing.T) {
	input := []byte(`@font-face{src:url('/cf-fonts/v/inter/normal.woff2')} .hero{background-image:url(/images/hero.png)}`)

	output := string(rewriteProxyCSS(input, "/browser/browser-1/"))

	if !strings.Contains(output, `url('/browser/browser-1/cf-fonts/v/inter/normal.woff2')`) {
		t.Fatalf("expected font URL to be rewritten, got %q", output)
	}
	if !strings.Contains(output, `url(/browser/browser-1/images/hero.png)`) {
		t.Fatalf("expected background URL to be rewritten, got %q", output)
	}
}

func TestRewriteProxyHTMLRewritesInlineStyleContent(t *testing.T) {
	input := []byte(`<!doctype html><html><head></head><body><style>@font-face{src:url('/cf-fonts/v/inter/normal.woff2')} .bg{background:url(/images/hero.png)}</style></body></html>`)

	output, err := rewriteProxyHTML(input, "/browser/browser-1/", "/", "browser-1")
	if err != nil {
		t.Fatalf("rewriteProxyHTML() error = %v", err)
	}
	html := string(output)

	if !strings.Contains(html, `url('/browser/browser-1/cf-fonts/v/inter/normal.woff2')`) {
		t.Fatalf("expected inline style font URL to be rewritten, got %q", html)
	}
	if !strings.Contains(html, `url(/browser/browser-1/images/hero.png)`) {
		t.Fatalf("expected inline style background URL to be rewritten, got %q", html)
	}
}

func TestRewriteProxyJavaScriptRewritesRootRelativeURLs(t *testing.T) {
	input := []byte("const asset=\"/assets/chunk.js\"; import(\"/assets/entry.js\"); const sw=`/sw.js`; navigator.serviceWorker.register(\"/sw.js\",{scope:\"/\"}); const icon='/images/logo.svg'; const preloaded=\"/_next/static/chunk.js\"; const font=\"/cf-fonts/v/inter/normal.woff2\";")

	output := string(rewriteProxyJavaScript(input, "/browser/browser-1/"))

	if !strings.Contains(output, `"/browser/browser-1/assets/chunk.js"`) {
		t.Fatalf("expected asset string rewrite, got %q", output)
	}
	if !strings.Contains(output, `"/browser/browser-1/assets/entry.js"`) {
		t.Fatalf("expected dynamic import rewrite, got %q", output)
	}
	if !strings.Contains(output, "`/browser/browser-1/sw.js`") {
		t.Fatalf("expected template literal service worker rewrite, got %q", output)
	}
	if !strings.Contains(output, `"/browser/browser-1/sw.js"`) {
		t.Fatalf("expected service worker script rewrite, got %q", output)
	}
	if !strings.Contains(output, `'/browser/browser-1/images/logo.svg'`) {
		t.Fatalf("expected single-quoted path rewrite, got %q", output)
	}
	if !strings.Contains(output, `"/browser/browser-1/_next/static/chunk.js"`) {
		t.Fatalf("expected _next path rewrite, got %q", output)
	}
	if !strings.Contains(output, `"/browser/browser-1/cf-fonts/v/inter/normal.woff2"`) {
		t.Fatalf("expected cf-fonts path rewrite, got %q", output)
	}
	// scope:"/" should NOT be rewritten — bare "/" is not a URL path
	if !strings.Contains(output, `scope:"/"`) {
		t.Fatalf("scope:/ must not be rewritten (it's not a URL), got %q", output)
	}
}

func TestRewriteProxyJavaScriptSkipsNonPathStrings(t *testing.T) {
	input := []byte(`const a="//cdn.example.com/file.js"; const b="data:text/plain,hello"; const c="already ok"; const d="mailto:test@test.com"; const e="/"; const f="/x";`)

	output := string(rewriteProxyJavaScript([]byte(input), "/browser/browser-1/"))

	if strings.Contains(output, "/browser/browser-1/cdn.example") {
		t.Fatalf("protocol-relative URL should not be rewritten, got %q", output)
	}
	if strings.Contains(output, "/browser/browser-1/data:") {
		t.Fatalf("data URI should not be rewritten, got %q", output)
	}
	if strings.Contains(output, "/browser/browser-1/mailto:") {
		t.Fatalf("mailto URI should not be rewritten, got %q", output)
	}
	if strings.Contains(output, `/browser/browser-1/"`) {
		t.Fatalf(`bare "/" should not be rewritten, got %q`, output)
	}
}
