package acpinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// adapterState holds the installed version + install metadata for one adapter.
type adapterState struct {
	Version     string    `json:"version"`
	Package     string    `json:"package,omitempty"`
	InstalledAt time.Time `json:"installedAt"`
	BinaryPath  string    `json:"binaryPath,omitempty"`
}

// stateFile holds the persisted state of all installed adapters.
type stateFile struct {
	Adapters map[string]adapterState `json:"adapters"`
}

var (
	// stateMu guards the in-memory cache AND serializes read-modify-write
	// cycles so concurrent updates on different adapters cannot lose entries.
	stateMu       sync.Mutex
	stateCache    *stateFile
	stateLoadOnce sync.Once
)

// loadState returns a shallow copy of the cached state. Caller must not mutate
// the returned struct without going through mutateState.
func loadState() *stateFile {
	stateMu.Lock()
	defer stateMu.Unlock()
	return loadStateLocked()
}

// loadStateLocked must be called with stateMu held.
func loadStateLocked() *stateFile {
	stateLoadOnce.Do(func() {
		stateCache = readStateFromDisk()
	})
	if stateCache == nil {
		stateCache = &stateFile{Adapters: map[string]adapterState{}}
	}
	cp := &stateFile{Adapters: make(map[string]adapterState, len(stateCache.Adapters))}
	for k, v := range stateCache.Adapters {
		cp.Adapters[k] = v
	}
	return cp
}

func readStateFromDisk() *stateFile {
	path, err := stateFilePath()
	if err != nil {
		return &stateFile{Adapters: map[string]adapterState{}}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &stateFile{Adapters: map[string]adapterState{}}
	}
	var s stateFile
	if err := json.Unmarshal(data, &s); err != nil {
		return &stateFile{Adapters: map[string]adapterState{}}
	}
	if s.Adapters == nil {
		s.Adapters = map[string]adapterState{}
	}
	return &s
}

// mutateState applies fn to a snapshot of the state under the lock, then
// persists the result atomically. This prevents read-modify-write races
// between concurrent goroutines updating different adapters.
func mutateState(fn func(*stateFile)) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s := loadStateLocked()
	fn(s)
	return saveStateLocked(s)
}

// saveStateLocked must be called with stateMu held.
func saveStateLocked(s *stateFile) error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: write to temp file, then rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename state file: %w", err)
	}

	stateCache = s
	return nil
}

// GetInstalledVersion returns the version of an adapter installed via the
// isolated prefix mechanism, or empty string if not tracked.
func GetInstalledVersion(agentID string) string {
	s := loadState()
	if entry, ok := s.Adapters[agentID]; ok {
		return entry.Version
	}
	return ""
}

func setAdapterState(agentID string, entry adapterState) error {
	return mutateState(func(s *stateFile) {
		s.Adapters[agentID] = entry
	})
}

func removeAdapterState(agentID string) error {
	return mutateState(func(s *stateFile) {
		delete(s.Adapters, agentID)
	})
}

// resetStateForTest clears the cached state and any once-init flags so that
// tests can run with a fresh JCE_ADAPTERS_DIR. Test-only.
func resetStateForTest() {
	stateMu.Lock()
	defer stateMu.Unlock()
	stateCache = &stateFile{Adapters: map[string]adapterState{}}
	stateLoadOnce = sync.Once{}
}
