package chat

import (
	"testing"
	"time"
)

func TestInputLockAcquire(t *testing.T) {
	lock := newInputLock(2 * time.Second)

	// No one holds the lock initially.
	if lock.holderID != "" {
		t.Fatal("Lock should start unheld")
	}

	// Client A acquires the lock.
	s := &ptySession{inputLock: lock}
	if !s.acquireInputLock("clientA") {
		t.Error("Client A should acquire the lock")
	}

	// Client B cannot acquire while A holds it.
	if s.acquireInputLock("clientB") {
		t.Error("Client B should not acquire while A holds")
	}

	// Client A can renew.
	if !s.acquireInputLock("clientA") {
		t.Error("Client A should be able to renew")
	}
}

func TestInputLockExpire(t *testing.T) {
	lock := newInputLock(50 * time.Millisecond)

	s := &ptySession{inputLock: lock}

	// Client A acquires.
	if !s.acquireInputLock("clientA") {
		t.Fatal("Client A should acquire")
	}

	// Wait for expiry.
	time.Sleep(100 * time.Millisecond)

	// Client B can now acquire after expiry.
	if !s.acquireInputLock("clientB") {
		t.Error("Client B should acquire after expiry")
	}
}

func TestInputLockRelease(t *testing.T) {
	lock := newInputLock(2 * time.Second)

	s := &ptySession{inputLock: lock}

	// Client A acquires.
	s.acquireInputLock("clientA")

	// Client A releases.
	s.ReleaseInputLock("clientA")

	// Client B can now acquire.
	if !s.acquireInputLock("clientB") {
		t.Error("Client B should acquire after release")
	}
}

func TestInputLockReleaseWrongHolder(t *testing.T) {
	lock := newInputLock(2 * time.Second)

	s := &ptySession{inputLock: lock}

	// Client A acquires.
	s.acquireInputLock("clientA")

	// Client B tries to release (should not work).
	s.ReleaseInputLock("clientB")

	// Client A still holds.
	if s.InputLockHolder() != "clientA" {
		t.Error("Client A should still hold the lock")
	}
}

func TestInputLockHolder(t *testing.T) {
	lock := newInputLock(2 * time.Second)

	s := &ptySession{inputLock: lock}

	// No holder initially.
	if s.InputLockHolder() != "" {
		t.Error("No holder expected initially")
	}

	// Client A acquires.
	s.acquireInputLock("clientA")

	if s.InputLockHolder() != "clientA" {
		t.Errorf("Expected 'clientA', got %q", s.InputLockHolder())
	}
}

func TestInputLockNilDisabled(t *testing.T) {
	// When inputLock is nil, lock is disabled (always succeeds).
	s := &ptySession{inputLock: nil}

	if !s.acquireInputLock("clientA") {
		t.Error("Lock should be disabled (always succeed)")
	}
	if s.InputLockHolder() != "" {
		t.Error("No holder expected when lock disabled")
	}
}

func TestIsPersistentError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", strErr("executable file not found"), true},
		{"permission denied", strErr("permission denied"), true},
		{"unauthorized", strErr("unauthorized access"), true},
		{"auth expired", strErr("auth expired"), true},
		{"invalid api key", strErr("invalid api key"), true},
		{"config error", strErr("config error"), true},
		{"not supported", strErr("not supported on this platform"), true},
		{"command not found", strErr("command not found"), true},
		{"network error", strErr("connection refused"), false},
		{"eof", strErr("EOF"), false},
		{"random error", strErr("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPersistentError(tt.err); got != tt.want {
				t.Errorf("isPersistentError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found (persistent)", strErr("executable file not found"), false},
		{"eof (transient)", strErr("EOF"), true},
		{"broken pipe (transient)", strErr("broken pipe"), true},
		{"connection reset (transient)", strErr("connection reset by peer"), true},
		{"signal killed (transient)", strErr("signal: killed"), true},
		{"timeout (transient)", strErr("context deadline exceeded"), true},
		{"unknown (transient default)", strErr("something unexpected"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientError(tt.err); got != tt.want {
				t.Errorf("isTransientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// strErr is a helper to create an error from a string for testing.
type strErr string

func (e strErr) Error() string { return string(e) }
