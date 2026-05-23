package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
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
	target, err := a.browser.ProxyTarget(tabID, requestPath, r.URL.RawQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	proxyPrefix := browserProxyPrefix(tabID)
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
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("Pragma", "no-cache")
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Security-Policy-Report-Only")
		resp.Header.Del("Cross-Origin-Opener-Policy")
		resp.Header.Del("Cross-Origin-Embedder-Policy")
		if strings.HasSuffix(resp.Request.URL.Path, "/sw.js") || strings.HasSuffix(resp.Request.URL.Path, "sw.js") {
			resp.Header.Set("Service-Worker-Allowed", proxyPrefix)
		}
		return rewriteProxyResponseBody(resp, proxyPrefix, remotePath, tabID)
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

func (a *API) requireBrowser(w http.ResponseWriter) bool {
	if a.browser == nil {
		http.Error(w, "browser is only available in full mode", http.StatusServiceUnavailable)
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
