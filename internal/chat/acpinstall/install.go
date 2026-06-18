package acpinstall

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	// installTimeout bounds a single install/update command.
	installTimeout = 120 * time.Second
	// upgradeTimeout bounds opencode's own `opencode upgrade` command.
	upgradeTimeout = 90 * time.Second
)

// AdapterInfo describes how to install/locate one ACP adapter.
type AdapterInfo struct {
	ID         string
	Package    string // legacy npm package (used as fallback when not in registry)
	Manager    string // currently always "npm"
	BinaryName string
}

var adapters = []AdapterInfo{
	{ID: "opencode", Package: "opencode-ai", Manager: "npm", BinaryName: "opencode"},
	{ID: "claude", Package: "@agentclientprotocol/claude-agent-acp", Manager: "npm", BinaryName: "claude-agent-acp"},
	{ID: "codex", Package: "@zed-industries/codex-acp", Manager: "npm", BinaryName: "codex-acp"},
	{ID: "pi", Package: "pi-acp", Manager: "npm", BinaryName: "pi-acp"},
	{ID: "amp", Package: "amp-acp", Manager: "npm", BinaryName: "amp-acp"},
	{ID: "copilot", Package: "github-copilot-cli", Manager: "npm", BinaryName: "github-copilot-cli"},
}

var (
	// adapterLocks holds one mutex per adapter so installs/updates of
	// different adapters can run in parallel, while concurrent operations
	// on the same adapter are serialized.
	adapterLocks   = map[string]*sync.Mutex{}
	adapterLocksMu sync.Mutex

	// updating tracks which adapters are currently being updated, so that
	// spawn attempts can detect and wait for an in-progress update.
	updating   = map[string]bool{}
	updatingMu sync.RWMutex

	// updateOnce ensures UpdateAllInstalled runs at most once per process.
	updateOnce sync.Once
)

// adapterLock returns the lock dedicated to an adapter ID, creating it if needed.
func adapterLock(agentID string) *sync.Mutex {
	adapterLocksMu.Lock()
	defer adapterLocksMu.Unlock()
	mu, ok := adapterLocks[agentID]
	if !ok {
		mu = &sync.Mutex{}
		adapterLocks[agentID] = mu
	}
	return mu
}

// GetAdapterInfo returns the adapter info for an agent ID, or nil.
func GetAdapterInfo(agentID string) *AdapterInfo {
	for i := range adapters {
		if adapters[i].ID == agentID {
			return &adapters[i]
		}
	}
	return nil
}

// IsInstalled returns true if the adapter binary is locatable, either via
// PATH (globally installed) or via the isolated prefix directory managed by 9ed.
func IsInstalled(agentID string) bool {
	info := GetAdapterInfo(agentID)
	if info == nil {
		return false
	}
	if _, err := exec.LookPath(info.BinaryName); err == nil {
		return true
	}
	if bin := IsolatedBinary(agentID); bin != "" {
		return true
	}
	return false
}

// IsUpdating returns true if the given adapter is currently being updated.
func IsUpdating(agentID string) bool {
	updatingMu.RLock()
	defer updatingMu.RUnlock()
	return updating[agentID]
}

func setUpdating(agentID string, v bool) {
	updatingMu.Lock()
	defer updatingMu.Unlock()
	if v {
		updating[agentID] = true
	} else {
		delete(updating, agentID)
	}
}

// WaitForUpdate blocks until the given adapter is no longer being updated,
// or until the context is cancelled. Returns true if the wait completed,
// false if cancelled.
func WaitForUpdate(ctx context.Context, agentID string) bool {
	for IsUpdating(agentID) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return true
}

// EnsureInstalled returns the path to a usable adapter binary, installing it
// to an isolated prefix directory if necessary.
//
// Resolution order:
//  1. If 9ed has previously installed it to ~/.9ed/adapters/npx/{id}, use that.
//  2. If a binary is on PATH (e.g. user installed globally), use that.
//  3. Otherwise, install to the isolated prefix and return the resulting path.
func EnsureInstalled(agentID string) (string, error) {
	info := GetAdapterInfo(agentID)
	if info == nil {
		return "", fmt.Errorf("no ACP adapter known for agent %q", agentID)
	}

	if bin := IsolatedBinary(agentID); bin != "" {
		return bin, nil
	}
	if path, err := exec.LookPath(info.BinaryName); err == nil {
		return path, nil
	}

	mu := adapterLock(agentID)
	mu.Lock()
	defer mu.Unlock()

	// Re-check after acquiring lock — another goroutine may have just installed.
	if bin := IsolatedBinary(agentID); bin != "" {
		return bin, nil
	}
	if path, err := exec.LookPath(info.BinaryName); err == nil {
		return path, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()

	bin, version, err := installIsolated(ctx, info, false)
	if err != nil {
		return "", fmt.Errorf("install %s: %w", info.ID, err)
	}
	log.Printf("[acpinstall] installed %s @ %s -> %s", info.ID, version, bin)
	return bin, nil
}

// Update reinstalls an adapter to its latest registry version (or force-refreshes).
func Update(agentID string) error {
	info := GetAdapterInfo(agentID)
	if info == nil {
		return fmt.Errorf("no ACP adapter known for agent %q", agentID)
	}

	mu := adapterLock(agentID)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()

	_, version, err := installIsolated(ctx, info, true)
	if err != nil {
		return err
	}
	log.Printf("[acpinstall] updated %s -> %s", info.ID, version)
	return nil
}

// RepairIfCorrupt force-reinstalls an ACP adapter when a spawn attempt
// returned a corruption error.
func RepairIfCorrupt(agentID string, err error) (bool, error) {
	if !IsCorruptInstallError(err) {
		return false, nil
	}

	info := GetAdapterInfo(agentID)
	if info == nil {
		return false, fmt.Errorf("no ACP adapter known for agent %q", agentID)
	}

	mu := adapterLock(agentID)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()

	if _, _, ferr := installIsolated(ctx, info, true); ferr != nil {
		return true, ferr
	}
	return true, nil
}

// IsCorruptInstallError detects errors that indicate a partially-replaced or
// locked binary, where a force-reinstall is the right recovery.
func IsCorruptInstallError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "failed to remap this bin") ||
		strings.Contains(msg, "corrupted node_modules") ||
		strings.Contains(msg, "could not create process") ||
		strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "the process cannot access the file") ||
		strings.Contains(msg, "text file busy")
}

// UpdateAllInstalled refreshes the registry and updates all installed adapters
// when their tracked version is older than the registry's published version.
//
// Safe to call from multiple goroutines: only the first call performs work
// (sync.Once). Designed to be invoked once on server startup.
func UpdateAllInstalled() {
	updateOnce.Do(doUpdateAllInstalled)
}

func doUpdateAllInstalled() {
	// Always start from the on-disk cache so we have something even if the
	// network is unreachable.
	LoadRegistryFromCache()

	// Refresh the registry from the network — throttled, so it is a no-op
	// if we recently fetched it.
	ctx, cancel := context.WithTimeout(context.Background(), registryFetchTimeout)
	defer cancel()
	if _, err := RefreshRegistry(ctx, false); err != nil {
		log.Printf("[acpinstall] registry refresh failed: %v (continuing with cache)", err)
	}

	var wg sync.WaitGroup
	for _, info := range adapters {
		// Only update adapters that 9ed has previously installed
		// (isolated prefix) or that the user has globally on PATH.
		hasIsolated := IsolatedBinary(info.ID) != ""
		_, pathErr := exec.LookPath(info.BinaryName)
		hasPath := pathErr == nil
		if !hasIsolated && !hasPath {
			continue
		}

		registryEntry := GetRegistryAgent(registryID(info.ID))
		if registryEntry == nil {
			continue
		}

		// Skip if our isolated install is already at the registry version.
		if hasIsolated && GetInstalledVersion(info.ID) == registryEntry.Version {
			continue
		}

		wg.Add(1)
		go func(info AdapterInfo, target string) {
			defer wg.Done()

			setUpdating(info.ID, true)
			defer setUpdating(info.ID, false)

			mu := adapterLock(info.ID)
			mu.Lock()
			defer mu.Unlock()

			log.Printf("[acpinstall] updating %s -> %s ...", info.ID, target)

			ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
			defer cancel()

			if _, _, err := installIsolated(ctx, &info, true); err != nil {
				log.Printf("[acpinstall] update %s failed: %v", info.ID, err)
			} else {
				log.Printf("[acpinstall] %s updated to %s", info.ID, target)
			}
		}(info, registryEntry.Version)
	}

	wg.Wait()
	log.Printf("[acpinstall] adapter update cycle complete")
}


