package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMockAdapter_ImplementsAdapterInterface is a compile-time assertion that
// *MockAdapter satisfies the Adapter interface (NewSession, ResumeSession,
// Prompt, Cancel, Done, Close, Err, SupportsResume). This validates the
// expectedBehavior item "Mock adapter implements Adapter interface".
func TestMockAdapter_ImplementsAdapterInterface(t *testing.T) {
	var _ Adapter = (*MockAdapter)(nil)
}

// TestMockAdapter_EchoesPromptsDeterministically verifies the mock echoes the
// user's prompt text back as an agent_message_chunk notification followed by
// an end_turn result. The echo is deterministic: same prompt -> same echo.
func TestMockAdapter_EchoesPromptsDeterministically(t *testing.T) {
	m := NewMockAdapter(MockConfig{})

	ctx := context.Background()
	newRes, err := m.NewSession(ctx, "/work")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if newRes.SessionID == "" {
		t.Fatal("SessionID is empty")
	}
	sessionID := newRes.SessionID

	prompt := []ContentBlock{{Type: "text", Text: "hello world"}}
	result, err := m.Prompt(ctx, sessionID, prompt)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if result.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonEndTurn)
	}

	// Drain notifications and verify the echoed chunk matches the prompt.
	var echoed string
	drained := false
	for !drained {
		select {
		case notif := <-m.Notifications():
			if notif.Method == MethodSessionUpdate {
				var base SessionUpdateBase
				if err := unmarshalRaw(notif.Params, &base); err == nil {
					if base.SessionUpdate == UpdateAgentMessageChunk {
						var chunk AgentMessageChunkUpdate
						if err := unmarshalRaw(notif.Params, &chunk); err == nil {
							echoed += chunk.Content.Text
						}
					}
				}
			}
		case <-time.After(200 * time.Millisecond):
			drained = true
		}
	}

	if echoed != "hello world" {
		t.Errorf("echoed text = %q, want %q", echoed, "hello world")
	}

	// Second prompt with different text echoes deterministically.
	prompt2 := []ContentBlock{{Type: "text", Text: "second message"}}
	if _, err := m.Prompt(ctx, sessionID, prompt2); err != nil {
		t.Fatalf("Prompt 2: %v", err)
	}
	echoed2 := drainText(m)
	if echoed2 != "second message" {
		t.Errorf("echoed text 2 = %q, want %q", echoed2, "second message")
	}
}

// TestMockAdapter_CrashOnN_CrashesAfterNPrompts verifies the crash-on-N mode:
// the mock serves N-1 prompts normally, then crashes (returns an error and
// closes Done) on the Nth prompt. This validates the expectedBehavior item
// "Mock adapter crashes after N prompts in crash-on-N mode".
func TestMockAdapter_CrashOnN_CrashesAfterNPrompts(t *testing.T) {
	m := NewMockAdapter(MockConfig{CrashOnNPrompt: 3})

	ctx := context.Background()
	newRes, err := m.NewSession(ctx, "/work")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sessionID := newRes.SessionID

	// Prompt 1 and 2 succeed.
	for i := 1; i <= 2; i++ {
		if _, err := m.Prompt(ctx, sessionID, []ContentBlock{{Type: "text", Text: "p"}}); err != nil {
			t.Fatalf("prompt %d unexpected error: %v", i, err)
		}
		_ = drainText(m)
	}

	// Prompt 3 crashes.
	_, err = m.Prompt(ctx, sessionID, []ContentBlock{{Type: "text", Text: "p"}})
	if err == nil {
		t.Fatal("expected crash error on 3rd prompt, got nil")
	}

	// Done channel should close after crash.
	select {
	case <-m.Done():
		// expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Done channel did not close after crash")
	}

	// Err should report a crash error.
	if err := m.Err(); err == nil {
		t.Fatal("Err is nil after crash; expected a crash error")
	} else if !strings.Contains(strings.ToLower(err.Error()), "crash") &&
		!strings.Contains(strings.ToLower(err.Error()), "killed") {
		t.Errorf("Err = %v; expected a crash-related message", err)
	}
}

// TestMockAdapter_CrashOnN_ZeroDoesNotCrash verifies that CrashOnNPrompt=0
// (disabled) never crashes, no matter how many prompts are sent.
func TestMockAdapter_CrashOnN_ZeroDoesNotCrash(t *testing.T) {
	m := NewMockAdapter(MockConfig{CrashOnNPrompt: 0})

	ctx := context.Background()
	newRes, err := m.NewSession(ctx, "/work")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sessionID := newRes.SessionID

	for i := 1; i <= 10; i++ {
		if _, err := m.Prompt(ctx, sessionID, []ContentBlock{{Type: "text", Text: "p"}}); err != nil {
			t.Fatalf("prompt %d unexpected error: %v", i, err)
		}
		_ = drainText(m)
	}

	select {
	case <-m.Done():
		t.Fatal("Done closed unexpectedly (no crash configured)")
	case <-time.After(50 * time.Millisecond):
		// expected: still alive
	}
}

// TestMockAdapter_SupportsResume verifies the mock reports resume support
// configurable via MockConfig.
func TestMockAdapter_SupportsResume(t *testing.T) {
	t.Run("default_supports_resume", func(t *testing.T) {
		m := NewMockAdapter(MockConfig{})
		if !m.SupportsResume() {
			t.Error("SupportsResume = false, want true (default)")
		}
	})
	t.Run("disabled", func(t *testing.T) {
		m := NewMockAdapter(MockConfig{SupportsResume: BoolPtr(false)})
		if m.SupportsResume() {
			t.Error("SupportsResume = true, want false")
		}
	})
}

// TestMockAdapter_ResumeSession verifies the mock accepts a resume call and
// returns the resumed session ID.
func TestMockAdapter_ResumeSession(t *testing.T) {
	m := NewMockAdapter(MockConfig{SupportsResume: BoolPtr(true)})
	ctx := context.Background()

	res, err := m.ResumeSession(ctx, "sess-resume-1", "/work")
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if res.SessionID != "sess-resume-1" {
		t.Errorf("SessionID = %q, want %q", res.SessionID, "sess-resume-1")
	}
}

// TestMockAdapter_ResumeSession_Unsupported verifies resume fails when
// SupportsResume is false.
func TestMockAdapter_ResumeSession_Unsupported(t *testing.T) {
	m := NewMockAdapter(MockConfig{SupportsResume: BoolPtr(false)})
	ctx := context.Background()

	_, err := m.ResumeSession(ctx, "sess-resume-1", "/work")
	if err == nil {
		t.Fatal("expected error when resume unsupported, got nil")
	}
}

// TestMockAdapter_Cancel verifies Cancel is a no-op that does not crash the
// adapter (returns nil) and increments a counter so callers can verify it was
// invoked.
func TestMockAdapter_Cancel(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	ctx := context.Background()
	newRes, _ := m.NewSession(ctx, "/work")

	if err := m.Cancel(newRes.SessionID); err != nil {
		t.Errorf("Cancel returned error: %v", err)
	}
	if m.CancelCount() != 1 {
		t.Errorf("CancelCount = %d, want 1", m.CancelCount())
	}
}

// TestMockAdapter_Close verifies Close closes the Done channel and subsequent
// calls are idempotent.
func TestMockAdapter_Close(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	_ = m.Close()
	select {
	case <-m.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Done did not close after Close")
	}
	// Idempotent.
	if err := m.Close(); err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
}

// TestMockAdapter_DoneNotClosedBeforeCrashOrClose verifies the Done channel
// stays open during normal operation.
func TestMockAdapter_DoneNotClosedBeforeCrashOrClose(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	ctx := context.Background()
	newRes, _ := m.NewSession(ctx, "/work")
	_, _ = m.Prompt(ctx, newRes.SessionID, []ContentBlock{{Type: "text", Text: "hi"}})
	_ = drainText(m)

	select {
	case <-m.Done():
		t.Fatal("Done closed unexpectedly during normal operation")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

// TestMockAdapter_PromptAfterClose verifies Prompt returns an error after the
// adapter is closed.
func TestMockAdapter_PromptAfterClose(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	ctx := context.Background()
	newRes, _ := m.NewSession(ctx, "/work")
	_ = m.Close()

	_, err := m.Prompt(ctx, newRes.SessionID, []ContentBlock{{Type: "text", Text: "hi"}})
	if err == nil {
		t.Fatal("expected error on Prompt after Close, got nil")
	}
}

// TestMockAdapter_ConcurrentPromptsAreSerialized verifies the mock is safe for
// concurrent use (no data races) when multiple goroutines prompt concurrently.
func TestMockAdapter_ConcurrentPromptsAreSerialized(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	ctx := context.Background()
	newRes, _ := m.NewSession(ctx, "/work")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Prompt(ctx, newRes.SessionID, []ContentBlock{{Type: "text", Text: "concurrent"}})
		}()
	}
	wg.Wait()
	_ = drainText(m)
}

// TestMockAdapter_PromptCount tracks how many prompts were processed.
func TestMockAdapter_PromptCount(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	ctx := context.Background()
	newRes, _ := m.NewSession(ctx, "/work")

	for i := 0; i < 5; i++ {
		_, _ = m.Prompt(ctx, newRes.SessionID, []ContentBlock{{Type: "text", Text: "p"}})
	}
	_ = drainText(m)

	if got := m.PromptCount(); got != 5 {
		t.Errorf("PromptCount = %d, want 5", got)
	}
}

// TestMockAdapter_ForceCrash_Sigkill verifies the ForceCrash method simulates
// a subprocess kill: it closes Done, sets Err, and makes subsequent Prompt
// calls fail. This mirrors the real adapter's behavior when its subprocess is
// killed by the debug crash endpoint.
func TestMockAdapter_ForceCrash_Sigkill(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	ctx := context.Background()
	newRes, _ := m.NewSession(ctx, "/work")

	m.ForceCrash(CrashModeSigkill)

	select {
	case <-m.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Done did not close after ForceCrash")
	}

	if err := m.Err(); err == nil {
		t.Fatal("Err is nil after ForceCrash")
	}

	_, err := m.Prompt(ctx, newRes.SessionID, []ContentBlock{{Type: "text", Text: "p"}})
	if err == nil {
		t.Fatal("expected Prompt to fail after ForceCrash, got nil")
	}
}

// TestMockAdapter_ForceCrash_Panic verifies the panic crash mode records a
// panic-style error.
func TestMockAdapter_ForceCrash_Panic(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	m.ForceCrash(CrashModePanic)

	if err := m.Err(); err == nil {
		t.Fatal("Err is nil")
	} else if !strings.Contains(strings.ToLower(err.Error()), "panic") {
		t.Errorf("Err = %v; expected panic-related message", err)
	}
}

// TestMockAdapter_ForceCrash_UncleanExit verifies the unclean-exit mode.
func TestMockAdapter_ForceCrash_UncleanExit(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	m.ForceCrash(CrashModeUncleanExit)

	if err := m.Err(); err == nil {
		t.Fatal("Err is nil")
	}

	select {
	case <-m.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Done did not close")
	}
}

// TestMockAdapter_ConfigOptions verifies the mock returns configurable config
// options from NewSession / ResumeSession.
func TestMockAdapter_ConfigOptions(t *testing.T) {
	m := NewMockAdapter(MockConfig{
		ConfigOptions: []SessionConfigOption{
			{ID: "model", Name: "Model", Type: "string", CurrentValue: "mock-1"},
		},
	})
	ctx := context.Background()
	res, err := m.NewSession(ctx, "/work")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(res.ConfigOptions) != 1 {
		t.Fatalf("ConfigOptions len = %d, want 1", len(res.ConfigOptions))
	}
	if res.ConfigOptions[0].ID != "model" {
		t.Errorf("ConfigOptions[0].ID = %q, want %q", res.ConfigOptions[0].ID, "model")
	}

	opts := m.ConfigOptions()
	if len(opts) != 1 || opts[0].CurrentValue != "mock-1" {
		t.Errorf("ConfigOptions() = %#v, want model=mock-1", opts)
	}
}

// TestMockAdapter_AgentInfo verifies the mock exposes configurable agent info.
func TestMockAdapter_AgentInfo(t *testing.T) {
	m := NewMockAdapter(MockConfig{
		AgentInfo: ImplementationInfo{Name: "mock-agent", Version: "test"},
	})
	info := m.AgentInfo()
	if info.Name != "mock-agent" {
		t.Errorf("AgentInfo().Name = %q, want %q", info.Name, "mock-agent")
	}
}

// TestCrashMode_IsValid verifies the crash mode validation helper used by the
// debug endpoint to reject unknown modes.
func TestCrashMode_IsValid(t *testing.T) {
	cases := []struct {
		mode CrashMode
		want bool
	}{
		{CrashModeSigkill, true},
		{CrashModePanic, true},
		{CrashModeUncleanExit, true},
		{CrashMode("bogus"), false},
		{CrashMode(""), false},
	}
	for _, c := range cases {
		if got := c.mode.IsValid(); got != c.want {
			t.Errorf("%q.IsValid() = %v, want %v", c.mode, got, c.want)
		}
	}
}

// TestParseCrashMode verifies the string -> CrashMode parsing used by the
// debug HTTP endpoint.
func TestParseCrashMode(t *testing.T) {
	cases := []struct {
		in      string
		want    CrashMode
		wantOk  bool
	}{
		{"sigkill", CrashModeSigkill, true},
		{"panic", CrashModePanic, true},
		{"unclean-exit", CrashModeUncleanExit, true},
		{"SIGKILL", CrashModeSigkill, true}, // case-insensitive
		{"bogus", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ParseCrashMode(c.in)
		if ok != c.wantOk {
			t.Errorf("ParseCrashMode(%q) ok = %v, want %v", c.in, ok, c.wantOk)
			continue
		}
		if ok && got != c.want {
			t.Errorf("ParseCrashMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// drainText reads agent_message_chunk notifications from the mock and returns
// the concatenated text. It returns when no notification arrives within 100ms.
func drainText(m *MockAdapter) string {
	var sb strings.Builder
	for {
		select {
		case notif := <-m.Notifications():
			if notif.Method == MethodSessionUpdate {
				var base SessionUpdateBase
				if err := unmarshalRaw(notif.Params, &base); err == nil && base.SessionUpdate == UpdateAgentMessageChunk {
					var chunk AgentMessageChunkUpdate
					if err := unmarshalRaw(notif.Params, &chunk); err == nil {
						sb.WriteString(chunk.Content.Text)
					}
				}
			}
		case <-time.After(100 * time.Millisecond):
			return sb.String()
		}
	}
}

// unmarshalRaw is a small helper to avoid repeating json.Unmarshal boilerplate.
func unmarshalRaw(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return errors.New("empty params")
	}
	return json.Unmarshal(raw, v)
}
