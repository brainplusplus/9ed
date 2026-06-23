//go:build !debug

package httpapi

import "net/http"

// registerDebugCrashRoute is a no-op in non-debug builds. The
// /api/_debug/crash-agent route is not registered, so requests to it get a 404
// from the default mux. This is the second layer of the two-gate design: the
// route only exists when the binary is built with -tags debug AND DEBUG=true
// is set at runtime.
func (a *API) registerDebugCrashRoute(_ *http.ServeMux) {
	// Intentionally empty: no route registered in non-debug builds.
}
