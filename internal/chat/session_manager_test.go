package chat

import (
	"context"
	"testing"
	"time"
)

// mockChatSession is a minimal ChatSession implementation for testing.
type mockChatSession struct {
	id     string
	closed bool
}

func (m *mockChatSession) ID() string                             { return m.id }
func (m *mockChatSession) AgentID() string                        { return "" }
func (m *mockChatSession) WorkDir() string                        { return "" }
func (m *mockChatSession) Mode() SessionMode                      { return ModePTY }
func (m *mockChatSession) Events() <-chan ChatEvent               { return nil }
func (m *mockChatSession) Done() <-chan struct{}                  { return nil }
func (m *mockChatSession) Err() error                             { return nil }
func (m *mockChatSession) ACPSessionID() string                   { return "" }
func (m *mockChatSession) IsResumed() bool                        { return false }
func (m *mockChatSession) RespondPermission(_ PermissionResponse) {}
func (m *mockChatSession) SetAutoApprove(_ bool)                  {}
func (m *mockChatSession) SetUseActiveTerminal(_ bool, _ string)  {}
func (m *mockChatSession) UseActiveTerminalEnabled() bool         { return false }
func (m *mockChatSession) ActiveTerminalID() string               { return "" }
func (m *mockChatSession) SetUseActiveBrowser(_ bool, _ string)   {}
func (m *mockChatSession) UseActiveBrowserEnabled() bool          { return false }
func (m *mockChatSession) ActiveBrowserTabID() string             { return "" }
func (m *mockChatSession) SetConfigOption(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockChatSession) Send(_ context.Context, _ string, _ []Attachment) error {
	return nil
}
func (m *mockChatSession) Cancel() error { return nil }
func (m *mockChatSession) Close() error {
	m.closed = true
	return nil
}

func TestGraceWindowImmediateTeardown(t *testing.T) {
	m := NewSessionManager()
	m.SetGraceWindow(0) // Disable grace window.

	session := &mockChatSession{id: "test-session-1"}

	m.mu.Lock()
	m.sessions["test-session-1"] = session
	m.recordIDs["test-session-1"] = "test-session-1"
	m.mu.Unlock()

	m.Remove("test-session-1")

	// Session should be removed immediately.
	if _, ok := m.Get("test-session-1"); ok {
		t.Error("Session should be removed immediately with grace=0")
	}
}

func TestGraceWindowDeferredTeardown(t *testing.T) {
	m := NewSessionManager()
	m.SetGraceWindow(100 * time.Millisecond)

	session := &mockChatSession{id: "test-session-2"}

	m.mu.Lock()
	m.sessions["test-session-2"] = session
	m.recordIDs["test-session-2"] = "test-session-2"
	m.mu.Unlock()

	m.Remove("test-session-2")

	// Session should still exist during grace window (check directly, not via Get
	// which would cancel the grace timer).
	m.mu.Lock()
	_, existsDuringGrace := m.sessions["test-session-2"]
	m.mu.Unlock()
	if !existsDuringGrace {
		t.Error("Session should exist during grace window")
	}

	// Wait for grace window to expire.
	time.Sleep(200 * time.Millisecond)

	// Now session should be gone.
	m.mu.Lock()
	_, existsAfterGrace := m.sessions["test-session-2"]
	m.mu.Unlock()
	if existsAfterGrace {
		t.Error("Session should be removed after grace window expires")
	}
}

func TestGraceWindowCancelledOnReconnect(t *testing.T) {
	m := NewSessionManager()
	m.SetGraceWindow(100 * time.Millisecond)

	session := &mockChatSession{id: "test-session-3"}

	m.mu.Lock()
	m.sessions["test-session-3"] = session
	m.recordIDs["test-session-3"] = "test-session-3"
	m.mu.Unlock()

	m.Remove("test-session-3")

	// Reconnect during grace window (Get cancels the timer).
	if _, ok := m.Get("test-session-3"); !ok {
		t.Fatal("Session should exist during grace window")
	}

	// Wait beyond original grace window.
	time.Sleep(200 * time.Millisecond)

	// Session should still exist because reconnect cancelled the timer.
	if _, ok := m.Get("test-session-3"); !ok {
		t.Error("Session should still exist after reconnect cancelled grace timer")
	}
}

func TestGraceWindowRemoveNow(t *testing.T) {
	m := NewSessionManager()
	m.SetGraceWindow(10 * time.Minute) // Long grace window.

	session := &mockChatSession{id: "test-session-4"}

	m.mu.Lock()
	m.sessions["test-session-4"] = session
	m.recordIDs["test-session-4"] = "test-session-4"
	m.mu.Unlock()

	// RemoveNow should bypass the grace window.
	m.RemoveNow("test-session-4")

	if _, ok := m.Get("test-session-4"); ok {
		t.Error("Session should be removed immediately by RemoveNow")
	}
}

func TestSessionManagerList(t *testing.T) {
	m := NewSessionManager()

	m.mu.Lock()
	m.sessions["s1"] = &mockChatSession{id: "s1"}
	m.sessions["s2"] = &mockChatSession{id: "s2"}
	m.mu.Unlock()

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("Expected 2 sessions, got %d", len(list))
	}
}

func TestSessionManagerIsLive(t *testing.T) {
	m := NewSessionManager()

	m.mu.Lock()
	m.sessions["live-1"] = &mockChatSession{id: "live-1"}
	m.mu.Unlock()

	if !m.IsLive("live-1") {
		t.Error("IsLive should return true for existing session")
	}
	if m.IsLive("nonexistent") {
		t.Error("IsLive should return false for nonexistent session")
	}
}

func TestSessionManagerRecordIDMapping(t *testing.T) {
	m := NewSessionManager()

	// Both session and record ID mapping must be present for LiveIDForRecordID.
	m.mu.Lock()
	m.sessions["live-1"] = &mockChatSession{id: "live-1"}
	m.recordIDs["live-1"] = "record-1"
	m.mu.Unlock()

	if m.RecordIDFor("live-1") != "record-1" {
		t.Errorf("Expected 'record-1', got %q", m.RecordIDFor("live-1"))
	}

	liveID, ok := m.LiveIDForRecordID("record-1")
	if !ok || liveID != "live-1" {
		t.Errorf("Expected live-1, got %q (found=%v)", liveID, ok)
	}
}
