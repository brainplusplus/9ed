package chat

import "os/exec"

// Agent represents a CLI agent that can be spawned as a chat backend.
type Agent struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Available bool     `json:"available"`
}

var knownAgents = []struct {
	id, label, cmd string
}{
	{"opencode", "OpenCode", "opencode"},
	{"claude", "Claude Code", "claude"},
}

// DiscoverAgents checks PATH for known agent binaries and returns their availability.
func DiscoverAgents() []Agent {
	agents := make([]Agent, 0, len(knownAgents))
	for _, ka := range knownAgents {
		a := Agent{
			ID:      ka.id,
			Label:   ka.label,
			Command: ka.cmd,
			Args:    []string{},
		}
		if _, err := exec.LookPath(ka.cmd); err == nil {
			a.Available = true
		}
		agents = append(agents, a)
	}
	return agents
}
