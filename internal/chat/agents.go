package chat

import (
	"os/exec"

	"github.com/brainplusplus/9ed/internal/chat/acpinstall"
)

// Agent represents a CLI agent that can be spawned as a chat backend (legacy PTY).
type Agent struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Available bool     `json:"available"`
}

var knownAgents = []struct {
	id      string
	label   string
	cmd     string
	acpCmd  string
	acpArgs []string
}{
	{"opencode", "OpenCode", "opencode", "opencode", []string{"acp"}},
	{"claude", "Claude Code", "claude", "claude-agent-acp", []string{}},
	{"codex", "Codex CLI", "codex", "codex-acp", []string{}},
	{"gemini", "Gemini CLI", "gemini", "gemini", []string{"--experimental-acp"}},
	{"pi", "Pi", "pi", "pi-acp", []string{}},
	{"amp", "Amp", "amp", "amp-acp", []string{}},
	{"copilot", "GitHub Copilot", "copilot", "github-copilot-cli", []string{}},
}

// DiscoverAgents checks PATH for known agent binaries and returns their availability (legacy).
func DiscoverAgents() []Agent {
	agents := make([]Agent, 0, len(knownAgents))
	for _, ka := range knownAgents {
		a := Agent{
			ID:      ka.id,
			Label:   ka.label,
			Command: ka.cmd,
			Args:    []string{},
		}
		if fullPath, err := exec.LookPath(ka.cmd); err == nil {
			a.Available = true
			a.Command = fullPath
		}
		agents = append(agents, a)
	}
	return agents
}

var ptyFallbackArgs = map[string][]string{
	"claude": {"--print", "--output-format", "stream-json", "--verbose"},
	"codex":  {"--quiet"},
	"gemini": {"--output-format", "stream-json", "--prompt"},
}

// DiscoverAgentDescriptors returns agents with ACP capability metadata.
func DiscoverAgentDescriptors() []AgentDescriptor {
	descriptors := make([]AgentDescriptor, 0, len(knownAgents))
	for _, ka := range knownAgents {
		d := AgentDescriptor{
			ID:      ka.id,
			Label:   ka.label,
			Command: ka.cmd,
			Args:    ptyFallbackArgs[ka.id],
		}
		if d.Args == nil {
			d.Args = []string{}
		}
		if fullPath, err := exec.LookPath(ka.cmd); err == nil {
			d.Available = true
			d.Command = fullPath
		}

		if ka.acpCmd == ka.cmd {
			d.SupportsACP = d.Available
			d.ACPArgs = ka.acpArgs
		} else if acpPath, err := exec.LookPath(ka.acpCmd); err == nil {
			d.SupportsACP = true
			d.ACPCommand = acpPath
			d.ACPArgs = ka.acpArgs
		} else if acpinstall.GetAdapterInfo(ka.id) != nil {
			d.ACPInstallable = true
		}

		descriptors = append(descriptors, d)
	}
	return descriptors
}
