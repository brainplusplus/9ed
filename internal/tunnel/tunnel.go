package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/brainplusplus/9ed/internal/debug"
)

var cloudflaredURLPattern = regexp.MustCompile(`https://[a-z0-9][a-z0-9-]*\.trycloudflare\.com`)

// Tunnel manages a tunnel subprocess.
type Tunnel struct {
	cmd    *exec.Cmd
	url    string
	cancel context.CancelFunc
	done   chan struct{}
	engine string
}

// Start launches a tunnel using the specified engine ("bore" or "cloudflare") proxying to localhost:port.
func Start(engine, port string) (*Tunnel, error) {
	switch engine {
	case "bore":
		return startBore(port)
	case "cloudflare":
		return startCloudflare(port)
	default:
		return nil, fmt.Errorf("unknown tunnel engine: %s", engine)
	}
}

// URL returns the public tunnel URL.
func (t *Tunnel) URL() string {
	return t.url
}

// Engine returns the tunnel engine name.
func (t *Tunnel) Engine() string {
	return t.engine
}

// Stop gracefully shuts down the tunnel process.
func (t *Tunnel) Stop() error {
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	t.cancel()

	stopProcess(t.cmd)

	select {
	case <-t.done:
	case <-time.After(10 * time.Second):
		_ = t.cmd.Process.Kill()
		select {
		case <-t.done:
		case <-time.After(2 * time.Second):
		}
	}

	return nil
}

func newTunnel(engine string, cmd *exec.Cmd, cancel context.CancelFunc) *Tunnel {
	return &Tunnel{
		cmd:    cmd,
		cancel: cancel,
		done:   make(chan struct{}),
		engine: engine,
	}
}

func (t *Tunnel) waitAndReap() {
	_ = t.cmd.Wait()
	close(t.done)
}

func scanLines(r io.Reader, prefix string, pattern *regexp.Regexp, urlCh chan<- string) {
	defer close(urlCh)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		debug.Printf("[%s] %s", prefix, line)
		if pattern != nil {
			if match := pattern.FindString(line); match != "" {
				urlCh <- match
				return
			}
		}
	}
}

func waitForURL(urlCh <-chan string, timeout time.Duration, t *Tunnel, engine string) error {
	select {
	case url := <-urlCh:
		if url == "" {
			_ = t.Stop()
			return fmt.Errorf("%s exited before publishing tunnel URL", engine)
		}
		t.url = url
		log.Printf("tunnel active: %s", url)
	case <-time.After(timeout):
		_ = t.Stop()
		return fmt.Errorf("timed out waiting for %s tunnel URL", engine)
	}
	return nil
}

func findBinary(name string) (string, error) {
	if runtime.GOOS == "windows" {
		name = name + ".exe"
	}

	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%s not found and cannot determine home dir: %w", name, err)
	}
	localBin := filepath.Join(home, ".9ed", name)
	if info, err := os.Stat(localBin); err == nil && !info.IsDir() {
		return localBin, nil
	}

	log.Printf("%s not found, auto-installing to %s ...", name, localBin)
	if err := installBinary(localBin, name); err != nil {
		return "", fmt.Errorf("%s auto-install failed: %w", name, err)
	}
	log.Printf("%s installed to %s", name, localBin)
	return localBin, nil
}

func installBinary(dest, toolName string) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	var repo, filename string
	var tag string
	switch toolName {
	case "cloudflared", "cloudflared.exe":
		repo = "cloudflare/cloudflared"
		switch goos {
		case "windows":
			filename = fmt.Sprintf("cloudflared-%s-%s.zip", goos, goarch)
		case "darwin":
			filename = fmt.Sprintf("cloudflared-%s-%s", goos, goarch)
		default:
			filename = fmt.Sprintf("cloudflared-%s-%s.tar.gz", goos, goarch)
		}
	case "bore", "bore.exe":
		repo = "ekzhang/bore"
		var err error
		tag, err = resolveLatestTag("ekzhang/bore")
		if err != nil {
			return fmt.Errorf("resolve bore version: %w", err)
		}
		var target string
		switch {
		case goos == "windows" && goarch == "amd64":
			target = "x86_64-pc-windows-msvc"
		case goos == "windows" && goarch == "386":
			target = "i686-pc-windows-msvc"
		case goos == "darwin" && goarch == "arm64":
			target = "aarch64-apple-darwin"
		case goos == "darwin" && goarch == "amd64":
			target = "x86_64-apple-darwin"
		case goos == "linux" && goarch == "amd64":
			target = "x86_64-unknown-linux-musl"
		case goos == "linux" && goarch == "arm64":
			target = "aarch64-unknown-linux-musl"
		case goos == "linux" && goarch == "arm":
			target = "arm-unknown-linux-musleabi"
		case goos == "linux" && goarch == "386":
			target = "i686-unknown-linux-musl"
		default:
			return fmt.Errorf("unsupported platform %s/%s for bore", goos, goarch)
		}
		ext := ".tar.gz"
		if goos == "windows" {
			ext = ".zip"
		}
		filename = fmt.Sprintf("bore-%s-%s%s", tag, target, ext)
	default:
		return fmt.Errorf("unknown tool: %s", toolName)
	}

	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, filename)
	if repo == "ekzhang/bore" {
		url = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, filename)
	}

	log.Printf("downloading %s ...", url)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), toolName+"-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("download write: %w", err)
	}
	tmp.Close()

	switch goos {
	case "windows":
		if err := extractZip(tmpPath, dest, toolName); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	case "darwin":
		if err := os.Rename(tmpPath, dest); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
	default:
		if err := extractTarGz(tmpPath, dest, toolName); err != nil {
			return fmt.Errorf("extract tar.gz: %w", err)
		}
	}

	return os.Chmod(dest, 0o755)
}

func resolveLatestTag(repo string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("github api %s: %w", repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api %s: HTTP %d", repo, resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode github release: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name for %s", repo)
	}
	log.Printf("resolved %s latest tag: %s", repo, release.TagName)
	return release.TagName, nil
}

func extractTarGz(archive, dest, toolName string) error {
	cmd := exec.Command("tar", "-xzf", archive, "-C", filepath.Dir(dest))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	extracted := filepath.Join(filepath.Dir(dest), toolName)
	if _, err := os.Stat(extracted); err != nil {
		return fmt.Errorf("%s binary not found after extraction", toolName)
	}
	return os.Rename(extracted, dest)
}

func extractZip(archive, dest, toolName string) error {
	cmd := exec.Command("tar", "-xf", archive, "-C", filepath.Dir(dest))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" && !strings.Contains(toolName, ".exe") {
		toolName += ".exe"
	}
	extracted := filepath.Join(filepath.Dir(dest), toolName)
	if _, err := os.Stat(extracted); err != nil {
		return fmt.Errorf("%s not found after extraction", toolName)
	}
	return os.Rename(extracted, dest)
}
