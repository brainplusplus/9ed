package chat

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type SessionManager struct {
	sessions    map[string]ChatSession
	recordIDs   map[string]string
	graceTimers map[string]*time.Timer
	graceWindow time.Duration
	mu          sync.Mutex
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

func (m *SessionManager) Create(ctx context.Context, agent AgentDescriptor, workDir string, opts SessionOptions) (ChatSession, error) {
	if !agent.Available {
		return nil, fmt.Errorf("agent %q is not available", agent.ID)
	}

	// ADR-0004: enable auto-restart for ACP sessions by default.
	if agent.SupportsACP {
		opts.AutoRestart = true
	}

	session, err := NewChatSession(ctx, agent, workDir, opts)
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

	// ADR-0004: enable auto-restart for resumed ACP sessions by default.
	opts.AutoRestart = true

	session, err := newACPResumedSession(ctx, agent, workDir, acpSessionID, opts)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.cancelGraceTimerLocked(session.ID())
	m.sessions[session.ID()] = session
	m.mu.Unlock()

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
