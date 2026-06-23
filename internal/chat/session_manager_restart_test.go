package chat

import (
	"context"
	"testing"
	"time"
)

// TestSessionManager_SetRestartConfig verifies that the config-derived restart
// tuning values (max retries, base delay, max delay) are stored on the
// SessionManager so they can be threaded into SessionOptions for every created
// or resumed ACP session. This validates the "Wire SessionOptions restart
// config fields" requirement of VAL-RESUME-001.
func TestSessionManager_SetRestartConfig(t *testing.T) {
	m := NewSessionManager()
	m.SetRestartConfig(RestartConfig{
		MaxRetries:       7,
		RestartBaseDelay: 250 * time.Millisecond,
		RestartMaxDelay:  90 * time.Second,
	})

	cfg := m.restartConfig()
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want 7", cfg.MaxRetries)
	}
	if cfg.RestartBaseDelay != 250*time.Millisecond {
		t.Errorf("RestartBaseDelay = %v, want 250ms", cfg.RestartBaseDelay)
	}
	if cfg.RestartMaxDelay != 90*time.Second {
		t.Errorf("RestartMaxDelay = %v, want 90s", cfg.RestartMaxDelay)
	}
}

// TestSessionManager_SetRestartConfig_Defaults verifies that a freshly
// constructed SessionManager reports ADR-0004 default restart tuning values
// before SetRestartConfig is called. This ensures auto-restart still works
// (with documented defaults) even when server wiring forgets to call
// SetRestartConfig.
func TestSessionManager_SetRestartConfig_Defaults(t *testing.T) {
	m := NewSessionManager()
	cfg := m.restartConfig()
	if cfg.MaxRetries != defaultRestartMaxRetries {
		t.Errorf("default MaxRetries = %d, want %d", cfg.MaxRetries, defaultRestartMaxRetries)
	}
	if cfg.RestartBaseDelay != defaultRestartBaseDelay {
		t.Errorf("default RestartBaseDelay = %v, want %v", cfg.RestartBaseDelay, defaultRestartBaseDelay)
	}
	if cfg.RestartMaxDelay != defaultRestartMaxDelay {
		t.Errorf("default RestartMaxDelay = %v, want %v", cfg.RestartMaxDelay, defaultRestartMaxDelay)
	}
}

// TestSessionManager_EnrichOptsAppliesConfig verifies that enrichOpts fills in
// the restart tuning fields on SessionOptions from the manager's stored
// RestartConfig when the caller did not supply them. This validates
// VAL-RESUME-001 (SessionOptions restart config fields wired from config).
func TestSessionManager_EnrichOptsAppliesConfig(t *testing.T) {
	m := NewSessionManager()
	m.SetRestartConfig(RestartConfig{
		MaxRetries:       4,
		RestartBaseDelay: 750 * time.Millisecond,
		RestartMaxDelay:  45 * time.Second,
	})

	t.Run("fills_zero_values_from_config", func(t *testing.T) {
		opts := SessionOptions{
			UseActiveTerminal: true,
			ActiveTerminalID:  "term-1",
		}
		enriched := m.enrichOpts(opts)
		if enriched.MaxRetries != 4 {
			t.Errorf("MaxRetries = %d, want 4", enriched.MaxRetries)
		}
		if enriched.RestartBaseDelay != 750*time.Millisecond {
			t.Errorf("RestartBaseDelay = %v, want 750ms", enriched.RestartBaseDelay)
		}
		if enriched.RestartMaxDelay != 45*time.Second {
			t.Errorf("RestartMaxDelay = %v, want 45s", enriched.RestartMaxDelay)
		}
		// Caller-supplied non-restart fields preserved.
		if !enriched.UseActiveTerminal || enriched.ActiveTerminalID != "term-1" {
			t.Errorf("caller fields not preserved: %+v", enriched)
		}
	})

	t.Run("preserves_caller_supplied_restart_values", func(t *testing.T) {
		opts := SessionOptions{
			MaxRetries:       10,
			RestartBaseDelay: 1 * time.Second,
			RestartMaxDelay:  2 * time.Minute,
		}
		enriched := m.enrichOpts(opts)
		if enriched.MaxRetries != 10 {
			t.Errorf("MaxRetries = %d, want 10 (caller override)", enriched.MaxRetries)
		}
		if enriched.RestartBaseDelay != 1*time.Second {
			t.Errorf("RestartBaseDelay = %v, want 1s (caller override)", enriched.RestartBaseDelay)
		}
		if enriched.RestartMaxDelay != 2*time.Minute {
			t.Errorf("RestartMaxDelay = %v, want 2m (caller override)", enriched.RestartMaxDelay)
		}
	})

	t.Run("falls_back_to_defaults_when_config_unset", func(t *testing.T) {
		m := NewSessionManager() // no SetRestartConfig call
		opts := SessionOptions{}
		enriched := m.enrichOpts(opts)
		if enriched.MaxRetries != defaultRestartMaxRetries {
			t.Errorf("MaxRetries = %d, want default %d", enriched.MaxRetries, defaultRestartMaxRetries)
		}
		if enriched.RestartBaseDelay != defaultRestartBaseDelay {
			t.Errorf("RestartBaseDelay = %v, want default %v", enriched.RestartBaseDelay, defaultRestartBaseDelay)
		}
		if enriched.RestartMaxDelay != defaultRestartMaxDelay {
			t.Errorf("RestartMaxDelay = %v, want default %v", enriched.RestartMaxDelay, defaultRestartMaxDelay)
		}
	})
}

// recordingAgentSession captures the SessionOptions passed to NewChatSession
// so we can assert the manager threaded the restart config through. We
// substitute NewChatSession via a constructor hook to avoid spawning a real
// subprocess.
type recordingAgentSession struct {
	opts SessionOptions
	agent AgentDescriptor
}

func (r *recordingAgentSession) ID() string                             { return "rec-1" }
func (r *recordingAgentSession) AgentID() string                        { return r.agent.ID }
func (r *recordingAgentSession) WorkDir() string                        { return "" }
func (r *recordingAgentSession) Mode() SessionMode                      { return ModeACP }
func (r *recordingAgentSession) Events() <-chan ChatEvent               { return nil }
func (r *recordingAgentSession) Done() <-chan struct{}                  { return nil }
func (r *recordingAgentSession) Err() error                             { return nil }
func (r *recordingAgentSession) ACPSessionID() string                   { return "rec-1" }
func (r *recordingAgentSession) IsResumed() bool                        { return false }
func (r *recordingAgentSession) RespondPermission(_ PermissionResponse) {}
func (r *recordingAgentSession) SetAutoApprove(_ bool)                  {}
func (r *recordingAgentSession) SetUseActiveTerminal(_ bool, _ string)  {}
func (r *recordingAgentSession) UseActiveTerminalEnabled() bool         { return false }
func (r *recordingAgentSession) ActiveTerminalID() string               { return "" }
func (r *recordingAgentSession) SetUseActiveBrowser(_ bool, _ string)   {}
func (r *recordingAgentSession) UseActiveBrowserEnabled() bool          { return false }
func (r *recordingAgentSession) ActiveBrowserTabID() string             { return "" }
func (r *recordingAgentSession) SetConfigOption(_ context.Context, _, _ string) error {
	return nil
}
func (r *recordingAgentSession) Send(_ context.Context, _ string, _ []Attachment) error {
	return nil
}
func (r *recordingAgentSession) Cancel() error { return nil }
func (r *recordingAgentSession) Close() error  { return nil }

// TestSessionManager_Create_ThreadsRestartConfigIntoOptions verifies the
// end-to-end Create path: when SetRestartConfig has been called, the
// SessionOptions handed to the session constructor carry the config-derived
// restart tuning values. This validates VAL-RESUME-001 at the integration
// boundary (manager -> session).
func TestSessionManager_Create_ThreadsRestartConfigIntoOptions(t *testing.T) {
	origNewChatSession := newChatSessionCtor
	t.Cleanup(func() { newChatSessionCtor = origNewChatSession })

	var capturedOpts SessionOptions
	var capturedAgent AgentDescriptor
	newChatSessionCtor = func(ctx context.Context, agent AgentDescriptor, workDir string, opts SessionOptions) (ChatSession, error) {
		capturedOpts = opts
		capturedAgent = agent
		return &recordingAgentSession{opts: opts, agent: agent}, nil
	}

	m := NewSessionManager()
	m.SetRestartConfig(RestartConfig{
		MaxRetries:       6,
		RestartBaseDelay: 300 * time.Millisecond,
		RestartMaxDelay:  60 * time.Second,
	})

	agent := AgentDescriptor{ID: "agent-x", Command: "echo", Available: true, SupportsACP: true}
	_, err := m.Create(context.Background(), agent, "", SessionOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if capturedAgent.ID != "agent-x" {
		t.Errorf("captured agent ID = %q, want %q", capturedAgent.ID, "agent-x")
	}
	// ADR-0004: ACP session should have AutoRestart enabled by the manager.
	if !capturedOpts.AutoRestart {
		t.Errorf("AutoRestart = false, want true (ACP session)")
	}
	if capturedOpts.MaxRetries != 6 {
		t.Errorf("MaxRetries = %d, want 6 (from config)", capturedOpts.MaxRetries)
	}
	if capturedOpts.RestartBaseDelay != 300*time.Millisecond {
		t.Errorf("RestartBaseDelay = %v, want 300ms (from config)", capturedOpts.RestartBaseDelay)
	}
	if capturedOpts.RestartMaxDelay != 60*time.Second {
		t.Errorf("RestartMaxDelay = %v, want 60s (from config)", capturedOpts.RestartMaxDelay)
	}
}

// TestSessionManager_Resume_ThreadsRestartConfigIntoOptions verifies the same
// for the Resume path (resumed sessions also need auto-restart config so a
// crashed resumed session can be re-resumed).
func TestSessionManager_Resume_ThreadsRestartConfigIntoOptions(t *testing.T) {
	origNewACPResumed := newACPResumedSessionCtor
	t.Cleanup(func() { newACPResumedSessionCtor = origNewACPResumed })

	var capturedOpts SessionOptions
	newACPResumedSessionCtor = func(ctx context.Context, agent AgentDescriptor, workDir, acpSessionID string, opts SessionOptions) (*acpSession, error) {
		capturedOpts = opts
		// Return a minimal acpSession so the manager can store it without
		// touching real adapter state.
		s := &acpSession{id: "resumed-1", sessionID: "resumed-1"}
		return s, nil
	}

	m := NewSessionManager()
	m.SetRestartConfig(RestartConfig{
		MaxRetries:       8,
		RestartBaseDelay: 400 * time.Millisecond,
		RestartMaxDelay:  75 * time.Second,
	})

	agent := AgentDescriptor{ID: "agent-y", Command: "echo", Available: true, SupportsACP: true}
	_, err := m.Resume(context.Background(), agent, "", "acp-sess-1", SessionOptions{})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if !capturedOpts.AutoRestart {
		t.Errorf("AutoRestart = false, want true (resumed ACP session)")
	}
	if capturedOpts.MaxRetries != 8 {
		t.Errorf("MaxRetries = %d, want 8 (from config)", capturedOpts.MaxRetries)
	}
	if capturedOpts.RestartBaseDelay != 400*time.Millisecond {
		t.Errorf("RestartBaseDelay = %v, want 400ms (from config)", capturedOpts.RestartBaseDelay)
	}
	if capturedOpts.RestartMaxDelay != 75*time.Second {
		t.Errorf("RestartMaxDelay = %v, want 75s (from config)", capturedOpts.RestartMaxDelay)
	}
}
