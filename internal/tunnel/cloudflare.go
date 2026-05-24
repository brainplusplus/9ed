package tunnel

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// startCloudflareProc starts a single cloudflared quick tunnel subprocess.
func startCloudflareProc(port string) (*tunnelProc, error) {
	bin, err := findBinary("cloudflared")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		"tunnel",
		"--no-autoupdate",
		"--url", fmt.Sprintf("http://localhost:%s", port),
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	setSysProcAttr(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cloudflared stderr pipe: %w", err)
	}
	stdout, _ := cmd.StdoutPipe()

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("cloudflared start: %w", err)
	}

	proc := newTunnelProc(cmd, cancel)

	go proc.waitAndReap()
	go func() { _, _ = io.Copy(io.Discard, stdout) }()

	urlCh := make(chan string, 1)
	go scanLines(stderr, "cloudflared", cloudflaredURLPattern, urlCh)

	url, err := recvURL(urlCh, 45*time.Second)
	if err != nil {
		stopProcess(cmd)
		cancel()
		return nil, fmt.Errorf("cloudflared: %w", err)
	}

	proc.url = url
	return proc, nil
}
