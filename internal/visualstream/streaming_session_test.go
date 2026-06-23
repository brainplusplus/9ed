package visualstream

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockFrameSource is a test FrameSource that emits a configurable number of
// frames and blocks until stopped.
type mockFrameSource struct {
	mu       sync.Mutex
	frames   chan Frame
	started  bool
	stopped  bool
}

func (m *mockFrameSource) Start(ctx context.Context) (<-chan Frame, error) {
	m.mu.Lock()
	m.started = true
	m.frames = make(chan Frame, 10)
	m.mu.Unlock()

	go func() {
		for i := 0; i < 5; i++ {
			select {
			case <-ctx.Done():
				return
			case m.frames <- Frame{Data: []byte{byte(i)}, Width: 100, Height: 100}:
			}
		}
		// Block until stopped.
		<-ctx.Done()
	}()
	return m.frames, nil
}

func (m *mockFrameSource) Stop() error {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	return nil
}

func (m *mockFrameSource) wasStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *mockFrameSource) wasStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// mockStrategy is a test VisualStreamStrategy that records all frames received.
type mockStrategy struct {
	mu     sync.Mutex
	frames []Frame
	closed bool
}

func (ms *mockStrategy) EncodeAndSend(frame Frame, peers []*PeerConnection) {
	ms.mu.Lock()
	ms.frames = append(ms.frames, frame)
	ms.mu.Unlock()
}

func (ms *mockStrategy) Close() error {
	ms.mu.Lock()
	ms.closed = true
	ms.mu.Unlock()
	return nil
}

func (ms *mockStrategy) getFrames() []Frame {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.frames
}

func (ms *mockStrategy) wasClosed() bool {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.closed
}

// mockInputHandler is a test InputHandler that records all events.
type mockInputHandler struct {
	mu     sync.Mutex
	events []InputEvent
	closed bool
}

func (mi *mockInputHandler) HandleInput(evt InputEvent) error {
	mi.mu.Lock()
	mi.events = append(mi.events, evt)
	mi.mu.Unlock()
	return nil
}

func (mi *mockInputHandler) Close() error {
	mi.mu.Lock()
	mi.closed = true
	mi.mu.Unlock()
	return nil
}

func (mi *mockInputHandler) wasClosed() bool {
	mi.mu.Lock()
	defer mi.mu.Unlock()
	return mi.closed
}

func (mi *mockInputHandler) getEvents() []InputEvent {
	mi.mu.Lock()
	defer mi.mu.Unlock()
	return mi.events
}

func TestStreamingSessionStartAndStop(t *testing.T) {
	source := &mockFrameSource{}
	strategy := &mockStrategy{}
	input := &mockInputHandler{}

	session := NewStreamingSession(source, strategy, input)

	if session == nil {
		t.Fatal("NewStreamingSession returned nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Starting again should be a no-op (idempotent).
	if err := session.Start(ctx); err != nil {
		t.Fatalf("Second Start failed: %v", err)
	}

	if !source.wasStarted() {
		t.Error("FrameSource was not started")
	}

	// Wait for frames to be processed.
	time.Sleep(100 * time.Millisecond)

	// Stop the session.
	session.Stop()

	if !source.wasStopped() {
		t.Error("FrameSource was not stopped")
	}
	if !strategy.wasClosed() {
		t.Error("Strategy was not closed")
	}
	if !input.wasClosed() {
		t.Error("InputHandler was not closed")
	}

	// Done channel should be closed.
	select {
	case <-session.Done():
		// Expected.
	case <-time.After(time.Second):
		t.Error("Done channel was not closed after Stop")
	}
}

func TestStreamingSessionProcessesFrames(t *testing.T) {
	source := &mockFrameSource{}
	strategy := &mockStrategy{}
	input := &mockInputHandler{}

	session := NewStreamingSession(source, strategy, input)

	// Add a fake peer so strategy receives frames.
	session.PeerManager().AddPeer(&PeerConnection{ID: "test-peer"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for all 5 frames to be processed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(strategy.getFrames()) >= 5 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	frames := strategy.getFrames()
	if len(frames) != 5 {
		t.Fatalf("Expected 5 frames processed, got %d", len(frames))
	}

	// Verify frame data.
	for i, f := range frames {
		if len(f.Data) != 1 || f.Data[0] != byte(i) {
			t.Errorf("Frame %d has unexpected data: %v", i, f.Data)
		}
	}

	session.Stop()
}

func TestStreamingSessionNoPeersSkipsStrategy(t *testing.T) {
	source := &mockFrameSource{}
	strategy := &mockStrategy{}
	input := &mockInputHandler{}

	session := NewStreamingSession(source, strategy, input)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait a short time for frames to be emitted by the source.
	time.Sleep(200 * time.Millisecond)

	// With no peers, strategy should not receive any frames.
	if len(strategy.getFrames()) > 0 {
		t.Errorf("Strategy received frames with no peers")
	}

	session.Stop()
}

func TestStreamingSessionStopIdempotent(t *testing.T) {
	source := &mockFrameSource{}
	strategy := &mockStrategy{}
	input := &mockInputHandler{}

	session := NewStreamingSession(source, strategy, input)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should not panic when called multiple times.
	session.Stop()
	session.Stop() // second stop should be a no-op
}

func TestStreamingSessionConcurrentStop(t *testing.T) {
	source := &mockFrameSource{}
	strategy := &mockStrategy{}
	input := &mockInputHandler{}

	session := NewStreamingSession(source, strategy, input)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Concurrent Stop calls should not panic (sync.Once protects close(done)).
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			session.Stop()
		}()
	}
	wg.Wait()

	// The Done channel should be closed.
	select {
	case <-session.Done():
		// OK
	default:
		t.Error("Done channel should be closed after Stop")
	}
}

func TestStreamingSessionDoneSignalsOnStop(t *testing.T) {
	source := &mockFrameSource{}
	session := NewStreamingSession(source, &mockStrategy{}, &mockInputHandler{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	done := session.Done()
	// Should not be closed yet.
	select {
	case <-done:
		t.Fatal("Done should not be closed before Stop")
	default:
		// OK
	}

	session.Stop()

	// Should be closed after Stop.
	select {
	case <-done:
		// OK
	case <-time.After(time.Second):
		t.Fatal("Done channel not closed after Stop")
	}
}

func TestPeerManagerAddRemove(t *testing.T) {
	pm := NewPeerManager()

	// Add a fake peer (PC is nil, but the test only checks the map).
	peer1 := &PeerConnection{ID: "peer1"}
	peer2 := &PeerConnection{ID: "peer2"}

	pm.AddPeer(peer1)
	pm.AddPeer(peer2)

	peers := pm.Peers()
	if len(peers) != 2 {
		t.Fatalf("Expected 2 peers, got %d", len(peers))
	}

	pm.RemovePeer("peer1")
	peers = pm.Peers()
	if len(peers) != 1 {
		t.Fatalf("Expected 1 peer after removal, got %d", len(peers))
	}
	if peers[0].ID != "peer2" {
		t.Errorf("Expected peer2 to remain, got %s", peers[0].ID)
	}

	pm.Close()
	peers = pm.Peers()
	if len(peers) != 0 {
		t.Errorf("Expected 0 peers after Close, got %d", len(peers))
	}
}

func TestPeerManagerEmptyPeers(t *testing.T) {
	pm := NewPeerManager()
	peers := pm.Peers()
	if len(peers) != 0 {
		t.Errorf("Expected 0 peers from empty manager, got %d", len(peers))
	}
}
