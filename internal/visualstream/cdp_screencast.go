package visualstream

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/brainplusplus/9ed/internal/debug"
	playwright "github.com/playwright-community/playwright-go"
)

// CDPScreencastSource implements FrameSource using CDP Page.startScreencast
// via Playwright's NewCDPSession (ADR-0001 Layer 1 — browser collaborative).
//
// It produces JPEG frame bytes from the headless Chromium browser tab.
type CDPScreencastSource struct {
	mu       sync.Mutex
	page     playwright.Page
	ctx      context.Context
	cancel   context.CancelFunc
	frames   chan Frame
	running  bool
	cdpSess  playwright.CDPSession
	quality  int    // JPEG quality (1-100)
	maxWidth int
	maxHeight int
}

// NewCDPScreencastSource creates a new CDP screencast frame source for the
// given Playwright page.
func NewCDPScreencastSource(page playwright.Page) *CDPScreencastSource {
	return &CDPScreencastSource{
		page:      page,
		quality:   60,
		maxWidth:  1280,
		maxHeight: 800,
	}
}

// Start begins CDP screencast and delivers frames via the returned channel.
func (s *CDPScreencastSource) Start(ctx context.Context) (<-chan Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return s.frames, nil
	}

	if s.page == nil {
		return nil, fmt.Errorf("cdp screencast: page is nil")
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.frames = make(chan Frame, 16)
	s.running = true

	// Create CDP session.
	cdpSess, err := s.page.Context().NewCDPSession(s.page)
	if err != nil {
		s.running = false
		return nil, fmt.Errorf("cdp session: %w", err)
	}
	s.cdpSess = cdpSess

	// Handle screencast frame events.
	cdpSess.On("Page.screencastFrame", func(params map[string]any) {
		s.handleScreencastFrame(params)
	})

	// Start screencast.
	_, err = cdpSess.Send("Page.startScreencast", map[string]any{
		"format":    "jpeg",
		"quality":   s.quality,
		"maxWidth":  s.maxWidth,
		"maxHeight": s.maxHeight,
	})
	if err != nil {
		_ = cdpSess.Detach()
		s.running = false
		return nil, fmt.Errorf("start screencast: %w", err)
	}

	debug.Printf("[visualstream/cdp] screencast started")
	return s.frames, nil
}

// Stop stops the screencast and marks the source as stopped. The frames
// channel is intentionally NOT closed here to avoid a data race between
// close(frames) in Stop() and send(frames<-) in handleScreencastFrame().
// Consumers detect shutdown via ctx cancellation (see frameLoop), and the
// channel is garbage-collected once the source is unreachable.
func (s *CDPScreencastSource) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	if s.cdpSess != nil {
		_, _ = s.cdpSess.Send("Page.stopScreencast", map[string]any{})
		_ = s.cdpSess.Detach()
		s.cdpSess = nil
	}

	if s.cancel != nil {
		s.cancel()
	}

	// Do NOT close(s.frames) — it would race with concurrent sends in
	// handleScreencastFrame. The channel is GC'd when unreachable.

	debug.Printf("[visualstream/cdp] screencast stopped")
	return nil
}

func (s *CDPScreencastSource) handleScreencastFrame(params map[string]any) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	cdpSess := s.cdpSess
	frames := s.frames
	s.mu.Unlock()

	dataStr, ok := params["data"].(string)
	if !ok {
		return
	}

	// Decode base64 JPEG.
	jpegBytes, err := base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		debug.Printf("[visualstream/cdp] base64 decode failed: %v", err)
		return
	}

	metadata, _ := params["metadata"].(map[string]any)
	width := 0
	height := 0
	if metadata != nil {
		if w, ok := metadata["deviceWidth"].(float64); ok {
			width = int(w)
		}
		if h, ok := metadata["deviceHeight"].(float64); ok {
			height = int(h)
		}
	}

	frame := Frame{
		Data:   jpegBytes,
		Width:  width,
		Height: height,
	}

	// Ack the frame. Must be done asynchronously to avoid deadlock:
	// Playwright's CDPSession.On event dispatcher holds an internal lock
	// during callback execution, and cdpSess.Send also acquires that lock.
	// Calling Send from within On callback would deadlock.
	sessionID, _ := params["sessionId"].(float64)
	if cdpSess != nil {
		go func(cdpSess playwright.CDPSession, sid int) {
			_, err := cdpSess.Send("Page.screencastFrameAck", map[string]any{
				"sessionId": sid,
			})
			if err != nil {
				debug.Printf("[visualstream/cdp] screencastFrameAck error: %v", err)
			}
		}(cdpSess, int(sessionID))
	}

	// Guard against send-on-closed-channel: since Stop() no longer closes
	// the channel, there is no race. If the source is stopped, the buffered
	// channel may fill up and frames are dropped via the default case.
	select {
	case frames <- frame:
	default:
		// Channel full, drop frame to avoid blocking the screencast event handler.
	}
}
