package acpinstall

import (
	"os"
	"path/filepath"
	"runtime"
)

// adaptersBaseDir returns the root directory where ACP adapters are installed
// in isolated prefix directories. Default: ~/.9ed/adapters/
//
// Override via the JCE_ADAPTERS_DIR environment variable for testing.
func adaptersBaseDir() (string, error) {
	if override := os.Getenv("JCE_ADAPTERS_DIR"); override != "" {
		return override, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".9ed", "adapters"), nil
}

// npxAdapterDir returns the install directory for an NPX-style adapter.
// Layout: ~/.9ed/adapters/npx/{agent-id}/
// The actual binary will be at: <dir>/node_modules/.bin/{binary}
func npxAdapterDir(agentID string) (string, error) {
	base, err := adaptersBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "npx", sanitizePathComponent(agentID)), nil
}

// binaryAdapterBaseDir returns the base cache directory for binary-style adapters.
// Layout: ~/.9ed/adapters/binary/{agent-id}/v_{version}_{hash}/
func binaryAdapterBaseDir(agentID string) (string, error) {
	base, err := adaptersBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "binary", sanitizePathComponent(agentID)), nil
}

// npxAdapterBinary returns the expected path to the installed binary for
// an NPX-style adapter.
func npxAdapterBinary(agentID, binaryName string) (string, error) {
	dir, err := npxAdapterDir(agentID)
	if err != nil {
		return "", err
	}
	bin := binaryName
	if runtime.GOOS == "windows" {
		// On Windows, npm creates both a .cmd shim and the raw binary.
		// Prefer the .cmd shim for reliable invocation.
		bin = binaryName + ".cmd"
	}
	return filepath.Join(dir, "node_modules", ".bin", bin), nil
}

// stateFilePath returns the path to the JSON file tracking installed versions.
func stateFilePath() (string, error) {
	base, err := adaptersBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "state.json"), nil
}

// registryCachePath returns the path to the cached registry JSON.
func registryCachePath() (string, error) {
	base, err := adaptersBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "registry.json"), nil
}

// sanitizePathComponent makes a string safe for use as a single directory name.
func sanitizePathComponent(input string) string {
	if input == "" {
		return "unknown"
	}
	out := make([]byte, 0, len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}
