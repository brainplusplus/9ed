package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Note: TestIsPersistentError and TestIsTransientError already exist in
// input_lock_test.go and cover ADR-0004 error classification. The tests below
// cover the additional auto-restart behavior (backoff, config wiring, epoch
// regeneration, canResume field).

// TestIsPersistentError_ExtendedCases adds edge cases beyond those in
// input_lock_test.go to ensure thorough classification coverage.
func TestIsPersistentError_ExtendedCases(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"no such file", errors.New("open /x/y: no such file or directory"), true},
		{"does not support resume", errors.New("agent does not support session/resume"), true},
		{"transient broken pipe", errors.New("write: broken pipe"), false},
		{"transient connection reset", errors.New("read: connection reset by peer"), false},
		{"transient signal killed", errors.New("signal: killed"), false},
		{"transient context deadline", errors.New("context deadline exceeded"), false},
		{"generic unknown", errors.New("something weird happened"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPersistentError(tt.err); got != tt.want {
				t.Errorf("isPersistentError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestIsTransientError_ExtendedCases adds edge cases beyond those in
// input_lock_test.go.
func TestIsTransientError_ExtendedCases(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"connection refused", errors.New("connection refused"), true},
		{"connection closed", errors.New("connection closed"), true},
		{"signal terminated", errors.New("signal: terminated"), true},
		{"i/o timeout", errors.New("i/o timeout"), true},
		{"persistent permission denied", errors.New("permission denied"), false},
		{"persistent auth", errors.New("unauthorized"), false},
		{"generic unknown defaults transient", errors.New("mystery error"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientError(tt.err); got != tt.want {
				t.Errorf("isTransientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestRestartBackoff_Timing verifies ADR-0004 exponential backoff with the
// configured base/max delays. The delay for attempt N is base*2^N capped at
// max, plus jitter (20% of the computed delay).
func TestRestartBackoff_Timing(t *testing.T) {
	s := &acpSession{
		restartBaseDelay: 100 * time.Millisecond,
		restartMaxDelay:  1600 * time.Millisecond,
	}

	cases := []struct {
		attempt int
		want    time.Duration // expected = min(base*2^attempt, max) * 1.2
	}{
		{0, 120 * time.Millisecond},   // 100*2^0 = 100ms, +20% = 120ms
		{1, 240 * time.Millisecond},   // 100*2^1 = 200ms, +20% = 240ms
		{2, 480 * time.Millisecond},   // 100*2^2 = 400ms, +20% = 480ms
		{3, 960 * time.Millisecond},   // 100*2^3 = 800ms, +20% = 960ms
		{4, 1920 * time.Millisecond},  // 100*2^4 = 1600ms (== max), +20% = 1920ms
		{5, 1920 * time.Millisecond},  // capped at max, +20% = 1920ms
		{10, 1920 * time.Millisecond}, // capped at max, +20% = 1920ms
	}
	for _, c := range cases {
		got := s.restartBackoff(c.attempt)
		if got != c.want {
			t.Errorf("attempt %d: backoff = %v, want %v", c.attempt, got, c.want)
		}
	}
}

// TestRestartBackoff_CappedAtMax verifies the backoff never exceeds maxDelay +
// 20% jitter.
func TestRestartBackoff_CappedAtMax(t *testing.T) {
	s := &acpSession{
		restartBaseDelay: 500 * time.Millisecond,
		restartMaxDelay:  1 * time.Second,
	}
	maxWithJitter := s.restartMaxDelay + s.restartMaxDelay*20/100 // 1.2s
	for attempt := 0; attempt < 20; attempt++ {
		got := s.restartBackoff(attempt)
		if got > maxWithJitter {
			t.Errorf("attempt %d: backoff %v exceeds max+jitter %v", attempt, got, maxWithJitter)
		}
	}
}

// TestRestartBackoff_UsesConfiguredValues verifies that the backoff uses the
// config-derived values stored on the session (not hardcoded defaults).
func TestRestartBackoff_UsesConfiguredValues(t *testing.T) {
	s := &acpSession{
		restartBaseDelay: 250 * time.Millisecond,
		restartMaxDelay:  2 * time.Second,
	}
	// attempt 0: base = 250ms; with 20% jitter -> 300ms
	got := s.restartBackoff(0)
	if got != 300*time.Millisecond {
		t.Errorf("expected backoff = 300ms (250ms + 20%%), got %v", got)
	}
}

// TestApplyRestartConfig verifies the pure helper that maps SessionOptions
// restart fields onto an acpSession. This validates the field-wiring for both
// the Create and Resume paths without spawning a subprocess.
func TestApplyRestartConfig(t *testing.T) {
	t.Run("auto_restart_enabled", func(t *testing.T) {
		s := &acpSession{}
		agent := AgentDescriptor{ID: "agent-a", Command: "echo"}
		opts := SessionOptions{
			AutoRestart:      true,
			MaxRetries:       5,
			RestartBaseDelay: 750 * time.Millisecond,
			RestartMaxDelay:  45 * time.Second,
		}
		applyRestartConfig(s, agent, opts)
		if s.agentDesc.ID != "agent-a" {
			t.Errorf("agentDesc.ID = %q, want %q", s.agentDesc.ID, "agent-a")
		}
		if !s.autoRestart {
			t.Errorf("autoRestart = false, want true")
		}
		if s.maxRetries != 5 {
			t.Errorf("maxRetries = %d, want 5", s.maxRetries)
		}
		if s.restartBaseDelay != 750*time.Millisecond {
			t.Errorf("restartBaseDelay = %v, want 750ms", s.restartBaseDelay)
		}
		if s.restartMaxDelay != 45*time.Second {
			t.Errorf("restartMaxDelay = %v, want 45s", s.restartMaxDelay)
		}
		if !s.sessionOpts.AutoRestart {
			t.Errorf("sessionOpts.AutoRestart = false, want true")
		}
	})

	t.Run("auto_restart_disabled", func(t *testing.T) {
		s := &acpSession{}
		agent := AgentDescriptor{ID: "agent-b", Command: "echo"}
		opts := SessionOptions{
			AutoRestart:      false,
			MaxRetries:       3,
			RestartBaseDelay: 500 * time.Millisecond,
			RestartMaxDelay:  30 * time.Second,
		}
		applyRestartConfig(s, agent, opts)
		if s.autoRestart {
			t.Errorf("autoRestart = true, want false")
		}
		// Fields are still recorded even when disabled (for potential manual resume).
		if s.maxRetries != 3 {
			t.Errorf("maxRetries = %d, want 3", s.maxRetries)
		}
	})

	t.Run("zero_values_fall_back_to_defaults", func(t *testing.T) {
		s := &acpSession{}
		agent := AgentDescriptor{ID: "agent-c", Command: "echo"}
		// opts has zero-valued restart fields (e.g., when config not wired).
		opts := SessionOptions{AutoRestart: true}
		applyRestartConfig(s, agent, opts)
		if s.maxRetries != defaultRestartMaxRetries {
			t.Errorf("maxRetries = %d, want default %d", s.maxRetries, defaultRestartMaxRetries)
		}
		if s.restartBaseDelay != defaultRestartBaseDelay {
			t.Errorf("restartBaseDelay = %v, want default %v", s.restartBaseDelay, defaultRestartBaseDelay)
		}
		if s.restartMaxDelay != defaultRestartMaxDelay {
			t.Errorf("restartMaxDelay = %v, want default %v", s.restartMaxDelay, defaultRestartMaxDelay)
		}
	})
}

// TestNewACPSession_SetsAutoRestartFields verifies the Create path applies the
// restart config. Because newACPSession spawns a real subprocess, we verify
// the field-wiring logic via applyRestartConfig (the same helper newACPSession
// calls). The full integration is covered by e2e tests.
func TestNewACPSession_SetsAutoRestartFields(t *testing.T) {
	opts := SessionOptions{
		AutoRestart:        true,
		MaxRetries:         5,
		RestartBaseDelay:   750 * time.Millisecond,
		RestartMaxDelay:    45 * time.Second,
		UseActiveTerminal:  true,
		ActiveTerminalID:   "term-1",
		UseActiveBrowser:   true,
		ActiveBrowserTabID: "tab-1",
	}
	agent := AgentDescriptor{ID: "test-agent", Command: "echo", SupportsACP: true}

	s := &acpSession{}
	applyRestartConfig(s, agent, opts)

	if s.agentDesc.ID != agent.ID {
		t.Errorf("agentDesc.ID = %q, want %q", s.agentDesc.ID, agent.ID)
	}
	if s.agentDesc.Command != agent.Command {
		t.Errorf("agentDesc.Command = %q, want %q", s.agentDesc.Command, agent.Command)
	}
	if !s.sessionOpts.AutoRestart {
		t.Errorf("sessionOpts.AutoRestart = false, want true")
	}
	if s.sessionOpts.UseActiveTerminal != opts.UseActiveTerminal {
		t.Errorf("sessionOpts.UseActiveTerminal = %v, want %v", s.sessionOpts.UseActiveTerminal, opts.UseActiveTerminal)
	}
	if !s.autoRestart {
		t.Errorf("autoRestart = false, want true")
	}
	if s.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", s.maxRetries)
	}
	if s.restartBaseDelay != 750*time.Millisecond {
		t.Errorf("restartBaseDelay = %v, want 750ms", s.restartBaseDelay)
	}
	if s.restartMaxDelay != 45*time.Second {
		t.Errorf("restartMaxDelay = %v, want 45s", s.restartMaxDelay)
	}
}

// TestShouldAttemptRestart verifies the pure decision logic used by tryRestart:
// persistent errors and missing resume support mean no retry.
func TestShouldAttemptRestart(t *testing.T) {
	transientErr := errors.New("signal: killed")
	persistentErr := errors.New("executable file not found")

	cases := []struct {
		name            string
		adapterErr      error
		supportsResume  bool
		autoRestart     bool
		wantShouldRetry bool
	}{
		{"transient_with_resume", transientErr, true, true, true},
		{"transient_without_resume", transientErr, false, true, false},
		{"persistent_with_resume", persistentErr, true, true, false},
		{"persistent_without_resume", persistentErr, false, true, false},
		{"auto_restart_disabled", transientErr, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldAttemptRestart(c.adapterErr, c.supportsResume, c.autoRestart)
			if got != c.wantShouldRetry {
				t.Errorf("shouldAttemptRestart = %v, want %v", got, c.wantShouldRetry)
			}
		})
	}
}

// TestCrashDoneEvent_IncludesCanResume verifies the done event emitted on
// unrecoverable crash includes CanResume reflecting the adapter's resume
// capability. This validates VAL-RESUME-005.
func TestCrashDoneEvent_IncludesCanResume(t *testing.T) {
	t.Run("canResume_true_when_adapter_supports_resume", func(t *testing.T) {
		evt := crashDoneEvent(true)
		if evt.Type != "done" {
			t.Errorf("Type = %q, want %q", evt.Type, "done")
		}
		if evt.StopReason != "agent_crash_unrecoverable" {
			t.Errorf("StopReason = %q, want %q", evt.StopReason, "agent_crash_unrecoverable")
		}
		if !evt.CanResume {
			t.Errorf("CanResume = false, want true")
		}
	})
	t.Run("canResume_false_when_adapter_no_resume", func(t *testing.T) {
		evt := crashDoneEvent(false)
		if evt.CanResume {
			t.Errorf("CanResume = true, want false")
		}
	})
}

// TestSessionResumedEvent_CarriesNewEpoch verifies the session_resumed event
// carries a non-empty Epoch UUID that differs each time. This validates the
// epoch regeneration behavior required by VAL-RESUME-001 / VAL-CATCHUP-005.
func TestSessionResumedEvent_CarriesNewEpoch(t *testing.T) {
	evt1 := sessionResumedEvent("sess-1")
	if evt1.Type != "session_resumed" {
		t.Errorf("Type = %q, want %q", evt1.Type, "session_resumed")
	}
	if evt1.Epoch == "" {
		t.Errorf("Epoch is empty; expected a UUID")
	}
	// Epoch should look like a UUID (36 chars with dashes).
	if len(evt1.Epoch) != 36 || strings.Count(evt1.Epoch, "-") != 4 {
		t.Errorf("Epoch %q does not look like a UUID", evt1.Epoch)
	}

	evt2 := sessionResumedEvent("sess-1")
	if evt2.Epoch == "" {
		t.Errorf("second Epoch is empty")
	}
	if evt1.Epoch == evt2.Epoch {
		t.Errorf("Epoch did not regenerate: both = %q", evt1.Epoch)
	}
}

// TestChatEvent_EpochAndCanResume_JSONTags verifies the new ChatEvent fields
// serialize with the correct JSON tags so clients receive epoch/canResume.
func TestChatEvent_EpochAndCanResume_JSONTags(t *testing.T) {
	evt := ChatEvent{Type: "done", StopReason: "agent_crash_unrecoverable", Epoch: "ep-123", CanResume: true}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"epoch":"ep-123"`) {
		t.Errorf("JSON %q missing epoch field", s)
	}
	if !strings.Contains(s, `"canResume":true`) {
		t.Errorf("JSON %q missing canResume field", s)
	}

	// session_resumed with epoch
	evt2 := ChatEvent{Type: "session_resumed", Text: "sess-1", Epoch: "ep-456"}
	data2, _ := json.Marshal(evt2)
	if !strings.Contains(string(data2), `"epoch":"ep-456"`) {
		t.Errorf("JSON %q missing epoch field for session_resumed", string(data2))
	}
}

// TestSessionResumedEvent_DifferentEpochsPerSession verifies that multiple
// sessions each get distinct epochs.
func TestSessionResumedEvent_DifferentEpochsPerSession(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		evt := sessionResumedEvent(fmt.Sprintf("sess-%d", i))
		if seen[evt.Epoch] {
			t.Errorf("duplicate epoch %q generated", evt.Epoch)
		}
		seen[evt.Epoch] = true
	}
}
