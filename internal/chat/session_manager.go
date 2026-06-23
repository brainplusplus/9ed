package chat

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RestartConfig carries ADR-0004 auto-restart tuning values, threaded from
// the server's config.Config into every SessionOptions produced by the
// SessionManager. Zero values are replaced with ADR-0004 defaults inside
// applyRestartConfig, but SetRestartConfig lets the server honor custom
// SESSION_RESUME_* env var values.
//
// It also carries ADR-0005 PTY tuning (PTYRingBufferSize, PTYInputLockTTL)
// so PTY fallback sessions honor the PTY_RING_BUFFER_SIZE / PTY_INPUT_LOCK_TTL
// env vars.
type RestartConfig struct {
	MaxRetries       int
	RestartBaseDelay time.Duration
	RestartMaxDelay  time.Duration
	// ADR-0005: PTY fallback tuning (threaded from config.Config).
	PTYRingBufferSize int
	PTYInputLockTTL   time.Duration
}

// newChatSessionCtor and newACPResumedSessionCtor are indirection points over
// NewChatSession / newACPResumedSession so tests can substitute a fake
// constructor (e.g., to capture the SessionOptions handed to the session
// without spawning a real subprocess). They default to the real constructors.
var (
	newChatSessionCtor        = NewChatSession
	newACPResumedSessionCtor  = newACPResumedSession
)

type SessionManager struct {
	sessions    map[string]ChatSession
	recordIDs   map[string]string
	graceTimers map[string]*time.Timer
	graceWindow time.Duration
	// ADR-0004: restart tuning threaded from config.Config (set via
	// SetRestartConfig). Zero-valued until configured; enrichOpts falls back
	// to defaultRestart* constants in that case.
	restartCfg RestartConfig
	mu         sync.Mutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:    make(map[string]ChatSession),
		recordIDs:   make(map[string]string),
		graceTimers: make(map[string]*time.Timer),
		graceWindow: 10 * time.Minute, // ADR-0003: default, configurable via SetGraceWindow
	}
}

// SetGraceWindow sets the grace window for session teardown (ADR-0003).
// When a session is removed, it stays alive for this duration before being
// closed, allowing reconnect with the same clientId to resume.
func (m *SessionManager) SetGraceWindow(d time.Duration) {
	m.mu.Lock()
	m.graceWindow = d
	m.mu.Unlock()
}

// SetRestartConfig stores ADR-0004 auto-restart tuning values so Create and
// Resume can thread them into every SessionOptions. Call this once at server
// startup with values parsed from config.Config (which itself reads
// SESSION_RESUME_MAX_RETRIES / SESSION_RESUME_BASE_DELAY /
// SESSION_RESUME_MAX_DELAY env vars).
func (m *SessionManager) SetRestartConfig(cfg RestartConfig) {
	m.mu.Lock()
	m.restartCfg = cfg
	m.mu.Unlock()
}

// restartConfig returns a copy of the stored RestartConfig with zero values
// replaced by ADR-0004 defaults. This keeps the default-fallback logic in one
// place so callers (enrichOpts) always see usable values.
func (m *SessionManager) restartConfig() RestartConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.restartCfg
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultRestartMaxRetries
	}
	if cfg.RestartBaseDelay <= 0 {
		cfg.RestartBaseDelay = defaultRestartBaseDelay
	}
	if cfg.RestartMaxDelay <= 0 {
		cfg.RestartMaxDelay = defaultRestartMaxDelay
	}
	return cfg
}

// enrichOpts returns a copy of opts with the ADR-0004 restart tuning fields
// (MaxRetries, RestartBaseDelay, RestartMaxDelay) populated from the manager's
// RestartConfig when the caller did not supply them. Caller-supplied non-zero
// values are preserved (allowing per-session overrides). This is the wiring
// point that ensures freshly created ACP sessions honor the
// SESSION_RESUME_* env vars (VAL-RESUME-001).
//
// It also threads the ADR-0005 PTY tuning (PTYRingBufferSize, PTYInputLockTTL)
// so PTY fallback sessions honor the PTY_RING_BUFFER_SIZE /
// PTY_INPUT_LOCK_TTL env vars.
func (m *SessionManager) enrichOpts(opts SessionOptions) SessionOptions {
	cfg := m.restartConfig()
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = cfg.MaxRetries
	}
	if opts.RestartBaseDelay <= 0 {
		opts.RestartBaseDelay = cfg.RestartBaseDelay
	}
	if opts.RestartMaxDelay <= 0 {
		opts.RestartMaxDelay = cfg.RestartMaxDelay
	}
	if opts.PTYRingBufferSize <= 0 {
		opts.PTYRingBufferSize = cfg.PTYRingBufferSize
	}
	if opts.PTYInputLockTTL <= 0 {
		opts.PTYInputLockTTL = cfg.PTYInputLockTTL
	}
	return opts
}

func (m *SessionManager) Create(ctx context.Context, agent AgentDescriptor, workDir string, opts SessionOptions) (ChatSession, error) {
	if !agent.Available {
		return nil, fmt.Errorf("agent %q is not available", agent.ID)
	}

	// ADR-0004: enable auto-restart for ACP sessions by default and thread
	// the config-derived restart tuning (max retries, base/max delay) into
	// SessionOptions so freshly created sessions honor the SESSION_RESUME_*
	// env vars (VAL-RESUME-001).
	if agent.SupportsACP {
		opts.AutoRestart = true
	}
	opts = m.enrichOpts(opts)

	session, err := newChatSessionCtor(ctx, agent, workDir, opts)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.cancelGraceTimerLocked(session.ID())
	m.sessions[session.ID()] = session
	m.recordIDs[session.ID()] = session.ID()
	m.mu.Unlock()

	return session, nil
}

func (m *SessionManager) Resume(ctx context.Context, agent AgentDescriptor, workDir, acpSessionID string, opts SessionOptions) (ChatSession, error) {
	if !agent.Available {
		return nil, fmt.Errorf("agent %q is not available", agent.ID)
	}
	if !agent.SupportsACP {
		return nil, fmt.Errorf("agent %q does not support ACP, cannot resume", agent.ID)
	}

	// ADR-0004: enable auto-restart for resumed ACP sessions by default and
	// thread the config-derived restart tuning into SessionOptions so a
	// crashed resumed session can be re-resumed with the configured backoff.
	opts.AutoRestart = true
	opts = m.enrichOpts(opts)

	session, err := newACPResumedSessionCtor(ctx, agent, workDir, acpSessionID, opts)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.cancelGraceTimerLocked(session.ID())
	// Close the old session if one exists with the same ID (common when the ACP
	// agent returns the same session ID after resume). Without this, the old
	// session's adapter subprocess and goroutines leak.
	var oldSession ChatSession
	if existing, ok := m.sessions[session.ID()]; ok {
		oldSession = existing
	}
	m.sessions[session.ID()] = session
	m.mu.Unlock()

	if oldSession != nil {
		go func(s ChatSession) { _ = s.Close() }(oldSession)
	}

	return session, nil
}

func (m *SessionManager) Get(id string) (ChatSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// ADR-0003: cancel any pending grace window teardown — the session is
	// being accessed again (reconnect).
	m.cancelGraceTimerLocked(id)
	s, ok := m.sessions[id]
	return s, ok
}

// Remove schedules session teardown after the grace window (ADR-0003).
// If the session is accessed (Get/Create/Resume) before the timer fires,
// the teardown is cancelled. Use RemoveNow for immediate teardown.
func (m *SessionManager) Remove(id string) {
	m.mu.Lock()
	grace := m.graceWindow
	if grace <= 0 {
		// Grace disabled — immediate teardown.
		m.removeNowLocked(id)
		m.mu.Unlock()
		return
	}

	_, ok := m.sessions[id]
	if !ok {
		// Already removed or never existed.
		m.cancelGraceTimerLocked(id)
		m.mu.Unlock()
		return
	}

	// Cancel any existing grace timer, then schedule a new one.
	m.cancelGraceTimerLocked(id)
	m.graceTimers[id] = time.AfterFunc(grace, func() {
		m.mu.Lock()
		m.removeNowLocked(id)
		delete(m.graceTimers, id)
		m.mu.Unlock()
	})
	m.mu.Unlock()
}

// RemoveNow immediately tears down a session without grace window.
func (m *SessionManager) RemoveNow(id string) {
	m.mu.Lock()
	m.removeNowLocked(id)
	m.mu.Unlock()
}

func (m *SessionManager) removeNowLocked(id string) {
	m.cancelGraceTimerLocked(id)
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
		delete(m.recordIDs, id)
	}
	if ok && s != nil {
		// Close outside the lock to avoid blocking other operations.
		go func(session ChatSession) {
			_ = session.Close()
		}(s)
	}
}

func (m *SessionManager) cancelGraceTimerLocked(id string) {
	if timer, ok := m.graceTimers[id]; ok {
		timer.Stop()
		delete(m.graceTimers, id)
	}
}

func (m *SessionManager) List() []ChatSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]ChatSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

func (m *SessionManager) IsLive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; ok {
		return true
	}
	for _, recordID := range m.recordIDs {
		if recordID == id {
			return true
		}
	}
	return false
}

func (m *SessionManager) LinkRecordID(liveSessionID, recordID string) {
	if liveSessionID == "" || recordID == "" {
		return
	}
	m.mu.Lock()
	m.recordIDs[liveSessionID] = recordID
	m.mu.Unlock()
}

func (m *SessionManager) RecordIDFor(liveSessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if recordID, ok := m.recordIDs[liveSessionID]; ok && recordID != "" {
		return recordID
	}
	return liveSessionID
}

func (m *SessionManager) LiveIDForRecordID(recordID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[recordID]; ok {
		return recordID, true
	}
	for liveID, mappedRecordID := range m.recordIDs {
		if mappedRecordID == recordID {
			if _, ok := m.sessions[liveID]; ok {
				return liveID, true
			}
		}
	}
	return "", false
}
