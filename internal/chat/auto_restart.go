package chat

import (
	"strings"
)

// isPersistentError classifies whether an error is persistent (config error,
// auth expired, binary not found) vs transient (network blip, EOF, OOM).
// Persistent errors should not be retried (ADR-0004).
func isPersistentError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	persistentMarkers := []string{
		"no such file or directory",
		"executable file not found",
		"command not found",
		"permission denied",
		"unauthorized",
		"authentication",
		"auth expired",
		"invalid api key",
		"config error",
		"not supported",
		"does not support",
	}
	for _, marker := range persistentMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// isTransientError classifies whether an error is transient (network blip,
// EOF, EPIPE, OOM, process killed). These should be retried (ADR-0004).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if isPersistentError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	transientMarkers := []string{
		"eof",
		"broken pipe",
		"connection reset",
		"connection refused",
		"connection closed",
		"epipe",
		"signal: killed",
		"signal: terminated",
		"context deadline exceeded",
		"timeout",
		"temporary failure",
	}
	for _, marker := range transientMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return true // default: treat unknown errors as transient (retry)
}
