//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func killProcessTree(pid int) error {
	// Prefer process-group kill when possible.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
		return nil
	}

	descendants := collectDescendants(pid)
	var firstErr error
	for i := len(descendants) - 1; i >= 0; i-- {
		if err := killSinglePID(descendants[i]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := killSinglePID(pid); err != nil {
		if firstErr != nil {
			return fmt.Errorf("kill parent failed: %w (child kill error: %v)", err, firstErr)
		}
		return err
	}
	return firstErr
}

func killSinglePID(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func collectDescendants(rootPID int) []int {
	out := make([]int, 0, 8)
	queue := []int{rootPID}
	seen := map[int]struct{}{rootPID: {}}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		children := listChildPIDs(parent)
		for _, child := range children {
			if child <= 0 {
				continue
			}
			if _, exists := seen[child]; exists {
				continue
			}
			seen[child] = struct{}{}
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}

func listChildPIDs(parentPID int) []int {
	if children, err := listChildPIDsWithPgrep(parentPID); err == nil {
		return children
	}
	if children, err := listChildPIDsWithPS(parentPID, "-eo", "pid=,ppid="); err == nil {
		return children
	}
	if children, err := listChildPIDsWithPS(parentPID, "-axo", "pid=,ppid="); err == nil {
		return children
	}
	return nil
}

func listChildPIDsWithPgrep(parentPID int) ([]int, error) {
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(parentPID)).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	children := make([]int, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		children = append(children, pid)
	}
	return children, nil
}

func listChildPIDsWithPS(parentPID int, args ...string) ([]int, error) {
	out, err := exec.Command("ps", args...).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	children := make([]int, 0, 4)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, errPID := strconv.Atoi(fields[0])
		ppid, errPPID := strconv.Atoi(fields[1])
		if errPID != nil || errPPID != nil {
			continue
		}
		if ppid == parentPID {
			children = append(children, pid)
		}
	}
	return children, nil
}
