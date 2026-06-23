package visualstream

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/pion/webrtc/v4"
)

// InputEvent represents a mouse or keyboard event from a remote client,
// delivered via the WebRTC DataChannel (ADR-0001 input layer).
type InputEvent struct {
	Type     string  `json:"type"`     // "mouse_move", "mouse_down", "mouse_up", "mouse_click", "key_down", "key_up", "scroll", "text"
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Button   int     `json:"button,omitempty"`
	Key      string  `json:"key,omitempty"`
	Code     string  `json:"code,omitempty"`
	Modifiers int    `json:"modifiers,omitempty"`
	Text     string  `json:"text,omitempty"`
	DeltaX   float64 `json:"deltaX,omitempty"`
	DeltaY   float64 `json:"deltaY,omitempty"`
}

// InputHandler injects input events into the visual surface (ADR-0001).
// Implementations: CDPInputHandler (browser), NativeInputHandler (remote desktop).
type InputHandler interface {
	HandleInput(evt InputEvent) error
	Close() error
}

// DataChannel message type bytes (first byte of binary messages).
const (
	msgTypeTile  byte = 0x01
	msgTypeInput byte = 0x02
)

// StreamingSession ties together a FrameSource, VisualStreamStrategy,
// PeerManager, and InputHandler for one visual streaming session (one browser
// tab or remote desktop). It runs the frame streaming loop:
//
//	source.Start() -> frames -> strategy.EncodeAndSend(frame, peers)
//
// and routes DataChannel input messages to the InputHandler (ADR-0001).
type StreamingSession struct {
	mu          sync.Mutex
	source      FrameSource
	strategy    VisualStreamStrategy
	peerMgr     *PeerManager
	input       InputHandler
	cancel      context.CancelFunc
	running     bool
	done        chan struct{}
	closeOnce   sync.Once
}

// NewStreamingSession creates a new streaming session with the given
// components. The session must be started via Start().
func NewStreamingSession(source FrameSource, strategy VisualStreamStrategy, input InputHandler) *StreamingSession {
	return &StreamingSession{
		source:   source,
		strategy: strategy,
		peerMgr:  NewPeerManager(),
		input:    input,
		done:     make(chan struct{}),
	}
}

// Start begins the frame streaming loop. Frames from the source are encoded
// and sent to all connected peers via the strategy.
func (ss *StreamingSession) Start(ctx context.Context) error {
	ss.mu.Lock()
	if ss.running {
		ss.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	frames, err := ss.source.Start(ctx)
	if err != nil {
		cancel()
		ss.mu.Unlock()
		return err
	}

	ss.cancel = cancel
	ss.running = true
	ss.mu.Unlock()

	go ss.frameLoop(ctx, frames)
	return nil
}

// Stop stops the streaming session, closes all peers, and releases resources.
// Safe to call from multiple goroutines; cleanup runs exactly once.
func (ss *StreamingSession) Stop() {
	ss.mu.Lock()
	if !ss.running {
		ss.mu.Unlock()
		return
	}
	ss.running = false
	if ss.cancel != nil {
		ss.cancel()
	}
	ss.mu.Unlock()

	_ = ss.source.Stop()
	ss.peerMgr.Close()
	if ss.input != nil {
		_ = ss.input.Close()
	}
	if ss.strategy != nil {
		_ = ss.strategy.Close()
	}
	ss.closeOnce.Do(func() {
		close(ss.done)
	})
}

// Done returns a channel that is closed when the session stops.
func (ss *StreamingSession) Done() <-chan struct{} {
	return ss.done
}

// PeerManager returns the peer manager for this session.
func (ss *StreamingSession) PeerManager() *PeerManager {
	return ss.peerMgr
}

// frameLoop reads frames from the source and encodes+sends them to all peers.
func (ss *StreamingSession) frameLoop(ctx context.Context, frames <-chan Frame) {
	defer func() {
		if r := recover(); r != nil {
			debug.Printf("[visualstream/session] frame loop panic: %v", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}
			peers := ss.peerMgr.Peers()
			if len(peers) == 0 {
				continue
			}
			if ss.strategy != nil {
				ss.strategy.EncodeAndSend(frame, peers)
			}
		}
	}
}

// AttachPeerInputHandler wires a peer's DataChannel to route input messages
// to the session's InputHandler. Called when a new peer is added.
func (ss *StreamingSession) AttachPeerInputHandler(peer *PeerConnection) {
	if peer.DataChannel == nil || ss.input == nil {
		return
	}

	peer.DataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString || len(msg.Data) == 0 {
			return
		}
		if msg.Data[0] != msgTypeInput {
			return
		}

		var evt InputEvent
		if err := json.Unmarshal(msg.Data[1:], &evt); err != nil {
			debug.Printf("[visualstream/session] input unmarshal failed: %v", err)
			return
		}
		if err := ss.input.HandleInput(evt); err != nil {
			debug.Printf("[visualstream/session] input handling failed: %v", err)
		}
	})
}
