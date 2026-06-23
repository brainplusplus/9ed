//go:build !debug

package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brainplusplus/9ed/internal/chat"
)

// TestDebugCrashAgent_NotRegisteredInNonDebugBuild verifies that in a non-debug
// build (without -tags debug), the /api/_debug/crash-agent route is not
// registered. The mux returns 404 for unregistered routes.
//
// This is the first layer of the two-gate design: the route only exists when
// the binary is built with -tags debug. The second layer is the DEBUG=true env
// var check at runtime (tested in debug_crash_test.go).
func TestDebugCrashAgent_NotRegisteredInNonDebugBuild(t *testing.T) {
	manager := &fakeChatRuntimeManager{
		sessions: map[string]chat.ChatSession{},
	}
	api := New(Dependencies{
		ChatSessionManager: manager,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/_debug/crash-agent", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 in non-debug build, got %d", rec.Code)
	}
}
