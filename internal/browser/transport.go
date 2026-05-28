package browser

import "context"

// TransportType identifies the browser rendering transport mechanism.
type TransportType string

const (
	// TransportProxy uses an iframe with a server-side reverse proxy.
	TransportProxy TransportType = "proxy"

	// TransportIframe is kept as an internal alias for older code paths.
	TransportIframe TransportType = TransportProxy

	// TransportScreencast uses CDP Page.startScreencast to stream frames
	// over WebSocket.
	TransportScreencast TransportType = "screencast"

	// TransportWebRTC streams the browser surface via WebRTC.
	TransportWebRTC TransportType = "webrtc"
)

// TransportState reports the current status of a browser transport.
type TransportState struct {
	Type      TransportType `json:"type"`
	Active    bool          `json:"active"`
	LastError string        `json:"lastError,omitempty"`
}

// Transport is the pluggable interface for browser rendering strategies.
type Transport interface {
	Type() TransportType
	Start(ctx context.Context) error
	Stop() error
	State() TransportState
}

// IframeTransport is stateless; browserapi.go handles request forwarding.
type IframeTransport struct{}

func (t *IframeTransport) Type() TransportType           { return TransportIframe }
func (t *IframeTransport) Start(_ context.Context) error { return nil }
func (t *IframeTransport) Stop() error                   { return nil }
func (t *IframeTransport) State() TransportState {
	return TransportState{Type: TransportIframe, Active: true}
}
