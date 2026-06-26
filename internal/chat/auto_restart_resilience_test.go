package chat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat/acp"
)

// TestTryRestartPreservesBrowserMCP verifies that tryRestart rebuilds the
// ACP adapter config from the session's CURRENT sessionOpts — not a stale
// snapshot — so that an active browser MCP toggle is honored when the agent
// subprocess is re-spawned after a crash (VAL-RESUME-001).
//
// The test substitutes newAdapterForRestart with a fake that records the
// acp.AdapterConfig.MCPServers it was handed. With UseActiveBrowser=true in
// sessionOpts and an active browser MCP server registered, the rebuilt config
// must include the browser server. This guards against regressions where
// tryRestart used a stale/captured opts snapshot that dropped the browser
// toggle the user enabled after session creation.
func TestTryRestartPreservesBrowserMCP(t *testing.T) {
	// Register an active browser MCP server so activeMCPServersForOptions
	// produces a non-empty browser slice when UseActiveBrowser is true.
	browserSrv := acp.MCPServer{Name: "test-browser-mcp", Command: "echo"}
	SetActiveBrowserMCPServers([]acp.MCPServer{browserSrv})
	t.Cleanup(func() { SetActiveBrowserMCPServers(nil) })

	// Fake adapter that: supports resume, reports a transient crash error,
	// and succeeds on ResumeSession. Done() never closes during the test.
	fakeAdapter := &resumeSucceedFakeAdapter{
		sessionID: "acp-sess-1",
	}

	var capturedCfgs []acp.AdapterConfig
	var cfgMu sync.Mutex
	origCtor := newAdapterForRestart
	newAdapterForRestart = func(_ context.Context, _ string, cfg acp.AdapterConfig) (acp.Adapter, error) {
		cfgMu.Lock()
		// Copy MCPServers so later mutation cannot affect the captured slice.
		servers := make([]acp.MCPServer, len(cfg.MCPServers))
		copy(servers, cfg.MCPServers)
		capturedCfgs = append(capturedCfgs, acp.AdapterConfig{
			Command:    cfg.Command,
			WorkDir:    cfg.WorkDir,
			MCPServers: servers,
		})
		cfgMu.Unlock()
		return fakeAdapter, nil
	}
	t.Cleanup(func() { newAdapterForRestart = origCtor })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s := &acpSession{
		id:        "live-1",
		agentID:   "opencode",
		workDir:   "/repo",
		sessionID: "acp-sess-1",
		ctx:       ctx,
		events:    make(chan ChatEvent, 64),
		done:      make(chan struct{}),
		adapter:   &resumeSucceedFakeAdapter{sessionID: "acp-sess-1"}, // pre-restart adapter (will be closed)
		agentDesc: AgentDescriptor{
			ID:          "opencode",
			Command:     "opencode",
			ACPCommand:  "opencode",
			Available:   true,
			SupportsACP: true,
		},
		// CURRENT sessionOpts with browser MCP enabled — tryRestart must use these.
		sessionOpts: SessionOptions{
			UseActiveBrowser:   true,
			ActiveBrowserTabID: "tab-9",
			AutoRestart:        true,
			MaxRetries:         1,
			RestartBaseDelay:   1 * time.Millisecond,
			RestartMaxDelay:    5 * time.Millisecond,
		},
		autoRestart:      true,
		maxRetries:       1,
		restartBaseDelay: 1 * time.Millisecond,
		restartMaxDelay:  5 * time.Millisecond,
	}

	if !s.tryRestart() {
		t.Fatalf("tryRestart returned false; expected successful restart")
	}

	cfgMu.Lock()
	defer cfgMu.Unlock()
	if len(capturedCfgs) == 0 {
		t.Fatalf("expected newAdapterForRestart to be invoked at least once")
	}
	cfg := capturedCfgs[len(capturedCfgs)-1]

	// The browser MCP server must be present in the rebuilt adapter config.
	foundBrowser := false
	for _, srv := range cfg.MCPServers {
		if srv.Name == "test-browser-mcp" {
			foundBrowser = true
			break
		}
	}
	if !foundBrowser {
		t.Errorf("expected rebuilt adapter config to include browser MCP server (UseActiveBrowser=true), got MCPServers=%v", cfg.MCPServers)
	}

	// Drain the session_resumed event emitted on success.
	select {
	case evt := <-s.events:
		if evt.Type != "session_resumed" {
			t.Errorf("expected session_resumed event, got %q", evt.Type)
		}
		if evt.Epoch == "" {
			t.Error("expected session_resumed event to carry a fresh epoch UUID")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for session_resumed event")
	}
}

// TestTryRestartPersistentErrorEmitsCrash verifies that when the adapter crash
// is a persistent error (not retryable), tryRestart returns false WITHOUT
// consuming a restart attempt, and the caller (processNotifications) is
// responsible for emitting crashDoneEvent. tryRestart itself must not emit a
// 'done' crash event that would pre-empt the outer crashDoneEvent
// (VAL-RESUME-005).
func TestTryRestartPersistentErrorEmitsCrash(t *testing.T) {
	origCtor := newAdapterForRestart
	newAdapterForRestart = func(_ context.Context, _ string, _ acp.AdapterConfig) (acp.Adapter, error) {
		t.Fatal("newAdapterForRestart should not be called for a persistent crash error")
		return nil, nil
	}
	t.Cleanup(func() { newAdapterForRestart = origCtor })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	crashedAdapter := &resumeSucceedFakeAdapter{
		sessionID: "acp-sess-2",
		crashErr:  errors.New("permission denied: cannot execute agent binary"),
	}
	s := &acpSession{
		id:        "live-2",
		agentID:   "claude",
		workDir:   "/repo",
		sessionID: "acp-sess-2",
		ctx:       ctx,
		events:    make(chan ChatEvent, 64),
		adapter:   crashedAdapter,
		agentDesc: AgentDescriptor{ID: "claude", Command: "claude", SupportsACP: true},
		sessionOpts: SessionOptions{
			AutoRestart:      true,
			MaxRetries:       3,
			RestartBaseDelay: 1 * time.Millisecond,
			RestartMaxDelay:  5 * time.Millisecond,
		},
		autoRestart:      true,
		maxRetries:       3,
		restartBaseDelay: 1 * time.Millisecond,
		restartMaxDelay:  5 * time.Millisecond,
	}

	if s.tryRestart() {
		t.Fatalf("tryRestart returned true for a persistent error; expected false")
	}

	// tryRestart should NOT emit a 'done'/'agent_crash_unrecoverable' event —
	// the processNotifications caller owns that via crashDoneEvent. It may
	// emit 'error' notices, which is acceptable.
	for {
		select {
		case evt := <-s.events:
			if evt.Type == "done" && evt.StopReason == "agent_crash_unrecoverable" {
				t.Errorf("tryRestart must not emit crashDoneEvent; the caller owns that. got: %+v", evt)
			}
		default:
			return
		}
	}
}

// resumeSucceedFakeAdapter is an acp.Adapter fake whose ResumeSession always
// succeeds, Done() never closes, and Err() returns crashErr (default nil).
// SupportsResume returns true so shouldAttemptRestart proceeds.
type resumeSucceedFakeAdapter struct {
	sessionID string
	crashErr  error
	mu        sync.Mutex
	closed    bool
}

func (a *resumeSucceedFakeAdapter) NewSession(_ context.Context, _ string) (*acp.SessionNewResult, error) {
	return &acp.SessionNewResult{SessionID: a.sessionID}, nil
}
func (a *resumeSucceedFakeAdapter) ResumeSession(_ context.Context, _, _ string) (*acp.SessionNewResult, error) {
	return &acp.SessionNewResult{SessionID: a.sessionID}, nil
}
func (a *resumeSucceedFakeAdapter) Prompt(_ context.Context, _ string, _ []acp.ContentBlock) (*acp.SessionPromptResult, error) {
	return &acp.SessionPromptResult{}, nil
}
func (a *resumeSucceedFakeAdapter) Cancel(_ string) error { return nil }
func (a *resumeSucceedFakeAdapter) Done() <-chan struct{} {
	return make(chan struct{}) // never closes
}
func (a *resumeSucceedFakeAdapter) Close() error {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	return nil
}
func (a *resumeSucceedFakeAdapter) Err() error { return a.crashErr }
func (a *resumeSucceedFakeAdapter) SupportsResume() bool { return true }
func (a *resumeSucceedFakeAdapter) SetConfigOption(_ context.Context, _, _, _ string) ([]acp.SessionConfigOption, error) {
	return nil, nil
}
func (a *resumeSucceedFakeAdapter) CloseSession(_ context.Context, _ string) error { return nil }
func (a *resumeSucceedFakeAdapter) Notifications() <-chan *acp.Notification { return make(chan *acp.Notification) }
func (a *resumeSucceedFakeAdapter) Requests() <-chan *acp.Request               { return make(chan *acp.Request) }
func (a *resumeSucceedFakeAdapter) Respond(_ int64, _ any, _ *acp.RPCError) error { return nil }
func (a *resumeSucceedFakeAdapter) AgentInfo() acp.ImplementationInfo            { return acp.ImplementationInfo{} }
func (a *resumeSucceedFakeAdapter) AgentCapabilities() acp.AgentCapabilities      { return acp.AgentCapabilities{} }
func (a *resumeSucceedFakeAdapter) ConfigOptions() []acp.SessionConfigOption      { return nil }
func (a *resumeSucceedFakeAdapter) Crash(_ acp.CrashMode) error                   { return nil }
