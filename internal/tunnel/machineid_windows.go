//go:build windows

package tunnel

import (
	"fmt"
	"os/exec"
	"strings"
)

func machineID() (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`[Convert]::ToBase64String([System.Text.Encoding]::Unicode.GetBytes((Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Cryptography').MachineGuid))`,
	).Output()
	if err != nil {
		return "", fmt.Errorf("read machine guid: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
