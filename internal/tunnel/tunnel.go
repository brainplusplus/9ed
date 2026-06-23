package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/brainplusplus/9ed/internal/debug"
)

var cloudflaredURLPattern = regexp.MustCompile(`https://[a-z0-9][a-z0-9-]*\.trycloudflare\.com`)

// Tunnel manages a tunnel subprocess with automatic restart on failure.
type Tunnel struct {
	engine      string
	port        string
	url         string
	urlMu       sync.RWMutex
	stopCh      chan struct{}
	stopped     atomic.Bool
	currentProc atomic.Pointer[tunnelProc]
	pidFile     string
}

// Start launches a tunnel using the specified engine ("bore" or "cloudflare") proxying to localhost:port.
// The returned Tunnel automatically restarts the underlying subprocess if it exits unexpectedly.
// Any orphan tunnel process from a previous server instance on the same port is killed first.
func Start(engine, port string) (*Tunnel, error) {
	pidFile := tunnelPIDFile(port)
	killOrphanByPIDFile(pidFile)

	t := &Tunnel{
		engine:  engine,
		port:    port,
		stopCh:  make(chan struct{}),
		pidFile: pidFile,
	}

	if err := t.launch(); err != nil {
		return nil, err
	}

	go t.watchdog()

	return t, nil
}

// URL returns the current public tunnel URL (may change after automatic restarts).
func (t *Tunnel) URL() string {
	t.urlMu.RLock()
	defer t.urlMu.RUnlock()
	return t.url
}

func (t *Tunnel) setURL(u string) {
	t.urlMu.Lock()
	t.url = u
	t.urlMu.Unlock()
}

// Engine returns the tunnel engine name.
func (t *Tunnel) Engine() string {
	return t.engine
}

// Stop gracefully shuts down the tunnel process and stops the restart watchdog.
func (t *Tunnel) Stop() error {
	if !t.stopped.CompareAndSwap(false, true) {
		return nil // already stopped
	}
	close(t.stopCh)
	if proc := t.currentProc.Load(); proc != nil {
		proc.stop(5 * time.Second)
	}
	os.Remove(t.pidFile)
	return nil
}

// launch starts a single tunnel subprocess and waits for its URL.
func (t *Tunnel) launch() error {
	var proc *tunnelProc
	var err error

	switch t.engine {
	case "bore":
		proc, err = startBoreProc(t.port)
	case "cloudflare":
		proc, err = startCloudflareProc(t.port)
	default:
		return fmt.Errorf("unknown tunnel engine: %s", t.engine)
	}
	if err != nil {
		return err
	}

	t.setURL(proc.url)
	log.Printf("tunnel active: %s (%s)", proc.url, t.engine)

	// Store proc so watchdog can wait on it.
	t.currentProc.Store(proc)

	// Write tunnel subprocess PID to file for orphan cleanup on next start.
	if proc.cmd.Process != nil {
		writePIDFile(t.pidFile, proc.cmd.Process.Pid)
	}

	return nil
}

// watchdog monitors the tunnel subprocess and restarts it on unexpected exit.
func (t *Tunnel) watchdog() {
	backoff := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

	for {
		proc := t.currentProc.Load()
		if proc == nil {
			return
		}

		// Wait for subprocess to exit.
		select {
		case <-proc.done:
		case <-t.stopCh:
			return
		}

		if t.stopped.Load() {
			return
		}

		log.Printf("tunnel: %s subprocess exited, restarting...", t.engine)

		// Restart with backoff.
		var lastErr error
		restarted := false
		for i, delay := range backoff {
			select {
			case <-t.stopCh:
				return
			case <-time.After(delay):
			}

			if err := t.launch(); err != nil {
				lastErr = err
				log.Printf("tunnel: restart attempt %d failed: %v", i+1, err)
				continue
			}
			restarted = true
			break
		}

		if !restarted {
			log.Printf("tunnel: gave up restarting after all attempts: %v", lastErr)
			return
		}
	}
}

// tunnelProc represents a single running tunnel subprocess.
type tunnelProc struct {
	cmd    *exec.Cmd
	url    string
	cancel context.CancelFunc
	done   chan struct{}
	errMu  sync.Mutex
	err    error
}

func newTunnelProc(cmd *exec.Cmd, cancel context.CancelFunc) *tunnelProc {
	return &tunnelProc{
		cmd:    cmd,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

func (p *tunnelProc) waitAndReap() {
	err := p.cmd.Wait()
	p.errMu.Lock()
	p.err = err
	p.errMu.Unlock()
	close(p.done)
}

func (p *tunnelProc) waitError() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

func (p *tunnelProc) stop(timeout time.Duration) {
	p.cancel()
	stopProcess(p.cmd)
	select {
	case <-p.done:
	case <-time.After(timeout):
		if p.cmd != nil && p.cmd.Process != nil {
			_ = killProcessTree(p.cmd.Process.Pid)
		}
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
		}
	}
}

func scanLines(r io.Reader, prefix string, pattern *regexp.Regexp, urlCh chan<- string) {
	scanLinesWithRecorder(r, prefix, pattern, nil, urlCh)
}

func scanLinesWithRecorder(r io.Reader, prefix string, pattern *regexp.Regexp, recorder *outputRecorder, urlCh chan<- string) {
	defer close(urlCh)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	sentURL := false
	for scanner.Scan() {
		line := scanner.Text()
		debug.Printf("[%s] %s", prefix, line)
		if recorder != nil {
			recorder.add(line)
		}
		if pattern != nil && !sentURL {
			if match := pattern.FindString(line); match != "" {
				urlCh <- match
				sentURL = true
			}
		}
	}
}

type outputRecorder struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newOutputRecorder(max int) *outputRecorder {
	return &outputRecorder{max: max}
}

func (r *outputRecorder) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.max <= 0 {
		return
	}
	if len(r.lines) >= r.max {
		copy(r.lines, r.lines[1:])
		r.lines[len(r.lines)-1] = line
		return
	}
	r.lines = append(r.lines, line)
}

func (r *outputRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
}

// recvURL waits for a tunnel URL from the channel with a timeout.
func recvURL(urlCh <-chan string, timeout time.Duration) (string, error) {
	select {
	case url := <-urlCh:
		if url == "" {
			return "", fmt.Errorf("exited before publishing tunnel URL")
		}
		return url, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out waiting for tunnel URL (%s)", timeout)
	}
}

func recvProcURL(proc *tunnelProc, urlCh <-chan string, timeout time.Duration, output *outputRecorder) (string, error) {
	select {
	case url := <-urlCh:
		if url == "" {
			return "", tunnelExitError(proc, "exited before publishing tunnel URL", output)
		}
		return url, nil
	case <-proc.done:
		return "", tunnelExitError(proc, "exited before publishing tunnel URL", output)
	case <-time.After(timeout):
		return "", tunnelExitError(proc, fmt.Sprintf("timed out waiting for tunnel URL (%s)", timeout), output)
	}
}

func tunnelExitError(proc *tunnelProc, msg string, output *outputRecorder) error {
	details := ""
	if output != nil {
		details = strings.TrimSpace(output.String())
	}
	if err := proc.waitError(); err != nil {
		if details != "" {
			return fmt.Errorf("%s: %w: %s", msg, err, details)
		}
		return fmt.Errorf("%s: %w", msg, err)
	}
	if details != "" {
		return fmt.Errorf("%s: %s", msg, details)
	}
	return fmt.Errorf("%s", msg)
}

// tunnelPIDFile returns the path to the PID file for a given port.
// File lives in ~/.9ed/tunnel-<port>.pid
func tunnelPIDFile(port string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".9ed", "tunnel-"+port+".pid")
}

// writePIDFile atomically writes a tunnel subprocess PID to the file.
func writePIDFile(path string, pid int) {
	if path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0o644)
}

// killOrphanByPIDFile reads a PID file, checks if that process is still alive,
// and kills it if so. Safe across OS — uses os.FindProcess + Signal/Kill.
func killOrphanByPIDFile(path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return // no PID file — nothing to clean up
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		// On Unix FindProcess always succeeds; on Windows it may fail for dead PIDs.
		os.Remove(path)
		return
	}

	// Check if process is actually alive.
	// On Unix: Signal(0) succeeds if process exists.
	if runtime.GOOS == "windows" {
		if err := killProcessTree(pid); err != nil {
			// Process already dead — just clean up PID file.
			os.Remove(path)
			return
		}
		log.Printf("tunnel: killed orphan process (PID %d) from previous run", pid)
	} else {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process already dead.
			os.Remove(path)
			return
		}
		log.Printf("tunnel: killing orphan process (PID %d) from previous run", pid)
		_ = killProcessTree(pid)
	}

	os.Remove(path)
}

func tunnelLogPrefix(engine, port string) string {
	engine = strings.TrimSpace(engine)
	port = strings.TrimSpace(port)
	if engine == "" {
		engine = "tunnel"
	}
	if port == "" {
		return engine
	}
	return fmt.Sprintf("%s:%s", engine, port)
}
