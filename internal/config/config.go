package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                 string
	BasicAuthUsername    string
	BasicAuthPassword    string
	WorkspaceRoot        string
	AutokillPort         bool
	Tunnel               bool
	TunnelEngine         string
	Debug                bool
	DebugWatcher         bool
	TerminalAIMaxLines   int
	LivenessPingInterval time.Duration
	LivenessTimeout      time.Duration
	StreamCoalesceWindow time.Duration
	SessionGraceWindow   time.Duration
	// ADR-0004: auto-restart configuration for ACP sessions.
	SessionResumeMaxRetries int
	SessionResumeBaseDelay  time.Duration
	SessionResumeMaxDelay   time.Duration
	// ADR-0006: liveness failure threshold and client reconnect tuning.
	// LivenessFailureThreshold is the number of consecutive missed pongs
	// before the server tears down the WebSocket connection (default 2).
	LivenessFailureThreshold int
	// ReconnectBaseDelay / ReconnectMaxDelay are client-side tuning knobs
	// (per ADR-0006). They are surfaced through Config so the server can
	// advertise them and so .env.example documents the client contract.
	// Defaults: 150ms base, 30s max.
	ReconnectBaseDelay time.Duration
	ReconnectMaxDelay  time.Duration
}

func LoadFromEnv() (Config, error) {
	_ = godotenv.Load()

	autokill := strings.TrimSpace(strings.ToLower(os.Getenv("AUTOKILL_PORT")))
	tunnel := strings.TrimSpace(strings.ToLower(os.Getenv("TUNNEL")))
	tunnelEngine := strings.TrimSpace(strings.ToLower(os.Getenv("TUNNEL_ENGINE")))
	dbg := strings.TrimSpace(strings.ToLower(os.Getenv("DEBUG")))
	dbgWatcher := strings.TrimSpace(strings.ToLower(os.Getenv("DEBUG_WATCHER")))
	termAILines := strings.TrimSpace(os.Getenv("TERMINAL_AI_MAX_LINES"))

	termAIMaxLines := 100
	if termAILines != "" {
		if v, err := strconv.Atoi(termAILines); err == nil {
			termAIMaxLines = v
		}
	}

	cfg := Config{
		Port:                 strings.TrimSpace(os.Getenv("PORT")),
		BasicAuthUsername:    os.Getenv("BASIC_AUTH_USERNAME"),
		BasicAuthPassword:    os.Getenv("BASIC_AUTH_PASSWORD"),
		WorkspaceRoot:        strings.TrimSpace(os.Getenv("WORKSPACE_ROOT")),
		AutokillPort:         autokill == "" || autokill == "true" || autokill == "1",
		Tunnel:               tunnel == "" || tunnel == "true" || tunnel == "1",
		TunnelEngine:         tunnelEngine,
		Debug:                dbg == "true" || dbg == "1",
		DebugWatcher:         dbgWatcher == "true" || dbgWatcher == "1",
		TerminalAIMaxLines:   termAIMaxLines,
		LivenessPingInterval: parseDurationEnv("LIVENESS_PING_INTERVAL", 10*time.Second),
		LivenessTimeout:      parseDurationEnv("LIVENESS_TIMEOUT", 15*time.Second),
		StreamCoalesceWindow: parseDurationEnv("STREAM_COALESCE_WINDOW", 60*time.Millisecond),
		SessionGraceWindow:   parseDurationEnv("SESSION_GRACE_WINDOW", 10*time.Minute),
		// ADR-0004: auto-restart tuning. Defaults: 3 retries, 500ms base, 30s max.
		SessionResumeMaxRetries: parseIntEnv("SESSION_RESUME_MAX_RETRIES", 3),
		SessionResumeBaseDelay:  parseDurationEnv("SESSION_RESUME_BASE_DELAY", 500*time.Millisecond),
		SessionResumeMaxDelay:   parseDurationEnv("SESSION_RESUME_MAX_DELAY", 30*time.Second),
		// ADR-0006: liveness failure threshold (default 2 consecutive missed
		// pongs before teardown) and client reconnect backoff (150ms base,
		// 30s max).
		LivenessFailureThreshold: parseIntEnv("LIVENESS_FAILURE_THRESHOLD", 2),
		ReconnectBaseDelay:       parseDurationEnv("RECONNECT_BASE_DELAY", 150*time.Millisecond),
		ReconnectMaxDelay:        parseDurationEnv("RECONNECT_MAX_DELAY", 30*time.Second),
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.TunnelEngine == "" {
		cfg.TunnelEngine = "cloudflare"
	}

	if cfg.TunnelEngine != "bore" && cfg.TunnelEngine != "cloudflare" {
		return Config{}, errors.New("TUNNEL_ENGINE must be 'bore' or 'cloudflare'")
	}

	if strings.TrimSpace(cfg.BasicAuthUsername) == "" || strings.TrimSpace(cfg.BasicAuthPassword) == "" {
		return Config{}, errors.New("BASIC_AUTH_USERNAME and BASIC_AUTH_PASSWORD are required")
	}

	return cfg, nil
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return fallback
}

// parseIntEnv reads an integer env var, returning fallback on missing/invalid.
func parseIntEnv(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return fallback
}
