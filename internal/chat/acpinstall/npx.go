package acpinstall

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// registryID maps local 9ed agent IDs to ACP registry IDs.
// 9ed historically used short IDs ("claude", "codex"), while the ACP
// registry uses package-like IDs ("claude-acp", "codex-acp").
func registryID(agentID string) string {
	switch agentID {
	case "claude":
		return "claude-acp"
	case "codex":
		return "codex-acp"
	case "copilot":
		return "github-copilot-cli"
	case "amp":
		return "amp-acp"
	case "pi":
		return "pi-acp"
	default:
		return agentID
	}
}

// localID maps ACP registry IDs back to 9ed local agent IDs.
func localID(registryID string) string {
	switch registryID {
	case "claude-acp":
		return "claude"
	case "codex-acp":
		return "codex"
	case "github-copilot-cli":
		return "copilot"
	case "amp-acp":
		return "amp"
	case "pi-acp":
		return "pi"
	default:
		return registryID
	}
}

// npxPackageFor returns the package spec to install for an adapter.
// Prefer the ACP registry package/version when available; fall back to the
// hard-coded package name from AdapterInfo.
func npxPackageFor(info *AdapterInfo) (pkg string, version string, registryEntry *RegistryAgent) {
	entry := GetRegistryAgent(registryID(info.ID))
	if entry != nil && entry.Distribution.NPX != nil && strings.TrimSpace(entry.Distribution.NPX.Package) != "" {
		return entry.Distribution.NPX.Package, entry.Version, entry
	}
	return info.Package, "", nil
}

// installNPMToPrefix installs an npm package into an isolated prefix directory.
// Unlike npm install -g, this does not mutate the user's global node_modules or PATH.
func installNPMToPrefix(ctx context.Context, info *AdapterInfo, pkg string) (string, error) {
	dir, err := npxAdapterDir(info.ID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	manager := "npm"
	if _, err := exec.LookPath("bun"); err == nil {
		// Use npm for isolated prefix installs even if bun is available. Bun's
		// global/bin remapping is the source of the intermittent Windows failures;
		// npm prefix installs are slower but more predictable for shim generation.
		manager = "npm"
	}
	if _, err := exec.LookPath(manager); err != nil {
		return "", fmt.Errorf("%s not found in PATH", manager)
	}

	installCtx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	cmd := exec.CommandContext(installCtx, manager, "install", "--prefix", dir, pkg)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if installCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("install %s timed out after %v", pkg, installTimeout)
		}
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}

	bin, err := npxAdapterBinary(info.ID, info.BinaryName)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(bin); err != nil {
		// On Windows, some packages may only create .ps1 or raw .exe shims.
		// Fall back to scanning node_modules/.bin for a matching prefix.
		fallback, scanErr := findPrefixBinary(dir, info.BinaryName)
		if scanErr != nil {
			return "", fmt.Errorf("installed %s but binary %q not found: %w", pkg, info.BinaryName, err)
		}
		bin = fallback
	}
	return bin, nil
}

func findPrefixBinary(prefixDir, binaryName string) (string, error) {
	binDir := filepath.Join(prefixDir, "node_modules", ".bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return "", err
	}
	wanted := strings.ToLower(binaryName)
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if name == wanted || strings.TrimSuffix(name, ".cmd") == wanted || strings.TrimSuffix(name, ".exe") == wanted || strings.TrimSuffix(name, ".ps1") == wanted {
			return filepath.Join(binDir, entry.Name()), nil
		}
	}
	return "", os.ErrNotExist
}

// IsolatedBinary returns the tracked isolated-prefix binary path for an adapter,
// if it exists on disk. Looks at both binary-archive installs and npm prefix installs.
func IsolatedBinary(agentID string) string {
	info := GetAdapterInfo(agentID)
	if info == nil {
		return ""
	}
	state := loadState()
	if entry, ok := state.Adapters[agentID]; ok && entry.BinaryPath != "" {
		if _, err := os.Stat(entry.BinaryPath); err == nil {
			return entry.BinaryPath
		}
	}
	bin, err := npxAdapterBinary(agentID, info.BinaryName)
	if err == nil {
		if _, statErr := os.Stat(bin); statErr == nil {
			return bin
		}
	}
	return ""
}

// IsolatedBinarySpec returns the path + registry-derived args/env for an
// adapter when 9ed has a managed install on disk.
//
// For binary-distribution agents (e.g. opencode 1.17.7), the registry
// supplies the args (such as ["acp"]) and env vars to use. For NPX-installed
// adapters, args/env are nil and callers should use their static fallback.
func IsolatedBinarySpec(agentID string) (path string, args []string, env map[string]string) {
	bin := IsolatedBinary(agentID)
	if bin == "" {
		return "", nil, nil
	}

	entry := GetRegistryAgent(registryID(agentID))
	if entry == nil {
		return bin, nil, nil
	}

	platform, err := currentPlatformKey()
	if err != nil {
		return bin, nil, nil
	}
	if target, ok := entry.Distribution.Binary[platform]; ok {
		// Only return registry-supplied args/env when the resolved binary
		// actually came from the binary distribution path.
		if expected, perr := versionedBinaryDir(agentID, entry.Version, target.Archive); perr == nil {
			if strings.HasPrefix(bin, expected) {
				return bin, append([]string(nil), target.Args...), copyEnv(target.Env)
			}
		}
	}
	return bin, nil, nil
}

func copyEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func installIsolated(ctx context.Context, info *AdapterInfo, force bool) (string, string, error) {
	entry := GetRegistryAgent(registryID(info.ID))

	// Match Zed's preference order:
	//  1. Binary distribution for the current platform (most predictable).
	//  2. NPX / npm package when no compatible binary exists.
	//  3. Legacy hard-coded package as a final fallback.
	if hasBinaryDistribution(entry) {
		bin, _, _, version, err := installBinaryDistribution(ctx, info, entry, force)
		return bin, version, err
	}

	pkg, version, _ := npxPackageFor(info)
	if strings.TrimSpace(pkg) == "" {
		// No npx package and no binary distribution for this platform — surface
		// a clear, actionable error rather than failing later in npm.
		if entry != nil && len(entry.Distribution.Binary) > 0 {
			platform, _ := currentPlatformKey()
			return "", "", fmt.Errorf("%s has no installer for platform %s; available binaries: %d", info.ID, platform, len(entry.Distribution.Binary))
		}
		return "", "", fmt.Errorf("no installable distribution for %s on this platform", info.ID)
	}

	if !force && version != "" && GetInstalledVersion(info.ID) == version {
		if bin := IsolatedBinary(info.ID); bin != "" {
			return bin, version, nil
		}
	}

	bin, err := installNPMToPrefix(ctx, info, pkg)
	if err != nil {
		return "", "", err
	}
	if version == "" {
		version = time.Now().UTC().Format(time.RFC3339)
	}
	if err := setAdapterState(info.ID, adapterState{
		Version:     version,
		Package:     pkg,
		InstalledAt: time.Now().UTC(),
		BinaryPath:  bin,
	}); err != nil {
		return "", "", err
	}
	return bin, version, nil
}
