package acpinstall

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

type AdapterInfo struct {
	ID         string
	Package    string
	Manager    string // "npm" or "pip"
	BinaryName string
}

var adapters = []AdapterInfo{
	{ID: "claude", Package: "@agentclientprotocol/claude-agent-acp", Manager: "npm", BinaryName: "claude-agent-acp"},
	{ID: "codex", Package: "@zed-industries/codex-acp", Manager: "npm", BinaryName: "codex-acp"},
	{ID: "pi", Package: "pi-acp", Manager: "npm", BinaryName: "pi-acp"},
	{ID: "amp", Package: "amp-acp", Manager: "npm", BinaryName: "amp-acp"},
	{ID: "copilot", Package: "github-copilot-cli", Manager: "npm", BinaryName: "github-copilot-cli"},
}

var (
	installMu sync.Mutex
	installed = map[string]bool{}
)

func GetAdapterInfo(agentID string) *AdapterInfo {
	for i := range adapters {
		if adapters[i].ID == agentID {
			return &adapters[i]
		}
	}
	return nil
}

func IsInstalled(agentID string) bool {
	info := GetAdapterInfo(agentID)
	if info == nil {
		return false
	}
	_, err := exec.LookPath(info.BinaryName)
	return err == nil
}

func EnsureInstalled(agentID string) (string, error) {
	info := GetAdapterInfo(agentID)
	if info == nil {
		return "", fmt.Errorf("no ACP adapter known for agent %q", agentID)
	}

	if path, err := exec.LookPath(info.BinaryName); err == nil {
		return path, nil
	}

	installMu.Lock()
	defer installMu.Unlock()

	if installed[agentID] {
		if path, err := exec.LookPath(info.BinaryName); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("adapter %q was installed but binary not found in PATH", info.BinaryName)
	}

	if err := install(info); err != nil {
		return "", fmt.Errorf("install %s: %w", info.Package, err)
	}

	installed[agentID] = true

	path, err := exec.LookPath(info.BinaryName)
	if err != nil {
		return "", fmt.Errorf("adapter installed but %q not found in PATH — may need shell restart", info.BinaryName)
	}
	return path, nil
}

func Update(agentID string) error {
	info := GetAdapterInfo(agentID)
	if info == nil {
		return fmt.Errorf("no ACP adapter known for agent %q", agentID)
	}

	installMu.Lock()
	defer installMu.Unlock()

	return install(info)
}

// UpdateAllInstalled checks all known adapters and updates the ones already installed.
// Runs updates concurrently. Logs progress to stdout.
func UpdateAllInstalled() {
	var wg sync.WaitGroup

	// Special case: opencode uses its own upgrade command
	if _, err := exec.LookPath("opencode"); err == nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("[acpinstall] updating opencode (opencode upgrade)...")
			cmd := exec.Command("opencode", "upgrade")
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("[acpinstall] update opencode failed: %v: %s", err, strings.TrimSpace(string(output)))
			} else {
				log.Printf("[acpinstall] opencode updated")
			}
		}()
	}

	for _, info := range adapters {
		if _, err := exec.LookPath(info.BinaryName); err != nil {
			continue // not installed, skip
		}

		wg.Add(1)
		go func(info AdapterInfo) {
			defer wg.Done()
			log.Printf("[acpinstall] updating %s (%s)...", info.ID, info.Package)
			if err := updateOne(&info); err != nil {
				log.Printf("[acpinstall] update %s failed: %v", info.ID, err)
			} else {
				log.Printf("[acpinstall] %s updated", info.ID)
			}
		}(info)
	}

	wg.Wait()
}

func updateOne(info *AdapterInfo) error {
	installMu.Lock()
	defer installMu.Unlock()

	return install(info)
}

func install(info *AdapterInfo) error {
	switch info.Manager {
	case "npm":
		return npmInstallGlobal(info.Package)
	case "pip":
		return pipInstall(info.Package)
	default:
		return fmt.Errorf("unknown package manager: %s", info.Manager)
	}
}

func npmInstallGlobal(pkg string) error {
	npm := "npm"
	if _, err := exec.LookPath("bun"); err == nil {
		npm = "bun"
	}

	var cmd *exec.Cmd
	if npm == "bun" {
		cmd = exec.Command("bun", "install", "-g", pkg)
	} else {
		cmd = exec.Command(npm, "install", "-g", pkg)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func pipInstall(pkg string) error {
	pip := "pip"
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("pip3"); err == nil {
			pip = "pip3"
		}
	}
	cmd := exec.Command(pip, "install", "--upgrade", pkg)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
