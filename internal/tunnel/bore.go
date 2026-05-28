package tunnel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var boreURLPattern = regexp.MustCompile(`bore\.pub:\d+`)

// startBoreProc starts a single bore subprocess, trying multiple ports if needed.
func startBoreProc(port string) (*tunnelProc, error) {
	bin, err := findBinary("bore")
	if err != nil {
		return nil, err
	}
	prefix := tunnelLogPrefix("bore", port)

	basePort := deriveBorePort(port)
	const maxRetries = 10

	for attempt := 0; attempt < maxRetries; attempt++ {
		remotePort := basePort + attempt
		proc, err := tryBoreProc(bin, port, remotePort, prefix)
		if err == nil {
			return proc, nil
		}
		if !isPortConflict(err) {
			return nil, err
		}
		if attempt == 0 {
			log.Printf("tunnel: port %d occupied, trying alternatives...", remotePort)
		}
		if proc != nil {
			proc.stop(5 * time.Second)
		}
	}

	return nil, fmt.Errorf("bore: could not find available port after %d attempts starting from %d", maxRetries, basePort)
}

func tryBoreProc(bin, localPort string, remotePort int, prefix string) (*tunnelProc, error) {
	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		"local", localPort,
		"--to", "bore.pub",
		"--port", strconv.Itoa(remotePort),
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	setSysProcAttr(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("bore stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("bore stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("bore start: %w", err)
	}

	proc := newTunnelProc(cmd, cancel)

	go proc.waitAndReap()
	output := newOutputRecorder(20)

	urlCh := make(chan string, 1)
	go func() {
		defer close(urlCh)
		rawCh := make(chan string, 1)
		go scanLines(stdout, prefix, boreURLPattern, rawCh)
		if url := <-rawCh; url != "" {
			urlCh <- "http://" + url
		}
	}()
	stderrCh := make(chan string, 1)
	go scanLinesWithRecorder(stderr, prefix, nil, output, stderrCh)

	url, err := recvProcURL(proc, urlCh, 30*time.Second, output)
	if err != nil {
		proc.stop(5 * time.Second)
		return proc, fmt.Errorf("bore local %s remote %d: %w", localPort, remotePort, err)
	}

	proc.url = url
	return proc, nil
}

func isPortConflict(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return (strings.Contains(s, "port") && strings.Contains(s, "use")) ||
		strings.Contains(s, "already") ||
		strings.Contains(s, "refused") ||
		strings.Contains(s, "conflict")
}

func deriveBorePort(localPort string) int {
	mid, err := machineID()
	if err != nil {
		mid = "fallback"
	}
	raw := fmt.Sprintf("%s:%s", mid, localPort)
	h := sha256.Sum256([]byte(raw))
	val := uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
	return int(val%50000) + 10000
}
