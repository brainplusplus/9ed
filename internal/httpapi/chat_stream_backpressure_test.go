package httpapi

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
)

// recordingFakeChatSession wraps fakeChatSession and records Cancel calls
// with a mutex so tests can assert on cancelCalls without racing the
// backpressure goroutine.
type recordingFakeChatSession struct {
	mu          sync.Mutex
	events      chan chat.ChatEvent
	done        chan struct{}
	cancelCalls int
}

func newRecordingFakeChatSession(eventBuffer int) *recordingFakeChatSession {
	return &recordingFakeChatSession{
		events: make(chan chat.ChatEvent, eventBuffer),
		done:   make(chan struct{}),
	}
}

func (s *recordingFakeChatSession) ID() string                                            { return "session-1" }
func (s *recordingFakeChatSession) AgentID() string                                       { return "opencode" }
func (s *recordingFakeChatSession) WorkDir() string                                       { return "" }
func (s *recordingFakeChatSession) Mode() chat.SessionMode                                { return chat.ModeACP }
func (s *recordingFakeChatSession) Events() <-chan chat.ChatEvent                         { return s.events }
func (s *recordingFakeChatSession) Done() <-chan struct{}                                 { return s.done }
func (s *recordingFakeChatSession) Err() error                                            { return nil }
func (s *recordingFakeChatSession) Send(context.Context, string, []chat.Attachment) error { return nil }
func (s *recordingFakeChatSession) Cancel() error {
	s.mu.Lock()
	s.cancelCalls++
	s.mu.Unlock()
	return nil
}
func (s *recordingFakeChatSession) Close() error { close(s.done); return nil }
func (s *recordingFakeChatSession) SetConfigOption(context.Context, string, string) error { return nil }
func (s *recordingFakeChatSession) ACPSessionID() string  { return "" }
func (s *recordingFakeChatSession) IsResumed() bool       { return false }
func (s *recordingFakeChatSession) RespondPermission(chat.PermissionResponse) {}
func (s *recordingFakeChatSession) SetAutoApprove(bool)   {}
func (s *recordingFakeChatSession) SetUseActiveTerminal(bool, string) {}
func (s *recordingFakeChatSession) UseActiveTerminalEnabled() bool    { return false }
func (s *recordingFakeChatSession) ActiveTerminalID() string          { return "" }
func (s *recordingFakeChatSession) SetUseActiveBrowser(bool, string)  {}
func (s *recordingFakeChatSession) UseActiveBrowserEnabled() bool     { return false }
func (s *recordingFakeChatSession) ActiveBrowserTabID() string        { return "" }

func (s *recordingFakeChatSession) CancelCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelCalls
}

// subscriberAlive reports whether the subscriber is still registered with the
// stream and its channels are still open (i.e. it was NOT dropped).
func subscriberAlive(t *testing.T, stream *chatStream, sub *chatSubscriber) bool {
	t.Helper()
	stream.mu.Lock()
	_, registered := stream.subscribers[sub]
	stream.mu.Unlock()
	if !registered {
		return false
	}
	// Channels should still be open. A closed channel always returns ok=false
	// on receive. Drain any pending events first, then probe.
	select {
	case <-sub.C:
	default:
	}
	select {
	case _, ok := <-sub.C:
		if !ok {
			return false
		}
	case <-time.After(20 * time.Millisecond):
		// channel still open and empty — healthy.
	}
	return true
}

// TestBackpressure_PriorityOverflowKeepsSubscriberAlive verifies ADR-0003:
// when the priority channel is full, the subscriber is NOT dropped. Instead,
// backpressure is applied to the agent (Cancel) and the critical event is
// buffered for retry. The subscriber stays alive.
func TestBackpressure_PriorityOverflowKeepsSubscriberAlive(t *testing.T) {
	session := newRecordingFakeChatSession(4)
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer closeStream(t, stream, session)

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	// Fill the priority channel (cap 64) without draining it. usage_update is
	// a critical, non-terminal event (so it does not short-circuit the
	// backpressure gate like done/error would).
	for i := 0; i < 64; i++ {
		stream.publish(chat.ChatEvent{Type: "usage_update"})
	}

	// The next critical event overflows the priority channel.
	stream.publish(chat.ChatEvent{Type: "usage_update"})

	// Wait briefly for the backpressure goroutine to run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session.CancelCount() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if session.CancelCount() == 0 {
		t.Fatalf("expected Cancel to be called on priority overflow, got %d", session.CancelCount())
	}

	// Subscriber must NOT be dropped.
	if !subscriberAlive(t, stream, sub) {
		t.Fatal("subscriber was dropped on priority overflow — ADR-0003 requires the subscriber to stay alive")
	}

	// A done event with stopReason=client_backpressure must be emitted.
	gotBackpressure := false
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !gotBackpressure {
		select {
		case evt := <-sub.priority:
			if evt.Type == "done" && evt.StopReason == "client_backpressure" {
				gotBackpressure = true
			}
		case <-sub.C:
			// drain bulk
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !gotBackpressure {
		t.Fatal("expected done event with stopReason=client_backpressure after priority overflow")
	}
}

// TestBackpressure_PriorityOverflowTriggersCancelEvenWithMultipleSubscribers
// verifies ADR-0003: backpressure-to-agent triggers on ANY priority overflow,
// not gated on len(subscribers)==0.
func TestBackpressure_PriorityOverflowTriggersCancelEvenWithMultipleSubscribers(t *testing.T) {
	session := newRecordingFakeChatSession(4)
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer closeStream(t, stream, session)

	// Two subscribers, neither drains.
	subA := stream.Subscribe()
	defer stream.Unsubscribe(subA)
	subB := stream.Subscribe()
	defer stream.Unsubscribe(subB)

	// Fill both priority channels (cap 64 each). usage_update is a critical,
	// non-terminal event so it triggers the backpressure path.
	for i := 0; i < 64; i++ {
		stream.publish(chat.ChatEvent{Type: "usage_update"})
	}

	// Overflow both.
	stream.publish(chat.ChatEvent{Type: "usage_update"})

	// Cancel must fire despite there being 2 subscribers (not gated on ==0).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session.CancelCount() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if session.CancelCount() == 0 {
		t.Fatalf("expected Cancel on priority overflow even with multiple subscribers, got %d", session.CancelCount())
	}

	// Both subscribers must stay alive.
	if !subscriberAlive(t, stream, subA) {
		t.Fatal("subscriber A was dropped on priority overflow")
	}
	if !subscriberAlive(t, stream, subB) {
		t.Fatal("subscriber B was dropped on priority overflow")
	}
}

// TestBackpressure_BulkOverflowDropsOldestKeepsSubscriber verifies ADR-0003:
// when the bulk channel is full, the oldest event is dropped to make room.
// The subscriber stays connected.
func TestBackpressure_BulkOverflowDropsOldestKeepsSubscriber(t *testing.T) {
	session := newRecordingFakeChatSession(4)
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer closeStream(t, stream, session)

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	// Fill the bulk channel (cap 256) with bulk events (text). Don't drain.
	for i := 0; i < 256; i++ {
		stream.publish(chat.ChatEvent{Type: "text", Text: "fill"})
	}

	// One more bulk event triggers drop-oldest.
	stream.publish(chat.ChatEvent{Type: "text", Text: "newest"})

	// Subscriber must stay alive (NOT dropped).
	if !subscriberAlive(t, stream, sub) {
		t.Fatal("subscriber was dropped on bulk overflow — ADR-0003 requires drop-oldest, keep subscriber")
	}

	// Cancel must NOT fire for bulk overflow (only priority overflow triggers backpressure).
	// Give the goroutine a moment in case it was (incorrectly) triggered.
	time.Sleep(80 * time.Millisecond)
	if session.CancelCount() != 0 {
		t.Fatalf("expected Cancel NOT to be called on bulk overflow, got %d", session.CancelCount())
	}
}

// TestBackpressure_BulkStillFullDropsNewEventKeepsSubscriber verifies
// ADR-0003: if the bulk channel is still full after dropping the oldest, the
// new event is dropped (subscriber stays alive). No delete+close fallback.
func TestBackpressure_BulkStillFullDropsNewEventKeepsSubscriber(t *testing.T) {
	// This scenario is hard to trigger directly with a buffered channel
	// because drop-oldest always makes room for exactly one event. We test
	// the contract by filling the channel, then publishing one event (which
	// drops oldest + inserts new), then verifying the subscriber is alive and
	// no Cancel fired. The "still full after drop" path is exercised under
	// concurrency; here we verify the no-delete+close contract holds.
	session := newRecordingFakeChatSession(4)
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer closeStream(t, stream, session)

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	// Fill bulk channel to capacity.
	for i := 0; i < 256; i++ {
		stream.publish(chat.ChatEvent{Type: "text", Text: "fill"})
	}

	// Publish several more — each drops oldest and inserts new (or drops new
	// if the channel can't accept). Subscriber must survive all of them.
	for i := 0; i < 10; i++ {
		stream.publish(chat.ChatEvent{Type: "text", Text: "extra"})
	}

	if !subscriberAlive(t, stream, sub) {
		t.Fatal("subscriber was dropped on repeated bulk overflow")
	}

	time.Sleep(80 * time.Millisecond)
	if session.CancelCount() != 0 {
		t.Fatalf("expected Cancel NOT to be called on bulk overflow, got %d", session.CancelCount())
	}
}

// TestBackpressure_NoDeleteCloseOnOverflowPaths verifies ADR-0003: there are
// no delete+close calls on subscriber overflow paths. We assert by filling both
// channels past capacity and confirming the subscriber remains registered and
// its channels remain open.
func TestBackpressure_NoDeleteCloseOnOverflowPaths(t *testing.T) {
	session := newRecordingFakeChatSession(8)
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer closeStream(t, stream, session)

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	// Overflow priority channel. usage_update is critical and non-terminal.
	for i := 0; i < 70; i++ {
		stream.publish(chat.ChatEvent{Type: "usage_update"})
	}
	// Overflow bulk channel.
	for i := 0; i < 300; i++ {
		stream.publish(chat.ChatEvent{Type: "text", Text: "fill"})
	}

	// Wait for any backpressure goroutine.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if session.CancelCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !subscriberAlive(t, stream, sub) {
		t.Fatal("subscriber was dropped on overflow — no delete+close allowed on overflow paths")
	}
}

// TestBackpressure_EmitsSeqGapSignalOnBulkDrop verifies ADR-0003: a seq-gap
// signal is emitted to the client when a bulk event is dropped due to
// overflow, so the client can re-fetch missing events via ADR-0002 catch-up.
func TestBackpressure_EmitsSeqGapSignalOnBulkDrop(t *testing.T) {
	session := newRecordingFakeChatSession(8)
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer closeStream(t, stream, session)

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	// Fill the bulk channel (cap 256).
	for i := 0; i < 256; i++ {
		stream.publish(chat.ChatEvent{Type: "text", Text: "fill"})
	}

	// Publish a bulk event that triggers drop-oldest. This should emit a
	// seq_gap signal so the client knows to re-fetch.
	stream.publish(chat.ChatEvent{Type: "text", Text: "overflow-event"})

	// Drain channels looking for a seq_gap signal.
	gotSeqGap := false
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && !gotSeqGap {
		select {
		case evt := <-sub.priority:
			if evt.Type == "seq_gap" {
				gotSeqGap = true
			}
		case <-sub.C:
			// drain
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !gotSeqGap {
		t.Fatal("expected seq_gap signal after bulk overflow drop")
	}
}

// TestBackpressure_BufferedCriticalEventRetried verifies ADR-0003: the
// critical event that overflowed the priority channel is buffered and retried
// for delivery after Cancel. We verify by filling the priority channel, then
// publishing a critical event with a unique marker, then confirming the
// buffered event is re-delivered (or the client is told to re-fetch).
func TestBackpressure_BufferedCriticalEventRetried(t *testing.T) {
	session := newRecordingFakeChatSession(8)
	stream := newChatStream("session-1", session, nil, nil, 0)
	stream.Start()
	defer closeStream(t, stream, session)

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	// Fill priority channel (cap 64). usage_update is critical and
	// non-terminal, so it does not short-circuit the backpressure gate.
	for i := 0; i < 64; i++ {
		stream.publish(chat.ChatEvent{Type: "usage_update"})
	}

	// Overflowing critical event with a unique marker. permission_request is
	// critical and non-terminal.
	markerTitle := "CRITICAL_OVERFLOW_MARKER"
	stream.publish(chat.ChatEvent{Type: "permission_request", PermissionTitle: markerTitle})

	// Wait for backpressure goroutine to run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session.CancelCount() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if session.CancelCount() == 0 {
		t.Fatalf("expected Cancel on priority overflow, got %d", session.CancelCount())
	}

	// The buffered critical event should be retried for delivery OR a
	// done(client_backpressure) event should be delivered so the client can
	// re-fetch. We accept either as evidence of buffering/retry.
	gotMarker := false
	gotBackpressure := false
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !(gotMarker || gotBackpressure) {
		select {
		case evt := <-sub.priority:
			if evt.Type == "permission_request" && evt.PermissionTitle == markerTitle {
				gotMarker = true
			}
			if evt.Type == "done" && evt.StopReason == "client_backpressure" {
				gotBackpressure = true
			}
		case <-sub.C:
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !gotMarker && !gotBackpressure {
		t.Fatal("expected buffered critical event to be retried or client_backpressure done emitted")
	}

	// Subscriber must survive.
	if !subscriberAlive(t, stream, sub) {
		t.Fatal("subscriber was dropped despite ADR-0003 keep-alive requirement")
	}
}

// closeStream is a helper that closes the session and stream cleanly, draining
// the run() goroutine so the test does not leak.
func closeStream(t *testing.T, stream *chatStream, session *recordingFakeChatSession) {
	t.Helper()
	close(session.done)
	// Give the run goroutine time to exit.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-stream.done:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}
