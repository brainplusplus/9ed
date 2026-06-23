package acp

import (
	"context"
)

// Adapter is the interface satisfied by both the real subprocess-backed
// *SubprocessAdapter (adapter.go) and the MockAdapter
// (mock_adapter.go). It captures the high-level protocol methods used by the
// chat session layer (internal/chat/agent.go) and by tests.
//
// Defining this interface lets unit/integration tests substitute a
// deterministic MockAdapter without spawning a real agent subprocess, and lets
// the dev-only crash endpoint depend on the contract rather than the concrete
// subprocess type.
type Adapter interface {
	// NewSession creates a new ACP session with the given working directory.
	NewSession(ctx context.Context, cwd string) (*SessionNewResult, error)
	// ResumeSession resumes an existing session via session/resume if the
	// agent supports it.
	ResumeSession(ctx context.Context, sessionID, cwd string) (*SessionNewResult, error)
	// Prompt sends a user message and returns when the turn completes.
	// Streaming updates are delivered via the Notifications channel.
	Prompt(ctx context.Context, sessionID string, content []ContentBlock) (*SessionPromptResult, error)
	// Cancel sends a cancellation notification for the current prompt turn.
	Cancel(sessionID string) error
	// Done returns a channel that closes when the subprocess exits.
	Done() <-chan struct{}
	// Close terminates the ACP subprocess (or mock) gracefully.
	Close() error
	// Err returns the client/subprocess error that caused Done to close.
	Err() error
	// SupportsResume returns whether the agent declared session/resume
	// capability.
	SupportsResume() bool

	// SetConfigOption changes a config option and returns updated state.
	SetConfigOption(ctx context.Context, sessionID, configID, value string) ([]SessionConfigOption, error)
	// CloseSession closes an active session if the agent supports it.
	CloseSession(ctx context.Context, sessionID string) error
	// Notifications returns the channel for streaming session/update
	// notifications.
	Notifications() <-chan *Notification
	// Requests returns the channel for incoming agent requests (fs, terminal,
	// permission).
	Requests() <-chan *Request
	// Respond sends a response to an incoming agent request.
	Respond(id int64, result any, rpcErr *RPCError) error
	// AgentInfo returns the agent's implementation info from initialization.
	AgentInfo() ImplementationInfo
	// AgentCapabilities returns the agent's capabilities from initialization.
	AgentCapabilities() AgentCapabilities
	// ConfigOptions returns the current config options from the last
	// session/new or set_config_option.
	ConfigOptions() []SessionConfigOption

	// Crash kills the agent subprocess deterministically using the given
	// crash mode (sigkill, panic, unclean-exit). It is used exclusively by
	// the dev-only POST /api/_debug/crash-agent endpoint for testing the
	// auto-restart logic. It must close Done and set Err.
	Crash(mode CrashMode) error
}

// Compile-time assertion that the production subprocess-backed adapter
// satisfies the Adapter interface.
var _ Adapter = (*SubprocessAdapter)(nil)
