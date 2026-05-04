package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStashSaveAndList(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Stash me"), 0644)

	if err := repo.StashSave("test stash"); err != nil {
		t.Fatalf("StashSave failed: %v", err)
	}

	stashes, err := repo.StashList()
	if err != nil {
		t.Fatalf("StashList failed: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("expected 1 stash, got %d", len(stashes))
	}
	if stashes[0].Message == "" {
		t.Error("expected non-empty stash message")
	}
}

func TestStashPop(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Stash me"), 0644)
	repo.StashSave("test")

	if err := repo.StashPop(0); err != nil {
		t.Fatalf("StashPop failed: %v", err)
	}

	stashes, _ := repo.StashList()
	if len(stashes) != 0 {
		t.Error("expected 0 stashes after pop")
	}
}
