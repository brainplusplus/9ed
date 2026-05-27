package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"log"

	"github.com/brainplusplus/9ed/internal/appmeta"
	"github.com/brainplusplus/9ed/internal/auth"
	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/chat/acp"
	"github.com/brainplusplus/9ed/internal/config"
	"github.com/brainplusplus/9ed/internal/httpapi"
	"github.com/brainplusplus/9ed/internal/shells"
	"github.com/brainplusplus/9ed/internal/terminal"
	"github.com/brainplusplus/9ed/internal/tunnel"
	"github.com/brainplusplus/9ed/internal/watcher"
	"github.com/brainplusplus/9ed/internal/webassets"
)

type Server struct {
	Config          config.Config
	api             *httpapi.API
	hs              *http.Server
	tunnelFn        func() string
	settingsTunnels *tunnel.Manager
	fileWatcher     *watcher.FileWatcher
}

func New(cfg config.Config) *Server {
	profiles := shells.Discover()
	manager := terminal.NewManager(terminal.NewPTYSpawnFunc())

	fw, err := watcher.New()
	if err != nil {
		log.Printf("warning: file watcher unavailable: %v", err)
	}

	chatSessionMgr := chat.NewSessionManager()
	terminalMCPToken := randomToken()
	cwd, _ := os.Getwd()
	chat.SetActiveTerminalMCPServers([]acp.MCPServer{{
		Name:    "9ed-active-terminal",
		Command: "go",
		Args:    []string{"run", filepath.Join(cwd, "cmd", "active-terminal-mcp")},
		Env: []acp.EnvVariable{
			{Name: "NINE_ED_MCP_ENDPOINT", Value: "http://127.0.0.1:" + cfg.Port + "/api/chat/terminal/run"},
			{Name: "NINE_ED_MCP_TOKEN", Value: terminalMCPToken},
		},
	}})

	chatStore, err := chat.NewChatStore(chat.DefaultDBPath())
	if err != nil {
		log.Printf("warning: chat history unavailable: %v", err)
	}

	browserMgr := browser.NewManager()
	settingsTunnels := tunnel.NewManager(chatStore, cfg.Port, cfg.Tunnel, cfg.TunnelEngine)
	if err := settingsTunnels.Load(); err != nil {
		log.Printf("warning: settings tunnels unavailable: %v", err)
	}

	api := httpapi.New(httpapi.Dependencies{
		Shells:             profiles,
		Sessions:           manager,
		WorkspaceRoot:      cfg.WorkspaceRoot,
		TerminalAIMaxLines: cfg.TerminalAIMaxLines,
		Watcher:            fw,
		ChatSessionManager: chatSessionMgr,
		ChatStore:          chatStore,
		Browser:            browserMgr,
		SettingsTunnels:    settingsTunnels,
		TunnelURL:          func() string { return "" }, // placeholder, set via SetTunnel
		TerminalMCPToken:   terminalMCPToken,
	})

	return &Server{
		Config:          cfg,
		api:             api,
		tunnelFn:        func() string { return "" },
		settingsTunnels: settingsTunnels,
		fileWatcher:     fw,
	}
}

func randomToken() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// SetTunnel injects the live tunnel URL provider after startup.
func (s *Server) SetTunnel(tunnelURL func() string) {
	s.tunnelFn = tunnelURL
	s.api.SetTunnelURL(tunnelURL)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", s.api.Handler())
	mux.Handle("/ws/", s.api.Handler())
	mux.Handle("/browser/", s.api.Handler())
	embeddedAssets, hasEmbeddedAssets := webassets.Embedded()
	mux.Handle("/", spaHandler(distDir(), embeddedAssets, hasEmbeddedAssets))

	protected := auth.Middleware(s.Config.BasicAuthUsername, s.Config.BasicAuthPassword)(mux)

	outer := http.NewServeMux()
	outer.Handle("/api/chat/terminal/run", s.api.Handler())
	outer.Handle("/", protected)
	return outer
}

func (s *Server) Addr() string {
	return ":" + s.Config.Port
}

func (s *Server) ListenAndServe() error {
	s.hs = &http.Server{
		Addr:    s.Addr(),
		Handler: s.Handler(),
	}
	return s.hs.ListenAndServe()
}

func (s *Server) Shutdown() {
	if s.settingsTunnels != nil {
		s.settingsTunnels.Shutdown()
	}
	if s.fileWatcher != nil {
		_ = s.fileWatcher.Close()
		s.fileWatcher = nil
	}
	if s.hs != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.hs.Shutdown(ctx)
	}
}

func distDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "dist"
	}
	return filepath.Join(cwd, "dist")
}

func spaHandler(root string, embeddedAssets fs.FS, hasEmbeddedAssets bool) http.Handler {
	if hasEmbeddedAssets && embeddedAssetsAvailable(embeddedAssets) {
		return embeddedSPAHandler(embeddedAssets)
	}

	fileServer := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(root); err != nil {
			http.Error(w, fmt.Sprintf("frontend assets not found in %s; run npm run build", root), http.StatusServiceUnavailable)
			return
		}

		relativePath := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
		requested := filepath.Join(root, relativePath)
		if info, err := os.Stat(requested); err == nil && !info.IsDir() {
			setStaticCacheHeaders(w, requested)
			fileServer.ServeHTTP(w, r)
			return
		}
		if shouldBypassSPAFallback(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		fallbackPath := filepath.Join(root, "ide.html")
		if _, err := os.Stat(fallbackPath); err != nil {
			http.Error(w, fmt.Sprintf("IDE frontend asset not found in %s; run npm run build", fallbackPath), http.StatusServiceUnavailable)
			return
		}

		setStaticCacheHeaders(w, fallbackPath)
		http.ServeFile(w, r, fallbackPath)
	})
}

func embeddedAssetsAvailable(assets fs.FS) bool {
	_, err := fs.Stat(assets, "ide.html")
	return err == nil
}

func embeddedSPAHandler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assetPath := strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(r.URL.Path)), "/")
		if assetPath != "." && assetPath != "" && fs.ValidPath(assetPath) {
			if info, err := fs.Stat(assets, assetPath); err == nil && !info.IsDir() {
				setStaticCacheHeaders(w, assetPath)
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		if shouldBypassSPAFallback(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		fallback := "ide.html"

		setStaticCacheHeaders(w, fallback)
		http.ServeFileFS(w, r, assets, fallback)
	})
}

func shouldBypassSPAFallback(requestPath string) bool {
	cleanPath := path.Clean("/" + strings.TrimSpace(requestPath))
	if strings.HasPrefix(cleanPath, "/assets/") {
		return true
	}
	ext := strings.ToLower(path.Ext(cleanPath))
	return ext != ""
}

func setStaticCacheHeaders(w http.ResponseWriter, path string) {
	assetPath := filepath.ToSlash(path)
	switch strings.ToLower(filepath.Ext(assetPath)) {
	case ".html":
		w.Header().Set("Cache-Control", "no-store")
	default:
		if strings.HasPrefix(assetPath, "assets/") || strings.Contains(assetPath, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
	}
}

func AppVersion() string {
	return appmeta.Version
}
