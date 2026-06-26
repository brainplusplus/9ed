package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brainplusplus/9ed/internal/chat"
)

// optsCapturingManager wraps fakeChatRuntimeManager and records the
// SessionOptions handed to Create and Resume so tests can assert that
// handleChatResume propagates request fields (e.g. UseActiveBrowser) into the
// session that backs the new/replacement chat session
// (fix-acp-reconnect-resilience / VAL-RESUME-002).
type optsCapturingManager struct {
	fakeChatRuntimeManager
	createOpts chat.SessionOptions
	resumeOpts chat.SessionOptions
}

func (m *optsCapturingManager) Create(_ context.Context, _ chat.AgentDescriptor, _ string, opts chat.SessionOptions) (chat.ChatSession, error) {
	m.createOpts = opts
	return m.fakeChatRuntimeManager.Create(nil, chat.AgentDescriptor{}, "", opts)
}

func (m *optsCapturingManager) Resume(_ context.Context, _ chat.AgentDescriptor, _, _ string, opts chat.SessionOptions) (chat.ChatSession, error) {
	m.resumeOpts = opts
	return m.fakeChatRuntimeManager.Resume(nil, chat.AgentDescriptor{}, "", "", opts)
}

// TestHandleChatResumeBrowserOptsPropagated verifies that handleChatResume
// forwards the request's UseActiveBrowser / ActiveBrowserTabID (and terminal
// fields) into the SessionOptions used to resume the ACP session. When ACP
// resume succeeds, the live session must reflect the requested browser opts
// so activeMCPServersForOptions(opts) registers the browser MCP tools for the
// resumed session (fix-acp-reconnect-resilience item 6 / VAL-RESUME-002).
func TestHandleChatResumeBrowserOptsPropagated(t *testing.T) {
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
	manager := &optsCapturingManager{
		fakeChatRuntimeManager: fakeChatRuntimeManager{
			resumeSession: &apiFakeChatSession{
				id:           "live-resumed",
				agentID:      "opencode",
				workDir:      "/repo",
				acpSessionID: "old-acp",
				mode:         chat.ModeACP,
				events:       make(chan chat.ChatEvent),
				done:         make(chan struct{}),
			},
		},
	}
	api := New(Dependencies{ChatSessionManager: manager, ChatStore: store})

	body, _ := json.Marshal(chatResumeRequest{
		SessionID:          "record-1",
		AgentID:            "opencode",
		WorkDir:            "/repo",
		ACPSessionID:       "old-acp",
		UseActiveTerminal:  true,
		ActiveTerminalID:   "term-7",
		UseActiveBrowser:   true,
		ActiveBrowserTabID: "browser-tab-9",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat/sessions/resume", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if manager.resumeCalls != 1 {
		t.Fatalf("expected 1 resume call, got %d", manager.resumeCalls)
	}
	if !manager.resumeOpts.UseActiveBrowser {
		t.Errorf("expected UseActiveBrowser=true propagated to Resume opts, got false")
	}
	if manager.resumeOpts.ActiveBrowserTabID != "browser-tab-9" {
		t.Errorf("expected ActiveBrowserTabID='browser-tab-9', got %q", manager.resumeOpts.ActiveBrowserTabID)
	}
	if !manager.resumeOpts.UseActiveTerminal {
		t.Errorf("expected UseActiveTerminal=true propagated to Resume opts, got false")
	}
	if manager.resumeOpts.ActiveTerminalID != "term-7" {
		t.Errorf("expected ActiveTerminalID='term-7', got %q", manager.resumeOpts.ActiveTerminalID)
	}
}

// TestHandleChatResumeBrowserOptsPropagatedToFallbackCreate verifies that when
// ACP resume fails and handleChatResume falls back to Create, the browser opts
// from the request still reach the new (replacement) session. This is the
// exact failure mode in fix-acp-reconnect-resilience item 6: a crashed agent
// that cannot be resumed must still get the browser MCP tools registered on
// the replacement session (VAL-RESUME-002).
func TestHandleChatResumeBrowserOptsPropagatedToFallbackCreate(t *testing.T) {
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
	manager := &optsCapturingManager{
		fakeChatRuntimeManager: fakeChatRuntimeManager{
			resumeErr: errSimple("ACP session/resume rejected by agent"),
			createSession: &apiFakeChatSession{
				id:           "live-new",
				agentID:      "opencode",
				workDir:      "/repo",
				acpSessionID: "fresh-acp",
				mode:         chat.ModeACP,
				events:       make(chan chat.ChatEvent),
				done:         make(chan struct{}),
			},
		},
	}
	api := New(Dependencies{ChatSessionManager: manager, ChatStore: store})

	body, _ := json.Marshal(chatResumeRequest{
		SessionID:          "record-1",
		AgentID:            "opencode",
		WorkDir:            "/repo",
		ACPSessionID:       "old-acp",
		UseActiveBrowser:   true,
		ActiveBrowserTabID: "browser-tab-9",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat/sessions/resume", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if manager.resumeCalls != 1 || manager.createCalls != 1 {
		t.Fatalf("expected 1 resume attempt + 1 fallback create, got resume=%d create=%d", manager.resumeCalls, manager.createCalls)
	}
	if !manager.createOpts.UseActiveBrowser {
		t.Errorf("expected UseActiveBrowser=true propagated to fallback Create opts, got false")
	}
	if manager.createOpts.ActiveBrowserTabID != "browser-tab-9" {
		t.Errorf("expected ActiveBrowserTabID='browser-tab-9' on fallback Create opts, got %q", manager.createOpts.ActiveBrowserTabID)
	}
}

// errSimple is a minimal error type used by tests in this file.
type errSimple string

func (e errSimple) Error() string { return string(e) }
