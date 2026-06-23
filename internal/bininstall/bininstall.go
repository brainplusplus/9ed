// Package bininstall provides shared binary discovery and auto-installation
// for external tools (ffmpeg, cloudflared, bore, etc.).
//
// FindBinary searches for a named executable in several well-known locations.
// If the binary is not found, it is automatically downloaded and installed to
// ~/.9ed/<name>. The download is platform-aware: each tool registers its own
// release manifest via a ToolSpec factory.
package bininstall

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// mu serialises binary resolution and installation so that concurrent
// goroutines do not download the same binary twice.
var mu sync.Mutex

// ToolSpec describes how to download and extract a tool for the current
// platform.
type ToolSpec struct {
	// Repo is the GitHub "owner/repo" used for constructing release URLs.
	Repo string
	// Tag is the specific release tag. If empty, the latest tag is resolved
	// via the GitHub API before calling AssetName.
	Tag string
	// AssetName returns the release asset filename given the resolved tag.
	AssetName func(tag string) string
	// Extract specifies how to extract the downloaded archive.
	Extract ExtractMethod
	// BinaryInArchive is the name of the executable to extract from inside
	// an archive. If empty, it defaults to the tool name (with .exe on Windows).
	BinaryInArchive string
}

// ExtractMethod specifies how to extract a downloaded archive.
type ExtractMethod string

const (
	ExtractRaw ExtractMethod = "raw" // single binary, no extraction
	ExtractZip ExtractMethod = "zip" // .zip archive
	ExtractTar ExtractMethod = "tar" // .tar.gz, .tar.xz, .tgz (extracted via tar)
)

// toolFactory returns a ToolSpec for the given GOOS/GOARCH, or an error if
// the platform is unsupported.
type toolFactory func(goos, goarch string) (*ToolSpec, error)

// toolRegistry maps tool names (without extension) to factory functions.
var toolRegistry = map[string]toolFactory{
	"cloudflared": cloudflaredSpec,
	"bore":        boreSpec,
	"ffmpeg":      ffmpegSpec,
}

// FindBinary locates a named binary, auto-installing it if missing.
//
// Search order:
//  1. Directory of the current executable (sidecar binaries).
//  2. System PATH (exec.LookPath).
//  3. ~/.9ed/<name> (previously auto-installed).
//  4. Auto-download and install to ~/.9ed/<name>.
//
// The returned path is always absolute. On Windows the ".exe" suffix is
// appended automatically.
func FindBinary(name string) (string, error) {
	mu.Lock()
	defer mu.Unlock()

	if runtime.GOOS == "windows" {
		name = name + ".exe"
	}

	// 1. Beside the current executable.
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	// 2. System PATH.
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	// 3. Previously auto-installed in ~/.9ed.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%s not found and cannot determine home dir: %w", name, err)
	}
	localBin := filepath.Join(home, ".9ed", name)
	if info, err := os.Stat(localBin); err == nil && !info.IsDir() {
		return localBin, nil
	}

	// 4. Auto-download.
	log.Printf("%s not found, auto-installing to %s ...", name, localBin)
	if err := install(localBin, name); err != nil {
		return "", fmt.Errorf("%s auto-install failed: %w", name, err)
	}
	log.Printf("%s installed to %s", name, localBin)
	return localBin, nil
}

// install downloads and installs a tool to dest.
func install(dest, name string) error {
	toolName := strings.TrimSuffix(name, ".exe")
	factory, ok := toolRegistry[toolName]
	if !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}

	spec, err := factory(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	// Resolve tag if not specified.
	tag := spec.Tag
	if tag == "" {
		tag, err = resolveLatestTag(spec.Repo)
		if err != nil {
			return fmt.Errorf("resolve %s version: %w", toolName, err)
		}
	}

	assetName := spec.AssetName(tag)
	if assetName == "" {
		return fmt.Errorf("empty asset name for %s (tag %s)", toolName, tag)
	}

	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", spec.Repo, tag, assetName)

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

	// Determine the binary name to search for inside archives.
	binInArchive := spec.BinaryInArchive
	if binInArchive == "" {
		binInArchive = name // already has .exe on Windows
	}

	switch spec.Extract {
	case ExtractZip:
		if err := extractZip(tmpPath, dest, binInArchive); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	case ExtractTar:
		if err := extractTar(tmpPath, dest, binInArchive); err != nil {
			return fmt.Errorf("extract tar: %w", err)
		}
	default:
		if err := os.Rename(tmpPath, dest); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
	}

	if err := os.Chmod(dest, 0o755); err != nil {
		return err
	}

	// Clear macOS quarantine so downloaded binary can run without Gatekeeper block.
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-d", "com.apple.quarantine", dest).Run()
	}

	return nil
}

// resolveLatestTag queries the GitHub API for the latest release tag.
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

// ── Tool specifications ──────────────────────────────────────────────────

// cloudflaredSpec returns the download spec for cloudflared.
func cloudflaredSpec(goos, goarch string) (*ToolSpec, error) {
	var filename string
	switch goos {
	case "windows":
		filename = fmt.Sprintf("cloudflared-%s-%s.exe", goos, goarch)
	case "darwin":
		filename = fmt.Sprintf("cloudflared-%s-%s.tgz", goos, goarch)
	default:
		filename = fmt.Sprintf("cloudflared-%s-%s", goos, goarch)
	}
	extract := ExtractRaw
	if goos == "darwin" {
		extract = ExtractTar
	}
	return &ToolSpec{
		Repo:      "cloudflare/cloudflared",
		AssetName: func(_ string) string { return filename },
		Extract:   extract,
	}, nil
}

// boreSpec returns the download spec for bore (ekzhang/bore).
func boreSpec(goos, goarch string) (*ToolSpec, error) {
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
		return nil, fmt.Errorf("unsupported platform %s/%s for bore", goos, goarch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return &ToolSpec{
		Repo:      "ekzhang/bore",
		AssetName: func(tag string) string { return fmt.Sprintf("bore-%s-%s%s", tag, target, ext) },
		Extract: func() ExtractMethod {
			if goos == "windows" {
				return ExtractZip
			}
			return ExtractTar
		}(),
	}, nil
}

// ffmpegSpec returns the download spec for ffmpeg.
//
// Platform-aware (ADR-0001):
//   - Windows: GyanD/codexffmpeg builds (essentials build, zip).
//   - Linux:   BtbN/FFmpeg-Builds (linux64 gpl, tar.xz).
//   - macOS:   BtbN/FFmpeg-Builds (macos gpl, tar.xz).
func ffmpegSpec(goos, goarch string) (*ToolSpec, error) {
	switch {
	case goos == "windows" && goarch == "amd64":
		// GyanD codexffmpeg: ffmpeg-<version>-essentials_build.zip
		// The archive contains ffmpeg.exe in a bin/ subdirectory.
		return &ToolSpec{
			Repo:            "GyanD/codexffmpeg",
			AssetName:       func(tag string) string { return fmt.Sprintf("%s-essentials_build.zip", tag) },
			Extract:         ExtractZip,
			BinaryInArchive: "ffmpeg.exe",
		}, nil
	case goos == "linux" && goarch == "amd64":
		return &ToolSpec{
			Repo:            "BtbN/FFmpeg-Builds",
			Tag:             "latest",
			AssetName:       func(_ string) string { return "ffmpeg-master-latest-linux64-gpl.tar.xz" },
			Extract:         ExtractTar,
			BinaryInArchive: "ffmpeg",
		}, nil
	case goos == "linux" && goarch == "arm64":
		return &ToolSpec{
			Repo:            "BtbN/FFmpeg-Builds",
			Tag:             "latest",
			AssetName:       func(_ string) string { return "ffmpeg-master-latest-linuxarm64-gpl.tar.xz" },
			Extract:         ExtractTar,
			BinaryInArchive: "ffmpeg",
		}, nil
	case goos == "darwin" && goarch == "arm64":
		return &ToolSpec{
			Repo:            "BtbN/FFmpeg-Builds",
			Tag:             "latest",
			AssetName:       func(_ string) string { return "ffmpeg-master-latest-macos-arm64-gpl.tar.xz" },
			Extract:         ExtractTar,
			BinaryInArchive: "ffmpeg",
		}, nil
	case goos == "darwin" && goarch == "amd64":
		return &ToolSpec{
			Repo:            "BtbN/FFmpeg-Builds",
			Tag:             "latest",
			AssetName:       func(_ string) string { return "ffmpeg-master-latest-macos-x86_64-gpl.tar.xz" },
			Extract:         ExtractTar,
			BinaryInArchive: "ffmpeg",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported platform %s/%s for ffmpeg", goos, goarch)
	}
}

// ── Test/exported helper functions ───────────────────────────────────────

// downloadInfo holds the resolved download information for a tool on a given
// platform. Used by getDownloadInfo for testing.
type downloadInfo struct {
	URL      string
	filename string
	extract  ExtractMethod
}

// getDownloadInfo returns the download URL, asset filename, and extraction
// method for a tool on the given platform. This is used by tests to verify
// platform-aware download URLs without actually downloading.
func getDownloadInfo(toolName, goos, goarch string) (*downloadInfo, error) {
	factory, ok := toolRegistry[toolName]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	spec, err := factory(goos, goarch)
	if err != nil {
		return nil, err
	}

	tag := spec.Tag
	if tag == "" {
		// For URL construction in tests, we use "latest" as a placeholder.
		// Real resolution happens in install().
	}

	assetName := spec.AssetName(tag)
	var url string
	if spec.Repo == "BtbN/FFmpeg-Builds" {
		// BtbN uses /releases/download/latest/ for its rolling releases.
		url = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", spec.Repo, "latest", assetName)
	} else {
		url = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", spec.Repo, tag, assetName)
	}

	return &downloadInfo{
		URL:      url,
		filename: assetName,
		extract:  spec.Extract,
	}, nil
}

// ffmpegDownloadURL returns the download URL and archive extension for ffmpeg
// on the given platform. Used for testing platform-aware download URLs.
func ffmpegDownloadURL(goos, goarch string) (string, string) {
	info, err := getDownloadInfo("ffmpeg", goos, goarch)
	if err != nil {
		return "", ""
	}
	ext := "tar.xz"
	if goos == "windows" {
		ext = "zip"
	}
	return info.URL, ext
}

// binaryName returns the binary name with .exe suffix on Windows.
func binaryName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

// ffmpegBinaryInArchive returns the path of the ffmpeg binary inside the
// downloaded archive for the given platform.
func ffmpegBinaryInArchive(goos string) string {
	spec, err := ffmpegSpec(goos, "amd64")
	if err != nil {
		// Fallback for arm64
		spec, err = ffmpegSpec(goos, "arm64")
		if err != nil {
			return "ffmpeg"
		}
	}
	if spec.BinaryInArchive != "" {
		return spec.BinaryInArchive
	}
	if goos == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}
