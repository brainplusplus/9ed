package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/shells"
	"github.com/brainplusplus/9ed/internal/terminal"

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

func TestFileDrivesIncludesWorkspaceRootVolume(t *testing.T) {
	volume := filepath.VolumeName(`D:\workspace`)
	if volume == "" {
		t.Skip("volume roots only apply on Windows")
	}
	api := New(Dependencies{Mode: "full", WorkspaceRoot: `D:\workspace`})
	req := httptest.NewRequest(http.MethodGet, "/api/files/drives", nil)
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var drives []string
	if err := json.Unmarshal(rec.Body.Bytes(), &drives); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	found := false
	for _, drive := range drives {
		if drive == `D:\` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected workspace root drive in list, got %#v", drives)
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

func TestChatHistoryPostRefreshesExistingSessionProjectMetadata(t *testing.T) {
	store := chatTempStoreForAPI(t)
	if err := store.CreateSessionFull("session-1", "opencode", "Old", "", ""); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}

	api := New(Dependencies{ChatStore: store})
	body := strings.NewReader(`{
		"sessionId":"session-1",
		"agentId":"opencode",
		"title":"Latest",
		"workDir":"/repo",
		"acpSessionId":"acp-1",
		"role":"user",
		"content":"hi"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/chat/history", body)
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	record, err := store.GetSession("session-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record == nil {
		t.Fatal("expected session record")
	}
	if record.WorkDir != "/repo" || record.ACPSessionID != "acp-1" {
		t.Fatalf("expected refreshed metadata, got workDir=%q acp=%q", record.WorkDir, record.ACPSessionID)
	}
	sessions, err := store.SessionsForProject("/repo", 10)
	if err != nil {
		t.Fatalf("SessionsForProject: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" {
		t.Fatalf("expected session-1 in project history, got %#v", sessions)
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

func TestChatEventPersisterSavesAssistantMessageOnDone(t *testing.T) {
	store := chatTempStoreForAPI(t)
	if err := store.CreateSessionFull("record-1", "opencode", "Persist test", "/repo", "acp-1"); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}
	manager := chat.NewSessionManager()
	manager.LinkRecordID("live-1", "record-1")
	api := New(Dependencies{ChatSessionManager: manager, ChatStore: store})

	persist := api.newChatEventPersister("live-1")
	persist(chat.ChatEvent{Type: "text", Text: "ya"})
	persist(chat.ChatEvent{Type: "session_info", ContextWindow: 200000, ContextUsed: 44000, CostAmount: 0.0123, CostCurrency: "USD"})
	persist(chat.ChatEvent{Type: "done", StopReason: "end_turn"})

	messages, err := store.GetMessages("record-1")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "assistant" || messages[0].Content != "ya" {
		t.Fatalf("expected persisted assistant message, got %#v", messages)
	}

	events, err := store.GetEvents("record-1")
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 transcript events, got %d: %#v", len(events), events)
	}
}

func TestChatEventPersisterSavesAssistantDraftBeforeDone(t *testing.T) {
	store := chatTempStoreForAPI(t)
	if err := store.CreateSessionFull("record-draft", "opencode", "Draft test", "/repo", "acp-1"); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}
	manager := chat.NewSessionManager()
	manager.LinkRecordID("live-draft", "record-draft")
	api := New(Dependencies{ChatSessionManager: manager, ChatStore: store})

	persist := api.newChatEventPersister("live-draft")
	persist(chat.ChatEvent{Type: "text", Text: "ya"})

	messages, err := store.GetMessages("record-draft")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "assistant" || messages[0].Content != "ya" {
		t.Fatalf("expected assistant draft before done, got %#v", messages)
	}

	persist(chat.ChatEvent{Type: "text", Text: " lanjut"})
	messages, err = store.GetMessages("record-draft")
	if err != nil {
		t.Fatalf("GetMessages after append: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "ya lanjut" {
		t.Fatalf("expected updated single assistant draft, got %#v", messages)
	}
}

func TestChatResumeFallsBackToReplacementSessionWhenACPResumeFails(t *testing.T) {
	withChatAgents(t, []chat.AgentDescriptor{{
		ID:          "opencode",
		Label:       "OpenCode",
		Command:     "opencode",
		Available:   true,
		SupportsACP: true,
	}})
	store := chatTempStoreForAPI(t)
	if err := store.CreateSessionFull("record-1", "opencode", "Old chat", "/repo", "old-acp"); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}
	manager := &fakeChatRuntimeManager{
		resumeErr: errors.New("old ACP session disappeared"),
		createSession: &apiFakeChatSession{
			id:           "live-new",
			agentID:      "opencode",
			workDir:      "/repo",
			acpSessionID: "fresh-acp",
			mode:         chat.ModeACP,
			events:       make(chan chat.ChatEvent),
			done:         make(chan struct{}),
		},
	}
	api := New(Dependencies{ChatSessionManager: manager, ChatStore: store})

	body, _ := json.Marshal(chatResumeRequest{
		SessionID:    "record-1",
		AgentID:      "opencode",
		WorkDir:      "/repo",
		ACPSessionID: "old-acp",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat/sessions/resume", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp chatCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.ID != "live-new" || resp.IsResumed {
		t.Fatalf("expected replacement live session, got %#v", resp)
	}
	if manager.resumeCalls != 1 || manager.createCalls != 1 {
		t.Fatalf("expected one resume attempt and one fallback create, got resume=%d create=%d", manager.resumeCalls, manager.createCalls)
	}
	if manager.recordIDs["live-new"] != "record-1" {
		t.Fatalf("expected live-new linked to record-1, got %#v", manager.recordIDs)
	}
	record, err := store.GetSession("record-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.ACPSessionID != "fresh-acp" {
		t.Fatalf("expected stored ACP session to refresh to fresh-acp, got %q", record.ACPSessionID)
	}
}

func TestChatResumeCreatesReplacementWhenACPSessionIDMissing(t *testing.T) {
	withChatAgents(t, []chat.AgentDescriptor{{
		ID:          "opencode",
		Label:       "OpenCode",
		Command:     "opencode",
		Available:   true,
		SupportsACP: true,
	}})
	store := chatTempStoreForAPI(t)
	if err := store.CreateSessionFull("record-2", "opencode", "Old chat", "/repo", ""); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}
	manager := &fakeChatRuntimeManager{
		createSession: &apiFakeChatSession{
			id:           "live-created",
			agentID:      "opencode",
			workDir:      "/repo",
			acpSessionID: "created-acp",
			mode:         chat.ModeACP,
			events:       make(chan chat.ChatEvent),
			done:         make(chan struct{}),
		},
	}
	api := New(Dependencies{ChatSessionManager: manager, ChatStore: store})

	body, _ := json.Marshal(chatResumeRequest{
		SessionID: "record-2",
		AgentID:   "opencode",
		WorkDir:   "/repo",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat/sessions/resume", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp chatCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.ID != "live-created" || resp.ACPSessionID != "created-acp" {
		t.Fatalf("expected created replacement session, got %#v", resp)
	}
	if manager.resumeCalls != 0 || manager.createCalls != 1 {
		t.Fatalf("expected create without resume attempt, got resume=%d create=%d", manager.resumeCalls, manager.createCalls)
	}
}

func withChatAgents(t *testing.T, agents []chat.AgentDescriptor) {
	t.Helper()
	original := discoverAgentDescriptors
	discoverAgentDescriptors = func() []chat.AgentDescriptor {
		return agents
	}
	t.Cleanup(func() {
		discoverAgentDescriptors = original
	})
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

type fakeChatRuntimeManager struct {
	resumeSession chat.ChatSession
	resumeErr     error
	createSession chat.ChatSession
	createErr     error
	sessions      map[string]chat.ChatSession
	recordIDs     map[string]string
	resumeCalls   int
	createCalls   int
}

func (m *fakeChatRuntimeManager) Create(_ context.Context, _ chat.AgentDescriptor, _ string) (chat.ChatSession, error) {
	m.createCalls++
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.sessions == nil {
		m.sessions = make(map[string]chat.ChatSession)
	}
	if m.createSession != nil {
		m.sessions[m.createSession.ID()] = m.createSession
	}
	return m.createSession, nil
}

func (m *fakeChatRuntimeManager) Resume(_ context.Context, _ chat.AgentDescriptor, _ string, _ string) (chat.ChatSession, error) {
	m.resumeCalls++
	if m.resumeErr != nil {
		return nil, m.resumeErr
	}
	if m.sessions == nil {
		m.sessions = make(map[string]chat.ChatSession)
	}
	if m.resumeSession != nil {
		m.sessions[m.resumeSession.ID()] = m.resumeSession
	}
	return m.resumeSession, nil
}

func (m *fakeChatRuntimeManager) Get(id string) (chat.ChatSession, bool) {
	session, ok := m.sessions[id]
	return session, ok
}

func (m *fakeChatRuntimeManager) Remove(id string) {
	delete(m.sessions, id)
	delete(m.recordIDs, id)
}

func (m *fakeChatRuntimeManager) List() []chat.ChatSession {
	list := make([]chat.ChatSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		list = append(list, session)
	}
	return list
}

func (m *fakeChatRuntimeManager) IsLive(id string) bool {
	if _, ok := m.sessions[id]; ok {
		return true
	}
	for _, recordID := range m.recordIDs {
		if recordID == id {
			return true
		}
	}
	return false
}

func (m *fakeChatRuntimeManager) LinkRecordID(liveSessionID, recordID string) {
	if m.recordIDs == nil {
		m.recordIDs = make(map[string]string)
	}
	m.recordIDs[liveSessionID] = recordID
}

func (m *fakeChatRuntimeManager) RecordIDFor(liveSessionID string) string {
	if recordID := m.recordIDs[liveSessionID]; recordID != "" {
		return recordID
	}
	return liveSessionID
}

func (m *fakeChatRuntimeManager) LiveIDForRecordID(recordID string) (string, bool) {
	if _, ok := m.sessions[recordID]; ok {
		return recordID, true
	}
	for liveID, mappedRecordID := range m.recordIDs {
		if mappedRecordID == recordID {
			if _, ok := m.sessions[liveID]; ok {
				return liveID, true
			}
		}
	}
	return "", false
}

type apiFakeChatSession struct {
	id           string
	agentID      string
	workDir      string
	acpSessionID string
	mode         chat.SessionMode
	events       chan chat.ChatEvent
	done         chan struct{}
}

func (s *apiFakeChatSession) ID() string                                            { return s.id }
func (s *apiFakeChatSession) AgentID() string                                       { return s.agentID }
func (s *apiFakeChatSession) WorkDir() string                                       { return s.workDir }
func (s *apiFakeChatSession) Mode() chat.SessionMode                                { return s.mode }
func (s *apiFakeChatSession) Events() <-chan chat.ChatEvent                         { return s.events }
func (s *apiFakeChatSession) Done() <-chan struct{}                                 { return s.done }
func (s *apiFakeChatSession) Send(context.Context, string) error                    { return nil }
func (s *apiFakeChatSession) Cancel() error                                         { return nil }
func (s *apiFakeChatSession) Close() error                                          { close(s.done); return nil }
func (s *apiFakeChatSession) SetConfigOption(context.Context, string, string) error { return nil }
func (s *apiFakeChatSession) ACPSessionID() string                                  { return s.acpSessionID }
func (s *apiFakeChatSession) IsResumed() bool                                       { return false }
func (s *apiFakeChatSession) RespondPermission(chat.PermissionResponse)             {}
func (s *apiFakeChatSession) SetAutoApprove(bool)                                   {}

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
