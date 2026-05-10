package debug

import (
	"log"
	"os"
	"strings"
)

var enabled bool

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

func Printf(format string, args ...any) {
	if enabled {
		log.Printf(format, args...)
	}
}
