package agentconfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func detectCodex() AgentConfig {
	cfg := AgentConfig{
		ID:      "codex",
		Label:   "Codex CLI",
		Models:  []ModelInfo{},
		Providers: []ProviderInfo{},
	}

	if _, err := exec.LookPath("codex"); err != nil {
		return cfg
	}
	cfg.Available = true

	dir := codexConfigDir()

	configPath := filepath.Join(dir, "config.toml")
	var tomlCfg struct {
		Model          string                       `toml:"model"`
		ModelProviders map[string]codexProviderEntry `toml:"model_providers"`
	}
	if _, err := toml.DecodeFile(configPath, &tomlCfg); err == nil {
		cfg.ConfigFound = true
		cfg.ActiveModel = tomlCfg.Model

		for provID, p := range tomlCfg.ModelProviders {
			name := p.Name
			if name == "" {
				name = provID
			}
			cfg.Providers = append(cfg.Providers, ProviderInfo{
				ID:      provID,
				Name:    name,
				BaseURL: p.BaseURL,
				HasKey:  p.APIKey != "",
			})
		}
	}

	cacheData, err := os.ReadFile(filepath.Join(dir, "models_cache.json"))
	if err == nil {
		var cache struct {
			Models []struct {
				Slug        string `json:"slug"`
				DisplayName string `json:"display_name"`
				Visibility  string `json:"visibility"`
				ContextWindow int  `json:"context_window"`
			} `json:"models"`
		}
		if json.Unmarshal(cacheData, &cache) == nil {
			for _, m := range cache.Models {
				if m.Visibility == "hide" {
					continue
				}
				name := m.DisplayName
				if name == "" {
					name = m.Slug
				}
				cfg.Models = append(cfg.Models, ModelInfo{
					ID:            m.Slug,
					Name:          name,
					Provider:      "openai",
					ContextWindow: m.ContextWindow,
				})
			}
		}
	}

	return cfg
}

type codexProviderEntry struct {
	Name    string `toml:"name"`
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
}
