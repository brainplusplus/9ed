package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"log"

	"github.com/brainplusplus/9ed/internal/auth"
	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/config"
	"github.com/brainplusplus/9ed/internal/httpapi"
	"github.com/brainplusplus/9ed/internal/shells"
	"github.com/brainplusplus/9ed/internal/terminal"
	"github.com/brainplusplus/9ed/internal/watcher"
)

type Server struct {
	Config config.Config
	api    *httpapi.API
	hs     *http.Server
}

func New(cfg config.Config) *Server {
	profiles := shells.Discover()
	manager := terminal.NewManager(terminal.NewPTYSpawnFunc())

	var fw *watcher.FileWatcher
	if cfg.Mode == "full" {
		var err error
		fw, err = watcher.New()
		if err != nil {
			log.Printf("warning: file watcher unavailable: %v", err)
		}
	}

	var chatSessionMgr *chat.SessionManager
	if cfg.Mode == "full" {
		chatSessionMgr = chat.NewSessionManager()
	}

	var chatStore *chat.ChatStore
	if cfg.Mode == "full" {
		var err error
		chatStore, err = chat.NewChatStore(chat.DefaultDBPath())
		if err != nil {
			log.Printf("warning: chat history unavailable: %v", err)
		}
	}

	var browserMgr *browser.Manager
	if cfg.Mode == "full" && cfg.UseBrowser {
		browserMgr = browser.NewManager()
	}

	api := httpapi.New(httpapi.Dependencies{
		Shells:             profiles,
		Sessions:           manager,
		Mode:               cfg.Mode,
		WorkspaceRoot:      cfg.WorkspaceRoot,
		UseBrowser:         cfg.UseBrowser && cfg.Mode == "full",
		Watcher:            fw,
		ChatSessionManager: chatSessionMgr,
		ChatStore:          chatStore,
		Browser:            browserMgr,
	})

	return &Server{
		Config: cfg,
		api:    api,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", s.api.Handler())
	mux.Handle("/ws/", s.api.Handler())
	mux.Handle("/browser/", s.api.Handler())
	mux.Handle("/", spaHandler(distDir(), s.Config.Mode))

	return auth.Middleware(s.Config.BasicAuthUsername, s.Config.BasicAuthPassword)(mux)
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

func spaHandler(root string, mode string) http.Handler {
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

		fallback := "index.html"
		if mode == "full" {
			idePath := filepath.Join(root, "ide.html")
			if _, err := os.Stat(idePath); err == nil {
				fallback = "ide.html"
			}
		}

		fallbackPath := filepath.Join(root, fallback)
		setStaticCacheHeaders(w, fallbackPath)
		http.ServeFile(w, r, fallbackPath)
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
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html":
		w.Header().Set("Cache-Control", "no-store")
	default:
		if strings.Contains(filepath.ToSlash(path), "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
	}
}
