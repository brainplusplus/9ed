package config

import (
	"os"
	"testing"
	"time"
)

func TestSessionResumeMaxRetries_Default(t *testing.T) {
	os.Unsetenv("SESSION_RESUME_MAX_RETRIES")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.SessionResumeMaxRetries != 3 {
		t.Errorf("expected default SessionResumeMaxRetries=3, got %d", cfg.SessionResumeMaxRetries)
	}
}

func TestSessionResumeMaxRetries_Custom(t *testing.T) {
	t.Setenv("SESSION_RESUME_MAX_RETRIES", "5")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.SessionResumeMaxRetries != 5 {
		t.Errorf("expected SessionResumeMaxRetries=5, got %d", cfg.SessionResumeMaxRetries)
	}
}

func TestSessionResumeMaxRetries_InvalidFallsBack(t *testing.T) {
	t.Setenv("SESSION_RESUME_MAX_RETRIES", "not-a-number")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.SessionResumeMaxRetries != 3 {
		t.Errorf("expected fallback SessionResumeMaxRetries=3, got %d", cfg.SessionResumeMaxRetries)
	}
}

func TestSessionResumeBaseDelay_Default(t *testing.T) {
	os.Unsetenv("SESSION_RESUME_BASE_DELAY")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.SessionResumeBaseDelay != 500*time.Millisecond {
		t.Errorf("expected default SessionResumeBaseDelay=500ms, got %v", cfg.SessionResumeBaseDelay)
	}
}

func TestSessionResumeBaseDelay_Custom(t *testing.T) {
	t.Setenv("SESSION_RESUME_BASE_DELAY", "2s")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.SessionResumeBaseDelay != 2*time.Second {
		t.Errorf("expected SessionResumeBaseDelay=2s, got %v", cfg.SessionResumeBaseDelay)
	}
}

func TestSessionResumeMaxDelay_Default(t *testing.T) {
	os.Unsetenv("SESSION_RESUME_MAX_DELAY")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.SessionResumeMaxDelay != 30*time.Second {
		t.Errorf("expected default SessionResumeMaxDelay=30s, got %v", cfg.SessionResumeMaxDelay)
	}
}

func TestSessionResumeMaxDelay_Custom(t *testing.T) {
	t.Setenv("SESSION_RESUME_MAX_DELAY", "1m")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.SessionResumeMaxDelay != 60*time.Second {
		t.Errorf("expected SessionResumeMaxDelay=1m, got %v", cfg.SessionResumeMaxDelay)
	}
}

// --- ADR-0006: liveness failure threshold + reconnect tuning ---

func TestLivenessFailureThreshold_Default(t *testing.T) {
	os.Unsetenv("LIVENESS_FAILURE_THRESHOLD")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.LivenessFailureThreshold != 2 {
		t.Errorf("expected default LivenessFailureThreshold=2, got %d", cfg.LivenessFailureThreshold)
	}
}

func TestLivenessFailureThreshold_Custom(t *testing.T) {
	t.Setenv("LIVENESS_FAILURE_THRESHOLD", "5")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.LivenessFailureThreshold != 5 {
		t.Errorf("expected LivenessFailureThreshold=5, got %d", cfg.LivenessFailureThreshold)
	}
}

func TestLivenessFailureThreshold_InvalidFallsBack(t *testing.T) {
	t.Setenv("LIVENESS_FAILURE_THRESHOLD", "not-a-number")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.LivenessFailureThreshold != 2 {
		t.Errorf("expected fallback LivenessFailureThreshold=2, got %d", cfg.LivenessFailureThreshold)
	}
}

func TestReconnectBaseDelay_Default(t *testing.T) {
	os.Unsetenv("RECONNECT_BASE_DELAY")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.ReconnectBaseDelay != 150*time.Millisecond {
		t.Errorf("expected default ReconnectBaseDelay=150ms, got %v", cfg.ReconnectBaseDelay)
	}
}

func TestReconnectBaseDelay_Custom(t *testing.T) {
	t.Setenv("RECONNECT_BASE_DELAY", "750ms")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.ReconnectBaseDelay != 750*time.Millisecond {
		t.Errorf("expected ReconnectBaseDelay=750ms, got %v", cfg.ReconnectBaseDelay)
	}
}

func TestReconnectMaxDelay_Default(t *testing.T) {
	os.Unsetenv("RECONNECT_MAX_DELAY")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.ReconnectMaxDelay != 30*time.Second {
		t.Errorf("expected default ReconnectMaxDelay=30s, got %v", cfg.ReconnectMaxDelay)
	}
}

func TestReconnectMaxDelay_Custom(t *testing.T) {
	t.Setenv("RECONNECT_MAX_DELAY", "2m")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.ReconnectMaxDelay != 120*time.Second {
		t.Errorf("expected ReconnectMaxDelay=2m, got %v", cfg.ReconnectMaxDelay)
	}
}

// --- ADR-0005: PTY ring buffer size + input lock TTL ---

// TestPTYRingBufferSize_Default verifies the default ring buffer size is 1MB
// (1048576 bytes) per ADR-0005.
func TestPTYRingBufferSize_Default(t *testing.T) {
	os.Unsetenv("PTY_RING_BUFFER_SIZE")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.PTYRingBufferSize != 1048576 {
		t.Errorf("expected default PTYRingBufferSize=1048576 (1MB), got %d", cfg.PTYRingBufferSize)
	}
}

// TestPTYRingBufferSize_Custom verifies a custom ring buffer size is honored.
func TestPTYRingBufferSize_Custom(t *testing.T) {
	t.Setenv("PTY_RING_BUFFER_SIZE", "5242880")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.PTYRingBufferSize != 5242880 {
		t.Errorf("expected PTYRingBufferSize=5242880 (5MB), got %d", cfg.PTYRingBufferSize)
	}
}

// TestPTYRingBufferSize_InvalidFallsBack verifies an invalid value falls back
// to the default 1MB.
func TestPTYRingBufferSize_InvalidFallsBack(t *testing.T) {
	t.Setenv("PTY_RING_BUFFER_SIZE", "not-a-number")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.PTYRingBufferSize != 1048576 {
		t.Errorf("expected fallback PTYRingBufferSize=1048576, got %d", cfg.PTYRingBufferSize)
	}
}

// TestPTYInputLockTTL_Default verifies the default input lock TTL is 2s.
func TestPTYInputLockTTL_Default(t *testing.T) {
	os.Unsetenv("PTY_INPUT_LOCK_TTL")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.PTYInputLockTTL != 2*time.Second {
		t.Errorf("expected default PTYInputLockTTL=2s, got %v", cfg.PTYInputLockTTL)
	}
}

// TestPTYInputLockTTL_Custom verifies a custom TTL is honored.
func TestPTYInputLockTTL_Custom(t *testing.T) {
	t.Setenv("PTY_INPUT_LOCK_TTL", "500ms")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.PTYInputLockTTL != 500*time.Millisecond {
		t.Errorf("expected PTYInputLockTTL=500ms, got %v", cfg.PTYInputLockTTL)
	}
}

// TestPTYInputLockTTL_InvalidFallsBack verifies an invalid value falls back.
func TestPTYInputLockTTL_InvalidFallsBack(t *testing.T) {
	t.Setenv("PTY_INPUT_LOCK_TTL", "not-a-duration")
	t.Setenv("BASIC_AUTH_USERNAME", "admin")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.PTYInputLockTTL != 2*time.Second {
		t.Errorf("expected fallback PTYInputLockTTL=2s, got %v", cfg.PTYInputLockTTL)
	}
}
