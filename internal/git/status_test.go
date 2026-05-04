package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatus(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified"), 0644)

	status, err := repo.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if len(status) < 2 {
		t.Fatalf("expected at least 2 entries, got %d: %+v", len(status), status)
	}

	hasModified := false
	hasUntracked := false
	for _, s := range status {
		if s.Path == "README.md" && s.Status == "modified" {
			hasModified = true
		}
		if s.Path == "new.txt" && s.Status == "untracked" {
			hasUntracked = true
		}
	}
	if !hasModified {
		t.Errorf("expected modified README.md in status, got: %+v", status)
	}
	if !hasUntracked {
		t.Errorf("expected untracked new.txt in status, got: %+v", status)
	}
}

func TestStageAndUnstage(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644)

	if err := repo.Stage([]string{"new.txt"}); err != nil {
		t.Fatalf("Stage failed: %v", err)
	}

	status, _ := repo.Status()
	for _, s := range status {
		if s.Path == "new.txt" && !s.Staged {
			t.Error("expected new.txt to be staged")
		}
	}

	if err := repo.Unstage([]string{"new.txt"}); err != nil {
		t.Fatalf("Unstage failed: %v", err)
	}

	status, _ = repo.Status()
	for _, s := range status {
		if s.Path == "new.txt" && s.Staged {
			t.Error("expected new.txt to be unstaged")
		}
	}
}

func TestDiscard(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified"), 0644)

	if err := repo.Discard([]string{"README.md"}); err != nil {
		t.Fatalf("Discard failed: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(content) != "# Test" {
		t.Errorf("expected original content, got %q", string(content))
	}
}

func TestCommit(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644)
	repo.Stage([]string{"new.txt"})

	if err := repo.Commit("test commit", false); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	commits, _ := repo.Log(1, 0)
	if commits[0].Message != "test commit" {
		t.Errorf("expected 'test commit', got %q", commits[0].Message)
	}
}
