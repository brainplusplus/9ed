package visualstream

import (
	"fmt"
	"math"
	"sync"
	"time"

	playwright "github.com/playwright-community/playwright-go"
)

// ── Input throttling (VAL-VISUAL-007) ──

// throttleCategory classifies input events for throttling purposes.
type throttleCategory int

const (
	throttleMouse throttleCategory = iota
	throttleKey
	throttleText
)

// throttleIntervals defines the minimum interval between events of each
// category. Events arriving faster than the interval are dropped.
//   - mouse: 8ms (~125 Hz, matching typical high-refresh display polling)
//   - key:   25ms (~40 Hz, smooth enough for key repeat without flooding)
//   - text:  100ms (text input is lower frequency; typing is throttled to
//           avoid overwhelming the CDP session with individual key events)
var throttleIntervals = map[throttleCategory]time.Duration{
	throttleMouse: 8 * time.Millisecond,
	throttleKey:   25 * time.Millisecond,
	throttleText:  100 * time.Millisecond,
}

// throttleCategoryFor maps an input event type string to its throttle category.
func throttleCategoryFor(evtType string) throttleCategory {
	switch evtType {
	case "key_down", "key_up":
		return throttleKey
	case "text":
		return throttleText
	default:
		// mouse_move, mouse_down, mouse_up, mouse_click, scroll, and any
		// unknown type all fall under the mouse throttle.
		return throttleMouse
	}
}

// inputThrottler enforces per-category minimum intervals between accepted
// events. It is safe for concurrent use.
type inputThrottler struct {
	mu          sync.Mutex
	lastApplied map[throttleCategory]time.Time
}

// newInputThrottler creates a new throttler with zero-valued last-applied
// timestamps, so the first event in each category is always allowed.
func newInputThrottler() *inputThrottler {
	return &inputThrottler{
		lastApplied: make(map[throttleCategory]time.Time),
	}
}

// allow returns true if the event should be processed (the interval since
// the last accepted event of the same category has elapsed), and updates
// the last-applied timestamp. Returns false if the event should be dropped.
func (t *inputThrottler) allow(cat throttleCategory) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	interval := throttleIntervals[cat]
	last := t.lastApplied[cat]

	if now.Sub(last) < interval {
		return false // throttled
	}

	t.lastApplied[cat] = now
	return true
}

// CDPInputHandler implements InputHandler by injecting mouse and keyboard
// events into a Playwright-controlled browser page via CDP
// (ADR-0001 input layer — browser collaborative).
type CDPInputHandler struct {
	page      playwright.Page
	mouse     playwright.Mouse
	keyboard  playwright.Keyboard
	throttler *inputThrottler
}

// NewCDPInputHandler creates a new CDP-based input handler for the given page.
func NewCDPInputHandler(page playwright.Page) *CDPInputHandler {
	return &CDPInputHandler{
		page:      page,
		mouse:     page.Mouse(),
		keyboard:  page.Keyboard(),
		throttler: newInputThrottler(),
	}
}

// HandleInput translates a remote InputEvent into Playwright mouse/keyboard
// actions on the browser page. Events are throttled per type: mouse 8ms,
// key 25ms, text 100ms (VAL-VISUAL-007).
func (h *CDPInputHandler) HandleInput(evt InputEvent) error {
	// Throttle: drop events arriving faster than the category interval.
	if h.throttler != nil && !h.throttler.allow(throttleCategoryFor(evt.Type)) {
		return nil
	}

	switch evt.Type {
	case "mouse_move":
		return h.mouse.Move(clamp(evt.X), clamp(evt.Y))

	case "mouse_down":
		return h.mouse.Down(playwright.MouseDownOptions{Button: buttonFromEvent(evt)})

	case "mouse_up":
		return h.mouse.Up(playwright.MouseUpOptions{Button: buttonFromEvent(evt)})

	case "mouse_click":
		return h.mouse.Click(clamp(evt.X), clamp(evt.Y), playwright.MouseClickOptions{
			Button: buttonFromEvent(evt),
		})

	case "scroll":
		return h.mouse.Wheel(evt.DeltaX, evt.DeltaY)

	case "key_down":
		return h.keyboard.Down(keyFromEvent(evt))

	case "key_up":
		return h.keyboard.Up(keyFromEvent(evt))

	case "text":
		return h.keyboard.Type(evt.Text)

	default:
		return fmt.Errorf("unknown input event type: %s", evt.Type)
	}
}

func (h *CDPInputHandler) Close() error {
	return nil
}

func buttonFromEvent(evt InputEvent) *playwright.MouseButton {
	switch evt.Button {
	case 0:
		return playwright.MouseButtonLeft
	case 1:
		return playwright.MouseButtonMiddle
	case 2:
		return playwright.MouseButtonRight
	default:
		return playwright.MouseButtonLeft
	}
}

// keyFromEvent maps common JS key codes to Playwright key names.
func keyFromEvent(evt InputEvent) string {
	if evt.Key != "" {
		return evt.Key
	}
	if evt.Code != "" {
		return evt.Code
	}
	return ""
}

// clamp ensures coordinates are finite and non-negative.
func clamp(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	return v
}
