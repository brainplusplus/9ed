package httpapi

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestLivenessState_FirstMissDoesNotTearDown_DefaultThreshold(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	l := newLivenessState(2, 5*time.Second)

	if got := l.missedPongsCount(); got != 0 {
		t.Fatalf("initial missedPongs = %d, want 0", got)
	}

	teardown, nextDeadline := l.onReadDeadlineExceeded(now)
	if teardown {
		t.Fatalf("first miss must NOT tear down connection (threshold=2)")
	}
	if got := l.missedPongsCount(); got != 1 {
		t.Fatalf("after first miss, missedPongs = %d, want 1", got)
	}
	wantDeadline := now.Add(5 * time.Second)
	if !nextDeadline.Equal(wantDeadline) {
		t.Fatalf("nextDeadline = %v, want %v", nextDeadline, wantDeadline)
	}
}

func TestLivenessState_SecondMissTearsDown_DefaultThreshold(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	l := newLivenessState(2, 5*time.Second)

	// First miss: extend.
	if teardown, _ := l.onReadDeadlineExceeded(now); teardown {
		t.Fatalf("first miss must not tear down")
	}

	// Second consecutive miss: tear down.
	teardown, nextDeadline := l.onReadDeadlineExceeded(now)
	if !teardown {
		t.Fatalf("second consecutive miss must tear down (threshold=2)")
	}
	if got := l.missedPongsCount(); got != 2 {
		t.Fatalf("after second miss (teardown), missedPongs = %d, want 2", got)
	}
	if !nextDeadline.IsZero() {
		t.Fatalf("on teardown nextDeadline must be zero, got %v", nextDeadline)
	}
}

func TestLivenessState_PongResetsCounter(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	l := newLivenessState(2, 5*time.Second)

	// First miss — counter goes to 1.
	if teardown, _ := l.onReadDeadlineExceeded(now); teardown {
		t.Fatalf("first miss must not tear down")
	}
	if got := l.missedPongsCount(); got != 1 {
		t.Fatalf("after first miss, missedPongs = %d, want 1", got)
	}

	// Pong arrives — counter resets to 0 and deadline extends.
	nextDeadline := l.resetOnPong(now)
	if got := l.missedPongsCount(); got != 0 {
		t.Fatalf("after pong, missedPongs = %d, want 0", got)
	}
	wantDeadline := now.Add(5 * time.Second)
	if !nextDeadline.Equal(wantDeadline) {
		t.Fatalf("resetOnPong nextDeadline = %v, want %v", nextDeadline, wantDeadline)
	}

	// After reset, the next miss is treated as the FIRST again (does not tear down).
	if teardown, _ := l.onReadDeadlineExceeded(now); teardown {
		t.Fatalf("miss after pong reset must not tear down (counter restarted)")
	}
	if got := l.missedPongsCount(); got != 1 {
		t.Fatalf("after miss following pong, missedPongs = %d, want 1", got)
	}
}

func TestLivenessState_Threshold3RequiresThreeMisses(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	l := newLivenessState(3, 5*time.Second)

	// Miss 1: extend.
	if teardown, _ := l.onReadDeadlineExceeded(now); teardown {
		t.Fatalf("miss 1 must not tear down (threshold=3)")
	}
	if got := l.missedPongsCount(); got != 1 {
		t.Fatalf("after miss 1, missedPongs = %d, want 1", got)
	}
	// Miss 2: extend.
	if teardown, _ := l.onReadDeadlineExceeded(now); teardown {
		t.Fatalf("miss 2 must not tear down (threshold=3)")
	}
	if got := l.missedPongsCount(); got != 2 {
		t.Fatalf("after miss 2, missedPongs = %d, want 2", got)
	}
	// Miss 3: tear down.
	teardown, _ := l.onReadDeadlineExceeded(now)
	if !teardown {
		t.Fatalf("miss 3 must tear down (threshold=3)")
	}
	if got := l.missedPongsCount(); got != 3 {
		t.Fatalf("after miss 3 (teardown), missedPongs = %d, want 3", got)
	}
}

// TestLivenessState_InterleavedMissesAndPongs verifies that the counter is
// strictly CONSECUTIVE: a pong between misses resets the count, so a later
// miss is treated as the first again. This is the core guarantee of
// VAL-LIVENESS-001 / VAL-LIVENESS-002.
func TestLivenessState_InterleavedMissesAndPongs(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	l := newLivenessState(2, 5*time.Second)

	// Miss 1 (counter=1, extend).
	if teardown, _ := l.onReadDeadlineExceeded(now); teardown {
		t.Fatalf("miss 1 must not tear down")
	}
	// Pong (counter=0).
	l.resetOnPong(now)
	// Miss again — should be treated as first miss (counter=1, extend).
	if teardown, _ := l.onReadDeadlineExceeded(now); teardown {
		t.Fatalf("miss after pong must not tear down")
	}
	// Miss — second consecutive since pong (teardown).
	teardown, _ := l.onReadDeadlineExceeded(now)
	if !teardown {
		t.Fatalf("second consecutive miss after pong must tear down")
	}
}

// TestLivenessState_InvalidThresholdFallsBack verifies the defensive default
// (ADR-0006 default of 2) when the configured threshold is non-positive.
func TestLivenessState_InvalidThresholdFallsBack(t *testing.T) {
	t.Parallel()

	l0 := newLivenessState(0, 5*time.Second)
	if l0.threshold != 2 {
		t.Fatalf("threshold=0 should fall back to 2, got %d", l0.threshold)
	}

	lNeg := newLivenessState(-5, 5*time.Second)
	if lNeg.threshold != 2 {
		t.Fatalf("threshold=-5 should fall back to 2, got %d", lNeg.threshold)
	}
}

// TestIsReadDeadlineExceeded_DetectsNetTimeout verifies that the helper
// recognises the error shape produced by gorilla/websocket when a read
// deadline fires (net.Error with Timeout()=true).
func TestIsReadDeadlineExceeded_DetectsNetTimeout(t *testing.T) {
	t.Parallel()

	// Use a real net.Error with Timeout()=true.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Skip("read did not return an error (got data); skipping")
	}
	if !isReadDeadlineExceeded(readErr) {
		t.Fatalf("isReadDeadlineExceeded(net timeout) = false; want true (err=%v)", readErr)
	}
}

// TestIsReadDeadlineExceeded_PlainErrorStrings verifies the defensive
// string-match fallback for "i/o timeout" / "read deadline exceeded".
func TestIsReadDeadlineExceeded_PlainErrorStrings(t *testing.T) {
	t.Parallel()

	cases := []string{"i/o timeout", "read deadline exceeded", "I/O Timeout"}
	for _, msg := range cases {
		if !isReadDeadlineExceeded(errors.New(msg)) {
			t.Errorf("isReadDeadlineExceeded(%q) = false, want true", msg)
		}
	}
}

func TestIsReadDeadlineExceeded_NonTimeoutErrorsAreFalse(t *testing.T) {
	t.Parallel()

	cases := []error{
		nil,
		errors.New("unexpected EOF"),
		errors.New("connection reset by peer"),
		errors.New("websocket: close 1006"),
		fmt.Errorf("wrapped non-timeout: %w", errors.New("boom")),
	}
	for i, tc := range cases {
		if isReadDeadlineExceeded(tc) {
			t.Errorf("case %d: isReadDeadlineExceeded(%v) = true, want false", i, tc)
		}
	}
}
