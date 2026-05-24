//go:build windows

package tunnel

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func stopProcess(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}

// killOrphanProcesses kills any running cloudflared or bore processes
// that may have been left behind by a previous server instance.
func killOrphanProcesses(engine string) {
	var name string
	switch engine {
	case "cloudflare":
		name = "cloudflared"
	case "bore":
		name = "bore"
	default:
		return
	}

	// tasklist outputs CSV like: "cloudflared.exe","1234","Console","1","12,345 K"
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name+".exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "INFO:") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		pidStr := strings.Trim(fields[1], `" `)
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if pid == os.Getpid() {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		log.Printf("tunnel: killing orphan %s process (PID %d)", name, pid)
		_ = proc.Kill()
	}
}
