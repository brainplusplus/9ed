//go:build !windows

package tunnel

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func machineID() (string, error) {
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err != nil {
			return "", fmt.Errorf("ioreg: %w", err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "IOPlatformUUID") {
				parts := strings.Split(line, `"`)
				if len(parts) >= 4 {
					return parts[3], nil
				}
			}
		}
	}
	return "", fmt.Errorf("machine-id not found")
}
