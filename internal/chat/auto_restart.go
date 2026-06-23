package chat

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// ADR-0004 default restart tuning. Used when SessionOptions does not carry
// config-derived values (e.g., direct construction outside SessionManager).
const (
	defaultRestartMaxRetries = 3
	defaultRestartBaseDelay  = 500 * time.Millisecond
	defaultRestartMaxDelay   = 30 * time.Second
)

// applyRestartConfig wires the auto-restart configuration onto an acpSession.
// It records the agent descriptor and session options (needed by tryRestart to
// re-spawn the subprocess) and copies the restart tuning fields, falling back
// to ADR-0004 defaults when the caller supplied zero values. Both newACPSession
// (Create path) and newACPResumedSession (Resume path) call this so the two
// paths stay in parity.
func applyRestartConfig(s *acpSession, agent AgentDescriptor, opts SessionOptions) {
	s.agentDesc = agent
	s.sessionOpts = opts
	s.autoRestart = opts.AutoRestart

	s.maxRetries = opts.MaxRetries
	if s.maxRetries <= 0 {
		s.maxRetries = defaultRestartMaxRetries
	}

	s.restartBaseDelay = opts.RestartBaseDelay
	if s.restartBaseDelay <= 0 {
		s.restartBaseDelay = defaultRestartBaseDelay
	}

	s.restartMaxDelay = opts.RestartMaxDelay
	if s.restartMaxDelay <= 0 {
		s.restartMaxDelay = defaultRestartMaxDelay
	}
}

// shouldAttemptRestart returns true if tryRestart should proceed with retry
// attempts given the adapter error, resume capability, and auto-restart flag.
// Persistent errors and missing resume support mean no retry (ADR-0004).
func shouldAttemptRestart(adapterErr error, supportsResume, autoRestart bool) bool {
	if !autoRestart {
		return false
	}
	if isPersistentError(adapterErr) {
		return false
	}
	if !supportsResume {
		return false
	}
	return true
}

// crashDoneEvent builds the done event emitted when the agent subprocess
// crashes unrecoverably. CanResume reflects whether the agent supports
// session/resume so the client can show an actionable recovery prompt
// (ADR-0004 / VAL-RESUME-005).
func crashDoneEvent(canResume bool) ChatEvent {
	return ChatEvent{
		Type:      "done",
		StopReason: "agent_crash_unrecoverable",
		CanResume: canResume,
	}
}

// sessionResumedEvent builds the session_resumed event carrying a freshly
// generated epoch UUID. Clients detect the epoch change and re-fetch the tail
// (ADR-0002 / ADR-0004 / VAL-CATCHUP-005).
func sessionResumedEvent(sessionID string) ChatEvent {
	return ChatEvent{
		Type:  "session_resumed",
		Text:  sessionID,
		Epoch: uuid.NewString(),
	}
}

// isPersistentError classifies whether an error is persistent (config error,
// auth expired, binary not found) vs transient (network blip, EOF, OOM).
// Persistent errors should not be retried (ADR-0004).
func isPersistentError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	persistentMarkers := []string{
		"no such file or directory",
		"executable file not found",
		"command not found",
		"permission denied",
		"unauthorized",
		"authentication",
		"auth expired",
		"invalid api key",
		"config error",
		"not supported",
		"does not support",
	}
	for _, marker := range persistentMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// isTransientError classifies whether an error is transient (network blip,
// EOF, EPIPE, OOM, process killed). These should be retried (ADR-0004).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if isPersistentError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	transientMarkers := []string{
		"eof",
		"broken pipe",
		"connection reset",
		"connection refused",
		"connection closed",
		"epipe",
		"signal: killed",
		"signal: terminated",
		"context deadline exceeded",
		"timeout",
		"temporary failure",
	}
	for _, marker := range transientMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return true // default: treat unknown errors as transient (retry)
}
