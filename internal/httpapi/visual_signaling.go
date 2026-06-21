package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/brainplusplus/9ed/internal/visualstream"
	"github.com/gorilla/websocket"
)

// handleVisualSignaling handles WebSocket-based WebRTC signaling for
// collaborative visual streaming (browser, remote desktop) — ADR-0001.
//
// Client connects to /ws/visual/{sessionId} and exchanges SDP offers/answers
// and ICE candidates to establish a pion PeerConnection for JPEG tile diff
// or H264 streaming.
//
// When the first offer arrives for a browser tab ID that is not yet
// registered, the handler lazily creates a StreamingSession with a CDP
// screencast source + JPEG tile diff strategy + CDP input handler, and starts
// the frame streaming loop.
func (a *API) handleVisualSignaling(w http.ResponseWriter, r *http.Request) {
	if a.visualSignaling == nil {
		http.Error(w, "visual streaming not available", http.StatusServiceUnavailable)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Lazily register a browser streaming session on first offer.
		if a.browser != nil {
			a.ensureBrowserStreamingSession(data)
		}

		resp, err := a.visualSignaling.HandleSignalingJSON(data)
		if err != nil {
			debug.Printf("[visual/signaling] error: %v", err)
			_ = conn.WriteJSON(visualstream.SignalingMessage{
				Type: "error",
			})
			continue
		}

		if resp != nil {
			if err := conn.WriteMessage(websocket.TextMessage, resp); err != nil {
				return
			}
		}
	}
}

// ensureBrowserStreamingSession lazily registers a StreamingSession for a
// browser tab when the first SDP offer arrives (ADR-0001). The session ID in
// the signaling message is the browser tab ID.
func (a *API) ensureBrowserStreamingSession(data []byte) {
	// Peek at the session ID in the signaling message.
	var peek struct {
		Type      string `json:"type"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return
	}
	if peek.Type != "offer" || peek.SessionID == "" {
		return
	}

	// Already registered?
	if a.visualSignaling.HasSession(peek.SessionID) {
		return
	}

	// Check that the browser tab exists.
	tab, ok := a.browser.Tab(peek.SessionID)
	if !ok {
		debug.Printf("[visual/signaling] browser tab %s not found, skipping registration", peek.SessionID)
		return
	}
	_ = tab

	// Acquire the Playwright page for this tab.
	ctx := context.Background()
	page, release, err := a.browser.AcquireTabPage(ctx, peek.SessionID)
	if err != nil {
		debug.Printf("[visual/signaling] failed to acquire page for tab %s: %v", peek.SessionID, err)
		return
	}
	defer release()

	// Create frame source (CDP screencast) + strategy (JPEG tile diff) + input handler.
	source := visualstream.NewCDPScreencastSource(page)
	strategy := visualstream.NewJpegTileDiffStrategy()
	input := visualstream.NewCDPInputHandler(page)

	ss := visualstream.NewStreamingSession(source, strategy, input)
	if err := ss.Start(ctx); err != nil {
		debug.Printf("[visual/signaling] failed to start streaming session for tab %s: %v", peek.SessionID, err)
		return
	}

	a.visualSignaling.RegisterSession(peek.SessionID, ss)
	debug.Printf("[visual/signaling] lazily registered streaming session for tab %s", peek.SessionID)

	// Clean up when the session ends (e.g., tab closed). Guard against the
	// re-registration case: if a new session replaced this one, don't unregister
	// the new session.
	go func() {
		<-ss.Done()
		if a.visualSignaling.SessionByID(peek.SessionID) == ss {
			a.visualSignaling.UnregisterSession(peek.SessionID)
			debug.Printf("[visual/signaling] cleaned up streaming session for tab %s", peek.SessionID)
		}
	}()
}

// visualSessionPath extracts the session ID from the visual signaling WS path.
func visualSessionPath(path string) string {
	return strings.TrimPrefix(path, "/ws/visual/")
}
