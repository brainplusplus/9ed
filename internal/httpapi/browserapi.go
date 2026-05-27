package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type browserNavigateRequest struct {
	URL string `json:"url"`
}

type browserSelectorRequest struct {
	Selector string `json:"selector"`
	Text     string `json:"text,omitempty"`
}

type browserEvaluateRequest struct {
	Expression string `json:"expression"`
}

type browserElementScreenshotRequest struct {
	URL       string   `json:"url"`
	Selectors []string `json:"selectors"`
	Name      string   `json:"name,omitempty"`
}

type browserElementScreenshotResponse struct {
	Path     string `json:"path"`
	DataURL  string `json:"dataUrl"`
	MimeType string `json:"mimeType"`
}

func (a *API) handleBrowserState(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, a.browser.State())
}

func (a *API) handleBrowserTabs(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.browser.ListTabs())
	case http.MethodPost:
		var req browserNavigateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		tab, err := a.browser.CreateTab(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, tab)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleBrowserTabByID(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	id, action := splitBrowserTabPath(strings.TrimPrefix(r.URL.Path, "/api/browser/tabs/"))
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodPost && action == "activate":
		tab, err := a.browser.ActivateTab(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodPost && action == "navigate":
		var req browserNavigateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		tab, err := a.browser.NavigateTab(id, req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodDelete && action == "":
		if err := a.browser.DeleteTab(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleBrowserProxy(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}

	rest := browserProxyRest(r.URL.Path)
	tabID, requestPath := splitBrowserProxyPath(rest)
	if tabID == "" {
		http.NotFound(w, r)
		return
	}
	a.serveBrowserProxy(w, r, tabID, requestPath)
}

func (a *API) serveBrowserProxy(w http.ResponseWriter, r *http.Request, tabID string, requestPath string) {
	var target *url.URL
	var err error
	responsePrefix := browserProxyPrefix(tabID)
	if external, ok := parseBrowserExternalProxyPath(requestPath); ok {
		target, err = a.browser.ProxyExternalTarget(tabID, external.scheme, external.host, external.path, r.URL.RawQuery)
		responsePrefix = browserExternalProxyPrefix(tabID, external.scheme, external.host)
	} else {
		target, err = a.browser.ProxyTarget(tabID, requestPath, r.URL.RawQuery)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	remotePath := target.Path
	if remotePath == "" {
		remotePath = "/"
	}
	if target.RawQuery != "" {
		remotePath += "?" + target.RawQuery
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = target.Path
		req.URL.RawQuery = target.RawQuery
		req.Host = target.Host
		req.Header.Del("Accept-Encoding")
		req.Header.Del("Origin")
		req.Header.Del("Referer")
		req.Header.Del("Cookie")
		if cookieHeader := a.browser.CookieHeader(tabID, target.Host); cookieHeader != "" {
			req.Header.Set("Cookie", cookieHeader)
		}
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		a.browser.StoreCookies(tabID, resp.Request.URL.Host, resp.Cookies())
		resp.Header.Del("Set-Cookie")
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("Pragma", "no-cache")
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Security-Policy-Report-Only")
		resp.Header.Del("Cross-Origin-Opener-Policy")
		resp.Header.Del("Cross-Origin-Embedder-Policy")
		if strings.HasSuffix(resp.Request.URL.Path, "/sw.js") || strings.HasSuffix(resp.Request.URL.Path, "sw.js") {
			resp.Header.Set("Service-Worker-Allowed", responsePrefix)
		}
		return rewriteProxyResponseBody(resp, responsePrefix, remotePath, tabID)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func (a *API) handleBrowserAutomationStatus(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, a.browser.AutomationStatus())
}

func (a *API) handleBrowserAutomationStart(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	status, err := a.browser.StartAutomation(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, status)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) handleBrowserAutomationNavigate(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var req browserNavigateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result, err := a.browser.AutomationNavigate(r.Context(), req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) handleBrowserAutomationClick(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var req browserSelectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := a.browser.AutomationClick(r.Context(), req.Selector); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleBrowserAutomationType(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var req browserSelectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := a.browser.AutomationType(r.Context(), req.Selector, req.Text); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleBrowserAutomationEvaluate(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var req browserEvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result, err := a.browser.AutomationEvaluate(r.Context(), req.Expression)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (a *API) handleBrowserAutomationInspect(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	result, err := a.browser.AutomationInspect(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) handleBrowserAutomationScreenshot(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	data, err := a.browser.AutomationScreenshot(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Captured-At", time.Now().Format(time.RFC3339))
	_, _ = w.Write(data)
}

func (a *API) handleBrowserAutomationElementScreenshot(w http.ResponseWriter, r *http.Request) {
	if !a.requireBrowser(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req browserElementScreenshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	data, err := a.browser.AutomationElementScreenshot(r.Context(), req.URL, req.Selectors...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	path, err := saveBrowserCapture(req.Name, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, browserElementScreenshotResponse{
		Path:     path,
		DataURL:  "data:image/png;base64," + base64.StdEncoding.EncodeToString(data),
		MimeType: "image/png",
	})
}

func (a *API) requireBrowser(w http.ResponseWriter) bool {
	if a.browser == nil {
		http.Error(w, "browser is disabled", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func splitBrowserTabPath(rest string) (string, string) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return "", ""
	}
	id, action, found := strings.Cut(rest, "/")
	if !found {
		return id, ""
	}
	return id, strings.Trim(action, "/")
}

func browserProxyRest(requestPath string) string {
	if strings.HasPrefix(requestPath, "/browser/") {
		return strings.TrimPrefix(requestPath, "/browser/")
	}
	return strings.TrimPrefix(requestPath, "/api/browser/proxy/")
}

type browserExternalProxyPath struct {
	scheme string
	host   string
	path   string
}

func parseBrowserExternalProxyPath(requestPath string) (browserExternalProxyPath, bool) {
	rest := strings.TrimPrefix(requestPath, "/_proxy/")
	if rest == requestPath {
		return browserExternalProxyPath{}, false
	}
	scheme, rest, found := strings.Cut(rest, "/")
	if !found {
		return browserExternalProxyPath{}, false
	}
	escapedHost, rest, found := strings.Cut(rest, "/")
	if !found {
		rest = ""
	}
	host, err := url.PathUnescape(escapedHost)
	if err != nil {
		return browserExternalProxyPath{}, false
	}
	targetPath := "/" + strings.TrimLeft(rest, "/")
	if targetPath == "/" && found {
		targetPath = "/"
	}
	return browserExternalProxyPath{
		scheme: scheme,
		host:   host,
		path:   targetPath,
	}, true
}

func splitBrowserProxyPath(rest string) (string, string) {
	rest = strings.TrimLeft(rest, "/")
	if rest == "" {
		return "", "/"
	}
	id, requestPath, found := strings.Cut(rest, "/")
	if !found {
		return id, "/"
	}
	return id, "/" + requestPath
}

func saveBrowserCapture(name string, data []byte) (string, error) {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "browser-selection"
	}
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, base)

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "."
	}
	dir := filepath.Join(home, ".9ed", "browser-captures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, time.Now().UnixMilli()))
	if runtime.GOOS == "windows" {
		path = filepath.Clean(path)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
