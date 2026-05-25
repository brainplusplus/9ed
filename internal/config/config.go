package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port              string
	BasicAuthUsername string
	BasicAuthPassword string
	Mode              string
	WorkspaceRoot     string
	AutokillPort      bool
	Tunnel            bool
	TunnelEngine      string
	UseBrowser        bool
	Debug             bool
	DebugWatcher      bool
	TerminalAIMaxLines int
}

func LoadFromEnv() (Config, error) {
	_ = godotenv.Load()

	autokill := strings.TrimSpace(strings.ToLower(os.Getenv("AUTOKILL_PORT")))
	tunnel := strings.TrimSpace(strings.ToLower(os.Getenv("TUNNEL")))
	tunnelEngine := strings.TrimSpace(strings.ToLower(os.Getenv("TUNNEL_ENGINE")))
	useBrowser := strings.TrimSpace(strings.ToLower(os.Getenv("USE_BROWSER")))
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
		Port:              strings.TrimSpace(os.Getenv("PORT")),
		BasicAuthUsername: os.Getenv("BASIC_AUTH_USERNAME"),
		BasicAuthPassword: os.Getenv("BASIC_AUTH_PASSWORD"),
		Mode:              strings.TrimSpace(strings.ToLower(os.Getenv("MODE"))),
		WorkspaceRoot:     strings.TrimSpace(os.Getenv("WORKSPACE_ROOT")),
		AutokillPort:      autokill == "" || autokill == "true" || autokill == "1",
		Tunnel:            tunnel == "" || tunnel == "true" || tunnel == "1",
		TunnelEngine:      tunnelEngine,
		UseBrowser:        useBrowser == "" || useBrowser == "true" || useBrowser == "1",
		Debug:             dbg == "true" || dbg == "1",
		DebugWatcher:      dbgWatcher == "true" || dbgWatcher == "1",
		TerminalAIMaxLines: termAIMaxLines,
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.Mode == "" {
		cfg.Mode = "simple"
	}

	if cfg.TunnelEngine == "" {
		cfg.TunnelEngine = "cloudflare"
	}

	if cfg.Mode != "simple" && cfg.Mode != "full" {
		return Config{}, fmt.Errorf("MODE must be 'simple' or 'full', got %q", cfg.Mode)
	}

	if cfg.TunnelEngine != "bore" && cfg.TunnelEngine != "cloudflare" {
		return Config{}, fmt.Errorf("TUNNEL_ENGINE must be 'bore' or 'cloudflare', got %q", cfg.TunnelEngine)
	}

	if strings.TrimSpace(cfg.BasicAuthUsername) == "" || strings.TrimSpace(cfg.BasicAuthPassword) == "" {
		return Config{}, errors.New("BASIC_AUTH_USERNAME and BASIC_AUTH_PASSWORD are required")
	}

	return cfg, nil
}
