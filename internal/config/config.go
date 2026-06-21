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
		Port:               strings.TrimSpace(os.Getenv("PORT")),
		BasicAuthUsername:  os.Getenv("BASIC_AUTH_USERNAME"),
		BasicAuthPassword:  os.Getenv("BASIC_AUTH_PASSWORD"),
		WorkspaceRoot:      strings.TrimSpace(os.Getenv("WORKSPACE_ROOT")),
		AutokillPort:       autokill == "" || autokill == "true" || autokill == "1",
		Tunnel:             tunnel == "" || tunnel == "true" || tunnel == "1",
		TunnelEngine:       tunnelEngine,
		Debug:              dbg == "true" || dbg == "1",
		DebugWatcher:       dbgWatcher == "true" || dbgWatcher == "1",
		TerminalAIMaxLines: termAIMaxLines,
		LivenessPingInterval: parseDurationEnv("LIVENESS_PING_INTERVAL", 10*time.Second),
		LivenessTimeout:      parseDurationEnv("LIVENESS_TIMEOUT", 15*time.Second),
		StreamCoalesceWindow: parseDurationEnv("STREAM_COALESCE_WINDOW", 60*time.Millisecond),
		SessionGraceWindow:   parseDurationEnv("SESSION_GRACE_WINDOW", 10*time.Minute),
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
