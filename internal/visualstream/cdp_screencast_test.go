package visualstream

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ── CDPScreencastSource Start/Stop ──

func TestCDPScreencastSourceStartWithNilPage(t *testing.T) {
	// Start with nil page should return an error, not panic.
	source := &CDPScreencastSource{}
	_, err := source.Start(context.Background())
	if err == nil {
		t.Error("expected error when starting with nil page")
		_ = source.Stop()
	}
}

// ── CDPScreencastSource Stop before Start ──

func TestCDPScreencastSourceStopBeforeStart(t *testing.T) {
	source := &CDPScreencastSource{}
	// Should not panic
	if err := source.Stop(); err != nil {
		t.Errorf("Stop before Start failed: %v", err)
	}
}

// ── CDPScreencastSource Stop is idempotent ──

func TestCDPScreencastSourceStopIdempotent(t *testing.T) {
	source := &CDPScreencastSource{}
	_ = source.Stop()
	_ = source.Stop()
	// Should not panic
}

// ── handleScreencastFrame after Stop should not send to closed channel ──

// This test simulates the race condition where handleScreencastFrame is called
// after Stop() has closed the frames channel. The running flag check under
// mutex should prevent the send.
func TestHandleScreencastFrameAfterStopNoPanic(t *testing.T) {
	source := &CDPScreencastSource{}

	// Manually set up the source state to simulate having been started
	source.mu.Lock()
	source.running = true
	source.frames = make(chan Frame, 1)
	source.mu.Unlock()

	// Now stop it (sets running=false, closes frames channel)
	_ = source.Stop()

	// Now call handleScreencastFrame — should check running under mutex
	// and bail out before sending to the closed channel.
	// This should NOT panic with "send on closed channel".
	source.handleScreencastFrame(map[string]any{
		"data":     "dGVzdA==", // "test" in base64
		"metadata": map[string]any{"deviceWidth": float64(100), "deviceHeight": float64(100)},
	})
}

// ── handleScreencastFrame with running=true should deliver frame ──

func TestHandleScreencastFrameDeliversFrame(t *testing.T) {
	source := &CDPScreencastSource{}

	source.mu.Lock()
	source.running = true
	source.frames = make(chan Frame, 1)
	source.cdpSess = nil // no CDP session — ack will be skipped
	source.mu.Unlock()

	source.handleScreencastFrame(map[string]any{
		"data":     "dGVzdA==", // "test" in base64
		"metadata": map[string]any{"deviceWidth": float64(100), "deviceHeight": float64(100)},
	})

	select {
	case frame := <-source.frames:
		if frame.Width != 100 || frame.Height != 100 {
			t.Errorf("expected 100x100, got %dx%d", frame.Width, frame.Height)
		}
		if len(frame.Data) != 4 {
			t.Errorf("expected 4 bytes of data, got %d", len(frame.Data))
		}
	case <-time.After(time.Second):
		t.Error("frame was not delivered to channel")
	}

	source.mu.Lock()
	source.running = false
	source.mu.Unlock()
}

// ── handleScreencastFrame with missing data field ──

func TestHandleScreencastFrameMissingData(t *testing.T) {
	source := &CDPScreencastSource{}

	source.mu.Lock()
	source.running = true
	source.frames = make(chan Frame, 1)
	source.mu.Unlock()

	// Missing "data" field — should return without panicking
	source.handleScreencastFrame(map[string]any{
		"metadata": map[string]any{"deviceWidth": float64(100), "deviceHeight": float64(100)},
	})

	// No frame should have been delivered
	select {
	case <-source.frames:
		t.Error("expected no frame when data is missing")
	default:
		// Expected
	}

	source.mu.Lock()
	source.running = false
	source.mu.Unlock()
}

// ── handleScreencastFrame with invalid base64 ──

func TestHandleScreencastFrameInvalidBase64(t *testing.T) {
	source := &CDPScreencastSource{}

	source.mu.Lock()
	source.running = true
	source.frames = make(chan Frame, 1)
	source.mu.Unlock()

	// Invalid base64 — should return without panicking
	source.handleScreencastFrame(map[string]any{
		"data":     "!!!invalid base64!!!",
		"metadata": map[string]any{"deviceWidth": float64(100), "deviceHeight": float64(100)},
	})

	select {
	case <-source.frames:
		t.Error("expected no frame for invalid base64")
	default:
		// Expected
	}

	source.mu.Lock()
	source.running = false
	source.mu.Unlock()
}

// ── handleScreencastFrame concurrent with Stop (race test) ──

func TestHandleScreencastFrameConcurrentWithStop(t *testing.T) {
	source := &CDPScreencastSource{}

	source.mu.Lock()
	source.running = true
	source.frames = make(chan Frame, 1)
	source.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: rapidly call handleScreencastFrame
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			source.handleScreencastFrame(map[string]any{
				"data":     "dGVzdA==",
				"metadata": map[string]any{"deviceWidth": float64(100), "deviceHeight": float64(100)},
			})
		}
	}()

	// Goroutine 2: stop the source concurrently
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		_ = source.Stop()
	}()

	wg.Wait()
	// If we get here without panicking, the race condition is handled
}

// ── handleScreencastFrame with nil metadata ──

func TestHandleScreencastFrameNilMetadata(t *testing.T) {
	source := &CDPScreencastSource{}

	source.mu.Lock()
	source.running = true
	source.frames = make(chan Frame, 1)
	source.cdpSess = nil
	source.mu.Unlock()

	source.handleScreencastFrame(map[string]any{
		"data": "dGVzdA==",
		// no metadata key
	})

	select {
	case frame := <-source.frames:
		if frame.Width != 0 || frame.Height != 0 {
			t.Errorf("expected 0x0 for nil metadata, got %dx%d", frame.Width, frame.Height)
		}
	case <-time.After(time.Second):
		t.Error("frame was not delivered")
	}

	source.mu.Lock()
	source.running = false
	source.mu.Unlock()
}
