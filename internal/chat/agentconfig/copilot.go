package agentconfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

func detectCopilot() AgentConfig {
	cfg := AgentConfig{
		ID:      "copilot",
		Label:   "GitHub Copilot",
		Models:  []ModelInfo{},
		Providers: []ProviderInfo{},
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return cfg
	}
	cfg.Available = true

	dir := copilotConfigDir()
	data, err := os.ReadFile(filepath.Join(dir, "hosts.json"))
	if err != nil {
		return cfg
	}
	cfg.ConfigFound = true

	var hosts map[string]struct {
		OAuthToken string `json:"oauth_token"`
	}
	if json.Unmarshal(data, &hosts) == nil {
		for _, h := range hosts {
			if h.OAuthToken != "" {
				cfg.Providers = append(cfg.Providers, ProviderInfo{
					ID:     "github",
					Name:   "GitHub",
					HasKey: true,
				})
				break
			}
		}
	}

	// Hardcoded known Copilot models
	knownModels := []struct {
		id, name string
		reason   bool
	}{
		{"gpt-4o", "GPT-4o", false},
		{"claude-sonnet-4", "Claude Sonnet 4", false},
		{"o3-mini", "o3-mini", true},
	}
	for _, m := range knownModels {
		cfg.Models = append(cfg.Models, ModelInfo{
			ID:        m.id,
			Name:      m.name,
			Provider:  "github",
			CanReason: m.reason,
		})
	}

	return cfg
}
