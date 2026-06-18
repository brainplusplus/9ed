package acpinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// registryURL is the ACP registry endpoint maintained by Zed Industries.
	// Same registry used by Zed editor — see crates/project/src/agent_registry_store.rs
	registryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

	// registryFetchTimeout bounds the registry HTTP request.
	registryFetchTimeout = 30 * time.Second

	// registryRefreshThrottle prevents hitting the network too frequently.
	// Same as Zed: max 1 refresh per hour.
	registryRefreshThrottle = 1 * time.Hour
)

// RegistryAgent describes one agent in the ACP registry.
type RegistryAgent struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	Repository   string                 `json:"repository,omitempty"`
	Website      string                 `json:"website,omitempty"`
	Icon         string                 `json:"icon,omitempty"`
	Distribution RegistryDistribution   `json:"distribution"`
}

// RegistryDistribution holds the available distribution methods for an agent.
type RegistryDistribution struct {
	NPX    *RegistryNpx                       `json:"npx,omitempty"`
	Binary map[string]RegistryBinaryTarget    `json:"binary,omitempty"`
	UVX    *RegistryUvx                       `json:"uvx,omitempty"`
}

// RegistryNpx describes the npm/npx distribution for an agent.
type RegistryNpx struct {
	Package string            `json:"package"` // e.g. "opencode-ai@1.17.7"
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// RegistryUvx describes the uvx (Python) distribution for an agent.
type RegistryUvx struct {
	Package string            `json:"package"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// RegistryBinaryTarget describes a platform-specific binary archive.
type RegistryBinaryTarget struct {
	Archive string            `json:"archive"`
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// registryIndex is the top-level structure of registry.json.
type registryIndex struct {
	Version string          `json:"version"`
	Agents  []RegistryAgent `json:"agents"`
}

var (
	registryMu          sync.RWMutex
	registryCache       []RegistryAgent
	registryLastFetch   time.Time
	registryFetchInFlight sync.Mutex
)

// GetRegistryAgent returns the registry entry for an agent ID, or nil if not found.
// This consults the in-memory cache; call RefreshRegistry to update.
func GetRegistryAgent(id string) *RegistryAgent {
	registryMu.RLock()
	defer registryMu.RUnlock()
	for i := range registryCache {
		if registryCache[i].ID == id {
			return &registryCache[i]
		}
	}
	return nil
}

// LoadRegistryFromCache loads the registry from disk into memory if available.
// Safe to call at startup.
func LoadRegistryFromCache() {
	path, err := registryCachePath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var idx registryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return
	}

	registryMu.Lock()
	registryCache = idx.Agents
	registryMu.Unlock()
}

// RefreshRegistry fetches the latest registry from the network, throttled to
// once per registryRefreshThrottle interval.
//
// Returns true if a refresh was performed (or attempted), false if skipped
// because of throttling.
func RefreshRegistry(ctx context.Context, force bool) (bool, error) {
	if !force {
		registryMu.RLock()
		last := registryLastFetch
		registryMu.RUnlock()
		if !last.IsZero() && time.Since(last) < registryRefreshThrottle {
			return false, nil
		}
	}

	// Serialize concurrent fetches.
	registryFetchInFlight.Lock()
	defer registryFetchInFlight.Unlock()

	// Re-check after acquiring lock — another goroutine may have just refreshed.
	if !force {
		registryMu.RLock()
		last := registryLastFetch
		registryMu.RUnlock()
		if !last.IsZero() && time.Since(last) < registryRefreshThrottle {
			return false, nil
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, registryFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, registryURL, nil)
	if err != nil {
		return true, fmt.Errorf("build registry request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "9ed/1.0 (+https://github.com/brainplusplus/9ed)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true, fmt.Errorf("fetch registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return true, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return true, fmt.Errorf("read registry body: %w", err)
	}

	var idx registryIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return true, fmt.Errorf("parse registry: %w", err)
	}

	// Persist to disk for next startup.
	cachePath, err := registryCachePath()
	if err == nil {
		if mkErr := os.MkdirAll(filepath.Dir(cachePath), 0o755); mkErr == nil {
			tmp := cachePath + ".tmp"
			if writeErr := os.WriteFile(tmp, body, 0o644); writeErr == nil {
				_ = os.Rename(tmp, cachePath)
			}
		}
	}

	registryMu.Lock()
	registryCache = idx.Agents
	registryLastFetch = time.Now()
	registryMu.Unlock()

	return true, nil
}
