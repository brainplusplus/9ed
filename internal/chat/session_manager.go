package chat

import (
	"context"
	"fmt"
	"sync"
)

type SessionManager struct {
	sessions  map[string]ChatSession
	recordIDs map[string]string
	mu        sync.Mutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:  make(map[string]ChatSession),
		recordIDs: make(map[string]string),
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
	m.recordIDs[session.ID()] = session.ID()
	m.mu.Unlock()

	return session, nil
}

func (m *SessionManager) Resume(ctx context.Context, agent AgentDescriptor, workDir, acpSessionID string) (ChatSession, error) {
	if !agent.Available {
		return nil, fmt.Errorf("agent %q is not available", agent.ID)
	}
	if !agent.SupportsACP {
		return nil, fmt.Errorf("agent %q does not support ACP, cannot resume", agent.ID)
	}

	session, err := newACPResumedSession(ctx, agent, workDir, acpSessionID)
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
		delete(m.recordIDs, id)
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
