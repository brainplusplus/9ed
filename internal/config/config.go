package config

import (
	"errors"
	"fmt"
	"os"
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
}

func LoadFromEnv() (Config, error) {
	_ = godotenv.Load()

	autokill := strings.TrimSpace(strings.ToLower(os.Getenv("AUTOKILL_PORT")))

	cfg := Config{
		Port:              strings.TrimSpace(os.Getenv("PORT")),
		BasicAuthUsername: os.Getenv("BASIC_AUTH_USERNAME"),
		BasicAuthPassword: os.Getenv("BASIC_AUTH_PASSWORD"),
		Mode:              strings.TrimSpace(strings.ToLower(os.Getenv("MODE"))),
		WorkspaceRoot:     strings.TrimSpace(os.Getenv("WORKSPACE_ROOT")),
		AutokillPort:      autokill == "" || autokill == "true" || autokill == "1",
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.Mode == "" {
		cfg.Mode = "simple"
	}

	if cfg.Mode != "simple" && cfg.Mode != "full" {
		return Config{}, fmt.Errorf("MODE must be 'simple' or 'full', got %q", cfg.Mode)
	}

	if strings.TrimSpace(cfg.BasicAuthUsername) == "" || strings.TrimSpace(cfg.BasicAuthPassword) == "" {
		return Config{}, errors.New("BASIC_AUTH_USERNAME and BASIC_AUTH_PASSWORD are required")
	}

	return cfg, nil
}
