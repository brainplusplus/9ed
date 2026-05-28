package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/shells"
	"github.com/brainplusplus/9ed/internal/terminal"
	"github.com/brainplusplus/9ed/internal/tunnel"
	"github.com/brainplusplus/9ed/internal/watcher"

	"github.com/gorilla/websocket"
)

type SessionManager interface {
	Create(profile terminal.ShellProfile) (*terminal.ManagedSession, error)
	Get(id string) (*terminal.ManagedSession, bool)
	Remove(id string) error
}

type ChatRuntimeManager interface {
	Create(context.Context, chat.AgentDescriptor, string, chat.SessionOptions) (chat.ChatSession, error)
	Resume(context.Context, chat.AgentDescriptor, string, string, chat.SessionOptions) (chat.ChatSession, error)
	Get(string) (chat.ChatSession, bool)
	Remove(string)
	List() []chat.ChatSession
	IsLive(string) bool
	LinkRecordID(string, string)
	RecordIDFor(string) string
	LiveIDForRecordID(string) (string, bool)
}

type Dependencies struct {
	Shells             []shells.Profile
	Sessions           SessionManager
	WorkspaceRoot      string
	TerminalAIMaxLines int
	Watcher            *watcher.FileWatcher
	ChatSessionManager ChatRuntimeManager
	ChatStore          *chat.ChatStore
	Browser            *browser.Manager
	SettingsTunnels    *tunnel.Manager
	TunnelURL          func() string // returns current tunnel URL (may change on restart)
	TerminalMCPToken   string
	BrowserMCPToken    string
}

type API struct {
	shells             []shells.Profile
	sessions           SessionManager
	upgrader           websocket.Upgrader
	workspaceRoot      string
	terminalAiMaxLines int
	watcher            *watcher.FileWatcher
	chatSessionManager ChatRuntimeManager
	chatStore          *chat.ChatStore
	chatStreams        *chatStreamRegistry
	browser            *browser.Manager
	settingsTunnels    *tunnel.Manager
	tunnelURL          func() string
	terminalMCPToken   string
	browserMCPToken    string
	terminalRunMu      sync.Mutex
	terminalRuns       map[string]time.Time
}

func New(deps Dependencies) *API {
	return &API{
		shells:             deps.Shells,
		sessions:           deps.Sessions,
		upgrader:           websocket.Upgrader{CheckOrigin: sameOrigin},
		workspaceRoot:      deps.WorkspaceRoot,
		terminalAiMaxLines: deps.TerminalAIMaxLines,
		watcher:            deps.Watcher,
		chatSessionManager: deps.ChatSessionManager,
		chatStore:          deps.ChatStore,
		chatStreams:        newChatStreamRegistry(),
		browser:            deps.Browser,
		settingsTunnels:    deps.SettingsTunnels,
		tunnelURL:          deps.TunnelURL,
		terminalMCPToken:   deps.TerminalMCPToken,
		browserMCPToken:    deps.BrowserMCPToken,
		terminalRuns:       make(map[string]time.Time),
	}
}

// SetTunnelURL updates the tunnel URL provider (called after tunnel starts).
func (a *API) SetTunnelURL(fn func() string) {
	a.tunnelURL = fn
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/shells", a.handleShells)
	mux.HandleFunc("/api/sessions", a.handleSessions)
	mux.HandleFunc("/api/sessions/", a.handleSessionByID)
	mux.HandleFunc("/ws/sessions/", a.handleSessionWebSocket)
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/files/drives", a.handleFileDrives)
	mux.HandleFunc("/api/files/tree", a.handleFileTree)
	mux.HandleFunc("/api/files/content", a.handleFileContent)
	mux.HandleFunc("/api/files/create", a.handleFileCreate)
	mux.HandleFunc("/api/files/rename", a.handleFileRename)
	mux.HandleFunc("/api/files/copy", a.handleFileCopy)
	mux.HandleFunc("/api/files/move", a.handleFileMove)
	mux.HandleFunc("/api/files/search", a.handleFileSearch)
	mux.HandleFunc("/api/files/download", a.handleFileDownload)
	mux.HandleFunc("/api/files/upload", a.handleFileUpload)
	mux.HandleFunc("/api/files", a.handleFileDelete)
	mux.HandleFunc("/ws/watch", a.handleFileWatch)

	mux.HandleFunc("/api/chat/agents", a.handleChatAgents)
	mux.HandleFunc("/api/chat/sessions", a.handleChatSessions)
	mux.HandleFunc("/api/chat/sessions/restore", a.handleChatRestore)
	mux.HandleFunc("/api/chat/sessions/resume", a.handleChatResume)
	mux.HandleFunc("/api/chat/sessions/", a.handleChatSessionByID)
	mux.HandleFunc("/api/chat/history", a.handleChatHistory)
	mux.HandleFunc("/api/chat/history/", a.handleChatHistoryByID)
	mux.HandleFunc("/api/chat/state/", a.handleChatStateByID)
	mux.HandleFunc("/api/chat/install-acp", a.handleChatInstallACP)
	mux.HandleFunc("/api/chat/terminal/run", a.handleChatTerminalRun)
	mux.HandleFunc("/api/chat/browser/run", a.handleChatBrowserRun)
	mux.HandleFunc("/ws/chat/", a.handleChatWebSocket)

	mux.HandleFunc("/api/browser/state", a.handleBrowserState)
	mux.HandleFunc("/api/browser/tabs", a.handleBrowserTabs)
	mux.HandleFunc("/api/browser/tabs/", a.handleBrowserTabByID)
	mux.HandleFunc("/api/browser/proxy/", a.handleBrowserProxy)
	mux.HandleFunc("/browser/", a.handleBrowserProxy)
	mux.HandleFunc("/api/browser/debug/mcp", a.handleBrowserMCPDebugLog)
	mux.HandleFunc("/api/browser/automation/status", a.handleBrowserAutomationStatus)
	mux.HandleFunc("/api/browser/automation/start", a.handleBrowserAutomationStart)
	mux.HandleFunc("/api/browser/automation/navigate", a.handleBrowserAutomationNavigate)
	mux.HandleFunc("/api/browser/automation/click", a.handleBrowserAutomationClick)
	mux.HandleFunc("/api/browser/automation/type", a.handleBrowserAutomationType)
	mux.HandleFunc("/api/browser/automation/evaluate", a.handleBrowserAutomationEvaluate)
	mux.HandleFunc("/api/browser/automation/inspect", a.handleBrowserAutomationInspect)
	mux.HandleFunc("/api/browser/automation/element-screenshot", a.handleBrowserAutomationElementScreenshot)
	mux.HandleFunc("/api/browser/automation/screenshot", a.handleBrowserAutomationScreenshot)

	mux.HandleFunc("/api/projects/recent", a.handleRecentProjects)
	mux.HandleFunc("/api/workspace/state", a.handleWorkspaceState)
	mux.HandleFunc("/api/settings/about", a.handleSettingsAbout)
	mux.HandleFunc("/api/settings/tunnels", a.handleSettingsTunnels)
	mux.HandleFunc("/api/settings/tunnels/", a.handleSettingsTunnelByID)

	mux.HandleFunc("/api/git/status", a.handleGitStatus)
	mux.HandleFunc("/api/git/log", a.handleGitLog)
	mux.HandleFunc("/api/git/branches", a.handleGitBranches)
	mux.HandleFunc("/api/git/stage", a.handleGitStage)
	mux.HandleFunc("/api/git/unstage", a.handleGitUnstage)
	mux.HandleFunc("/api/git/commit", a.handleGitCommit)
	mux.HandleFunc("/api/git/push", a.handleGitPush)
	mux.HandleFunc("/api/git/pull", a.handleGitPull)
	mux.HandleFunc("/api/git/branch", a.handleGitBranch)
	mux.HandleFunc("/api/git/merge", a.handleGitMerge)
	mux.HandleFunc("/api/git/stash", a.handleGitStash)
	mux.HandleFunc("/api/git/diff", a.handleGitDiff)
	mux.HandleFunc("/api/git/diff-lines", a.handleGitDiffLines)
	mux.HandleFunc("/api/git/blame", a.handleGitBlame)
	mux.HandleFunc("/api/git/discard", a.handleGitDiscard)
	mux.HandleFunc("/api/git/file-at-head", a.handleGitFileAtHEAD)
	mux.HandleFunc("/api/git/files", a.handleGitFiles)

	return mux
}

func (a *API) handleShells(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, a.shells)
}

func (a *API) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	profile, ok := a.lookupShell(req.ShellID)
	if !ok {
		http.Error(w, "unknown shell profile", http.StatusBadRequest)
		return
	}

	session, err := a.sessions.Create(terminal.ShellProfile{
		ID:      profile.ID,
		Label:   profile.Label,
		Command: profile.Command,
		Args:    profile.Args,
		CWD:     req.CWD,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, createSessionResponse{ID: session.ID, Profile: session.Profile})
}

func (a *API) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodDelete {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if err := a.sessions.Remove(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleSessionWebSocket(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/ws/sessions/")
	session, ok := a.sessions.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var once sync.Once
	writeError := func(message string) {
		once.Do(func() {
			_ = conn.WriteJSON(wsOutboundMessage{Type: "error", Data: message})
		})
	}

	includeReplay := r.URL.Query().Get("replay") != "0"
	output, unsubscribe := session.Subscribe(includeReplay)
	defer unsubscribe()

	go func() {
		for data := range output {
			if err := conn.WriteJSON(wsOutboundMessage{Type: "output", Data: string(data)}); err != nil {
				return
			}
		}
	}()

	for {
		var message wsInboundMessage
		if err := conn.ReadJSON(&message); err != nil {
			return
		}

		switch message.Type {
		case "input":
			if _, err := session.Write([]byte(message.Data)); err != nil {
				writeError(err.Error())
				return
			}
		case "resize":
			if err := session.Resize(message.Cols, message.Rows); err != nil {
				writeError(err.Error())
				return
			}
		default:
			writeError("unsupported websocket message type")
			return
		}
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}

	return strings.EqualFold(originURL.Host, r.Host)
}

func (a *API) lookupShell(id string) (shells.Profile, bool) {
	for _, profile := range a.shells {
		if profile.ID == id {
			return profile, true
		}
	}
	return shells.Profile{}, false
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
