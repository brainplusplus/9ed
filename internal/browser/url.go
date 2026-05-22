package browser

import (
	"fmt"
	"net/url"
	"strings"
)

func NormalizeURL(raw string) (*url.URL, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, fmt.Errorf("url is required")
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "localhost:") ||
		strings.HasPrefix(lower, "127.") ||
		strings.HasPrefix(lower, "0.0.0.0:") ||
		strings.HasPrefix(lower, "[::1]:") ||
		looksLikeHostPort(value) {
		value = "http://" + value
	} else if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("url host is required")
	}
	return parsed, nil
}

func looksLikeHostPort(value string) bool {
	if strings.Contains(value, "/") || strings.Contains(value, "://") {
		return false
	}
	host, port, found := strings.Cut(value, ":")
	return found && host != "" && port != ""
}
