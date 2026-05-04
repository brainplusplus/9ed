package agentconfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

func detectPi() AgentConfig {
	cfg := AgentConfig{
		ID:      "pi",
		Label:   "Pi",
		Models:  []ModelInfo{},
		Providers: []ProviderInfo{},
	}

	if _, err := exec.LookPath("pi"); err != nil {
		return cfg
	}
	cfg.Available = true

	dir := piConfigDir()

	settingsData, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err == nil {
		cfg.ConfigFound = true
		var settings struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(settingsData, &settings) == nil {
			cfg.ActiveModel = settings.Model
		}
	}

	if cfg.ActiveModel == "" {
		routingData, err := os.ReadFile(filepath.Join(dir, "routing.json"))
		if err == nil {
			cfg.ConfigFound = true
			var routing struct {
				Default string `json:"default"`
			}
			if json.Unmarshal(routingData, &routing) == nil {
				cfg.ActiveModel = routing.Default
			}
		}
	}

	modelsData, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err == nil {
		cfg.ConfigFound = true
		var modelsFile struct {
			Providers map[string]struct {
				Name   string `json:"name"`
				APIKey string `json:"apiKey"`
				Models []struct {
					ID            string `json:"id"`
					Name          string `json:"name"`
					ContextWindow int    `json:"contextWindow"`
					MaxTokens     int    `json:"maxTokens"`
					CanReason     bool   `json:"canReason"`
				} `json:"models"`
			} `json:"providers"`
		}
		if json.Unmarshal(modelsData, &modelsFile) == nil {
			for provID, p := range modelsFile.Providers {
				name := p.Name
				if name == "" {
					name = provID
				}
				cfg.Providers = append(cfg.Providers, ProviderInfo{
					ID:     provID,
					Name:   name,
					HasKey: p.APIKey != "",
				})
				for _, m := range p.Models {
					mName := m.Name
					if mName == "" {
						mName = m.ID
					}
					cfg.Models = append(cfg.Models, ModelInfo{
						ID:            m.ID,
						Name:          mName,
						Provider:      provID,
						ContextWindow: m.ContextWindow,
						MaxTokens:     m.MaxTokens,
						CanReason:     m.CanReason,
					})
				}
			}
		}
	}

	return cfg
}
