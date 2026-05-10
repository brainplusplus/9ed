package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go-webttyd/internal/chat"
	"go-webttyd/internal/shells"
	"go-webttyd/internal/terminal"

	"github.com/gorilla/websocket"
)

func TestRouterReturnsShellProfiles(t *testing.T) {
	api := New(Dependencies{
		Shells:   []shells.Profile{{ID: "bash", Label: "Bash", Command: "/usr/bin/bash"}},
		Sessions: &fakeManager{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/shells", nil)
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRouterCreatesAndDeletesSession(t *testing.T) {
	manager := &fakeManager{}
	api := New(Dependencies{
		Shells:   []shells.Profile{{ID: "bash", Label: "Bash", Command: "bash"}},
		Sessions: manager,
	})

	body, err := json.Marshal(createSessionRequest{ShellID: "bash"})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, createReq)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/sessions/session-1", nil)
	deleteRec := httptest.NewRecorder()
	api.Handler().ServeHTTP(deleteRec, deleteReq)

	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", deleteRec.Code)
	}
	if manager.removedID != "session-1" {
		t.Fatalf("expected removed session id session-1, got %q", manager.removedID)
	}
}

func TestWebSocketDisconnectRemovesSession(t *testing.T) {
	manager := terminal.NewManager(func(profile terminal.ShellProfile) (terminal.PtySession, error) {
		return &blockingSession{id: "session-1", profile: profile, closed: make(chan struct{})}, nil
	})

	session, err := manager.Create(terminal.ShellProfile{ID: "pwsh", Label: "PowerShell 7", Command: "pwsh.exe"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	api := New(Dependencies{Sessions: manager})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/sessions/" + session.ID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := manager.Get(session.ID); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected session to be removed after websocket disconnect")
}

func TestUpgraderRejectsDifferentOrigin(t *testing.T) {
	api := New(Dependencies{Sessions: &fakeManager{}})
	req := httptest.NewRequest(http.MethodGet, "http://localhost/ws/sessions/session-1", nil)
	req.Host = "localhost"
	req.Header.Set("Origin", "http://evil.example")

	if api.upgrader.CheckOrigin(req) {
		t.Fatal("expected foreign origin to be rejected")
	}
}

func TestChatHistoryFiltersByProjectWorkDir(t *testing.T) {
	store := chatTempStoreForAPI(t)
	if err := store.CreateSessionFull("repo-1", "claude", "Repo 1", "/repo", ""); err != nil {
		t.Fatalf("CreateSessionFull repo-1: %v", err)
	}
	if err := store.CreateSessionFull("other-1", "opencode", "Other 1", "/other", ""); err != nil {
		t.Fatalf("CreateSessionFull other-1: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := store.CreateSessionFull("repo-2", "claude", "Repo 2", "/repo", ""); err != nil {
		t.Fatalf("CreateSessionFull repo-2: %v", err)
	}

	api := New(Dependencies{ChatStore: store})
	req := httptest.NewRequest(http.MethodGet, "/api/chat/history?workDir="+url.QueryEscape("/repo"), nil)
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var sessions []chat.SessionRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "repo-2" || sessions[1].ID != "repo-1" {
		t.Fatalf("expected repo-2 then repo-1, got %q then %q", sessions[0].ID, sessions[1].ID)
	}
}

func TestChatSessionStateReturnsRichTranscriptAndSnapshot(t *testing.T) {
	store := chatTempStoreForAPI(t)
	if err := store.CreateSessionFull("session-1", "opencode", "State test", "/repo", "acp-1"); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}
	if err := store.AppendEvent(chat.EventRecord{ID: "evt-1", SessionID: "session-1", Kind: "tool_call", PayloadJSON: `{"toolCall":{"toolCallId":"tc1","title":"edit .env.local.example","kind":"edit","status":"completed"}}`, Seq: 1, Timestamp: 1000}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.SaveSnapshot(chat.SessionSnapshot{SessionID: "session-1", CommandsJSON: `[{"name":"help","description":"Show commands"}]`, ConfigOptsJSON: `[{"id":"model","name":"Model","type":"string","currentValue":"gpt-5","options":[{"value":"gpt-5","name":"GPT-5"}]}]`}); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	api := New(Dependencies{ChatStore: store})
	req := httptest.NewRequest(http.MethodGet, "/api/chat/state/session-1", nil)
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Session  chat.SessionRecord    `json:"session"`
		Events   []chat.EventRecord    `json:"events"`
		Snapshot *chat.SessionSnapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body.Session.ID != "session-1" {
		t.Fatalf("expected session id session-1, got %q", body.Session.ID)
	}
	if len(body.Events) != 1 || body.Events[0].Kind != "tool_call" {
		t.Fatalf("expected one tool_call event, got %#v", body.Events)
	}
	if body.Snapshot == nil || body.Snapshot.CommandsJSON == "" || body.Snapshot.ConfigOptsJSON == "" {
		t.Fatal("expected snapshot with commands and config options")
	}
}

func chatTempStoreForAPI(t *testing.T) *chat.ChatStore {
	t.Helper()
	store, err := chat.NewChatStore(t.TempDir() + "/chat.db")
	if err != nil {
		t.Fatalf("NewChatStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

type fakeManager struct {
	removedID string
}

func (f *fakeManager) Create(profile terminal.ShellProfile) (*terminal.ManagedSession, error) {
	return &terminal.ManagedSession{ID: "session-1", Profile: profile}, nil
}

func (f *fakeManager) Get(id string) (*terminal.ManagedSession, bool) {
	return nil, false
}

func (f *fakeManager) Remove(id string) error {
	f.removedID = id
	return nil
}

type blockingSession struct {
	id      string
	profile terminal.ShellProfile
	closed  chan struct{}
}

func (s *blockingSession) ID() string {
	return s.id
}

func (s *blockingSession) Profile() terminal.ShellProfile {
	return s.profile
}

func (s *blockingSession) Read([]byte) (int, error) {
	<-s.closed
	return 0, io.EOF
}

func (s *blockingSession) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *blockingSession) Resize(cols uint16, rows uint16) error {
	return nil
}

func (s *blockingSession) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}
