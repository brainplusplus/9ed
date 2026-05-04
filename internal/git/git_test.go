package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0644)
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func TestIsRepo(t *testing.T) {
	repoDir := setupTestRepo(t)
	repo := New(repoDir)

	if !repo.IsRepo() {
		t.Fatal("expected IsRepo to return true for git repo")
	}

	nonRepo := t.TempDir()
	nonRepoGit := New(nonRepo)
	if nonRepoGit.IsRepo() {
		t.Fatal("expected IsRepo to return false for non-git dir")
	}
}

func TestExecGit(t *testing.T) {
	repoDir := setupTestRepo(t)
	repo := New(repoDir)

	out, err := repo.exec("rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if out != "true" {
		t.Fatalf("expected 'true', got %q", out)
	}
}
