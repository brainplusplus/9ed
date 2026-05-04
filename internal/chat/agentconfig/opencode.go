package agentconfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// trailingCommaRe strips trailing commas before ] or } (JSONC/JSON5 tolerance).
var trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)

func stripTrailingCommas(data []byte) []byte {
	return trailingCommaRe.ReplaceAll(data, []byte("$1"))
}

func detectOpenCode() AgentConfig {
	cfg := AgentConfig{
		ID:        "opencode",
		Label:     "OpenCode",
		Models:    []ModelInfo{},
		Providers: []ProviderInfo{},
	}

	if _, err := exec.LookPath("opencode"); err != nil {
		return cfg
	}
	cfg.Available = true

	dir := openCodeConfigDir()
	data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		return cfg
	}
	cfg.ConfigFound = true

	data = stripTrailingCommas(data)

	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return cfg
	}

	if provRaw, ok := raw["provider"]; ok {
		var providers map[string]json.RawMessage
		if json.Unmarshal(provRaw, &providers) == nil {
			for provID, provData := range providers {
			var prov struct {
				Name    string `json:"name"`
				Options struct {
					APIKey  string `json:"apiKey"`
					BaseURL string `json:"baseURL"`
				} `json:"options"`
				Models map[string]struct {
					Name  string `json:"name"`
					Limit struct {
						Context int `json:"context"`
						Output  int `json:"output"`
					} `json:"limit"`
				} `json:"models"`
			}
				if json.Unmarshal(provData, &prov) != nil {
					continue
				}

			provName := prov.Name
			if provName == "" {
				provName = provID
			}
			cfg.Providers = append(cfg.Providers, ProviderInfo{
				ID:      provID,
				Name:    provName,
				BaseURL: prov.Options.BaseURL,
				HasKey:  prov.Options.APIKey != "",
			})

			for modelID, m := range prov.Models {
				name := m.Name
				if name == "" {
					name = modelID
				}
				cfg.Models = append(cfg.Models, ModelInfo{
					ID:            modelID,
					Name:          name,
					Provider:      provID,
					ContextWindow: m.Limit.Context,
					MaxTokens:     m.Limit.Output,
				})
			}
			}
		}
	}

	omcData, err := os.ReadFile(filepath.Join(dir, "oh-my-opencode.json"))
	if err == nil {
		var omc struct {
			Agents struct {
				Sisyphus struct {
					Model string `json:"model"`
				} `json:"sisyphus"`
			} `json:"agents"`
		}
		if json.Unmarshal(omcData, &omc) == nil && omc.Agents.Sisyphus.Model != "" {
			cfg.ActiveModel = omc.Agents.Sisyphus.Model
			parts := strings.SplitN(omc.Agents.Sisyphus.Model, "/", 2)
			if len(parts) == 2 {
				cfg.AgentName = parts[1]
			}
		}
	}

	return cfg
}
