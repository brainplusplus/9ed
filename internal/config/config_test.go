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
