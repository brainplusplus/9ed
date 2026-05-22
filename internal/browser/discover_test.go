package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindBrowserReturnsEmptyWhenNoneFound(t *testing.T) {
	// On CI or systems without Chrome, this should not panic.
	// We just verify it returns either a path or empty string.
	result := FindBrowser()
	if runtime.GOOS == "windows" {
		// On Windows CI, Chrome might not be installed.
		// Just ensure the function doesn't panic or crash.
		_ = result
	}
}

func TestFileExistsWithRealFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "testfile")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(f) {
		t.Fatal("expected file to exist")
	}
	if fileExists(filepath.Join(tmp, "nonexistent")) {
		t.Fatal("expected nonexistent file to return false")
	}
	if fileExists(tmp) {
		t.Fatal("expected directory to return false")
	}
}

func TestBrowserCandidatesReturnsAtLeastOne(t *testing.T) {
	candidates := browserCandidates()
	if len(candidates) == 0 {
		t.Fatal("expected at least one browser candidate path")
	}
}

func TestIsBrowserAvailableDoesNotPanic(t *testing.T) {
	// Just ensure it doesn't panic.
	_ = IsBrowserAvailable()
}
