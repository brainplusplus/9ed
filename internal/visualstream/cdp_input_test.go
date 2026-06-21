package visualstream

import (
	"math"
	"testing"
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
