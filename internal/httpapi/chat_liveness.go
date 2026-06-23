package httpapi

import (
	"errors"
	"net"
	"strings"
	"time"
)

// livenessState tracks consecutive missed pongs for a single WebSocket
// connection (ADR-0006).
//
// The server does NOT tear down the connection on the first missed pong.
// Instead it tracks a miss counter and only tears down after
// LIVENESS_FAILURE_THRESHOLD (default 2) consecutive misses. A pong (or any
// successful read) resets the counter to 0.
//
// This type is safe for use from a single goroutine (the read loop). The pong
// handler runs in the same goroutine that calls ReadMessage/ReadJSON (gorilla
// invokes pong handlers synchronously from the read path), so no mutex is
// needed when used from a single ReadJSON loop.
type livenessState struct {
	// missedPongs is the number of consecutive pongs that have been missed
	// (read deadline exceeded without a pong arriving) since the last
	// successful pong or successful read.
	missedPongs int
	// threshold is the number of consecutive misses after which the
	// connection is torn down. Always >= 1.
	threshold int
	// timeout is the per-read deadline window used to extend the deadline
	// after a tolerated miss (or after a pong).
	timeout time.Duration
}

// newLivenessState creates a livenessState with the given failure threshold
// and per-read deadline window. A threshold < 1 is clamped to the ADR-0006
// default of 2 (VAL-LIVENESS-003).
func newLivenessState(threshold int, timeout time.Duration) livenessState {
	if threshold < 1 {
		threshold = 2
	}
	return livenessState{threshold: threshold, timeout: timeout}
}

// missedPongsCount returns the current consecutive-miss count. Used for
// debug logging and tests.
func (l *livenessState) missedPongsCount() int {
	return l.missedPongs
}

// resetOnPong resets the miss counter to 0 and returns the next read deadline
// (now + timeout). Called from SetPongHandler when a pong is received
// (VAL-LIVENESS-002).
func (l *livenessState) resetOnPong(now time.Time) time.Time {
	l.missedPongs = 0
	return now.Add(l.timeout)
}

// onReadDeadlineExceeded is called when a read returns a deadline-exceeded
// error. It returns:
//   - teardown=true if the connection should be torn down (threshold
//     consecutive misses reached); nextDeadline is the zero time in this case.
//   - teardown=false if the deadline should be extended and the read loop
//     should continue (incrementing the miss counter); nextDeadline is the
//     new read deadline (now + timeout).
//
// For threshold=2 (default):
//   - 1st miss: missedPongs(0) < threshold-1(1) -> increment to 1, extend
//   - 2nd miss: missedPongs(1) < threshold-1(1) is false -> teardown
//
// So the first missed pong does NOT tear down (extends instead), and teardown
// happens only after `threshold` consecutive misses (VAL-LIVENESS-001).
func (l *livenessState) onReadDeadlineExceeded(now time.Time) (teardown bool, nextDeadline time.Time) {
	if l.missedPongs < l.threshold-1 {
		l.missedPongs++
		return false, now.Add(l.timeout)
	}
	l.missedPongs++
	return true, time.Time{}
}

// isReadDeadlineExceeded reports whether err is a network read deadline
// exceeded (timeout) error. gorilla/websocket surfaces the underlying net.Conn
// timeout error from ReadMessage/ReadJSON, so errors.As against net.Error
// (which has a Timeout() method) is the primary check. As a defensive
// fallback, common timeout error strings are also recognised — gorilla and
// the Go runtime sometimes wrap timeout errors in ways that defeat errors.As
// across wrapped layers.
func isReadDeadlineExceeded(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "read deadline exceeded") {
		return true
	}
	return false
}
