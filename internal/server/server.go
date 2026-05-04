package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"log"

	"go-webttyd/internal/auth"
	"go-webttyd/internal/chat"
	"go-webttyd/internal/config"
	"go-webttyd/internal/httpapi"
	"go-webttyd/internal/shells"
	"go-webttyd/internal/terminal"
	"go-webttyd/internal/watcher"
)

type Server struct {
	Config config.Config
	api    *httpapi.API
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

	api := httpapi.New(httpapi.Dependencies{
		Shells:             profiles,
		Sessions:           manager,
		Mode:               cfg.Mode,
		WorkspaceRoot:      cfg.WorkspaceRoot,
		Watcher:            fw,
		ChatSessionManager: chatSessionMgr,
		ChatStore:          chatStore,
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
	mux.Handle("/", spaHandler(distDir(), s.Config.Mode))

	return auth.Middleware(s.Config.BasicAuthUsername, s.Config.BasicAuthPassword)(mux)
}

func (s *Server) Addr() string {
	return ":" + s.Config.Port
}

func (s *Server) ListenAndServe() error {
	return http.ListenAndServe(s.Addr(), s.Handler())
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
			fileServer.ServeHTTP(w, r)
			return
		}

		fallback := "index.html"
		if mode == "full" {
			idePath := filepath.Join(root, "ide.html")
			if _, err := os.Stat(idePath); err == nil {
				fallback = "ide.html"
			}
		}

		http.ServeFile(w, r, filepath.Join(root, fallback))
	})
}
