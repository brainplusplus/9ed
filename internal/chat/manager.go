package chat

import (
	"fmt"
	"sync"
)

type Manager struct {
	sessions map[string]*Session
	mu       sync.Mutex
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) Create(agent Agent) (*Session, error) {
	if !agent.Available {
		return nil, fmt.Errorf("agent %q is not available", agent.ID)
	}

	session, err := newSession(agent)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	return session, nil
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Manager) Remove(id string) {
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

func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

func (m *Manager) ReplaceSession(oldID string, newSession *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, oldID)
	m.sessions[newSession.ID] = newSession
}
