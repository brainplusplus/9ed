package chat

import (
	"regexp"
	"strings"
)

// ansiRegex matches ANSI escape sequences including private mode (CSI ? sequences),
// OSC sequences, and single-character escapes.
var ansiRegex = regexp.MustCompile(`\x1b\[[?]?[0-9;]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[^[\]()]|\x1b\([AB012]`)

// StripANSI removes ANSI escape codes and control characters from the input string.
func StripANSI(input string) string {
	result := ansiRegex.ReplaceAllString(input, "")
	result = strings.ReplaceAll(result, "\r", "")
	return result
}
