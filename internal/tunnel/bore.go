package tunnel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var boreURLPattern = regexp.MustCompile(`bore\.pub:\d+`)

func startBore(port string) (*Tunnel, error) {
	bin, err := findBinary("bore")
	if err != nil {
		return nil, err
	}

	basePort := deriveBorePort(port)
	const maxRetries = 10

	for attempt := 0; attempt < maxRetries; attempt++ {
		remotePort := basePort + attempt
		t, err := tryBore(bin, port, remotePort)
		if err == nil {
			return t, nil
		}
		if !isPortConflict(err) {
			return nil, err
		}
		if attempt == 0 {
			log.Printf("tunnel: port %d occupied, trying alternatives...", remotePort)
		}
		_ = t.Stop()
	}

	return nil, fmt.Errorf("bore: could not find available port after %d attempts starting from %d", maxRetries, basePort)
}

func tryBore(bin, localPort string, remotePort int) (*Tunnel, error) {
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
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("bore start: %w", err)
	}

	t := newTunnel("bore", cmd, cancel)

	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	urlCh := make(chan string, 1)
	go func() {
		defer close(urlCh)
		rawCh := make(chan string, 1)
		go scanLines(stdout, "bore", boreURLPattern, rawCh)
		if url := <-rawCh; url != "" {
			urlCh <- "http://" + url
		}
	}()

	if err := waitForURL(urlCh, 30*time.Second, t, "bore"); err != nil {
		return t, err
	}

	go t.waitAndReap()
	return t, nil
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
