package chat

import "regexp"

// ansiRegex matches ANSI escape sequences.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[^[\]()]`)

// StripANSI removes ANSI escape codes from the input string.
func StripANSI(input string) string {
	return ansiRegex.ReplaceAllString(input, "")
}
