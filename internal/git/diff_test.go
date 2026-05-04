package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiff(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified\nNew line"), 0644)

	diff, err := repo.Diff("README.md")
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if diff == "" {
		t.Fatal("expected non-empty diff")
	}
}

func TestDiffLines(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified\nNew line"), 0644)

	changes, err := repo.DiffLines("README.md")
	if err != nil {
		t.Fatalf("DiffLines failed: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("expected at least one gutter change")
	}

	hasModified := false
	for _, c := range changes {
		if c.Type == "modified" || c.Type == "added" {
			hasModified = true
		}
	}
	if !hasModified {
		t.Error("expected modified or added change type")
	}
}

func TestDiffFileContent(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	content, err := repo.FileAtHEAD("README.md")
	if err != nil {
		t.Fatalf("FileAtHEAD failed: %v", err)
	}
	if content != "# Test" {
		t.Errorf("expected '# Test', got %q", content)
	}
}
