package terminal

import (
	"errors"
	"sync"

	"github.com/google/uuid"
)

const replayBufferMaxBytes = 200_000

type ShellProfile struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	CWD     string   `json:"cwd,omitempty"`
}

type PtySession interface {
	ID() string
	Profile() ShellProfile
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(cols uint16, rows uint16) error
	Close() error
}

type SpawnFunc func(profile ShellProfile) (PtySession, error)

type ManagedSession struct {
	ID          string       `json:"id"`
	Profile     ShellProfile `json:"profile"`
	pty         PtySession
	mu          sync.Mutex
	replay      []byte
	subscribers map[chan []byte]struct{}
	closeOnce   sync.Once
}

func (s *ManagedSession) Read(p []byte) (int, error) {
	return s.pty.Read(p)
}

func (s *ManagedSession) Write(p []byte) (int, error) {
	return s.pty.Write(p)
}

func (s *ManagedSession) Resize(cols uint16, rows uint16) error {
	return s.pty.Resize(cols, rows)
}

func (s *ManagedSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.pty.Close()
		s.closeSubscribers()
	})
	return err
}

func (s *ManagedSession) Subscribe(includeReplay bool) (<-chan []byte, func()) {
	ch := make(chan []byte, 128)

	s.mu.Lock()
	var replay []byte
	if includeReplay && len(s.replay) > 0 {
		replay = append([]byte(nil), s.replay...)
	}
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	if len(replay) > 0 {
		ch <- replay
	}

	unsubscribe := func() {
		s.mu.Lock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
		s.mu.Unlock()
	}

	return ch, unsubscribe
}

func (s *ManagedSession) startOutputPump() {
	go func() {
		buffer := make([]byte, 4096)
		for {
			count, err := s.pty.Read(buffer)
			if count > 0 {
				s.broadcast(buffer[:count])
			}
			if err != nil {
				s.closeSubscribers()
				return
			}
		}
	}()
}

func (s *ManagedSession) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)

	s.mu.Lock()
	s.replay = append(s.replay, chunk...)
	if len(s.replay) > replayBufferMaxBytes {
		s.replay = append([]byte(nil), s.replay[len(s.replay)-replayBufferMaxBytes:]...)
	}
	for ch := range s.subscribers {
		select {
		case ch <- chunk:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *ManagedSession) closeSubscribers() {
	s.mu.Lock()
	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
	s.mu.Unlock()
}

func NewManagedSession(id string, profile ShellProfile, pty PtySession) *ManagedSession {
	session := &ManagedSession{
		ID:          id,
		Profile:     profile,
		pty:         pty,
		subscribers: make(map[chan []byte]struct{}),
	}
	session.startOutputPump()
	return session
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*ManagedSession
	spawn    SpawnFunc
}

func NewManager(spawn SpawnFunc) *Manager {
	return &Manager{
		sessions: make(map[string]*ManagedSession),
		spawn:    spawn,
	}
}

func (m *Manager) Create(profile ShellProfile) (*ManagedSession, error) {
	if m.spawn == nil {
		return nil, errors.New("spawn function is required")
	}

	ptySession, err := m.spawn(profile)
	if err != nil {
		return nil, err
	}

	id := ptySession.ID()
	if id == "" {
		id = uuid.NewString()
	}

	session := NewManagedSession(id, ptySession.Profile(), ptySession)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = session

	return session, nil
}

func (m *Manager) Get(id string) (*ManagedSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[id]
	return session, ok
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}

	return session.Close()
}
