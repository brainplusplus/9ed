package agentconfig

import "os/exec"

func detectAmp() AgentConfig {
	cfg := AgentConfig{
		ID:        "amp",
		Label:     "Amp",
		Models:    []ModelInfo{},
		Providers: []ProviderInfo{},
	}

	if _, err := exec.LookPath("amp"); err != nil {
		return cfg
	}
	cfg.Available = true

	return cfg
}
