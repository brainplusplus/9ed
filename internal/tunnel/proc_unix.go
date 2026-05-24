//go:build !windows

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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func stopProcess(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
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

	// pgrep lists PIDs of matching processes, one per line.
	out, err := exec.Command("pgrep", "-x", name).Output()
	if err != nil {
		return // pgrep returns exit code 1 when no matches found
	}

	myPid := os.Getpid()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		if pid == myPid {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		log.Printf("tunnel: killing orphan %s process (PID %d)", name, pid)
		_ = proc.Signal(syscall.SIGTERM)
	}
}
