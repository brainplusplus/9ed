package acpinstall

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const binaryDownloadTimeout = 120 * time.Second

// supportedPlatforms enumerates the platform keys that 9ed knows how to install
// binary distributions for. This mirrors Zed's matrix:
// darwin/linux/windows × x86_64/aarch64.
var supportedPlatforms = map[string]struct{}{
	"darwin-x86_64":   {},
	"darwin-aarch64":  {},
	"linux-x86_64":    {},
	"linux-aarch64":   {},
	"windows-x86_64":  {},
	"windows-aarch64": {},
}

// CurrentPlatformKey returns the platform identifier for the running process
// in the form "<os>-<arch>" (e.g. "linux-x86_64", "windows-aarch64").
//
// Returns an error if either the OS or architecture is not in the supported
// matrix. Use IsPlatformSupported for a quick check that does not allocate.
func CurrentPlatformKey() (string, error) {
	return currentPlatformKey()
}

// IsPlatformSupported reports whether 9ed can install binary distributions
// on the current OS/arch.
func IsPlatformSupported() bool {
	key, err := currentPlatformKey()
	if err != nil {
		return false
	}
	_, ok := supportedPlatforms[key]
	return ok
}

func currentPlatformKey() (string, error) {
	var osPart string
	switch runtime.GOOS {
	case "darwin":
		osPart = "darwin"
	case "linux":
		osPart = "linux"
	case "windows":
		osPart = "windows"
	default:
		return "", fmt.Errorf("unsupported OS: %s (supported: darwin, linux, windows)", runtime.GOOS)
	}

	var archPart string
	switch runtime.GOARCH {
	case "amd64":
		archPart = "x86_64"
	case "arm64":
		archPart = "aarch64"
	default:
		return "", fmt.Errorf("unsupported architecture: %s (supported: amd64, arm64)", runtime.GOARCH)
	}

	key := osPart + "-" + archPart
	if _, ok := supportedPlatforms[key]; !ok {
		return "", fmt.Errorf("unsupported platform: %s", key)
	}
	return key, nil
}

func hasBinaryDistribution(entry *RegistryAgent) bool {
	if entry == nil || len(entry.Distribution.Binary) == 0 {
		return false
	}
	platform, err := currentPlatformKey()
	if err != nil {
		return false
	}
	_, ok := entry.Distribution.Binary[platform]
	return ok
}

func versionedBinaryDir(agentID, version, archiveURL string) (string, error) {
	base, err := binaryAdapterBaseDir(agentID)
	if err != nil {
		return "", err
	}
	versionHash := shortHash(version)
	urlHash := shortHash(archiveURL)
	return filepath.Join(base, fmt.Sprintf("v_%s_%s_%s", sanitizePathComponent(version), versionHash, urlHash)), nil
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// installBinaryDistribution downloads/extracts a registry binary distribution
// into a versioned cache directory and returns the command path + args/env.
func installBinaryDistribution(ctx context.Context, info *AdapterInfo, entry *RegistryAgent, force bool) (path string, args []string, env map[string]string, version string, err error) {
	platform, err := currentPlatformKey()
	if err != nil {
		return "", nil, nil, "", fmt.Errorf("install %s: %w", info.ID, err)
	}
	target, ok := entry.Distribution.Binary[platform]
	if !ok {
		available := make([]string, 0, len(entry.Distribution.Binary))
		for k := range entry.Distribution.Binary {
			available = append(available, k)
		}
		return "", nil, nil, "", fmt.Errorf("%s has no binary target for platform %s (available: %v)", info.ID, platform, available)
	}

	versionDir, err := versionedBinaryDir(info.ID, entry.Version, target.Archive)
	if err != nil {
		return "", nil, nil, "", err
	}

	cmdPath, err := resolveBinaryCommandPath(versionDir, target.Cmd)
	if err == nil && !force {
		if _, statErr := os.Stat(cmdPath); statErr == nil {
			_ = setAdapterState(info.ID, adapterState{
				Version:     entry.Version,
				Package:     "binary:" + target.Archive,
				InstalledAt: time.Now().UTC(),
				BinaryPath:  cmdPath,
			})
			return cmdPath, target.Args, target.Env, entry.Version, nil
		}
	}

	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", nil, nil, "", err
	}

	if err := downloadAndExtract(ctx, target.Archive, versionDir); err != nil {
		return "", nil, nil, "", err
	}

	cmdPath, err = resolveBinaryCommandPath(versionDir, target.Cmd)
	if err != nil {
		return "", nil, nil, "", err
	}
	if _, err := os.Stat(cmdPath); err != nil {
		return "", nil, nil, "", fmt.Errorf("missing command %s after extraction: %w", cmdPath, err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(cmdPath, 0o755)
	}

	if err := setAdapterState(info.ID, adapterState{
		Version:     entry.Version,
		Package:     "binary:" + target.Archive,
		InstalledAt: time.Now().UTC(),
		BinaryPath:  cmdPath,
	}); err != nil {
		return "", nil, nil, "", err
	}

	go cleanupStaleBinaryDirs(info.ID, versionDir)
	return cmdPath, target.Args, target.Env, entry.Version, nil
}

func resolveBinaryCommandPath(versionDir, cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", fmt.Errorf("empty binary command")
	}
	cmd = strings.TrimPrefix(cmd, "./")
	cmd = strings.TrimPrefix(cmd, `.\\`)
	cmd = strings.TrimPrefix(cmd, `.\`)
	if strings.Contains(cmd, "..") {
		return "", fmt.Errorf("command path cannot contain '..': %s", cmd)
	}
	return filepath.Join(versionDir, filepath.FromSlash(strings.ReplaceAll(cmd, "\\", "/"))), nil
}

func downloadAndExtract(ctx context.Context, url, destDir string) error {
	dlCtx, cancel := context.WithTimeout(ctx, binaryDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "9ed/1.0 (+https://github.com/brainplusplus/9ed)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s returned status %d", url, resp.StatusCode)
	}

	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(resp.Body, destDir)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gz.Close()
		return extractTar(gz, destDir)
	case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
		return extractTar(bzip2.NewReader(resp.Body), destDir)
	default:
		// Raw binary URL.
		name := filepath.Base(strings.Split(url, "?")[0])
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "agent"
		}
		path := filepath.Join(destDir, sanitizePathComponent(name))
		out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, resp.Body)
		return err
	}
}

func extractZip(r io.Reader, destDir string) error {
	tmp, err := os.CreateTemp("", "9ed-archive-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if err := extractZipFile(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, destDir string) error {
	path, err := safeJoin(destDir, f.Name)
	if err != nil {
		return err
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(path, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	mode := f.FileInfo().Mode()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	return copyAndChmod(out, rc, path, mode)
}

func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode)
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if err := copyAndChmod(out, tr, path, mode); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

func copyAndChmod(out *os.File, r io.Reader, path string, mode os.FileMode) error {
	if _, err := io.Copy(out, r); err != nil {
		return err
	}
	if runtime.GOOS != "windows" && mode&0o111 != 0 {
		return os.Chmod(path, mode)
	}
	return nil
}

func safeJoin(base, name string) (string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(name))
	if cleanName == "." || filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) || cleanName == ".." {
		return "", fmt.Errorf("unsafe archive path: %s", name)
	}
	path := filepath.Join(base, cleanName)
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if cleanPath != cleanBase && !strings.HasPrefix(cleanPath, cleanBase+string(filepath.Separator)) {
		return "", fmt.Errorf("archive path escapes destination: %s", name)
	}
	return path, nil
}

func cleanupStaleBinaryDirs(agentID, currentDir string) {
	base, err := binaryAdapterBaseDir(agentID)
	if err != nil {
		return
	}
	currentInfo, err := os.Stat(currentDir)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "v_") {
			continue
		}
		path := filepath.Join(base, entry.Name())
		if samePath(path, currentDir) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		// Only remove directories older than the current version dir.
		if info.ModTime().Before(currentInfo.ModTime()) {
			_ = os.RemoveAll(path)
		}
	}
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}
