package chat

import (
	"testing"
)

// newPrimaryClientTestSession builds a ptySession with enough state for primary
// client tracking tests, without spawning a real PTY subprocess.
func newPrimaryClientTestSession() *ptySession {
	return &ptySession{
		id:         "test-primary",
		agentID:    "test",
		ringBuffer: newPtyRingBuffer(1024),
		inputLock:  newInputLock(defaultInputLockTTL),
	}
}

// TestPrimaryClient_FirstSubscriberWins (VAL-PTY-006): the first client to
// register as primary becomes the primaryClientID. Subsequent calls do not
// change it.
func TestPrimaryClient_FirstSubscriberWins(t *testing.T) {
	s := newPrimaryClientTestSession()

	// Initially no primary client.
	if got := s.PrimaryClientID(); got != "" {
		t.Fatalf("expected empty primaryClientID initially, got %q", got)
	}

	// First client becomes primary.
	if !s.SetPrimaryClientID("clientA") {
		t.Error("SetPrimaryClientID should return true when setting a new primary")
	}
	if got := s.PrimaryClientID(); got != "clientA" {
		t.Fatalf("expected primaryClientID 'clientA', got %q", got)
	}

	// Second client does NOT replace the primary.
	if s.SetPrimaryClientID("clientB") {
		t.Error("SetPrimaryClientID should return false when primary already set")
	}
	if got := s.PrimaryClientID(); got != "clientA" {
		t.Fatalf("primaryClientID should remain 'clientA', got %q", got)
	}
}

// TestPrimaryClient_EmptyStringIgnored: setting an empty clientID does not
// establish a primary (guards against hello messages without clientId).
func TestPrimaryClient_EmptyStringIgnored(t *testing.T) {
	s := newPrimaryClientTestSession()

	// Empty clientID should not set the primary.
	if s.SetPrimaryClientID("") {
		t.Error("SetPrimaryClientID should return false for empty clientID")
	}
	if got := s.PrimaryClientID(); got != "" {
		t.Fatalf("expected empty primaryClientID, got %q", got)
	}

	// A real clientID after an empty attempt still works.
	if !s.SetPrimaryClientID("clientA") {
		t.Error("SetPrimaryClientID should return true for clientA")
	}
	if got := s.PrimaryClientID(); got != "clientA" {
		t.Fatalf("expected 'clientA', got %q", got)
	}
}

// TestPrimaryClient_ClearPrimary allows clearing the primary (used when the
// primary client disconnects and no subscribers remain).
func TestPrimaryClient_ClearPrimary(t *testing.T) {
	s := newPrimaryClientTestSession()

	s.SetPrimaryClientID("clientA")
	s.ClearPrimaryClientID()

	if got := s.PrimaryClientID(); got != "" {
		t.Fatalf("expected empty primaryClientID after clear, got %q", got)
	}

	// After clearing, a new client can become primary.
	if !s.SetPrimaryClientID("clientB") {
		t.Error("SetPrimaryClientID should return true after clear")
	}
	if got := s.PrimaryClientID(); got != "clientB" {
		t.Fatalf("expected 'clientB', got %q", got)
	}
}
