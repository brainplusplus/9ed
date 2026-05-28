package debug

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type BrowserMCPEntry struct {
	Timestamp int64  `json:"timestamp"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

const browserMCPBufferSize = 240

var (
	browserMCPMu      sync.Mutex
	browserMCPEntries []BrowserMCPEntry
)

func BrowserMCPEnabled() bool {
	if !Enabled() {
		return false
	}
	return envFlag("DEBUG_BROWSER_MCP", true)
}

func BrowserMCPLog(source, level, format string, args ...any) {
	if !BrowserMCPEnabled() {
		return
	}
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		source = "server"
	}
	level = strings.TrimSpace(strings.ToLower(level))
	if level == "" {
		level = "info"
	}
	message := fmt.Sprintf(format, args...)
	log.Printf("[browser/mcp/%s] %s", source, message)
	appendBrowserMCPEntry(BrowserMCPEntry{
		Timestamp: time.Now().UnixMilli(),
		Source:    source,
		Level:     level,
		Message:   message,
	})
}

func BrowserMCPEntries(limit int) []BrowserMCPEntry {
	browserMCPMu.Lock()
	defer browserMCPMu.Unlock()

	if len(browserMCPEntries) == 0 {
		return []BrowserMCPEntry{}
	}
	if limit <= 0 || limit > len(browserMCPEntries) {
		limit = len(browserMCPEntries)
	}
	start := len(browserMCPEntries) - limit
	out := make([]BrowserMCPEntry, limit)
	copy(out, browserMCPEntries[start:])
	return out
}

func appendBrowserMCPEntry(entry BrowserMCPEntry) {
	browserMCPMu.Lock()
	defer browserMCPMu.Unlock()

	browserMCPEntries = append(browserMCPEntries, entry)
	if len(browserMCPEntries) <= browserMCPBufferSize {
		return
	}
	browserMCPEntries = append([]BrowserMCPEntry(nil), browserMCPEntries[len(browserMCPEntries)-browserMCPBufferSize:]...)
}

func envFlag(key string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return defaultValue
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
