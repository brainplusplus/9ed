package httpapi

import (
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/google/uuid"
)

// chatConnectionRegistry tracks logical client connections (ADR-0006 clientId).
//
// Multiple WS sockets can share a clientId (multi-tab browser, multi-device).
// A SessionConnection holds subscriber state and outlives individual socket
// drops during the grace window (ADR-0003).
type chatConnectionRegistry struct {
	mu          sync.Mutex
	connections map[string]*chatConnection // keyed by clientId
}

type chatConnection struct {
	clientID  string
	sessionID string

	mu       sync.Mutex
	sockets  map[string]*chatSocket // keyed by socket id
	lastSeen time.Time
}

type chatSocket struct {
	id       string
	conn     wsConn
	clientID string
	sessionID string
}

// wsConn is the minimal interface a WebSocket connection must implement for
// the connection registry. *gorilla/websocket.Conn satisfies this.
type wsConn interface {
	WriteJSON(v any) error
	WriteMessage(messageType int, data []byte) error
	Close() error
}

func newChatConnectionRegistry() *chatConnectionRegistry {
	return &chatConnectionRegistry{connections: make(map[string]*chatConnection)}
}

// registerSocket associates a WS socket with a clientId. If a chatConnection
// already exists for that clientId (reconnect or multi-tab), the socket joins
// it. Otherwise a new chatConnection is created. Returns the chatConnection.
func (r *chatConnectionRegistry) registerSocket(clientID, sessionID string, socket wsConn) (*chatConnection, *chatSocket) {
	if clientID == "" {
		clientID = uuid.NewString()
	}
	socketID := uuid.NewString()
	cs := &chatSocket{id: socketID, conn: socket, clientID: clientID, sessionID: sessionID}

	r.mu.Lock()
	cc, ok := r.connections[clientID]
	if !ok {
		cc = &chatConnection{
			clientID:  clientID,
			sessionID: sessionID,
			sockets:   make(map[string]*chatSocket),
		}
		r.connections[clientID] = cc
	}
	cc.mu.Lock()
	cc.sockets[socketID] = cs
	cc.lastSeen = time.Now()
	cc.mu.Unlock()
	r.mu.Unlock()

	debug.Printf("[chat/conn] register socket=%s client=%s session=%s sockets=%d", socketID, clientID, sessionID, len(cc.sockets))
	return cc, cs
}

// removeSocket removes a socket from its chatConnection. If the chatConnection
// has no remaining sockets, it is NOT deleted here — the grace window
// (ADR-0003) keeps it alive. Use sweepExpired to clean up expired connections.
func (r *chatConnectionRegistry) removeSocket(cs *chatSocket) {
	if cs == nil {
		return
	}
	r.mu.Lock()
	cc, ok := r.connections[cs.clientID]
	r.mu.Unlock()
	if !ok {
		return
	}
	cc.mu.Lock()
	delete(cc.sockets, cs.id)
	remaining := len(cc.sockets)
	cc.mu.Unlock()
	debug.Printf("[chat/conn] remove socket=%s client=%s remaining=%d", cs.id, cs.clientID, remaining)
}

// broadcast sends a message to all sockets in a chatConnection (fan-out to
// multi-tab/multi-device). Sockets that fail to write are removed.
func (r *chatConnectionRegistry) broadcast(cc *chatConnection, msg any) {
	if cc == nil {
		return
	}
	cc.mu.Lock()
	sockets := make([]*chatSocket, 0, len(cc.sockets))
	for _, s := range cc.sockets {
		sockets = append(sockets, s)
	}
	cc.mu.Unlock()

	for _, s := range sockets {
		if err := s.conn.WriteJSON(msg); err != nil {
			r.removeSocket(s)
		}
	}
}

// sweepExpired removes chatConnections that have no sockets and haven't been
// seen since the given TTL. Called periodically by the grace window sweeper
// (ADR-0003).
func (r *chatConnectionRegistry) sweepExpired(ttl time.Duration) int {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for id, cc := range r.connections {
		cc.mu.Lock()
		empty := len(cc.sockets) == 0
		stale := now.Sub(cc.lastSeen) > ttl
		cc.mu.Unlock()
		if empty && stale {
			delete(r.connections, id)
			removed++
		}
	}
	return removed
}
