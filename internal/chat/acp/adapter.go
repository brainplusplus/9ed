package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// AdapterConfig configures how to spawn an ACP agent subprocess.
type AdapterConfig struct {
	Command    string
	Args       []string
	WorkDir    string
	Env        []string
	MCPServers []MCPServer
}

// SubprocessAdapter manages an ACP agent subprocess and provides high-level
// protocol methods. It is the production implementation of the Adapter
// interface (adapter_interface.go). The MockAdapter (mock_adapter.go) is the
// test double.
type SubprocessAdapter struct {
	cfg    AdapterConfig
	cmd    *exec.Cmd
	client *Client

	stderr *cappedBuffer

	sessionID     string
	agentInfo     ImplementationInfo
	agentCaps     AgentCapabilities
	configOptions []SessionConfigOption

	mu     sync.Mutex
	closed bool
}

// NewSubprocessAdapter spawns the ACP subprocess, initializes the connection,
// and returns a ready adapter.
func NewSubprocessAdapter(ctx context.Context, cfg AdapterConfig) (*SubprocessAdapter, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(os.Environ(), cfg.Env...)
	stderr := &cappedBuffer{max: 8 * 1024}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	client := NewClient(stdin, stdout)

	a := &SubprocessAdapter{
		cfg:    cfg,
		cmd:    cmd,
		client: client,
		stderr: stderr,
	}

	if err := a.initialize(ctx); err != nil {
		_ = a.Close()
		return nil, fmt.Errorf("initialize: %w%s", err, stderr.suffix())
	}

	return a, nil
}

func (a *SubprocessAdapter) initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: 1,
		ClientCapabilities: ClientCapabilities{
			FS: &FSCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
		ClientInfo: &ImplementationInfo{
			Name:    "9ed",
			Title:   "Web IDE Terminal",
			Version: "1.0.0",
		},
	}

	raw, err := a.client.Call(ctx, MethodInitialize, params)
	if err != nil {
		return err
	}

	var result InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("unmarshal initialize result: %w", err)
	}

	if result.AgentInfo != nil {
		a.agentInfo = *result.AgentInfo
	}
	a.agentCaps = result.AgentCapabilities
	return nil
}

// NewSession creates a new ACP session with the given working directory.
func (a *SubprocessAdapter) NewSession(ctx context.Context, cwd string) (*SessionNewResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if cwd == "" {
		cwd = a.cfg.WorkDir
	}

	params := SessionNewParams{
		CWD:        cwd,
		MCPServers: a.cfg.MCPServers,
	}

	raw, err := a.client.Call(ctx, MethodSessionNew, params)
	if err != nil {
		return nil, err
	}

	var result SessionNewResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal session/new result: %w", err)
	}

	a.sessionID = result.SessionID
	a.configOptions = result.ConfigOptions
	return &result, nil
}

// ResumeSession resumes an existing session via session/resume if the agent supports it.
// Returns the session new result (with config options) or an error if resume is unsupported.
func (a *SubprocessAdapter) ResumeSession(ctx context.Context, sessionID, cwd string) (*SessionNewResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.agentCaps.SessionCapabilities == nil || a.agentCaps.SessionCapabilities.Resume == nil {
		return nil, fmt.Errorf("agent does not support session/resume")
	}

	if cwd == "" {
		cwd = a.cfg.WorkDir
	}

	params := SessionResumeParams{
		SessionID:  sessionID,
		CWD:        cwd,
		MCPServers: a.cfg.MCPServers,
	}

	raw, err := a.client.Call(ctx, MethodSessionResume, params)
	if err != nil {
		return nil, fmt.Errorf("session/resume: %w", err)
	}

	var result SessionNewResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal session/resume result: %w", err)
	}

	a.sessionID = result.SessionID
	a.configOptions = result.ConfigOptions
	return &result, nil
}

// SupportsResume returns whether the agent declared session/resume capability.
func (a *SubprocessAdapter) SupportsResume() bool {
	return a.agentCaps.SessionCapabilities != nil && a.agentCaps.SessionCapabilities.Resume != nil
}

// SetConfigOption changes a config option (model, mode, etc) and returns updated state.
func (a *SubprocessAdapter) SetConfigOption(ctx context.Context, sessionID, configID, value string) ([]SessionConfigOption, error) {
	params := SetConfigOptionParams{
		SessionID: sessionID,
		ConfigID:  configID,
		Value:     value,
	}

	raw, err := a.client.Call(ctx, MethodSessionSetConfigOption, params)
	if err != nil {
		return nil, err
	}

	var result SetConfigOptionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal set_config_option result: %w", err)
	}

	a.mu.Lock()
	a.configOptions = result.ConfigOptions
	a.mu.Unlock()

	return result.ConfigOptions, nil
}

// ConfigOptions returns the current config options from the last session/new or set_config_option.
func (a *SubprocessAdapter) ConfigOptions() []SessionConfigOption {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.configOptions
}

// Prompt sends a user message and returns when the turn completes.
// Streaming updates are delivered via the Notifications channel.
func (a *SubprocessAdapter) Prompt(ctx context.Context, sessionID string, content []ContentBlock) (*SessionPromptResult, error) {
	params := SessionPromptParams{
		SessionID: sessionID,
		Prompt:    content,
	}

	raw, err := a.client.Call(ctx, MethodSessionPrompt, params)
	if err != nil {
		return nil, err
	}

	var result SessionPromptResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal session/prompt result: %w", err)
	}
	return &result, nil
}

// Cancel sends a cancellation notification for the current prompt turn.
func (a *SubprocessAdapter) Cancel(sessionID string) error {
	return a.client.Notify(MethodSessionCancel, SessionCancelParams{
		SessionID: sessionID,
	})
}

// CloseSession closes an active session if the agent supports it.
func (a *SubprocessAdapter) CloseSession(ctx context.Context, sessionID string) error {
	if a.agentCaps.SessionCapabilities == nil || a.agentCaps.SessionCapabilities.Close == nil {
		return nil
	}
	_, err := a.client.Call(ctx, MethodSessionClose, SessionCloseParams{
		SessionID: sessionID,
	})
	return err
}

// Notifications returns the channel for streaming session/update notifications.
func (a *SubprocessAdapter) Notifications() <-chan *Notification {
	return a.client.Notifications()
}

// Requests returns the channel for incoming agent requests (fs, terminal, permission).
func (a *SubprocessAdapter) Requests() <-chan *Request {
	return a.client.Requests()
}

// Respond sends a response to an incoming agent request.
func (a *SubprocessAdapter) Respond(id int64, result any, rpcErr *RPCError) error {
	return a.client.Respond(id, result, rpcErr)
}

// AgentInfo returns the agent's implementation info from initialization.
func (a *SubprocessAdapter) AgentInfo() ImplementationInfo {
	return a.agentInfo
}

// AgentCapabilities returns the agent's capabilities from initialization.
func (a *SubprocessAdapter) AgentCapabilities() AgentCapabilities {
	return a.agentCaps
}

// Done returns a channel that closes when the subprocess exits.
func (a *SubprocessAdapter) Done() <-chan struct{} {
	return a.client.Done()
}

// Err returns the client/subprocess error that caused Done to close, if any.
func (a *SubprocessAdapter) Err() error {
	return a.client.Err()
}

// Close terminates the ACP subprocess gracefully.
func (a *SubprocessAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return nil
	}
	a.closed = true

	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
		_ = a.cmd.Wait()
	}
	return nil
}

// Crash kills the agent subprocess deterministically using the given crash
// mode. It is used exclusively by the dev-only POST /api/_debug/crash-agent
// endpoint (gated by the debug build tag + DEBUG=true env) to test the
// auto-restart logic (ADR-0004) without waiting for a natural crash.
//
// Modes:
//   - CrashModeSigkill: immediately sends os.Kill to the subprocess (no
//     shutdown handshake). Simulates the most common real-world crash.
//   - CrashModePanic: same as sigkill for the subprocess, but Err() returns a
//     panic-style error so restart classification can be tested.
//   - CrashModeUncleanExit: kills the subprocess with an error simulating a
//     non-zero exit code.
//
// Crash is idempotent: calling it on an already-closed/crashed adapter is a
// no-op. It closes Done and sets Err via the client's subprocess-exit watcher.
func (a *SubprocessAdapter) Crash(mode CrashMode) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	a.mu.Unlock()

	if a.cmd == nil || a.cmd.Process == nil {
		return fmt.Errorf("no subprocess to crash")
	}

	// All modes kill the process immediately. The mode determines the error
	// text that Err() reports (via the client's exit watcher / our crashErrorFor
	// helper). For panic and unclean-exit, we set the client error so Err()
	// returns the expected classification even if the OS exit code is generic.
	switch mode {
	case CrashModePanic, CrashModeUncleanExit:
		// Set the error on the client before killing so Err() classifies
		// correctly even if the exit watcher hasn't fired yet.
		if a.client != nil {
			a.client.SetCrashError(crashErrorFor(mode))
		}
	}

	_ = a.cmd.Process.Kill()
	_ = a.cmd.Wait()
	return nil
}

// cappedBuffer is a thread-safe writer that retains at most max bytes of the
// most recent output. It is used to capture a subprocess's stderr tail so that
// fatal startup messages (e.g. a corrupted Bun/npm global install) can be
// surfaced in the returned error instead of being lost to os.Stderr only.
type cappedBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	if c.max > 0 && len(c.buf) > c.max {
		c.buf = c.buf[len(c.buf)-c.max:]
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.buf)
}

// suffix returns the captured stderr formatted for appending to an error
// message, or an empty string when nothing was captured.
func (c *cappedBuffer) suffix() string {
	s := strings.TrimSpace(c.String())
	if s == "" {
		return ""
	}
	return ": " + s
}
