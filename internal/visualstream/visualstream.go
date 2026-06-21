// Package visualstream provides the visual streaming infrastructure for
// collaborative browser and remote desktop surfaces (ADR-0001).
//
// It uses pion/webrtc as the unified transport, with pluggable frame sources
// (CDP screencast for browser, native capture for remote desktop) and
// pluggable visual stream strategies (JPEG tile diff for static content,
// H264 full frame for full motion).
package visualstream

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/pion/webrtc/v4"
)

// Frame represents a single raw frame from a FrameSource.
type Frame struct {
	Data   []byte // raw pixel data (format depends on source)
	Width  int
	Height int
}

// FrameSource produces frames from a visual surface (ADR-0001 Layer 1).
// Implementations: CDP screencast (browser), native screen capture (remote desktop).
type FrameSource interface {
	// Start begins producing frames. Frames are delivered via the returned channel.
	Start(ctx context.Context) (<-chan Frame, error)
	// Stop stops producing frames and releases resources.
	Stop() error
}

// VisualStreamStrategy encodes frames and distributes them to subscribers via
// pion/webrtc (ADR-0001 Layer 2). Implementations: JPEG tile diff, H264 full frame.
type VisualStreamStrategy interface {
	// EncodeAndSend encodes a frame and sends it to all connected peers.
	EncodeAndSend(frame Frame, peers []*PeerConnection)
	// Close releases strategy resources.
	Close() error
}

// PeerConnection wraps a pion PeerConnection with metadata for a connected client.
type PeerConnection struct {
	ID         string
	PC         *webrtc.PeerConnection
	DataChannel *webrtc.DataChannel // for input events + JPEG tiles (Strategy A)
	VideoTrack  *webrtc.TrackLocalStaticSample // for H264 (Strategy B)
	mu         sync.Mutex
}

// SignalingMessage is exchanged over the signaling WebSocket to establish
// WebRTC connections between 9ed server and browser clients.
type SignalingMessage struct {
	Type      string          `json:"type"`      // "offer", "answer", "ice-candidate"
	SessionID string          `json:"sessionId"` // browser tab or remote desktop session ID
	SDP       string          `json:"sdp,omitempty"`
	ICEServers []webrtc.ICEServer `json:"iceServers,omitempty"`
	ICE       *webrtc.ICECandidateInit `json:"ice,omitempty"`
}

// Encode is a helper to marshal a signaling message to JSON.
func (m SignalingMessage) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// PeerManager manages WebRTC peer connections for a visual streaming session.
type PeerManager struct {
	mu    sync.Mutex
	peers map[string]*PeerConnection
}

func NewPeerManager() *PeerManager {
	return &PeerManager{peers: make(map[string]*PeerConnection)}
}

func (pm *PeerManager) AddPeer(peer *PeerConnection) {
	pm.mu.Lock()
	pm.peers[peer.ID] = peer
	pm.mu.Unlock()
}

func (pm *PeerManager) RemovePeer(id string) {
	pm.mu.Lock()
	if peer, ok := pm.peers[id]; ok {
		if peer.PC != nil {
			_ = peer.PC.Close()
		}
		delete(pm.peers, id)
	}
	pm.mu.Unlock()
}

func (pm *PeerManager) Peers() []*PeerConnection {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	list := make([]*PeerConnection, 0, len(pm.peers))
	for _, p := range pm.peers {
		list = append(list, p)
	}
	return list
}

func (pm *PeerManager) Close() {
	pm.mu.Lock()
	for _, peer := range pm.peers {
		if peer.PC != nil {
			_ = peer.PC.Close()
		}
	}
	pm.peers = make(map[string]*PeerConnection)
	pm.mu.Unlock()
}
