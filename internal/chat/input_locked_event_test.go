package chat

import (
	"encoding/json"
	"testing"
	"time"
)

// TestInputLockedEvent_DedicatedType (VAL-PTY-004): the input_locked event is
// a dedicated event type with Holder and TTL fields, NOT a piggyback on the
// generic error event.
func TestInputLockedEvent_DedicatedType(t *testing.T) {
	evt := inputLockedEvent("clientA", 2*time.Second)

	if evt.Type != "input_locked" {
		t.Errorf("expected type 'input_locked', got %q", evt.Type)
	}
	if evt.Holder != "clientA" {
		t.Errorf("expected Holder 'clientA', got %q", evt.Holder)
	}
	if evt.TTL != 2000 {
		t.Errorf("expected TTL 2000 (ms), got %d", evt.TTL)
	}
	// Must NOT be an error piggyback.
	if evt.Error != "" {
		t.Errorf("input_locked event must not carry an Error field, got %q", evt.Error)
	}
}

// TestInputLockedEvent_JSONShape verifies the JSON serialization exposes the
// dedicated event shape {type:'input_locked', holder, ttl} with no error field.
func TestInputLockedEvent_JSONShape(t *testing.T) {
	evt := inputLockedEvent("clientB", 1500*time.Millisecond)

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if parsed["type"] != "input_locked" {
		t.Errorf("json type = %v, want input_locked", parsed["type"])
	}
	if parsed["holder"] != "clientB" {
		t.Errorf("json holder = %v, want clientB", parsed["holder"])
	}
	// JSON numbers decode as float64.
	if ttl, ok := parsed["ttl"].(float64); !ok || ttl != 1500 {
		t.Errorf("json ttl = %v, want 1500", parsed["ttl"])
	}
	if _, hasError := parsed["error"]; hasError {
		t.Errorf("input_locked event must not serialize an error field; got %v", parsed["error"])
	}
}

// TestInputLockedEvent_DistinctFromError verifies that an input_locked event
// is distinguishable from a regular error event by Type alone (so the client
// can branch on data.type === 'input_locked' rather than sniffing error text).
func TestInputLockedEvent_DistinctFromError(t *testing.T) {
	locked := inputLockedEvent("clientA", 2*time.Second)
	errEvt := ChatEvent{Type: "error", Error: "something broke"}

	if locked.Type == errEvt.Type {
		t.Fatalf("input_locked event type (%q) must differ from error type (%q)",
			locked.Type, errEvt.Type)
	}
	if locked.Error != "" {
		t.Errorf("input_locked event must not carry an Error field")
	}
	if errEvt.Holder != "" {
		t.Errorf("plain error event should not carry a Holder field")
	}
}

// TestInputLockedEvent_EmptyHolder verifies that an empty holder produces a
// well-formed event (used when the holder is unknown/expired).
func TestInputLockedEvent_EmptyHolder(t *testing.T) {
	evt := inputLockedEvent("", 2*time.Second)
	if evt.Holder != "" {
		t.Errorf("expected empty Holder, got %q", evt.Holder)
	}
	if evt.TTL != 2000 {
		t.Errorf("expected TTL 2000, got %d", evt.TTL)
	}
}
