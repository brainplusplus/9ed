//go:build windows

package tunnel

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := killProcessTree(cmd.Process.Pid); err == nil {
		return
	}
	_ = cmd.Process.Kill()
}

func killProcessTree(pid int) error {
	kill := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	if err := kill.Run(); err == nil {
		return nil
	}
	p, findErr := os.FindProcess(pid)
	if findErr != nil {
		return findErr
	}
	return p.Kill()
}
