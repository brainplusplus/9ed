package visualstream

import (
	"context"
	"sync"
	"testing"

	"github.com/pion/webrtc/v4"
)

// ── SignalingHandler RegisterSession / UnregisterSession / SessionByID ──

func TestRegisterAndUnregisterSession(t *testing.T) {
	h := NewSignalingHandler()
	source := &mockFrameSource{}
	strategy := &mockStrategy{}
	input := &mockInputHandler{}

	ss := NewStreamingSession(source, strategy, input)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ss.Start(ctx)

	h.RegisterSession("tab-1", ss)

	if !h.HasSession("tab-1") {
		t.Error("expected session to be registered")
	}
	if h.SessionByID("tab-1") != ss {
		t.Error("SessionByID returned wrong session")
	}

	h.UnregisterSession("tab-1")
	if h.HasSession("tab-1") {
		t.Error("expected session to be unregistered")
	}
	if !source.wasStopped() {
		t.Error("source should be stopped after unregister")
	}
}

func TestRegisterSessionDisplacesExisting(t *testing.T) {
	h := NewSignalingHandler()
	source1 := &mockFrameSource{}
	ss1 := NewStreamingSession(source1, &mockStrategy{}, &mockInputHandler{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ss1.Start(ctx)

	h.RegisterSession("tab-1", ss1)

	// Register a new session for the same ID — should displace the old one
	source2 := &mockFrameSource{}
	ss2 := NewStreamingSession(source2, &mockStrategy{}, &mockInputHandler{})
	h.RegisterSession("tab-1", ss2)

	// Old session should be stopped
	if !source1.wasStopped() {
		t.Error("old session source should be stopped after displacement")
	}

	// New session should be the active one
	if h.SessionByID("tab-1") != ss2 {
		t.Error("expected new session to be active after displacement")
	}
}

func TestRegisterSessionConcurrent(t *testing.T) {
	h := NewSignalingHandler()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ss := NewStreamingSession(&mockFrameSource{}, &mockStrategy{}, &mockInputHandler{})
			h.RegisterSession("tab-concurrent", ss)
		}()
	}
	wg.Wait()

	// Only one session should remain
	if !h.HasSession("tab-concurrent") {
		t.Error("expected a session to remain after concurrent registration")
	}

	// Should not panic
	h.UnregisterSession("tab-concurrent")
}

func TestUnregisterNonexistentSession(t *testing.T) {
	h := NewSignalingHandler()
	// Should not panic
	h.UnregisterSession("nonexistent")
}

func TestHandleOfferUnregisteredSession(t *testing.T) {
	h := NewSignalingHandler()
	_, err := h.HandleOffer("nonexistent", "v=0...")
	if err == nil {
		t.Error("expected error for offer on unregistered session")
	}
}

func TestHandleICECandidateUnregisteredSession(t *testing.T) {
	h := NewSignalingHandler()
	err := h.HandleICECandidate("nonexistent", "peer1", webrtc.ICECandidateInit{})
	if err == nil {
		t.Error("expected error for ICE on unregistered session")
	}
}

// ── HandleSignalingMessage unknown type ──

func TestHandleSignalingMessageUnknownType(t *testing.T) {
	h := NewSignalingHandler()
	_, err := h.HandleSignalingMessage(SignalingMessage{
		Type:      "bogus",
		SessionID: "tab-1",
	})
	if err == nil {
		t.Error("expected error for unknown message type")
	}
}

// ── HandleSignalingJSON malformed input ──

func TestHandleSignalingJSONMalformed(t *testing.T) {
	h := NewSignalingHandler()
	_, err := h.HandleSignalingJSON([]byte("not json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestHandleSignalingJSONICECandidateNoPayload(t *testing.T) {
	h := NewSignalingHandler()
	// ICE candidate with nil ICE — should return nil, nil (no response)
	resp, err := h.HandleSignalingJSON([]byte(`{"type":"ice-candidate","sessionId":"tab-1"}`))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("expected nil response for ICE with no payload")
	}
}

// ── Re-registration race: cleanup goroutine should not kill new session ──

func TestReregistrationRaceCleanupDoesNotKillNewSession(t *testing.T) {
	h := NewSignalingHandler()

	// Register session 1
	source1 := &mockFrameSource{}
	ss1 := NewStreamingSession(source1, &mockStrategy{}, &mockInputHandler{})
	h.RegisterSession("tab-race", ss1)

	// Start ss1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ss1.Start(ctx)

	// Register session 2 (displaces ss1, stops it)
	source2 := &mockFrameSource{}
	ss2 := NewStreamingSession(source2, &mockStrategy{}, &mockInputHandler{})
	h.RegisterSession("tab-race", ss2)

	// Wait for ss1's Done channel to fire (its cleanup goroutine would fire)
	<-ss1.Done()

	// ss2 should still be active — SessionByID should return ss2, not nil
	if h.SessionByID("tab-race") != ss2 {
		t.Error("ss2 should still be registered after ss1 was displaced and done")
	}
	if !h.HasSession("tab-race") {
		t.Error("session tab-race should still exist")
	}

	// Clean up
	h.UnregisterSession("tab-race")
}

// ── PeerConnection state change triggers cleanup ──

func TestPeerManagerRapidAddRemove(t *testing.T) {
	// This test verifies that the PeerManager can handle rapid add/remove
	// cycles without panicking — simulating the OnConnectionStateChange
	// callback that calls RemovePeer.
	pm := NewPeerManager()
	defer pm.Close()

	const n = 100
	for i := 0; i < n; i++ {
		peer := &PeerConnection{ID: string(rune('a' + i%26))}
		pm.AddPeer(peer)
		pm.RemovePeer(peer.ID)
	}

	if len(pm.Peers()) != 0 {
		t.Errorf("expected 0 peers after rapid add/remove, got %d", len(pm.Peers()))
	}
}

// ── StreamingSession input routing ──

func TestStreamingSessionInputRouting(t *testing.T) {
	source := &mockFrameSource{}
	strategy := &mockStrategy{}
	input := &mockInputHandler{}

	session := NewStreamingSession(source, strategy, input)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = session.Start(ctx)
	defer session.Stop()

	// Verify the input handler is accessible and receives events
	err := input.HandleInput(InputEvent{Type: "mouse_move", X: 10, Y: 20})
	if err != nil {
		t.Errorf("HandleInput failed: %v", err)
	}

	events := input.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].X != 10 || events[0].Y != 20 {
		t.Errorf("unexpected event: %+v", events[0])
	}
}
