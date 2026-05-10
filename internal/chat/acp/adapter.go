package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// AdapterConfig configures how to spawn an ACP agent subprocess.
type AdapterConfig struct {
	Command string
	Args    []string
	WorkDir string
	Env     []string
}

// Adapter manages an ACP agent subprocess and provides high-level protocol methods.
type Adapter struct {
	cfg    AdapterConfig
	cmd    *exec.Cmd
	client *Client

	sessionID     string
	agentInfo     ImplementationInfo
	agentCaps     AgentCapabilities
	configOptions []SessionConfigOption

	mu     sync.Mutex
	closed bool
}

// NewAdapter spawns the ACP subprocess, initializes the connection, and returns a ready adapter.
func NewAdapter(ctx context.Context, cfg AdapterConfig) (*Adapter, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(os.Environ(), cfg.Env...)
	cmd.Stderr = os.Stderr

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

	a := &Adapter{
		cfg:    cfg,
		cmd:    cmd,
		client: client,
	}

	if err := a.initialize(ctx); err != nil {
		_ = a.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	return a, nil
}

func (a *Adapter) initialize(ctx context.Context) error {
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
func (a *Adapter) NewSession(ctx context.Context, cwd string) (*SessionNewResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if cwd == "" {
		cwd = a.cfg.WorkDir
	}

	params := SessionNewParams{
		CWD:        cwd,
		MCPServers: []MCPServer{},
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
func (a *Adapter) ResumeSession(ctx context.Context, sessionID, cwd string) (*SessionNewResult, error) {
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
		MCPServers: []MCPServer{},
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
func (a *Adapter) SupportsResume() bool {
	return a.agentCaps.SessionCapabilities != nil && a.agentCaps.SessionCapabilities.Resume != nil
}

// SetConfigOption changes a config option (model, mode, etc) and returns updated state.
func (a *Adapter) SetConfigOption(ctx context.Context, sessionID, configID, value string) ([]SessionConfigOption, error) {
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
func (a *Adapter) ConfigOptions() []SessionConfigOption {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.configOptions
}

// Prompt sends a user message and returns when the turn completes.
// Streaming updates are delivered via the Notifications channel.
func (a *Adapter) Prompt(ctx context.Context, sessionID string, content []ContentBlock) (*SessionPromptResult, error) {
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
func (a *Adapter) Cancel(sessionID string) error {
	return a.client.Notify(MethodSessionCancel, SessionCancelParams{
		SessionID: sessionID,
	})
}

// CloseSession closes an active session if the agent supports it.
func (a *Adapter) CloseSession(ctx context.Context, sessionID string) error {
	if a.agentCaps.SessionCapabilities == nil || a.agentCaps.SessionCapabilities.Close == nil {
		return nil
	}
	_, err := a.client.Call(ctx, MethodSessionClose, SessionCloseParams{
		SessionID: sessionID,
	})
	return err
}

// Notifications returns the channel for streaming session/update notifications.
func (a *Adapter) Notifications() <-chan *Notification {
	return a.client.Notifications()
}

// Requests returns the channel for incoming agent requests (fs, terminal, permission).
func (a *Adapter) Requests() <-chan *Request {
	return a.client.Requests()
}

// Respond sends a response to an incoming agent request.
func (a *Adapter) Respond(id int64, result any, rpcErr *RPCError) error {
	return a.client.Respond(id, result, rpcErr)
}

// AgentInfo returns the agent's implementation info from initialization.
func (a *Adapter) AgentInfo() ImplementationInfo {
	return a.agentInfo
}

// AgentCapabilities returns the agent's capabilities from initialization.
func (a *Adapter) AgentCapabilities() AgentCapabilities {
	return a.agentCaps
}

// Done returns a channel that closes when the subprocess exits.
func (a *Adapter) Done() <-chan struct{} {
	return a.client.Done()
}

// Close terminates the ACP subprocess gracefully.
func (a *Adapter) Close() error {
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
