package httpapi

import (
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/debug"
)

// tuiSnapshotTimeout is the default timeout for TUI snapshot requests
// (VAL-PTY-006). If no tui_snapshot response arrives within this window,
// the server falls back to ring buffer replay.
var tuiSnapshotTimeout = 3 * time.Second

// tuiSnapshotState tracks an in-flight TUI snapshot request (VAL-PTY-006).
// It coordinates the 3s timeout fallback and response de-duplication.
type tuiSnapshotState struct {
	mu               sync.Mutex
	accepted         bool            // true once a tui_snapshot response is accepted
	timer            *time.Timer     // fires the fallback after the timeout
	waitingSub       *chatSubscriber // the new subscriber waiting for the snapshot
	fallbackSnapshot []byte          // ring buffer content for the fallback
}

// SetSubscriberClientID associates a subscriber with a logical clientID
// (VAL-PTY-006). This enables routing tui_snapshot_request to a specific
// subscriber (the primary client). If the subscriber already has a clientID,
// the old mapping is removed before the new one is set.
func (s *chatStream) SetSubscriberClientID(sub *chatSubscriber, clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.clientID != "" {
		delete(s.subscribersByClient, sub.clientID)
	}
	sub.clientID = clientID
	if clientID != "" {
		s.subscribersByClient[clientID] = sub
	}
}

// PublishToClient delivers an event to only the subscriber registered with
// the given clientID (VAL-PTY-006). Used to route tui_snapshot_request to the
// primary client without broadcasting to all subscribers. If no subscriber
// matches, the event is silently dropped.
func (s *chatStream) PublishToClient(clientID string, evt chat.ChatEvent) {
	s.mu.Lock()
	sub, ok := s.subscribersByClient[clientID]
	s.mu.Unlock()
	if !ok {
		debug.Printf("[chat/stream] PublishToClient: no subscriber for clientID=%s session=%s", clientID, s.sessionID)
		return
	}
	// Deliver to the bulk channel (non-blocking, drop-oldest semantics).
	select {
	case sub.C <- evt:
	default:
		select {
		case <-sub.C:
		default:
		}
		select {
		case sub.C <- evt:
		default:
		}
	}
}

// RequestTUISnapshot sends a tui_snapshot_request to the primary client only
// (VAL-PTY-006). If primaryClientID is empty (no primary registered), it
// immediately falls back to ring buffer replay. A timeout starts; if no
// tui_snapshot response arrives before it fires, the ring buffer replay is
// sent to the waiting subscriber. The waitingSub receives either the
// tui_snapshot (via AcceptTUISnapshot broadcast) or the pty_replay fallback.
func (s *chatStream) RequestTUISnapshot(primaryClientID string, waitingSub *chatSubscriber, fallbackSnapshot []byte, timeout time.Duration) {
	s.mu.Lock()
	// Cancel any previous in-flight snapshot request.
	if s.snapshot != nil && s.snapshot.timer != nil {
		s.snapshot.timer.Stop()
	}
	ss := &tuiSnapshotState{
		waitingSub:       waitingSub,
		fallbackSnapshot: fallbackSnapshot,
	}
	s.snapshot = ss
	s.mu.Unlock()

	if primaryClientID == "" {
		// No primary client: immediately fall back to ring buffer replay.
		debug.Printf("[chat/snapshot] no primary client, immediate ring buffer fallback session=%s", s.sessionID)
		s.deliverSnapshotFallback(ss)
		return
	}

	// Send the request to the primary client only.
	s.PublishToClient(primaryClientID, chat.ChatEvent{Type: "tui_snapshot_request"})
	debug.Printf("[chat/snapshot] requested TUI snapshot from primary=%s session=%s", primaryClientID, s.sessionID)

	// Start the timeout fallback.
	ss.mu.Lock()
	ss.timer = time.AfterFunc(timeout, func() {
		s.deliverSnapshotFallback(ss)
	})
	ss.mu.Unlock()
}

// deliverSnapshotFallback sends the ring buffer replay to the waiting
// subscriber if no tui_snapshot response has been accepted yet (VAL-PTY-006).
func (s *chatStream) deliverSnapshotFallback(ss *tuiSnapshotState) {
	ss.mu.Lock()
	if ss.accepted {
		ss.mu.Unlock()
		return // a snapshot response already arrived
	}
	ss.accepted = true
	waitingSub := ss.waitingSub
	fallback := ss.fallbackSnapshot
	ss.mu.Unlock()

	if waitingSub == nil || len(fallback) == 0 {
		return
	}
	// Deliver the ring buffer replay to the waiting subscriber only.
	evt := chat.ChatEvent{Type: "pty_replay", Text: string(fallback)}
	select {
	case waitingSub.C <- evt:
	default:
		select {
		case <-waitingSub.C:
		default:
		}
		select {
		case waitingSub.C <- evt:
		default:
		}
	}
	debug.Printf("[chat/snapshot] delivered ring buffer fallback (%d bytes) session=%s", len(fallback), s.sessionID)
}

// AcceptTUISnapshot accepts a tui_snapshot response (VAL-PTY-006). Only the
// first response per request is accepted; subsequent responses are rejected
// (de-duplicated). When accepted, the snapshot is broadcast to all subscribers
// (so the new joiner receives it) and the timeout fallback is cancelled.
// Returns true if this response was accepted.
func (s *chatStream) AcceptTUISnapshot(text string) bool {
	s.mu.Lock()
	ss := s.snapshot
	s.mu.Unlock()
	if ss == nil {
		return false // no in-flight request
	}
	ss.mu.Lock()
	if ss.accepted {
		ss.mu.Unlock()
		debug.Printf("[chat/snapshot] rejected duplicate tui_snapshot response session=%s", s.sessionID)
		return false // de-dup: already accepted
	}
	ss.accepted = true
	if ss.timer != nil {
		ss.timer.Stop()
	}
	ss.mu.Unlock()

	// Broadcast the snapshot to all subscribers (including the new joiner).
	s.publishDirect(chat.ChatEvent{Type: "tui_snapshot", Text: text})
	debug.Printf("[chat/snapshot] accepted tui_snapshot response (%d chars) session=%s", len(text), s.sessionID)
	return true
}
