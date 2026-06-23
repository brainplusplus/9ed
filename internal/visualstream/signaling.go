package visualstream

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// SignalingHandler manages WebRTC peer connections for visual streaming
// sessions. It processes SDP offers and ICE candidates from browser clients
// and establishes pion PeerConnections (ADR-0001).
//
// Each session ID maps to a StreamingSession that owns its frame source,
// strategy, peer manager, and input handler.
type SignalingHandler struct {
	mu          sync.Mutex
	sessions    map[string]*StreamingSession
	iceServers  []webrtc.ICEServer
}

// NewSignalingHandler creates a new signaling handler with default STUN servers.
func NewSignalingHandler() *SignalingHandler {
	return &SignalingHandler{
		sessions:   make(map[string]*StreamingSession),
		iceServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
}

// RegisterSession associates a StreamingSession (frame source + strategy +
// input handler) with a session ID. If a session is already registered for
// this ID, it is stopped to avoid resource leaks. Returns the session that
// was displaced (if any) so callers can detect the re-registration case.
func (h *SignalingHandler) RegisterSession(sessionID string, ss *StreamingSession) {
	h.mu.Lock()
	existing := h.sessions[sessionID]
	h.sessions[sessionID] = ss
	h.mu.Unlock()
	if existing != nil {
		existing.Stop()
	}
	debug.Printf("[visualstream/signaling] registered session=%s", sessionID)
}

// SessionByID returns the currently registered StreamingSession for the given
// ID, or nil if none is registered.
func (h *SignalingHandler) SessionByID(sessionID string) *StreamingSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[sessionID]
}

// UnregisterSession removes a session and closes all peer connections.
func (h *SignalingHandler) UnregisterSession(sessionID string) {
	h.mu.Lock()
	ss, ok := h.sessions[sessionID]
	if ok {
		delete(h.sessions, sessionID)
	}
	h.mu.Unlock()
	if ok {
		ss.Stop()
	}
	debug.Printf("[visualstream/signaling] unregistered session=%s", sessionID)
}

// HasSession reports whether a session is registered.
func (h *SignalingHandler) HasSession(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.sessions[sessionID]
	return ok
}

// HandleOffer processes an SDP offer from a browser client and returns the
// SDP answer. Creates a new PeerConnection with DataChannel for input +
// JPEG tiles (Strategy A), or video track (Strategy B). Attaches the input
// handler to the DataChannel so remote input events are routed to the surface.
func (h *SignalingHandler) HandleOffer(sessionID string, sdp string) (string, error) {
	h.mu.Lock()
	ss, ok := h.sessions[sessionID]
	h.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("session %s not registered", sessionID)
	}

	// Create new PeerConnection.
	peerID := uuid.NewString()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: h.iceServers,
	})
	if err != nil {
		return "", fmt.Errorf("create PC: %w", err)
	}

	peer := &PeerConnection{
		ID: peerID,
		PC: pc,
	}

	// Create DataChannel for input events + JPEG tiles (Strategy A).
	dc, err := pc.CreateDataChannel("visual", &webrtc.DataChannelInit{
		Ordered: boolPtr(false),
	})
	if err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("create DataChannel: %w", err)
	}
	peer.DataChannel = dc

	// Set remote description (offer).
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("set remote description: %w", err)
	}

	// Create answer.
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("create answer: %w", err)
	}

	// Set local description — triggers ICE gathering.
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("set local description: %w", err)
	}

	// Wait for ICE gathering to complete so the answer SDP contains all
	// local candidates (vanilla ICE). This avoids the need for trickle ICE
	// — the client gets a complete answer with embedded candidates, which is
	// sufficient for local connections and simpler than implementing a
	// candidate trickle channel.
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	// Read the updated local description with gathered candidates.
	finalAnswer := pc.LocalDescription()

	ss.PeerManager().AddPeer(peer)
	// Wire DataChannel input messages to the session's InputHandler.
	ss.AttachPeerInputHandler(peer)

	// Handle DataChannel close/error to clean up peer resources.
	dc.OnClose(func() {
		ss.PeerManager().RemovePeer(peer.ID)
		debug.Printf("[visualstream/signaling] DataChannel closed, peer removed session=%s peer=%s", sessionID, peerID)
	})
	dc.OnError(func(err error) {
		debug.Printf("[visualstream/signaling] DataChannel error session=%s peer=%s: %v", sessionID, peerID, err)
	})

	// Remove peer when the connection fails or closes to avoid leaks.
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			ss.PeerManager().RemovePeer(peer.ID)
			debug.Printf("[visualstream/signaling] peer removed session=%s peer=%s state=%s", sessionID, peerID, state)
		}
	})

	debug.Printf("[visualstream/signaling] peer connected session=%s peer=%s", sessionID, peerID)

	return finalAnswer.SDP, nil
}

// HandleICECandidate processes a remote ICE candidate from a browser client.
func (h *SignalingHandler) HandleICECandidate(sessionID, peerID string, candidate webrtc.ICECandidateInit) error {
	h.mu.Lock()
	ss, ok := h.sessions[sessionID]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Add candidate to all peers in the session (trickle ICE).
	for _, peer := range ss.PeerManager().Peers() {
		if err := peer.PC.AddICECandidate(candidate); err != nil {
			debug.Printf("[visualstream/signaling] add ICE candidate failed peer=%s: %v", peer.ID, err)
		}
	}
	return nil
}

// HandleSignalingMessage processes a JSON signaling message from the WS.
// Returns a response message to send back to the client.
func (h *SignalingHandler) HandleSignalingMessage(msg SignalingMessage) (*SignalingMessage, error) {
	switch msg.Type {
	case "offer":
		answer, err := h.HandleOffer(msg.SessionID, msg.SDP)
		if err != nil {
			return nil, err
		}
		return &SignalingMessage{
			Type:      "answer",
			SessionID: msg.SessionID,
			SDP:       answer,
		}, nil
	case "ice-candidate":
		if msg.ICE != nil {
			err := h.HandleICECandidate(msg.SessionID, "", *msg.ICE)
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown signaling message type: %s", msg.Type)
	}
}

// HandleSignalingJSON processes a raw JSON signaling message.
func (h *SignalingHandler) HandleSignalingJSON(data []byte) ([]byte, error) {
	var msg SignalingMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal signaling: %w", err)
	}

	resp, err := h.HandleSignalingMessage(msg)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return resp.Encode()
}

func boolPtr(b bool) *bool {
	return &b
}
