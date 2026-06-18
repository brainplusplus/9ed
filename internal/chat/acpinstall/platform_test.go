package acpinstall

import (
	"runtime"
	"strings"
	"testing"
)

func TestCurrentPlatformKey(t *testing.T) {
	key, err := CurrentPlatformKey()
	if err != nil {
		t.Fatalf("CurrentPlatformKey returned error: %v", err)
	}

	if !strings.Contains(key, "-") {
		t.Errorf("expected key in form os-arch, got %q", key)
	}

	// Validate the OS prefix matches runtime.GOOS for known OSes.
	switch runtime.GOOS {
	case "darwin":
		if !strings.HasPrefix(key, "darwin-") {
			t.Errorf("on darwin, expected key prefix 'darwin-', got %q", key)
		}
	case "linux":
		if !strings.HasPrefix(key, "linux-") {
			t.Errorf("on linux, expected key prefix 'linux-', got %q", key)
		}
	case "windows":
		if !strings.HasPrefix(key, "windows-") {
			t.Errorf("on windows, expected key prefix 'windows-', got %q", key)
		}
	}

	// Validate the arch suffix matches runtime.GOARCH for known archs.
	switch runtime.GOARCH {
	case "amd64":
		if !strings.HasSuffix(key, "-x86_64") {
			t.Errorf("on amd64, expected suffix '-x86_64', got %q", key)
		}
	case "arm64":
		if !strings.HasSuffix(key, "-aarch64") {
			t.Errorf("on arm64, expected suffix '-aarch64', got %q", key)
		}
	}
}

func TestIsPlatformSupported(t *testing.T) {
	// Should be true on every CI platform we run on (darwin/linux/windows × amd64/arm64).
	if !IsPlatformSupported() {
		t.Fatalf("expected IsPlatformSupported true on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func TestSupportedPlatformsMatrix(t *testing.T) {
	// All 6 combinations must be present in the supported matrix.
	want := []string{
		"darwin-x86_64",
		"darwin-aarch64",
		"linux-x86_64",
		"linux-aarch64",
		"windows-x86_64",
		"windows-aarch64",
	}
	for _, p := range want {
		if _, ok := supportedPlatforms[p]; !ok {
			t.Errorf("supported matrix missing platform: %s", p)
		}
	}
	if len(supportedPlatforms) != len(want) {
		t.Errorf("supportedPlatforms has %d entries, expected %d", len(supportedPlatforms), len(want))
	}
}

func TestHasBinaryDistribution(t *testing.T) {
	t.Run("nil entry", func(t *testing.T) {
		if hasBinaryDistribution(nil) {
			t.Error("expected false for nil entry")
		}
	})

	t.Run("empty binary map", func(t *testing.T) {
		entry := &RegistryAgent{Distribution: RegistryDistribution{}}
		if hasBinaryDistribution(entry) {
			t.Error("expected false for empty Binary map")
		}
	})

	t.Run("matches current platform", func(t *testing.T) {
		platform, err := CurrentPlatformKey()
		if err != nil {
			t.Skipf("unsupported platform: %v", err)
		}
		entry := &RegistryAgent{
			Distribution: RegistryDistribution{
				Binary: map[string]RegistryBinaryTarget{
					platform: {Archive: "https://example.com/x.zip", Cmd: "./x"},
				},
			},
		}
		if !hasBinaryDistribution(entry) {
			t.Errorf("expected true when entry contains current platform %s", platform)
		}
	})

	t.Run("only other platforms", func(t *testing.T) {
		platform, err := CurrentPlatformKey()
		if err != nil {
			t.Skipf("unsupported platform: %v", err)
		}
		other := "linux-x86_64"
		if platform == other {
			other = "darwin-aarch64"
		}
		entry := &RegistryAgent{
			Distribution: RegistryDistribution{
				Binary: map[string]RegistryBinaryTarget{
					other: {Archive: "https://example.com/x.zip", Cmd: "./x"},
				},
			},
		}
		if hasBinaryDistribution(entry) {
			t.Errorf("expected false when entry only contains %s (current is %s)", other, platform)
		}
	})
}
