package httpapi

import (
	"sync"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/debug"
)

type chatEventPersister func(chat.ChatEvent)

type chatStreamRegistry struct {
	mu       sync.Mutex
	streams  map[string]*chatStream
	latestID string
}

func newChatStreamRegistry() *chatStreamRegistry {
	return &chatStreamRegistry{streams: make(map[string]*chatStream)}
}

func (r *chatStreamRegistry) GetOrCreate(sessionID string, session chat.ChatSession, persist chatEventPersister) *chatStream {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stream, ok := r.streams[sessionID]; ok {
		r.latestID = sessionID
		debug.Printf("[chat/stream] reuse session=%s", sessionID)
		return stream
	}

	var stream *chatStream
	stream = newChatStream(sessionID, session, persist, func() {
		r.mu.Lock()
		if r.streams[sessionID] == stream {
			delete(r.streams, sessionID)
		}
		r.mu.Unlock()
	})
	r.streams[sessionID] = stream
	r.latestID = sessionID
	debug.Printf("[chat/stream] create session=%s agent=%s mode=%s record=%s", sessionID, session.AgentID(), session.Mode(), session.ACPSessionID())
	stream.Start()
	return stream
}

func (r *chatStreamRegistry) Touch(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.streams[sessionID]; !ok {
		return false
	}
	r.latestID = sessionID
	return true
}

func (r *chatStreamRegistry) LatestID() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latestID != "" {
		if _, ok := r.streams[r.latestID]; ok {
			return r.latestID, true
		}
	}
	for id := range r.streams {
		return id, true
	}
	return "", false
}

type chatSubscriber struct {
	C chan chat.ChatEvent
}

type chatStream struct {
	sessionID   string
	session     chat.ChatSession
	persist     chatEventPersister
	onDone      func()
	subscribers map[*chatSubscriber]struct{}
	mu          sync.Mutex
	done        chan struct{}
}

func newChatStream(sessionID string, session chat.ChatSession, persist chatEventPersister, onDone func()) *chatStream {
	return &chatStream{
		sessionID:   sessionID,
		session:     session,
		persist:     persist,
		onDone:      onDone,
		subscribers: make(map[*chatSubscriber]struct{}),
		done:        make(chan struct{}),
	}
}

func (s *chatStream) Start() {
	go s.run()
}

func (s *chatStream) Subscribe() *chatSubscriber {
	sub := &chatSubscriber{C: make(chan chat.ChatEvent, 256)}
	s.mu.Lock()
	select {
	case <-s.done:
		close(sub.C)
	default:
		s.subscribers[sub] = struct{}{}
		debug.Printf("[chat/stream] subscribe session=%s subscribers=%d", s.sessionID, len(s.subscribers))
	}
	s.mu.Unlock()
	return sub
}

func (s *chatStream) Unsubscribe(sub *chatSubscriber) {
	s.mu.Lock()
	if _, ok := s.subscribers[sub]; ok {
		delete(s.subscribers, sub)
		close(sub.C)
		debug.Printf("[chat/stream] unsubscribe session=%s subscribers=%d", s.sessionID, len(s.subscribers))
	}
	s.mu.Unlock()
}

func (s *chatStream) run() {
	defer func() {
		s.mu.Lock()
		subscriberCount := len(s.subscribers)
		close(s.done)
		for sub := range s.subscribers {
			delete(s.subscribers, sub)
			close(sub.C)
		}
		s.mu.Unlock()
		debug.Printf("[chat/stream] closed session=%s subscribersClosed=%d", s.sessionID, subscriberCount)
		if s.onDone != nil {
			s.onDone()
		}
	}()

	for {
		select {
		case <-s.session.Done():
			debug.Printf("[chat/stream] session done session=%s", s.sessionID)
			s.publish(chat.ChatEvent{Type: "done", StopReason: "session_closed"})
			return
		case evt, ok := <-s.session.Events():
			if !ok {
				debug.Printf("[chat/stream] events closed session=%s", s.sessionID)
				return
			}
			s.publish(evt)
		}
	}
}

func (s *chatStream) publish(evt chat.ChatEvent) {
	if evt.Type == "text" || evt.Type == "done" || evt.Type == "error" || evt.Type == "usage_update" {
		debug.Printf("[chat/stream] publish session=%s type=%s textChars=%d stop=%q err=%q", s.sessionID, evt.Type, len(evt.Text), evt.StopReason, evt.Error)
	}
	if s.persist != nil {
		s.persist(evt)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subscribers {
		select {
		case sub.C <- evt:
		default:
			delete(s.subscribers, sub)
			close(sub.C)
		}
	}
}
