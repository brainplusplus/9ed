package tunnel

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/brainplusplus/9ed/internal/bininstall"
)

// startCloudflareProc starts a single cloudflared quick tunnel subprocess.
func startCloudflareProc(port string) (*tunnelProc, error) {
	bin, err := bininstall.FindBinary("cloudflared")
	if err != nil {
		return nil, err
	}
	prefix := tunnelLogPrefix("cloudflared", port)

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

	output := newOutputRecorder(20)
	urlCh := make(chan string, 1)
	go scanLinesWithRecorder(stderr, prefix, cloudflaredURLPattern, output, urlCh)

	url, err := recvProcURL(proc, urlCh, 45*time.Second, output)
	if err != nil {
		proc.stop(5 * time.Second)
		return proc, fmt.Errorf("cloudflared: %w", err)
	}

	proc.url = url
	return proc, nil
}
