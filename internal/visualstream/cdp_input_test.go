package visualstream

import (
	"math"
	"testing"
	"time"
)

// ── clamp() ──

func TestClampNormal(t *testing.T) {
	if v := clamp(100.5); v != 100.5 {
		t.Errorf("expected 100.5, got %f", v)
	}
}

func TestClampZero(t *testing.T) {
	if v := clamp(0); v != 0 {
		t.Errorf("expected 0, got %f", v)
	}
}

func TestClampNegative(t *testing.T) {
	if v := clamp(-50); v != 0 {
		t.Errorf("expected 0 for negative, got %f", v)
	}
}

func TestClampNaN(t *testing.T) {
	if v := clamp(math.NaN()); v != 0 {
		t.Errorf("expected 0 for NaN, got %f", v)
	}
}

func TestClampPositiveInf(t *testing.T) {
	if v := clamp(math.Inf(1)); v != 0 {
		t.Errorf("expected 0 for +Inf, got %f", v)
	}
}

func TestClampNegativeInf(t *testing.T) {
	if v := clamp(math.Inf(-1)); v != 0 {
		t.Errorf("expected 0 for -Inf, got %f", v)
	}
}

// ── keyFromEvent() ──

func TestKeyFromEventWithKey(t *testing.T) {
	evt := InputEvent{Key: "Enter"}
	if k := keyFromEvent(evt); k != "Enter" {
		t.Errorf("expected 'Enter', got '%s'", k)
	}
}

func TestKeyFromEventWithCode(t *testing.T) {
	evt := InputEvent{Code: "Enter"}
	if k := keyFromEvent(evt); k != "Enter" {
		t.Errorf("expected 'Enter' from code, got '%s'", k)
	}
}

func TestKeyFromEventEmpty(t *testing.T) {
	evt := InputEvent{}
	if k := keyFromEvent(evt); k != "" {
		t.Errorf("expected empty string, got '%s'", k)
	}
}

func TestKeyFromEventKeyTakesPrecedence(t *testing.T) {
	evt := InputEvent{Key: "a", Code: "KeyA"}
	if k := keyFromEvent(evt); k != "a" {
		t.Errorf("expected key 'a' to take precedence, got '%s'", k)
	}
}

// ── buttonFromEvent() ──

func TestButtonFromEventLeft(t *testing.T) {
	evt := InputEvent{Button: 0}
	b := buttonFromEvent(evt)
	if b == nil {
		t.Fatal("expected non-nil button")
	}
	// playwright.MouseButtonLeft is a *string
	if *b != "left" {
		t.Errorf("expected 'left', got '%s'", *b)
	}
}

func TestButtonFromEventMiddle(t *testing.T) {
	evt := InputEvent{Button: 1}
	b := buttonFromEvent(evt)
	if *b != "middle" {
		t.Errorf("expected 'middle', got '%s'", *b)
	}
}

func TestButtonFromEventRight(t *testing.T) {
	evt := InputEvent{Button: 2}
	b := buttonFromEvent(evt)
	if *b != "right" {
		t.Errorf("expected 'right', got '%s'", *b)
	}
}

func TestButtonFromEventDefault(t *testing.T) {
	evt := InputEvent{Button: 99}
	b := buttonFromEvent(evt)
	if *b != "left" {
		t.Errorf("expected 'left' as default, got '%s'", *b)
	}
}

// ── Input throttling (VAL-VISUAL-007) ──

// TestThrottleCategory tests that input events are categorized into the
// correct throttle category.
func TestThrottleCategory(t *testing.T) {
	tests := []struct {
		evtType  string
		expected throttleCategory
	}{
		{"mouse_move", throttleMouse},
		{"mouse_down", throttleMouse},
		{"mouse_up", throttleMouse},
		{"mouse_click", throttleMouse},
		{"scroll", throttleMouse},
		{"key_down", throttleKey},
		{"key_up", throttleKey},
		{"text", throttleText},
		{"unknown", throttleMouse}, // default to mouse
	}
	for _, tc := range tests {
		cat := throttleCategoryFor(tc.evtType)
		if cat != tc.expected {
			t.Errorf("throttleCategoryFor(%q) = %v, want %v", tc.evtType, cat, tc.expected)
		}
	}
}

// TestThrottleIntervals verifies the throttle intervals:
// mouse 8ms, key 25ms, text 100ms.
func TestThrottleIntervals(t *testing.T) {
	if throttleIntervals[throttleMouse] != 8*time.Millisecond {
		t.Errorf("mouse throttle = %v, want 8ms", throttleIntervals[throttleMouse])
	}
	if throttleIntervals[throttleKey] != 25*time.Millisecond {
		t.Errorf("key throttle = %v, want 25ms", throttleIntervals[throttleKey])
	}
	if throttleIntervals[throttleText] != 100*time.Millisecond {
		t.Errorf("text throttle = %v, want 100ms", throttleIntervals[throttleText])
	}
}

// TestInputThrottleAllowsFirst tests that the first event is always allowed.
func TestInputThrottleAllowsFirst(t *testing.T) {
	th := newInputThrottler()
	if !th.allow(throttleMouse) {
		t.Error("first mouse event should be allowed")
	}
	if !th.allow(throttleKey) {
		t.Error("first key event should be allowed")
	}
	if !th.allow(throttleText) {
		t.Error("first text event should be allowed")
	}
}

// TestInputThrottleDropsRapidMouseEvents verifies that rapid mouse events
// within the 8ms throttle window are dropped.
func TestInputThrottleDropsRapidMouseEvents(t *testing.T) {
	th := newInputThrottler()
	if !th.allow(throttleMouse) {
		t.Fatal("first mouse event should be allowed")
	}
	// Immediately after — should be throttled
	if th.allow(throttleMouse) {
		t.Error("second mouse event within 8ms should be dropped")
	}
}

// TestInputThrottleAllowsAfterInterval verifies that events are allowed
// after the throttle interval passes.
func TestInputThrottleAllowsAfterInterval(t *testing.T) {
	th := newInputThrottler()
	th.allow(throttleMouse)
	// Simulate time passing beyond the 8ms mouse throttle
	th.lastApplied[throttleMouse] = time.Now().Add(-10 * time.Millisecond)
	if !th.allow(throttleMouse) {
		t.Error("mouse event after 8ms should be allowed")
	}
}

// TestInputThrottleCategoriesIndependent verifies that different event
// categories are throttled independently.
func TestInputThrottleCategoriesIndependent(t *testing.T) {
	th := newInputThrottler()
	th.allow(throttleMouse)
	// Mouse is throttled, but key should still be allowed
	if !th.allow(throttleKey) {
		t.Error("key event should be allowed even if mouse is throttled")
	}
	if !th.allow(throttleText) {
		t.Error("text event should be allowed even if mouse is throttled")
	}
}

// TestInputThrottleText100ms verifies that text events are throttled at 100ms.
func TestInputThrottleText100ms(t *testing.T) {
	th := newInputThrottler()
	th.allow(throttleText)
	// Simulate 50ms passing — within 100ms window, should be dropped
	th.lastApplied[throttleText] = time.Now().Add(-50 * time.Millisecond)
	if th.allow(throttleText) {
		t.Error("text event within 100ms should be dropped")
	}
	// Simulate 101ms passing — should be allowed
	th.lastApplied[throttleText] = time.Now().Add(-101 * time.Millisecond)
	if !th.allow(throttleText) {
		t.Error("text event after 100ms should be allowed")
	}
}

// TestInputThrottleKey25ms verifies that key events are throttled at 25ms.
func TestInputThrottleKey25ms(t *testing.T) {
	th := newInputThrottler()
	th.allow(throttleKey)
	// 20ms — within 25ms window, should be dropped
	th.lastApplied[throttleKey] = time.Now().Add(-20 * time.Millisecond)
	if th.allow(throttleKey) {
		t.Error("key event within 25ms should be dropped")
	}
	// 26ms — should be allowed
	th.lastApplied[throttleKey] = time.Now().Add(-26 * time.Millisecond)
	if !th.allow(throttleKey) {
		t.Error("key event after 25ms should be allowed")
	}
}
