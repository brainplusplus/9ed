package agentconfig

// AgentConfig holds configuration details for a CLI AI agent.
type AgentConfig struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Available   bool           `json:"available"`
	ConfigFound bool           `json:"configFound"`
	ActiveModel string         `json:"activeModel"`
	Models      []ModelInfo    `json:"models"`
	Providers   []ProviderInfo `json:"providers"`
	AgentName   string         `json:"agentName,omitempty"`
}

// ModelInfo describes a model available to an agent.
type ModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
	CanReason     bool   `json:"canReason,omitempty"`
}

// ProviderInfo describes an AI provider configured for an agent.
type ProviderInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl,omitempty"`
	HasKey  bool   `json:"hasKey"`
}

// DetectAll scans all known agent config locations and returns their configurations.
func DetectAll() []AgentConfig {
	readers := []func() AgentConfig{
		detectOpenCode,
		detectClaude,
		detectCodex,
		detectGemini,
		detectPi,
		detectAmp,
		detectCopilot,
	}

	configs := make([]AgentConfig, 0, len(readers))
	for _, r := range readers {
		configs = append(configs, r())
	}
	return configs
}
