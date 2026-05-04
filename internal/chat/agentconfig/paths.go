package agentconfig

import (
	"os"
	"path/filepath"
	"runtime"
)

func openCodeConfigDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, ".config", "opencode")
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "opencode")
		}
		return filepath.Join(home, ".config", "opencode")
	}
}

func claudeConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func codexConfigDir() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func piConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}

func copilotConfigDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "github-copilot")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "github-copilot")
	default:
		return filepath.Join(home, ".config", "github-copilot")
	}
}
