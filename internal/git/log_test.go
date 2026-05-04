package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLog(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second"), 0644)
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "second commit")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
	cmd.Run()

	commits, err := repo.Log(10, 0)
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}

	if commits[0].Message != "second commit" {
		t.Errorf("expected first commit message 'second commit', got %q", commits[0].Message)
	}
	if commits[0].ShortHash == "" {
		t.Error("expected non-empty short hash")
	}
	if commits[0].Author == "" {
		t.Error("expected non-empty author")
	}
}

func TestLogPagination(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	commits, err := repo.Log(1, 0)
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit with limit=1, got %d", len(commits))
	}
}
