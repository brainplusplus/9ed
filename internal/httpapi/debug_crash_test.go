//go:build debug

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/debug"
)

// crashableFakeSession is a fake ChatSession that also implements
// chat.CrashableSession so the debug crash endpoint can call CrashAgent on it.
type crashableFakeSession struct {
	apiFakeChatSession
	crashMode  string
	crashErr   error
	crashCalls int
}

func (s *crashableFakeSession) CrashAgent(mode string) error {
	s.crashCalls++
	s.crashMode = mode
	return s.crashErr
}

// TestDebugCrashAgent_Sigkill verifies that POST /api/_debug/crash-agent with
// mode "sigkill" calls CrashAgent on the target session. The endpoint is only
// available when built with -tags debug and DEBUG=true is set at runtime.
func TestDebugCrashAgent_Sigkill(t *testing.T) {
	// Enable debug mode at runtime.
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	sess := &crashableFakeSession{
		apiFakeChatSession: apiFakeChatSession{
			id:     "sess-crash-1",
			mode:   chat.ModeACP,
			events: make(chan chat.ChatEvent, 1),
			done:   make(chan struct{}),
		},
	}

	mgr := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{"sess-crash-1": sess},
	}

	api := New(Dependencies{ChatSessionManager: mgr})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "sess-crash-1",
		Mode:      "sigkill",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if sess.crashCalls != 1 {
		t.Errorf("expected CrashAgent called once, got %d", sess.crashCalls)
	}
	if sess.crashMode != "sigkill" {
		t.Errorf("expected crash mode 'sigkill', got %q", sess.crashMode)
	}

	var resp debugCrashAgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK {
		t.Error("expected OK=true in response")
	}
	if resp.Mode != "sigkill" {
		t.Errorf("expected mode 'sigkill' in response, got %q", resp.Mode)
	}
}

// TestDebugCrashAgent_Panic verifies the "panic" crash mode is forwarded.
func TestDebugCrashAgent_Panic(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	sess := &crashableFakeSession{
		apiFakeChatSession: apiFakeChatSession{
			id:     "sess-crash-2",
			mode:   chat.ModeACP,
			events: make(chan chat.ChatEvent, 1),
			done:   make(chan struct{}),
		},
	}

	mgr := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{"sess-crash-2": sess},
	}

	api := New(Dependencies{ChatSessionManager: mgr})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "sess-crash-2",
		Mode:      "panic",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if sess.crashMode != "panic" {
		t.Errorf("expected crash mode 'panic', got %q", sess.crashMode)
	}
}

// TestDebugCrashAgent_UncleanExit verifies the "unclean-exit" mode.
func TestDebugCrashAgent_UncleanExit(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	sess := &crashableFakeSession{
		apiFakeChatSession: apiFakeChatSession{
			id:     "sess-crash-3",
			mode:   chat.ModeACP,
			events: make(chan chat.ChatEvent, 1),
			done:   make(chan struct{}),
		},
	}

	mgr := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{"sess-crash-3": sess},
	}

	api := New(Dependencies{ChatSessionManager: mgr})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "sess-crash-3",
		Mode:      "unclean-exit",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if sess.crashMode != "unclean-exit" {
		t.Errorf("expected crash mode 'unclean-exit', got %q", sess.crashMode)
	}
}

// TestDebugCrashAgent_Returns404WhenDebugDisabled verifies the runtime gate:
// even in a debug-tagged build, the endpoint returns 404 when DEBUG is not set.
func TestDebugCrashAgent_Returns404WhenDebugDisabled(t *testing.T) {
	// Ensure DEBUG is off at runtime.
	debug.Enable(false)

	sess := &crashableFakeSession{
		apiFakeChatSession: apiFakeChatSession{
			id:     "sess-crash-4",
			mode:   chat.ModeACP,
			events: make(chan chat.ChatEvent, 1),
			done:   make(chan struct{}),
		},
	}

	mgr := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{"sess-crash-4": sess},
	}

	api := New(Dependencies{ChatSessionManager: mgr})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "sess-crash-4",
		Mode:      "sigkill",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when DEBUG disabled, got %d", rec.Code)
	}
	if sess.crashCalls != 0 {
		t.Errorf("expected CrashAgent not called, got %d calls", sess.crashCalls)
	}
}

// TestDebugCrashAgent_SessionNotFound returns 404 for an unknown session ID.
func TestDebugCrashAgent_SessionNotFound(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	mgr := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{},
	}

	api := New(Dependencies{ChatSessionManager: mgr})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "nonexistent",
		Mode:      "sigkill",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// TestDebugCrashAgent_MethodNotAllowed verifies GET returns 405.
func TestDebugCrashAgent_MethodNotAllowed(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	api := New(Dependencies{
		ChatSessionManager: &fakeChatRuntimeManager{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/_debug/crash-agent", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestDebugCrashAgent_MissingSessionID verifies a missing sessionId returns 400.
func TestDebugCrashAgent_MissingSessionID(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	api := New(Dependencies{
		ChatSessionManager: &fakeChatRuntimeManager{},
	})

	body, _ := json.Marshal(debugCrashAgentRequest{
		Mode: "sigkill",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestDebugCrashAgent_MissingMode verifies a missing mode returns 400.
func TestDebugCrashAgent_MissingMode(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	api := New(Dependencies{
		ChatSessionManager: &fakeChatRuntimeManager{},
	})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "sess-1",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestDebugCrashAgent_NonCrashableSession verifies that a session that does
// not implement CrashableSession (e.g. a PTY session) returns 409 Conflict.
func TestDebugCrashAgent_NonCrashableSession(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	// apiFakeChatSession does NOT implement CrashableSession.
	sess := &apiFakeChatSession{
		id:     "sess-pty-1",
		mode:   chat.ModePTY,
		events: make(chan chat.ChatEvent, 1),
		done:   make(chan struct{}),
	}

	mgr := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{"sess-pty-1": sess},
	}

	api := New(Dependencies{ChatSessionManager: mgr})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "sess-pty-1",
		Mode:      "sigkill",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for non-crashable session, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDebugCrashAgent_InvalidJSON verifies malformed JSON returns 400.
func TestDebugCrashAgent_InvalidJSON(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	api := New(Dependencies{
		ChatSessionManager: &fakeChatRuntimeManager{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestDebugCrashAgent_CrashErrorReturns500 verifies that when CrashAgent
// returns an error, the endpoint responds with 500.
func TestDebugCrashAgent_CrashErrorReturns500(t *testing.T) {
	debug.Enable(true)
	t.Cleanup(func() { debug.Enable(false) })

	sess := &crashableFakeSession{
		apiFakeChatSession: apiFakeChatSession{
			id:     "sess-crash-err",
			mode:   chat.ModeACP,
			events: make(chan chat.ChatEvent, 1),
			done:   make(chan struct{}),
		},
		crashErr: context.DeadlineExceeded,
	}

	mgr := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{"sess-crash-err": sess},
	}

	api := New(Dependencies{ChatSessionManager: mgr})

	body, _ := json.Marshal(debugCrashAgentRequest{
		SessionID: "sess-crash-err",
		Mode:      "sigkill",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
