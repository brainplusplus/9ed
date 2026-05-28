package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/debug"
)

type browserNavigateRequest struct {
	URL       string `json:"url"`
	Transport string `json:"transport,omitempty"`
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
	TabID     string   `json:"tabId,omitempty"`
	Selectors []string `json:"selectors"`
	Name      string   `json:"name,omitempty"`
}

type browserPointRequest struct {
	Selector     string   `json:"selector,omitempty"`
	Direction    string   `json:"direction,omitempty"`
	Text         string   `json:"text,omitempty"`
	Key          string   `json:"key,omitempty"`
	X            *float64 `json:"x,omitempty"`
	Y            *float64 `json:"y,omitempty"`
	DeltaX       float64  `json:"deltaX,omitempty"`
	DeltaY       float64  `json:"deltaY,omitempty"`
	Width        int      `json:"width,omitempty"`
	Height       int      `json:"height,omitempty"`
	URL          string   `json:"url,omitempty"`
	Title        string   `json:"title,omitempty"`
	CanGoBack    bool     `json:"canGoBack,omitempty"`
	CanGoForward bool     `json:"canGoForward,omitempty"`
}

type browserElementScreenshotResponse struct {
	Path     string `json:"path"`
	DataURL  string `json:"dataUrl"`
	MimeType string `json:"mimeType"`
}

type browserMCPDebugResponse struct {
	Enabled bool                    `json:"enabled"`
	Entries []debug.BrowserMCPEntry `json:"entries"`
}

func parseBrowserMultipartUploads(r *http.Request) ([]browser.UploadedFile, error) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		return nil, fmt.Errorf("failed to parse upload: %w", err)
	}
	form := r.MultipartForm
	if form == nil || len(form.File["files"]) == 0 {
		return nil, fmt.Errorf("no files uploaded")
	}
	files := make([]browser.UploadedFile, 0, len(form.File["files"]))
	for _, header := range form.File["files"] {
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		files = append(files, browser.UploadedFile{
			Name:     header.Filename,
			MimeType: header.Header.Get("Content-Type"),
			Buffer:   data,
		})
	}
	return files, nil
}

func materializeBrowserUploads(files []browser.UploadedFile) ([]string, func(), error) {
	tempDir, err := os.MkdirTemp("", "nine-browser-upload-*")
	if err != nil {
		return nil, nil, err
	}
	paths := make([]string, 0, len(files))
	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}
	usedNames := make(map[string]int)
	for idx, file := range files {
		name := filepath.Base(strings.TrimSpace(file.Name))
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = fmt.Sprintf("upload-%d.bin", idx+1)
		}
		if count := usedNames[name]; count > 0 {
			ext := filepath.Ext(name)
			base := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s-%d%s", base, count+1, ext)
		}
		usedNames[name]++
		tempPath := filepath.Join(tempDir, name)
		if err := os.WriteFile(tempPath, file.Buffer, 0o600); err != nil {
			cleanup()
			return nil, nil, err
		}
		paths = append(paths, tempPath)
	}
	return paths, cleanup, nil
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
		transport, err := browser.NormalizeTransport(browser.TransportType(req.Transport))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tab, err := a.browser.CreateTabWithTransport(req.URL, transport)
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
		existing, ok := a.browser.Tab(id)
		if !ok {
			http.Error(w, "browser tab not found", http.StatusNotFound)
			return
		}
		if existing.Transport == string(browser.TransportWebRTC) {
			if _, err := a.browser.TabNavigate(r.Context(), id, req.URL); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			tab, ok := a.browser.Tab(id)
			if !ok {
				http.Error(w, "browser tab not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, tab)
			return
		}
		tab, err := a.browser.NavigateTab(id, req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodPost && action == "sync":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		tab, err := a.browser.SyncTabState(id, req.URL, req.Title, req.CanGoBack, req.CanGoForward)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodPost && action == "back":
		tab, err := a.browser.TabGoBack(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodPost && action == "forward":
		tab, err := a.browser.TabGoForward(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodPost && action == "reload":
		tab, err := a.browser.TabReload(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodPost && action == "stop":
		if err := a.browser.TabStop(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "upload":
		files, err := parseBrowserMultipartUploads(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		paths, cleanup, err := materializeBrowserUploads(files)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer cleanup()
		tab, err := a.browser.TabUploadFilePaths(r.Context(), id, paths)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, tab)
	case r.Method == http.MethodPost && action == "paste":
		files, err := parseBrowserMultipartUploads(r)
		if err != nil && !strings.Contains(err.Error(), "no files uploaded") {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		text := ""
		if r.MultipartForm != nil && len(r.MultipartForm.Value["text"]) > 0 {
			text = r.MultipartForm.Value["text"][0]
		}
		if err := a.browser.TabPasteClipboard(r.Context(), id, text, files); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && action == "screenshot":
		data, err := a.browser.TabScreenshot(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	case r.Method == http.MethodPost && action == "click":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabClick(r.Context(), id, req.Selector, req.X, req.Y); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "mouse-down":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.X == nil || req.Y == nil {
			http.Error(w, "x and y are required", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabMouseDown(r.Context(), id, *req.X, *req.Y); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "mouse-move":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.X == nil || req.Y == nil {
			http.Error(w, "x and y are required", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabMouseMove(r.Context(), id, *req.X, *req.Y); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "mouse-up":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.X == nil || req.Y == nil {
			http.Error(w, "x and y are required", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabMouseUp(r.Context(), id, *req.X, *req.Y); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "type":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabType(r.Context(), id, req.Selector, req.Text); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "press":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabPress(r.Context(), id, req.Key); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "scroll":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabScroll(r.Context(), id, req.DeltaX, req.DeltaY); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && action == "inspect":
		result, err := a.browser.TabInspect(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case r.Method == http.MethodPost && action == "inspect-point":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.X == nil || req.Y == nil {
			http.Error(w, "x and y are required", http.StatusBadRequest)
			return
		}
		result, err := a.browser.TabInspectAtPoint(r.Context(), id, *req.X, *req.Y)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case r.Method == http.MethodPost && action == "inspect-navigate":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Selector) == "" || strings.TrimSpace(req.Direction) == "" {
			http.Error(w, "selector and direction are required", http.StatusBadRequest)
			return
		}
		result, err := a.browser.TabInspectNavigate(r.Context(), id, req.Selector, req.Direction)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case r.Method == http.MethodPost && action == "viewport":
		var req browserPointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := a.browser.TabSetViewport(r.Context(), id, req.Width, req.Height); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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

func (a *API) handleBrowserMCPDebugLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	limit := 80
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 240 {
		limit = 240
	}
	writeJSON(w, http.StatusOK, browserMCPDebugResponse{
		Enabled: debug.BrowserMCPEnabled(),
		Entries: debug.BrowserMCPEntries(limit),
	})
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
	var data []byte
	var err error
	if req.TabID != "" {
		data, err = a.browser.TabElementScreenshot(r.Context(), req.TabID, req.Selectors...)
	} else {
		data, err = a.browser.AutomationElementScreenshot(r.Context(), req.URL, req.Selectors...)
	}
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
