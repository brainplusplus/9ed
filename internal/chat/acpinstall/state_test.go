package acpinstall

import (
	"sync"
	"testing"
	"time"
)

// withTempAdaptersDir points the package at a fresh temp dir for one test.
// It also resets the cached state so the test starts clean.
func withTempAdaptersDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("JCE_ADAPTERS_DIR", dir)
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	return dir
}

func TestStateRoundTrip(t *testing.T) {
	withTempAdaptersDir(t)

	if got := GetInstalledVersion("opencode"); got != "" {
		t.Errorf("expected empty version on fresh state, got %q", got)
	}

	want := adapterState{
		Version:     "1.17.7",
		Package:     "binary:https://example.com/opencode-1.17.7.zip",
		InstalledAt: time.Now().UTC(),
		BinaryPath:  "/tmp/opencode",
	}
	if err := setAdapterState("opencode", want); err != nil {
		t.Fatalf("setAdapterState failed: %v", err)
	}

	if got := GetInstalledVersion("opencode"); got != "1.17.7" {
		t.Errorf("expected version 1.17.7, got %q", got)
	}

	// Force re-read from disk by clearing the cache, simulating a fresh process.
	resetStateForTest()
	if got := GetInstalledVersion("opencode"); got != "1.17.7" {
		t.Errorf("after cache reset, expected version 1.17.7 from disk, got %q", got)
	}

	if err := removeAdapterState("opencode"); err != nil {
		t.Fatalf("removeAdapterState failed: %v", err)
	}
	if got := GetInstalledVersion("opencode"); got != "" {
		t.Errorf("expected empty version after remove, got %q", got)
	}
}

// TestStateConcurrentMutate exercises the mutateState path with many goroutines
// writing different adapter IDs simultaneously. With the global stateMu in
// place, all writes must be observable in the final on-disk state.
func TestStateConcurrentMutate(t *testing.T) {
	withTempAdaptersDir(t)

	const writers = 16
	var wg sync.WaitGroup
	wg.Add(writers)

	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := "agent" + itoa(i)
			err := setAdapterState(id, adapterState{
				Version:     "v" + itoa(i),
				Package:     "pkg" + itoa(i),
				InstalledAt: time.Now().UTC(),
				BinaryPath:  "/tmp/" + id,
			})
			if err != nil {
				t.Errorf("setAdapterState %s failed: %v", id, err)
			}
		}()
	}
	wg.Wait()

	// Re-read from disk to make sure the file actually contains every entry.
	resetStateForTest()
	for i := 0; i < writers; i++ {
		id := "agent" + itoa(i)
		got := GetInstalledVersion(id)
		want := "v" + itoa(i)
		if got != want {
			t.Errorf("agent %s: expected %q, got %q", id, want, got)
		}
	}
}

// itoa avoids the strconv import in this small test file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
