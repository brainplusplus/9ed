package agentconfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

func detectClaude() AgentConfig {
	cfg := AgentConfig{
		ID:      "claude",
		Label:   "Claude Code",
		Models:  []ModelInfo{},
		Providers: []ProviderInfo{},
	}

	if _, err := exec.LookPath("claude"); err != nil {
		return cfg
	}
	cfg.Available = true

	dir := claudeConfigDir()

	var data []byte
	var err error
	data, err = os.ReadFile(filepath.Join(dir, "settings.local.json"))
	if err != nil {
		data, err = os.ReadFile(filepath.Join(dir, "settings.json"))
		if err != nil {
			return cfg
		}
	}
	cfg.ConfigFound = true

	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return cfg
	}

	hasKey := false

	if envRaw, ok := raw["env"]; ok {
		var env map[string]string
		if json.Unmarshal(envRaw, &env) == nil {
			if env["ANTHROPIC_API_KEY"] != "" {
				hasKey = true
			}
			if m := env["ANTHROPIC_DEFAULT_SONNET_MODEL"]; m != "" {
				cfg.ActiveModel = m
			}
		}
	}

	if cfg.ActiveModel == "" {
		if modelRaw, ok := raw["model"]; ok {
			var model string
			if json.Unmarshal(modelRaw, &model) == nil {
				cfg.ActiveModel = model
			}
		}
	}

	cfg.Providers = append(cfg.Providers, ProviderInfo{
		ID:     "anthropic",
		Name:   "Anthropic",
		HasKey: hasKey,
	})

	knownModels := []struct {
		id, name string
		reason   bool
	}{
		{"claude-sonnet-4-20250514", "Claude Sonnet 4", false},
		{"claude-opus-4-20250514", "Claude Opus 4", true},
		{"claude-haiku-3-5-20241022", "Claude Haiku 3.5", false},
	}
	for _, m := range knownModels {
		cfg.Models = append(cfg.Models, ModelInfo{
			ID:        m.id,
			Name:      m.name,
			Provider:  "anthropic",
			CanReason: m.reason,
		})
	}

	return cfg
}
