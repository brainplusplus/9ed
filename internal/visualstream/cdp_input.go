package visualstream

import (
	"fmt"
	"math"

	playwright "github.com/playwright-community/playwright-go"
)

// CDPInputHandler implements InputHandler by injecting mouse and keyboard
// events into a Playwright-controlled browser page via CDP
// (ADR-0001 input layer — browser collaborative).
type CDPInputHandler struct {
	page   playwright.Page
	mouse  playwright.Mouse
	keyboard playwright.Keyboard
}

// NewCDPInputHandler creates a new CDP-based input handler for the given page.
func NewCDPInputHandler(page playwright.Page) *CDPInputHandler {
	return &CDPInputHandler{
		page:     page,
		mouse:    page.Mouse(),
		keyboard: page.Keyboard(),
	}
}

// HandleInput translates a remote InputEvent into Playwright mouse/keyboard
// actions on the browser page.
func (h *CDPInputHandler) HandleInput(evt InputEvent) error {
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
