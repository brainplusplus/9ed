package watcher

import "testing"

func TestShouldSkipEventPathSkipsTransientFiles(t *testing.T) {
	cases := []string{
		`C:\repo\orchestration-state.json.tmp-2139576-1779438989476`,
		"/repo/file.tmp",
		"/repo/file.swp",
		"/repo/.DS_Store",
		"/repo/node_modules/pkg/index.js",
	}
	for _, path := range cases {
		if !shouldSkipEventPath(path) {
			t.Fatalf("expected transient path to be skipped: %s", path)
		}
	}
}

func TestShouldSkipEventPathKeepsUserFiles(t *testing.T) {
	cases := []string{
		"/repo/.env",
		"/repo/src/main.go",
		"/repo/tmp-notes.md",
	}
	for _, path := range cases {
		if shouldSkipEventPath(path) {
			t.Fatalf("expected user path to be kept: %s", path)
		}
	}
}
