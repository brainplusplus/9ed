package agentconfig

import "os/exec"

func detectGemini() AgentConfig {
	cfg := AgentConfig{
		ID:        "gemini",
		Label:     "Gemini CLI",
		Models:    []ModelInfo{},
		Providers: []ProviderInfo{},
	}

	if _, err := exec.LookPath("gemini"); err != nil {
		return cfg
	}
	cfg.Available = true

	return cfg
}
