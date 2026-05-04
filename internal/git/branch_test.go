package git

import "testing"

func TestBranches(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	branches, err := repo.Branches()
	if err != nil {
		t.Fatalf("Branches failed: %v", err)
	}

	if len(branches) == 0 {
		t.Fatal("expected at least one branch")
	}

	found := false
	for _, b := range branches {
		if b.Current {
			found = true
		}
	}
	if !found {
		t.Error("expected one branch to be current")
	}
}

func TestBranchCreateAndSwitch(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	if err := repo.BranchCreate("feature-x"); err != nil {
		t.Fatalf("BranchCreate failed: %v", err)
	}

	if err := repo.BranchSwitch("feature-x"); err != nil {
		t.Fatalf("BranchSwitch failed: %v", err)
	}

	branches, _ := repo.Branches()
	for _, b := range branches {
		if b.Name == "feature-x" && !b.Current {
			t.Error("expected feature-x to be current branch")
		}
	}
}

func TestBranchDelete(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	repo.BranchCreate("to-delete")
	if err := repo.BranchDelete("to-delete"); err != nil {
		t.Fatalf("BranchDelete failed: %v", err)
	}

	branches, _ := repo.Branches()
	for _, b := range branches {
		if b.Name == "to-delete" {
			t.Error("expected branch to be deleted")
		}
	}
}
