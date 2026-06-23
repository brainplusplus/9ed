package httpapi

import (
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
)

// TestPublishToClient_OnlyTargetReceives (VAL-PTY-006): PublishToClient
// delivers an event only to the subscriber registered with the given
// clientID. Other subscribers do not receive it.
func TestPublishToClient_OnlyTargetReceives(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer stream.invalidate()

	primary := stream.Subscribe()
	secondary := stream.Subscribe()

	stream.SetSubscriberClientID(primary, "clientA")
	stream.SetSubscriberClientID(secondary, "clientB")

	stream.PublishToClient("clientA", chat.ChatEvent{Type: "tui_snapshot_request"})

	// Primary receives the request.
	evt := readSubEvent(t, primary)
	if evt.Type != "tui_snapshot_request" {
		t.Fatalf("primary expected tui_snapshot_request, got %s", evt.Type)
	}

	// Secondary does NOT receive it.
	assertNoSubEvent(t, secondary, 200*time.Millisecond)
}

// TestRequestTUISnapshot_SentToPrimaryOnly (VAL-PTY-006): when a new
// subscriber joins a TUI-mode PTY session, the snapshot request is sent only
// to the primary client, not to all subscribers.
func TestRequestTUISnapshot_SentToPrimaryOnly(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer stream.invalidate()

	// Primary subscriber (already connected).
	primary := stream.Subscribe()
	stream.SetSubscriberClientID(primary, "clientA")

	// New subscriber joins.
	newcomer := stream.Subscribe()

	// Request snapshot from primary only. Use a long timeout so the fallback
	// does not fire during this test.
	stream.RequestTUISnapshot("clientA", newcomer, nil, 10*time.Second)

	// Primary receives the snapshot request.
	evt := readSubEventTimeout(t, primary, time.Second)
	if evt.Type != "tui_snapshot_request" {
		t.Fatalf("primary expected tui_snapshot_request, got %s", evt.Type)
	}

	// Newcomer does NOT receive the request.
	assertNoSubEvent(t, newcomer, 200*time.Millisecond)
}

// TestRequestTUISnapshot_TimeoutFallback (VAL-PTY-006): if no tui_snapshot
// response arrives within the timeout, the server falls back to sending the
// ring buffer replay to the waiting subscriber.
func TestRequestTUISnapshot_TimeoutFallback(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer stream.invalidate()

	primary := stream.Subscribe()
	stream.SetSubscriberClientID(primary, "clientA")

	newcomer := stream.Subscribe()

	fallback := []byte("ring-buffer-content")
	// Short timeout for testing.
	stream.RequestTUISnapshot("clientA", newcomer, fallback, 100*time.Millisecond)

	// Primary receives the request (consuming it).
	_ = readSubEventTimeout(t, primary, time.Second)

	// After the timeout, the newcomer should receive a pty_replay fallback
	// with the ring buffer content.
	evt := readSubEventTimeout(t, newcomer, time.Second)
	if evt.Type != "pty_replay" {
		t.Fatalf("expected pty_replay fallback, got %s", evt.Type)
	}
	if evt.Text != "ring-buffer-content" {
		t.Fatalf("expected ring-buffer-content, got %q", evt.Text)
	}
}

// TestRequestTUISnapshot_ResponsePreemptsFallback (VAL-PTY-006): if a
// tui_snapshot response arrives before the timeout, the fallback is cancelled
// and the newcomer receives the snapshot (not the ring buffer replay).
func TestRequestTUISnapshot_ResponsePreemptsFallback(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer stream.invalidate()

	primary := stream.Subscribe()
	stream.SetSubscriberClientID(primary, "clientA")

	newcomer := stream.Subscribe()

	fallback := []byte("ring-buffer-content")
	stream.RequestTUISnapshot("clientA", newcomer, fallback, 500*time.Millisecond)

	// Primary receives the request.
	_ = readSubEventTimeout(t, primary, time.Second)

	// A snapshot response arrives before the timeout.
	if !stream.AcceptTUISnapshot("serialized-terminal-state") {
		t.Fatal("AcceptTUISnapshot should return true for the first response")
	}

	// The newcomer receives the snapshot via broadcast.
	evt := readSubEventTimeout(t, newcomer, time.Second)
	if evt.Type != "tui_snapshot" {
		t.Fatalf("expected tui_snapshot, got %s", evt.Type)
	}
	if evt.Text != "serialized-terminal-state" {
		t.Fatalf("expected serialized-terminal-state, got %q", evt.Text)
	}

	// No fallback should fire after this.
	assertNoSubEvent(t, newcomer, 600*time.Millisecond)
}

// TestAcceptTUISnapshot_Deduplication (VAL-PTY-006): only the first
// tui_snapshot response per request is accepted. Subsequent responses are
// rejected (de-duplicated).
func TestAcceptTUISnapshot_Deduplication(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer stream.invalidate()

	primary := stream.Subscribe()
	stream.SetSubscriberClientID(primary, "clientA")

	newcomer := stream.Subscribe()

	stream.RequestTUISnapshot("clientA", newcomer, nil, 10*time.Second)

	// First response is accepted.
	if !stream.AcceptTUISnapshot("first-snapshot") {
		t.Fatal("first AcceptTUISnapshot should return true")
	}

	// Second response is rejected (de-dup).
	if stream.AcceptTUISnapshot("second-snapshot") {
		t.Fatal("second AcceptTUISnapshot should return false (de-dup)")
	}

	// The newcomer receives only the first snapshot.
	evt := readSubEventTimeout(t, newcomer, time.Second)
	if evt.Text != "first-snapshot" {
		t.Fatalf("expected first-snapshot, got %q", evt.Text)
	}

	// No second snapshot should arrive.
	assertNoSubEvent(t, newcomer, 200*time.Millisecond)
}

// TestRequestTUISnapshot_NoPrimaryFallsBackImmediately (VAL-PTY-006): if
// there is no primary client registered (e.g. the primary disconnected), the
// request immediately falls back to ring buffer replay.
func TestRequestTUISnapshot_NoPrimaryFallsBackImmediately(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer stream.invalidate()

	newcomer := stream.Subscribe()

	fallback := []byte("ring-buffer-content")
	// No primary registered (empty clientID).
	stream.RequestTUISnapshot("", newcomer, fallback, 10*time.Second)

	// Newcomer immediately receives the ring buffer fallback.
	evt := readSubEventTimeout(t, newcomer, time.Second)
	if evt.Type != "pty_replay" {
		t.Fatalf("expected pty_replay fallback, got %s", evt.Type)
	}
	if evt.Text != "ring-buffer-content" {
		t.Fatalf("expected ring-buffer-content, got %q", evt.Text)
	}
}
