package browser

import "context"

// TransportType identifies the browser rendering transport mechanism.
type TransportType string

const (
	// TransportIframe uses an iframe with a server-side reverse proxy.
	// Phase 1 — works for localhost and sites that allow framing.
	TransportIframe TransportType = "iframe"

	// TransportScreencast uses CDP Page.startScreencast to stream frames
	// over WebSocket. Phase 2 — fallback for iframe-blocked sites.
	TransportScreencast TransportType = "screencast"

	// TransportWebRTC streams the browser surface via WebRTC.
	// Phase 3 — lowest latency, highest quality.
	TransportWebRTC TransportType = "webrtc"
)

// TransportState reports the current status of a browser transport.
type TransportState struct {
	Type     TransportType `json:"type"`
	Active   bool          `json:"active"`
	LastError string       `json:"lastError,omitempty"`
}

// Transport is the pluggable interface for browser rendering strategies.
// Phase 1 only implements IframeTransport (which is stateless on the backend).
// Future phases add ScreencastTransport and WebRTCTransport.
type Transport interface {
	// Type returns the transport mechanism identifier.
	Type() TransportType

	// Start initializes the transport (e.g., connects to CDP, sets up WebRTC).
	Start(ctx context.Context) error

	// Stop shuts down the transport and releases resources.
	Stop() error

	// State reports the current transport status.
	State() TransportState
}

// IframeTransport is the Phase 1 transport — stateless, no backend work needed.
// The iframe proxy in browserapi.go handles the actual request forwarding.
type IframeTransport struct{}

func (t *IframeTransport) Type() TransportType       { return TransportIframe }
func (t *IframeTransport) Start(_ context.Context) error { return nil }
func (t *IframeTransport) Stop() error               { return nil }
func (t *IframeTransport) State() TransportState {
	return TransportState{Type: TransportIframe, Active: true}
}
