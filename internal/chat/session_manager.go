package chat

import (
	"context"
	"fmt"
	"sync"
)

// SessionManager manages ChatSession instances (ACP + PTY).
type SessionManager struct {
	sessions map[string]ChatSession
	mu       sync.Mutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]ChatSession),
	}
}

func (m *SessionManager) Create(ctx context.Context, agent AgentDescriptor, workDir string) (ChatSession, error) {
	if !agent.Available {
		return nil, fmt.Errorf("agent %q is not available", agent.ID)
	}

	session, err := NewChatSession(ctx, agent, workDir)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[session.ID()] = session
	m.mu.Unlock()

	return session, nil
}

func (m *SessionManager) Get(id string) (ChatSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *SessionManager) Remove(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if ok && s != nil {
		_ = s.Close()
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
