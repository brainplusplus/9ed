package debug

import (
	"log"
	"os"
	"strings"
)

var enabled bool
var watcherEnabled bool

func init() {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("DEBUG")))
	enabled = v == "true" || v == "1"
}

func Enable(on bool) {
	enabled = on
}

func Enabled() bool {
	return enabled
}

func SetWatcherEnabled(on bool) {
	watcherEnabled = on
}

func WatcherPrintf(format string, args ...any) {
	if enabled && watcherEnabled {
		log.Printf(format, args...)
	}
}

func Printf(format string, args ...any) {
	if enabled {
		log.Printf(format, args...)
	}
}
