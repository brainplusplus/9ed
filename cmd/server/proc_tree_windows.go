//go:build windows

package main

import (
	"os"
	"os/exec"
	"strconv"
)

func killProcessTree(pid int) error {
	kill := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	if err := kill.Run(); err == nil {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
