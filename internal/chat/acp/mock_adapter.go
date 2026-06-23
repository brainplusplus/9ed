package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// CrashMode selects how a MockAdapter (or the dev-only crash endpoint)
// terminates the agent subprocess. Mirrors the modes accepted by
// POST /api/_debug/crash-agent.
type CrashMode string

const (
	// CrashModeSigkill kills the subprocess immediately (simulates SIGKILL /
	// Process.Kill). The adapter's Done channel closes and Err returns a
	// killed-style error.
	CrashModeSigkill CrashMode = "sigkill"
	// CrashModePanic simulates a subprocess panic: Err returns a panic-style
	// error.
	CrashModePanic CrashMode = "panic"
	// CrashModeUncleanExit simulates a subprocess that exits with a non-zero
	// status without a clean shutdown handshake.
	CrashModeUncleanExit CrashMode = "unclean-exit"
)

// IsValid reports whether the crash mode is a recognized value.
func (m CrashMode) IsValid() bool {
	switch m {
	case CrashModeSigkill, CrashModePanic, CrashModeUncleanExit:
		return true
	}
	return false
}

// ParseCrashMode parses a case-insensitive crash mode string. Returns the
// CrashMode and ok=true on success, or ("", false) for unknown values. Used by
// the dev-only crash HTTP endpoint.
func ParseCrashMode(s string) (CrashMode, bool) {
	var m CrashMode
	switch normalizeMode(s) {
	case "sigkill":
		m = CrashModeSigkill
	case "panic":
		m = CrashModePanic
	case "unclean-exit":
		m = CrashModeUncleanExit
	default:
		return "", false
	}
	return m, true
}

// normalizeMode lower-cases and trims the input. It is split out so it can be
// tested independently of ParseCrashMode.
func normalizeMode(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

// MockConfig configures a MockAdapter's behavior.
type MockConfig struct {
	// CrashOnNPrompt, when > 0, causes the mock to crash on the Nth Prompt
	// call (1-indexed). Prompts 1..N-1 complete normally with an echo.
	// 0 (default) disables crash-on-N.
	CrashOnNPrompt int
	// SupportsResume controls whether SupportsResume() returns true and
	// whether ResumeSession succeeds. nil (default) enables resume; set to
	// BoolPtr(false) to disable.
	SupportsResume *bool
	// AgentInfo is returned by AgentInfo(). Defaults to a placeholder.
	AgentInfo ImplementationInfo
	// ConfigOptions is returned by NewSession/ResumeSession and ConfigOptions().
	ConfigOptions []SessionConfigOption
}

// BoolPtr returns a pointer to b. Useful for setting *bool fields in
// MockConfig (e.g. SupportsResume).
func BoolPtr(b bool) *bool { return &b }

// MockAdapter is a deterministic, in-process implementation of the Adapter
// interface for unit and integration tests. It does not spawn a subprocess.
//
// Behavior:
//   - NewSession/ResumeSession return a deterministic session ID and any
//     configured ConfigOptions.
//   - Prompt echoes the user's prompt text back as an
//     agent_message_chunk session/update notification, then returns an
//     end_turn result. The echo is deterministic: the same prompt text always
//     produces the same echoed chunk.
//   - When CrashOnNPrompt > 0, the Nth Prompt call crashes the adapter
//     (returns an error, closes Done, sets Err) instead of echoing.
//   - ForceCrash lets a caller simulate a subprocess crash on demand (used by
//     the dev-only crash endpoint tests).
//
// All public methods are safe for concurrent use.
type MockAdapter struct {
	cfg MockConfig

	mu        sync.Mutex
	sessionID string
	closed    bool
	crashed   bool
	crashErr  error

	promptCount  int
	cancelCount  int

	notifications chan *Notification
	requests      chan *Request
	done          chan struct{}
	doneOnce      sync.Once
}

// Compile-time assertion that *MockAdapter satisfies the Adapter interface.
var _ Adapter = (*MockAdapter)(nil)

// NewMockAdapter returns a ready MockAdapter with the given configuration.
func NewMockAdapter(cfg MockConfig) *MockAdapter {
	if cfg.AgentInfo.Name == "" {
		cfg.AgentInfo = ImplementationInfo{
			Name:    "mock-agent",
			Title:   "Mock Agent",
			Version: "test",
		}
	}
	return &MockAdapter{
		cfg:           cfg,
		notifications: make(chan *Notification, 64),
		requests:      make(chan *Request, 16),
		done:          make(chan struct{}),
	}
}

// supportsResume returns the effective resume-support setting: true when the
// caller left SupportsResume nil (default), or the explicit value when set.
func (m *MockAdapter) supportsResume() bool {
	if m.cfg.SupportsResume == nil {
		return true
	}
	return *m.cfg.SupportsResume
}

// NewSession creates a new mock session. The session ID is a deterministic
// UUID generated once and reused for the lifetime of the adapter.
func (m *MockAdapter) NewSession(_ context.Context, _ string) (*SessionNewResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed || m.crashed {
		return nil, fmt.Errorf("mock adapter closed")
	}
	if m.sessionID == "" {
		m.sessionID = "mock-" + uuid.NewString()
	}
	return &SessionNewResult{
		SessionID:     m.sessionID,
		ConfigOptions: m.cfg.ConfigOptions,
	}, nil
}

// ResumeSession resumes a mock session. Fails if SupportsResume is false.
func (m *MockAdapter) ResumeSession(_ context.Context, sessionID, _ string) (*SessionNewResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed || m.crashed {
		return nil, fmt.Errorf("mock adapter closed")
	}
	if !m.supportsResume() {
		return nil, fmt.Errorf("mock agent does not support session/resume")
	}
	if sessionID == "" {
		sessionID = "mock-" + uuid.NewString()
	}
	m.sessionID = sessionID
	return &SessionNewResult{
		SessionID:     sessionID,
		ConfigOptions: m.cfg.ConfigOptions,
	}, nil
}

// SupportsResume reports whether resume is enabled for this mock. Defaults to
// true unless explicitly disabled via MockConfig.SupportsResume.
func (m *MockAdapter) SupportsResume() bool {
	return m.supportsResume()
}

// Prompt echoes the prompt text back as an agent_message_chunk notification
// and returns an end_turn result. When CrashOnNPrompt is reached, it crashes
// instead.
func (m *MockAdapter) Prompt(_ context.Context, _ string, content []ContentBlock) (*SessionPromptResult, error) {
	m.mu.Lock()
	if m.closed || m.crashed {
		err := m.crashErr
		m.mu.Unlock()
		if err == nil {
			err = fmt.Errorf("mock adapter closed")
		}
		return nil, err
	}

	m.promptCount++
	count := m.promptCount
	crashN := m.cfg.CrashOnNPrompt

	// Crash-on-N: if this is the Nth prompt, crash instead of echoing.
	if crashN > 0 && count >= crashN {
		m.crashed = true
		m.crashErr = fmt.Errorf("mock agent crashed on prompt %d (sigkill)", count)
		m.mu.Unlock()
		m.finalize(fmt.Errorf("mock agent crashed on prompt %d (sigkill)", count))
		return nil, m.crashErr
	}

	// Build the echo text from the prompt content blocks.
	echoText := extractText(content)
	m.mu.Unlock()

	// Emit an agent_message_chunk notification with the echoed text. This is
	// deterministic: the same prompt text always yields the same chunk.
	m.emitAgentMessageChunk(echoText)

	return &SessionPromptResult{StopReason: StopReasonEndTurn}, nil
}

// Cancel is a no-op that records the call. It does not crash the adapter.
func (m *MockAdapter) Cancel(_ string) error {
	m.mu.Lock()
	m.cancelCount++
	m.mu.Unlock()
	return nil
}

// CancelCount returns the number of times Cancel was called (for test
// assertions).
func (m *MockAdapter) CancelCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancelCount
}

// PromptCount returns the number of Prompt calls received (for test
// assertions), including the one that triggered a crash.
func (m *MockAdapter) PromptCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.promptCount
}

// Close shuts down the mock gracefully. Idempotent. Subsequent Prompt/Resume
// calls return an error.
func (m *MockAdapter) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	m.finalize(nil)
	return nil
}

// CloseSession is a no-op for the mock (no real session to close).
func (m *MockAdapter) CloseSession(_ context.Context, _ string) error {
	return nil
}

// Done returns a channel that closes when the mock crashes or is closed.
func (m *MockAdapter) Done() <-chan struct{} {
	return m.done
}

// Err returns the error that caused Done to close (e.g. a crash error), or nil
// if the mock is still running or was closed cleanly.
func (m *MockAdapter) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.crashErr
}

// Notifications returns the channel for streaming session/update notifications.
func (m *MockAdapter) Notifications() <-chan *Notification {
	return m.notifications
}

// Requests returns the channel for incoming agent requests. The mock never
// emits requests, so this channel stays empty until closed on Done.
func (m *MockAdapter) Requests() <-chan *Request {
	return m.requests
}

// Respond is a no-op for the mock (the mock never sends requests).
func (m *MockAdapter) Respond(_ int64, _ any, _ *RPCError) error {
	return nil
}

// AgentInfo returns the configured agent implementation info.
func (m *MockAdapter) AgentInfo() ImplementationInfo {
	return m.cfg.AgentInfo
}

// AgentCapabilities returns capabilities reflecting the SupportsResume config.
func (m *MockAdapter) AgentCapabilities() AgentCapabilities {
	caps := AgentCapabilities{}
	if m.supportsResume() {
		caps.SessionCapabilities = &SessionCapabilities{Resume: &struct{}{}, Close: &struct{}{}}
	}
	return caps
}

// ConfigOptions returns the configured config options.
func (m *MockAdapter) ConfigOptions() []SessionConfigOption {
	return m.cfg.ConfigOptions
}

// ForceCrash simulates a subprocess crash with the given mode. It sets Err,
// closes Done, and makes subsequent Prompt calls fail. This is the hook used
// by the dev-only crash endpoint (via the acpSession.Crash method) to kill an
// agent subprocess deterministically for e2e testing.
//
// ForceCrash is safe to call from any goroutine and is idempotent.
func (m *MockAdapter) ForceCrash(mode CrashMode) {
	m.mu.Lock()
	if m.closed || m.crashed {
		m.mu.Unlock()
		return
	}
	m.crashed = true
	m.crashErr = crashErrorFor(mode)
	m.mu.Unlock()
	m.finalize(m.crashErr)
}

// Crash implements the Adapter interface. It is the method called by
// acpSession.CrashAgent (and thus the dev-only crash endpoint). It delegates
// to ForceCrash so the mock and the real subprocess adapter share the same
// crash semantics.
func (m *MockAdapter) Crash(mode CrashMode) error {
	m.ForceCrash(mode)
	return nil
}

// finalize closes the done channel and drains the notification/request
// channels so blocked senders do not leak. Safe to call multiple times.
func (m *MockAdapter) finalize(err error) {
	m.doneOnce.Do(func() {
		close(m.done)
	})
}

// emitAgentMessageChunk sends an agent_message_chunk session/update
// notification containing the echoed text. Non-blocking: if the notification
// buffer is full the chunk is dropped (the mock is for tests where the buffer
// is large enough).
func (m *MockAdapter) emitAgentMessageChunk(text string) {
	chunk := AgentMessageChunkUpdate{
		SessionUpdate: UpdateAgentMessageChunk,
		Content: ContentBlock{
			Type: "text",
			Text: text,
		},
	}
	params, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	notif := &Notification{
		JSONRPC: "2.0",
		Method:  MethodSessionUpdate,
		Params:  params,
	}
	select {
	case m.notifications <- notif:
	default:
	}
}

// crashErrorFor returns the error message used for each crash mode. Split out
// so the dev endpoint and the mock agree on the error text.
func crashErrorFor(mode CrashMode) error {
	switch mode {
	case CrashModeSigkill:
		return fmt.Errorf("mock agent killed by sigkill")
	case CrashModePanic:
		return fmt.Errorf("mock agent panic: runtime error: intentional crash")
	case CrashModeUncleanExit:
		return fmt.Errorf("mock agent unclean exit: subprocess exited with status 1")
	default:
		return fmt.Errorf("mock agent crashed (unknown mode)")
	}
}

// extractText concatenates the Text field of all text content blocks in the
// prompt. Non-text blocks are ignored for the echo.
func extractText(blocks []ContentBlock) string {
	var sb []byte
	for _, b := range blocks {
		if b.Type == "text" {
			sb = append(sb, b.Text...)
		}
	}
	return string(sb)
}

// SetConfigOption is a no-op for the mock; it returns the configured config
// options unchanged.
func (m *MockAdapter) SetConfigOption(_ context.Context, _, _, _ string) ([]SessionConfigOption, error) {
	return m.cfg.ConfigOptions, nil
}
