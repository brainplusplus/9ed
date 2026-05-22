package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// FindBrowser attempts to locate a locally installed Chromium-based browser.
// It searches for Chrome, Edge, and Chromium in that order.
// Returns the executable path or an empty string if none is found.
func FindBrowser() string {
	candidates := browserCandidates()
	for _, path := range candidates {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func browserCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			// Chrome
			expandProgramFiles(`Google\Chrome\Application\chrome.exe`),
			// Edge
			expandProgramFiles(`Microsoft\Edge\Application\msedge.exe`),
			// Chromium
			expandProgramFiles(`Chromium\Application\chrome.exe`),
			// Brave
			expandProgramFiles(`BraveSoftware\Brave-Browser\Application\brave.exe`),
		}
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	default: // linux and other unix
		return []string{
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
			"/usr/bin/microsoft-edge-stable",
			"/usr/bin/microsoft-edge",
			"/snap/bin/chromium",
		}
	}
}

func expandProgramFiles(relative string) string {
	// Try both ProgramFiles and ProgramFiles(x86) as well as
	// the forward-slash variants that Go's os.Getenv returns on Windows.
	dirs := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		`C:\Program Files`,
		`C:\Program Files (x86)`,
	}
	seen := make(map[string]bool)
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		full := filepath.Join(dir, relative)
		if fileExists(full) {
			return full
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// IsBrowserAvailable returns true if a local browser was found.
func IsBrowserAvailable() bool {
	return strings.TrimSpace(FindBrowser()) != ""
}
