//go:build debug

package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/debug"
)

// debugCrashAgentRequest is the JSON body for POST /api/_debug/crash-agent.
type debugCrashAgentRequest struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"` // "sigkill" | "panic" | "unclean-exit"
}

// debugCrashAgentResponse is the JSON response for a successful crash.
type debugCrashAgentResponse struct {
	OK      bool   `json:"ok"`
	Mode    string `json:"mode"`
	Message string `json:"message,omitempty"`
}

// registerDebugCrashRoute registers the POST /api/_debug/crash-agent endpoint
// on the given mux. This file is compiled ONLY when the "debug" build tag is
// active (go build -tags debug). In non-debug builds,
// registerDebugCrashRoute (in debug_crash_stub.go) is a no-op so the route is
// absent and requests get a 404 from the default mux.
//
// The endpoint is additionally gated at runtime by DEBUG=true: even in a debug
// build, the endpoint returns 404 unless the DEBUG environment variable is set
// to "true" or "1". This two-layer gate (build tag + env var) ensures the
// crash endpoint can never be reached in production.
func (a *API) registerDebugCrashRoute(mux *http.ServeMux) {
	mux.HandleFunc("/api/_debug/crash-agent", a.handleDebugCrashAgent)
}

// handleDebugCrashAgent handles POST /api/_debug/crash-agent.
//
// Requires:
//   - Built with -tags debug (otherwise this file is not compiled)
//   - DEBUG=true env var at runtime (otherwise 404)
//   - POST method (otherwise 405)
//   - JSON body {sessionId, mode} where mode is "sigkill", "panic", or
//     "unclean-exit"
//
// The handler looks up the chat session by sessionId, type-asserts it to
// chat.CrashableSession, and calls CrashAgent(mode) to deterministically kill
// the underlying ACP subprocess. This is used to test the auto-restart logic
// (ADR-0004) without waiting for a natural crash.
func (a *API) handleDebugCrashAgent(w http.ResponseWriter, r *http.Request) {
	// Runtime gate: even in a debug build, require DEBUG=true.
	if !debug.Enabled() {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req debugCrashAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		http.Error(w, "sessionId is required", http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		http.Error(w, "mode is required (sigkill, panic, or unclean-exit)", http.StatusBadRequest)
		return
	}

	// Look up the chat session.
	session, ok := a.chatSessionManager.Get(req.SessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Type-assert to CrashableSession (only ACP sessions support crashing).
	crashable, ok := session.(chat.CrashableSession)
	if !ok {
		http.Error(w, "session does not support crashing (not an ACP session)", http.StatusConflict)
		return
	}

	// Crash the agent subprocess.
	if err := crashable.CrashAgent(req.Mode); err != nil {
		http.Error(w, "crash failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, debugCrashAgentResponse{
		OK:      true,
		Mode:    req.Mode,
		Message: "agent subprocess crashed; auto-restart will attempt to recover if enabled",
	})
}
