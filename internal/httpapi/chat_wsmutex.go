package httpapi

import "sync"

// writeJSONSafe serializes a JSON write to a gorilla/websocket connection.
//
// gorilla/websocket documents: "Connections support one concurrent reader and
// one concurrent writer." In handleChatWebSocket there are two concurrent
// writers — the outbound fan-out goroutine (chat events + protocol pings) and
// the main read loop (pong/hello_ack + fetch_timeline responses). Calling
// conn.WriteJSON concurrently corrupts frames and can panic with
// "concurrent write to websocket connection" under rapid page reloads or
// multi-client load.
//
// writeJSONSafe locks writeMu for the entire duration of the write so that all
// callers across both goroutines are serialized.
func writeJSONSafe(writeMu *sync.Mutex, conn wsConn, v any) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteJSON(v)
}

// writeMessageSafe is the WriteMessage counterpart of writeJSONSafe. It is used
// for RFC6455 protocol pings emitted from the outbound goroutine.
func writeMessageSafe(writeMu *sync.Mutex, conn wsConn, messageType int, data []byte) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteMessage(messageType, data)
}
