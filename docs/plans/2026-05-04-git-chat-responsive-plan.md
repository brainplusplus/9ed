# Git Panel, Chat UI & Responsive Layout — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add full git source control, AI chat (via CLI agent harness), and responsive breakpoints to the IDE mode.

**Architecture:** Three independent phases. Phase 1 adds `internal/git/` Go package + frontend git panel + Monaco gutter decorations. Phase 2 adds `internal/chat/` agent harness + chat panel + inline prompt. Phase 3 retrofits responsive breakpoints across all panels.

**Tech Stack:** Go 1.24 (os/exec for git CLI), React 18, TypeScript, Zustand, Monaco Editor, xterm.js, react-resizable-panels, WebSocket (gorilla/websocket)

**Reference Spec:** `docs/superpowers/specs/2026-05-04-git-chat-responsive-design.md`

---

## Phase 1: Git Backend + Git Panel UI + Git Gutter

---

### Task 1: Git Core Package — exec helper & repo detection

**Files:**
- Create: `internal/git/git.go`
- Create: `internal/git/git_test.go`

**Step 1: Write the test file**

```go
// internal/git/git_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -v -run TestIsRepo`
Expected: FAIL — package does not exist yet

**Step 3: Write the implementation**

```go
// internal/git/git.go
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Repo provides git operations scoped to a directory.
type Repo struct {
	dir string
}

// New creates a Repo bound to the given directory.
func New(dir string) *Repo {
	return &Repo{dir: dir}
}

// Dir returns the repository working directory.
func (r *Repo) Dir() string {
	return r.dir
}

// IsRepo returns true if the directory is inside a git work tree.
func (r *Repo) IsRepo() bool {
	_, err := r.exec("rev-parse", "--is-inside-work-tree")
	return err == nil
}

// exec runs a git command in the repo directory and returns trimmed stdout.
func (r *Repo) exec(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// execLines runs a git command and returns output split by newlines (empty lines removed).
func (r *Repo) execLines(args ...string) ([]string, error) {
	out, err := r.exec(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/
git commit -m "feat(git): add core git package with exec helper and repo detection"
```

---

### Task 2: Git Status — status, stage, unstage, discard

**Files:**
- Create: `internal/git/status.go`
- Create: `internal/git/status_test.go`

**Step 1: Write the test**

```go
// internal/git/status_test.go
package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatus(t *testing.T) {
	dir := setupTestRepo(t)
	repo := New(dir)

	// Create a new file (untracked)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644)
	// Modify existing file
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified"), 0644)

	status, err := repo.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if len(status) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(status), status)
	}

	// Check we have modified and untracked
	hasModified := false
	hasUntracked := false
	for _, s := range status {
		if s.Path == "README.md" && s.Status == "modified" && !s.Staged {
			hasModified = true
		}
		if s.Path == "new.txt" && s.Status == "untracked" && !s.Staged {
			hasUntracked = true
		}
	}
	if !hasModified {
		t.Error("expected modified README.md")
	}
	if !hasUntracked {
		t.Error("expected untracked new.txt")
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -v -run "TestStatus|TestStage|TestDiscard"`
Expected: FAIL — functions not defined

**Step 3: Write the implementation**

```go
// internal/git/status.go
package git

import "strings"

// FileStatus represents the status of a single file.
type FileStatus struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "modified", "added", "deleted", "renamed", "untracked"
	Staged bool   `json:"staged"`
}

// Status returns the working tree status.
func (r *Repo) Status() ([]FileStatus, error) {
	out, err := r.exec("status", "--porcelain=v1", "-uall")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	result := make([]FileStatus, 0, len(lines))

	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		x := line[0] // index (staged) status
		y := line[1] // worktree status
		path := strings.TrimSpace(line[3:])

		// Handle renames: "R  old -> new"
		if idx := strings.Index(path, " -> "); idx != -1 {
			path = path[idx+4:]
		}

		if x != ' ' && x != '?' {
			result = append(result, FileStatus{
				Path:   path,
				Status: porcelainToStatus(x),
				Staged: true,
			})
		}
		if y != ' ' {
			status := porcelainToStatus(y)
			if x == '?' {
				status = "untracked"
			}
			result = append(result, FileStatus{
				Path:   path,
				Status: status,
				Staged: false,
			})
		}
	}

	return result, nil
}

// Stage adds files to the index.
func (r *Repo) Stage(paths []string) error {
	args := append([]string{"add", "--"}, paths...)
	_, err := r.exec(args...)
	return err
}

// Unstage removes files from the index (keeps working tree changes).
func (r *Repo) Unstage(paths []string) error {
	args := append([]string{"reset", "HEAD", "--"}, paths...)
	_, err := r.exec(args...)
	return err
}

// Discard reverts working tree changes for tracked files.
func (r *Repo) Discard(paths []string) error {
	args := append([]string{"checkout", "--"}, paths...)
	_, err := r.exec(args...)
	return err
}

func porcelainToStatus(c byte) string {
	switch c {
	case 'M':
		return "modified"
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case '?':
		return "untracked"
	default:
		return "modified"
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/git/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/status.go internal/git/status_test.go
git commit -m "feat(git): add status, stage, unstage, discard operations"
```

---

### Task 3: Git Log — commit history with pagination

**Files:**
- Create: `internal/git/log.go`
- Create: `internal/git/log_test.go`

**Step 1: Write the test**

```go
// internal/git/log_test.go
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

	// Add a second commit
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -v -run TestLog`
Expected: FAIL

**Step 3: Write the implementation**

```go
// internal/git/log.go
package git

import (
	"fmt"
	"strings"
)

// Commit represents a single git commit.
type Commit struct {
	Hash         string `json:"hash"`
	ShortHash    string `json:"shortHash"`
	Message      string `json:"message"`
	Author       string `json:"author"`
	Date         string `json:"date"`         // ISO 8601
	RelativeDate string `json:"relativeDate"` // "2 hours ago"
}

// Log returns commit history with pagination.
func (r *Repo) Log(limit, offset int) ([]Commit, error) {
	format := "%H%n%h%n%s%n%an%n%aI%n%ar%n---"
	args := []string{
		"log",
		fmt.Sprintf("--format=%s", format),
		fmt.Sprintf("-n%d", limit),
	}
	if offset > 0 {
		args = append(args, fmt.Sprintf("--skip=%d", offset))
	}

	out, err := r.exec(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	entries := strings.Split(out, "---")
	commits := make([]Commit, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		lines := strings.Split(entry, "\n")
		if len(lines) < 6 {
			continue
		}
		commits = append(commits, Commit{
			Hash:         lines[0],
			ShortHash:    lines[1],
			Message:      lines[2],
			Author:       lines[3],
			Date:         lines[4],
			RelativeDate: lines[5],
		})
	}

	return commits, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/git/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/log.go internal/git/log_test.go
git commit -m "feat(git): add commit log with pagination"
```

---

### Task 4: Git Branch — list, create, switch, delete, merge

**Files:**
- Create: `internal/git/branch.go`
- Create: `internal/git/branch_test.go`

**Step 1: Write the test**

```go
// internal/git/branch_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -v -run TestBranch`
Expected: FAIL

**Step 3: Write the implementation**

```go
// internal/git/branch.go
package git

import (
	"strconv"
	"strings"
)

// Branch represents a git branch.
type Branch struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Remote  string `json:"remote,omitempty"`
	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
}

// Branches lists all local branches with tracking info.
func (r *Repo) Branches() ([]Branch, error) {
	out, err := r.exec("branch", "-vv", "--format=%(HEAD)%(refname:short)%09%(upstream:short)%09%(upstream:track)")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	branches := make([]Branch, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}
		current := line[0] == '*'
		rest := line[1:]
		parts := strings.Split(rest, "\t")

		b := Branch{
			Name:    strings.TrimSpace(parts[0]),
			Current: current,
		}

		if len(parts) > 1 {
			b.Remote = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			track := strings.TrimSpace(parts[2])
			b.Ahead, b.Behind = parseTrackInfo(track)
		}

		branches = append(branches, b)
	}

	return branches, nil
}

// BranchCreate creates a new branch.
func (r *Repo) BranchCreate(name string) error {
	_, err := r.exec("branch", name)
	return err
}

// BranchSwitch switches to a branch.
func (r *Repo) BranchSwitch(name string) error {
	_, err := r.exec("checkout", name)
	return err
}

// BranchDelete deletes a branch.
func (r *Repo) BranchDelete(name string) error {
	_, err := r.exec("branch", "-d", name)
	return err
}

// Merge merges a branch into the current branch.
func (r *Repo) Merge(branch string) error {
	_, err := r.exec("merge", branch)
	return err
}

func parseTrackInfo(track string) (ahead, behind int) {
	// Format: [ahead 2, behind 1] or [ahead 2] or [behind 1] or empty
	track = strings.Trim(track, "[]")
	if track == "" {
		return 0, 0
	}
	parts := strings.Split(track, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "ahead ") {
			ahead, _ = strconv.Atoi(strings.TrimPrefix(p, "ahead "))
		}
		if strings.HasPrefix(p, "behind ") {
			behind, _ = strconv.Atoi(strings.TrimPrefix(p, "behind "))
		}
	}
	return
}
```

**Step 4: Run tests**

Run: `go test ./internal/git/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/branch.go internal/git/branch_test.go
git commit -m "feat(git): add branch list, create, switch, delete, merge"
```

---

### Task 5: Git Diff — file diff & line-level diff for gutter

**Files:**
- Create: `internal/git/diff.go`
- Create: `internal/git/diff_test.go`

**Step 1: Write the test**

```go
// internal/git/diff_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -v -run "TestDiff"`
Expected: FAIL

**Step 3: Write the implementation**

```go
// internal/git/diff.go
package git

import (
	"fmt"
	"strconv"
	"strings"
)

// GutterChange represents a line-level change for editor gutter decorations.
type GutterChange struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Type      string `json:"type"` // "added", "modified", "deleted"
}

// Diff returns the unified diff for a file (working tree vs HEAD).
func (r *Repo) Diff(path string) (string, error) {
	return r.exec("diff", "HEAD", "--", path)
}

// DiffStaged returns the unified diff for staged changes.
func (r *Repo) DiffStaged(path string) (string, error) {
	return r.exec("diff", "--cached", "--", path)
}

// DiffLines returns line-level changes for gutter decorations.
func (r *Repo) DiffLines(path string) ([]GutterChange, error) {
	out, err := r.exec("diff", "HEAD", "--unified=0", "--", path)
	if err != nil {
		// File might be untracked — all lines are "added"
		content, readErr := r.exec("diff", "--no-index", "/dev/null", path)
		if readErr != nil && content == "" {
			return nil, err
		}
		out = content
	}
	if out == "" {
		return nil, nil
	}

	return parseDiffHunks(out), nil
}

// FileAtHEAD returns the content of a file at HEAD.
func (r *Repo) FileAtHEAD(path string) (string, error) {
	return r.exec("show", fmt.Sprintf("HEAD:%s", path))
}

// DiffCommit returns the diff for a specific commit.
func (r *Repo) DiffCommit(hash string) (string, error) {
	return r.exec("show", "--format=", hash)
}

func parseDiffHunks(diff string) []GutterChange {
	lines := strings.Split(diff, "\n")
	changes := make([]GutterChange, 0)

	for _, line := range lines {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		// Parse @@ -old,count +new,count @@
		parts := strings.Split(line, " ")
		if len(parts) < 3 {
			continue
		}

		oldInfo := strings.TrimPrefix(parts[1], "-")
		newInfo := strings.TrimPrefix(parts[2], "+")

		oldCount := parseHunkCount(oldInfo)
		newStart, newCount := parseHunkStartCount(newInfo)

		if oldCount == 0 && newCount > 0 {
			// Pure addition
			changes = append(changes, GutterChange{
				StartLine: newStart,
				EndLine:   newStart + newCount - 1,
				Type:      "added",
			})
		} else if newCount == 0 && oldCount > 0 {
			// Pure deletion
			changes = append(changes, GutterChange{
				StartLine: newStart,
				EndLine:   newStart,
				Type:      "deleted",
			})
		} else {
			// Modification
			changes = append(changes, GutterChange{
				StartLine: newStart,
				EndLine:   newStart + newCount - 1,
				Type:      "modified",
			})
		}
	}

	return changes
}

func parseHunkCount(info string) int {
	parts := strings.Split(info, ",")
	if len(parts) < 2 {
		return 1
	}
	count, _ := strconv.Atoi(parts[1])
	return count
}

func parseHunkStartCount(info string) (start, count int) {
	parts := strings.Split(info, ",")
	start, _ = strconv.Atoi(parts[0])
	if len(parts) < 2 {
		return start, 1
	}
	count, _ = strconv.Atoi(parts[1])
	return start, count
}
```

**Step 4: Run tests**

Run: `go test ./internal/git/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/diff.go internal/git/diff_test.go
git commit -m "feat(git): add diff, diff-lines for gutter, and file-at-HEAD"
```

---

### Task 6: Git Remote — push, pull, fetch

**Files:**
- Create: `internal/git/remote.go`

**Step 1: Write the implementation**

```go
// internal/git/remote.go
package git

// Push pushes to a remote.
func (r *Repo) Push(remote, branch string) (string, error) {
	args := []string{"push"}
	if remote != "" {
		args = append(args, remote)
	}
	if branch != "" {
		args = append(args, branch)
	}
	return r.exec(args...)
}

// Pull pulls from a remote.
func (r *Repo) Pull(remote, branch string) (string, error) {
	args := []string{"pull"}
	if remote != "" {
		args = append(args, remote)
	}
	if branch != "" {
		args = append(args, branch)
	}
	return r.exec(args...)
}

// Fetch fetches from all remotes.
func (r *Repo) Fetch() error {
	_, err := r.exec("fetch", "--all")
	return err
}
```

Note: Push/Pull are hard to unit test without a remote. These will be tested via integration/manual testing.

**Step 2: Commit**

```bash
git add internal/git/remote.go
git commit -m "feat(git): add push, pull, fetch operations"
```

---

### Task 7: Git Stash — save, pop, apply, drop, list

**Files:**
- Create: `internal/git/stash.go`
- Create: `internal/git/stash_test.go`

**Step 1: Write the test**

```go
// internal/git/stash_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -v -run TestStash`
Expected: FAIL

**Step 3: Write the implementation**

```go
// internal/git/stash.go
package git

import (
	"fmt"
	"strings"
)

// Stash represents a git stash entry.
type Stash struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// StashList returns all stash entries.
func (r *Repo) StashList() ([]Stash, error) {
	out, err := r.exec("stash", "list", "--format=%gd%n%s%n---")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	entries := strings.Split(out, "---")
	stashes := make([]Stash, 0, len(entries))

	for i, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		lines := strings.Split(entry, "\n")
		msg := ""
		if len(lines) > 1 {
			msg = lines[1]
		}
		stashes = append(stashes, Stash{
			Index:   i,
			Message: msg,
		})
	}

	return stashes, nil
}

// StashSave creates a new stash.
func (r *Repo) StashSave(message string) error {
	args := []string{"stash", "push"}
	if message != "" {
		args = append(args, "-m", message)
	}
	_, err := r.exec(args...)
	return err
}

// StashPop applies and removes a stash.
func (r *Repo) StashPop(index int) error {
	_, err := r.exec("stash", "pop", fmt.Sprintf("stash@{%d}", index))
	return err
}

// StashApply applies a stash without removing it.
func (r *Repo) StashApply(index int) error {
	_, err := r.exec("stash", "apply", fmt.Sprintf("stash@{%d}", index))
	return err
}

// StashDrop removes a stash entry.
func (r *Repo) StashDrop(index int) error {
	_, err := r.exec("stash", "drop", fmt.Sprintf("stash@{%d}", index))
	return err
}
```

**Step 4: Run tests**

Run: `go test ./internal/git/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/stash.go internal/git/stash_test.go
git commit -m "feat(git): add stash save, pop, apply, drop, list"
```

---

### Task 8: Git Blame

**Files:**
- Create: `internal/git/blame.go`

**Step 1: Write the implementation**

```go
// internal/git/blame.go
package git

import "strings"

// BlameLine represents a single line of blame output.
type BlameLine struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// Blame returns blame info for a file.
func (r *Repo) Blame(path string) ([]BlameLine, error) {
	out, err := r.exec("blame", "--porcelain", path)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	result := make([]BlameLine, 0)
	var current BlameLine
	lineNum := 0

	for _, line := range lines {
		if len(line) >= 40 && line[0] != '\t' && !strings.ContainsAny(line[:1], " \t") {
			// Commit header line: hash origLine finalLine [numLines]
			parts := strings.Fields(line)
			if len(parts) >= 3 && len(parts[0]) == 40 {
				current.Hash = parts[0][:7]
				lineNum++
				current.Line = lineNum
			}
		} else if strings.HasPrefix(line, "author ") {
			current.Author = strings.TrimPrefix(line, "author ")
		} else if strings.HasPrefix(line, "author-time ") {
			current.Date = strings.TrimPrefix(line, "author-time ")
		} else if strings.HasPrefix(line, "\t") {
			current.Content = line[1:]
			result = append(result, current)
			current = BlameLine{}
		}
	}

	return result, nil
}
```

**Step 2: Commit**

```bash
git add internal/git/blame.go
git commit -m "feat(git): add blame support"
```

---

### Task 9: Git Commit operation

**Files:**
- Modify: `internal/git/status.go` (add Commit function)

**Step 1: Add to status.go**

Add at the end of `internal/git/status.go`:

```go
// Commit creates a commit with the given message.
func (r *Repo) Commit(message string, amend bool) error {
	args := []string{"commit", "-m", message}
	if amend {
		args = append(args, "--amend")
	}
	_, err := r.exec(args...)
	return err
}
```

**Step 2: Add test to status_test.go**

```go
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
```

**Step 3: Run tests**

Run: `go test ./internal/git/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/git/status.go internal/git/status_test.go
git commit -m "feat(git): add commit operation"
```

---

### Task 10: Git API endpoints

**Files:**
- Create: `internal/httpapi/gitapi.go`
- Modify: `internal/httpapi/router.go` (add git routes + Repo dependency)

**Step 1: Create gitapi.go**

```go
// internal/httpapi/gitapi.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"go-webttyd/internal/git"
)

func (a *API) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	if !repo.IsRepo() {
		writeJSON(w, http.StatusOK, []git.FileStatus{})
		return
	}

	status, err := repo.Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if status == nil {
		status = []git.FileStatus{}
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) handleGitLog(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	repo := git.New(projectPath)
	commits, err := repo.Log(limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if commits == nil {
		commits = []git.Commit{}
	}
	writeJSON(w, http.StatusOK, commits)
}

func (a *API) handleGitBranches(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	branches, err := repo.Branches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if branches == nil {
		branches = []git.Branch{}
	}
	writeJSON(w, http.StatusOK, branches)
}

func (a *API) handleGitStage(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string   `json:"project"`
		Paths   []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	if err := repo.Stage(req.Paths); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGitUnstage(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string   `json:"project"`
		Paths   []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	if err := repo.Unstage(req.Paths); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Message string `json:"message"`
		Amend   bool   `json:"amend"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Message == "" && !req.Amend {
		http.Error(w, "commit message required", http.StatusBadRequest)
		return
	}

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	if err := repo.Commit(req.Message, req.Amend); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGitPush(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Remote  string `json:"remote"`
		Branch  string `json:"branch"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	output, err := repo.Push(req.Remote, req.Branch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": output})
}

func (a *API) handleGitPull(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Remote  string `json:"remote"`
		Branch  string `json:"branch"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	output, err := repo.Pull(req.Remote, req.Branch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": output})
}

func (a *API) handleGitBranch(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Action  string `json:"action"` // "create", "delete", "switch"
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	var err error
	switch req.Action {
	case "create":
		err = repo.BranchCreate(req.Name)
	case "delete":
		err = repo.BranchDelete(req.Name)
	case "switch":
		err = repo.BranchSwitch(req.Name)
	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGitMerge(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Branch  string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	if err := repo.Merge(req.Branch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGitStash(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Action  string `json:"action"` // "save", "pop", "apply", "drop", "list"
		Index   int    `json:"index"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)

	switch req.Action {
	case "list":
		stashes, err := repo.StashList()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if stashes == nil {
			stashes = []git.Stash{}
		}
		writeJSON(w, http.StatusOK, stashes)
		return
	case "save":
		if err := repo.StashSave(req.Message); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "pop":
		if err := repo.StashPop(req.Index); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "apply":
		if err := repo.StashApply(req.Index); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "drop":
		if err := repo.StashDrop(req.Index); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "invalid stash action", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	repo := git.New(projectPath)
	diff, err := repo.Diff(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}

func (a *API) handleGitDiffLines(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	repo := git.New(projectPath)
	changes, err := repo.DiffLines(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if changes == nil {
		changes = []git.GutterChange{}
	}
	writeJSON(w, http.StatusOK, changes)
}

func (a *API) handleGitBlame(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	repo := git.New(projectPath)
	blame, err := repo.Blame(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if blame == nil {
		blame = []git.BlameLine{}
	}
	writeJSON(w, http.StatusOK, blame)
}

func (a *API) handleGitDiscard(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string   `json:"project"`
		Paths   []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	projectPath := req.Project
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}

	repo := git.New(projectPath)
	if err := repo.Discard(req.Paths); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGitFileAtHEAD(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		projectPath = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	repo := git.New(projectPath)
	content, err := repo.FileAtHEAD(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (a *API) requireFullMode(w http.ResponseWriter) bool {
	if a.mode != "full" {
		http.Error(w, "IDE mode required", http.StatusForbidden)
		return false
	}
	return true
}

func queryInt(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	n := 0
	for _, c := range val {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n == 0 {
		return defaultVal
	}
	return n
}
```

**Step 2: Register routes in router.go**

Add to the `Handler()` method in `internal/httpapi/router.go`, after the existing file routes:

```go
	// Git routes
	mux.HandleFunc("/api/git/status", a.handleGitStatus)
	mux.HandleFunc("/api/git/log", a.handleGitLog)
	mux.HandleFunc("/api/git/branches", a.handleGitBranches)
	mux.HandleFunc("/api/git/stage", a.handleGitStage)
	mux.HandleFunc("/api/git/unstage", a.handleGitUnstage)
	mux.HandleFunc("/api/git/commit", a.handleGitCommit)
	mux.HandleFunc("/api/git/push", a.handleGitPush)
	mux.HandleFunc("/api/git/pull", a.handleGitPull)
	mux.HandleFunc("/api/git/branch", a.handleGitBranch)
	mux.HandleFunc("/api/git/merge", a.handleGitMerge)
	mux.HandleFunc("/api/git/stash", a.handleGitStash)
	mux.HandleFunc("/api/git/diff", a.handleGitDiff)
	mux.HandleFunc("/api/git/diff-lines", a.handleGitDiffLines)
	mux.HandleFunc("/api/git/blame", a.handleGitBlame)
	mux.HandleFunc("/api/git/discard", a.handleGitDiscard)
	mux.HandleFunc("/api/git/file-at-head", a.handleGitFileAtHEAD)
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: Success

**Step 4: Commit**

```bash
git add internal/httpapi/gitapi.go internal/httpapi/router.go
git commit -m "feat(api): add all git API endpoints"
```

---

### Task 11: Frontend Types — extend with Git types

**Files:**
- Modify: `frontend/src/types.ts`

**Step 1: Add git types at end of file**

```typescript
// Git types
export type GitFileStatus = {
  path: string;
  status: 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked';
  staged: boolean;
};

export type GitBranch = {
  name: string;
  current: boolean;
  remote?: string;
  ahead: number;
  behind: number;
};

export type GitCommit = {
  hash: string;
  shortHash: string;
  message: string;
  author: string;
  date: string;
  relativeDate: string;
};

export type GitStash = {
  index: number;
  message: string;
};

export type GutterChange = {
  startLine: number;
  endLine: number;
  type: 'added' | 'modified' | 'deleted';
};
```

Also update `ActivePanel`:

```typescript
export type ActivePanel = 'explorer' | 'search' | 'projects' | 'terminal' | 'git';
```

**Step 2: Verify typecheck**

Run: `npm run typecheck`
Expected: PASS

**Step 3: Commit**

```bash
git add frontend/src/types.ts
git commit -m "feat(types): add git types and extend ActivePanel"
```

---

### Task 12: Frontend API — add git API functions

**Files:**
- Modify: `frontend/src/api.ts`

**Step 1: Add git API functions at end of file**

```typescript
import type { ..., GitFileStatus, GitBranch, GitCommit, GitStash, GutterChange } from './types';

// Git API
export async function getGitStatus(project: string): Promise<GitFileStatus[]> {
  const response = await fetch(`/api/git/status?project=${encodeURIComponent(project)}`, { credentials: 'include' });
  return parseResponse<GitFileStatus[]>(response);
}

export async function getGitLog(project: string, limit = 20, offset = 0): Promise<GitCommit[]> {
  const params = new URLSearchParams({ project, limit: String(limit), offset: String(offset) });
  const response = await fetch(`/api/git/log?${params}`, { credentials: 'include' });
  return parseResponse<GitCommit[]>(response);
}

export async function getGitBranches(project: string): Promise<GitBranch[]> {
  const response = await fetch(`/api/git/branches?project=${encodeURIComponent(project)}`, { credentials: 'include' });
  return parseResponse<GitBranch[]>(response);
}

export async function gitStage(project: string, paths: string[]): Promise<void> {
  const response = await fetch('/api/git/stage', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, paths }),
  });
  if (!response.ok) throw new Error(await response.text());
}

export async function gitUnstage(project: string, paths: string[]): Promise<void> {
  const response = await fetch('/api/git/unstage', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, paths }),
  });
  if (!response.ok) throw new Error(await response.text());
}

export async function gitCommit(project: string, message: string, amend = false): Promise<void> {
  const response = await fetch('/api/git/commit', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, message, amend }),
  });
  if (!response.ok) throw new Error(await response.text());
}

export async function gitPush(project: string, remote?: string, branch?: string): Promise<string> {
  const response = await fetch('/api/git/push', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, remote, branch }),
  });
  const data = await parseResponse<{ output: string }>(response);
  return data.output;
}

export async function gitPull(project: string, remote?: string, branch?: string): Promise<string> {
  const response = await fetch('/api/git/pull', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, remote, branch }),
  });
  const data = await parseResponse<{ output: string }>(response);
  return data.output;
}

export async function gitBranchAction(project: string, action: 'create' | 'delete' | 'switch', name: string): Promise<void> {
  const response = await fetch('/api/git/branch', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, action, name }),
  });
  if (!response.ok) throw new Error(await response.text());
}

export async function gitMerge(project: string, branch: string): Promise<void> {
  const response = await fetch('/api/git/merge', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, branch }),
  });
  if (!response.ok) throw new Error(await response.text());
}

export async function gitStash(project: string, action: string, index?: number, message?: string): Promise<GitStash[] | void> {
  const response = await fetch('/api/git/stash', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, action, index, message }),
  });
  if (action === 'list') return parseResponse<GitStash[]>(response);
  if (!response.ok) throw new Error(await response.text());
}

export async function getGitDiff(project: string, path: string): Promise<string> {
  const params = new URLSearchParams({ project, path });
  const response = await fetch(`/api/git/diff?${params}`, { credentials: 'include' });
  const data = await parseResponse<{ diff: string }>(response);
  return data.diff;
}

export async function getGitDiffLines(project: string, path: string): Promise<GutterChange[]> {
  const params = new URLSearchParams({ project, path });
  const response = await fetch(`/api/git/diff-lines?${params}`, { credentials: 'include' });
  return parseResponse<GutterChange[]>(response);
}

export async function getGitFileAtHEAD(project: string, path: string): Promise<string> {
  const params = new URLSearchParams({ project, path });
  const response = await fetch(`/api/git/file-at-head?${params}`, { credentials: 'include' });
  const data = await parseResponse<{ content: string }>(response);
  return data.content;
}

export async function gitDiscard(project: string, paths: string[]): Promise<void> {
  const response = await fetch('/api/git/discard', {
    method: 'POST', credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project, paths }),
  });
  if (!response.ok) throw new Error(await response.text());
}
```

**Step 2: Verify typecheck**

Run: `npm run typecheck`
Expected: PASS

**Step 3: Commit**

```bash
git add frontend/src/api.ts
git commit -m "feat(api): add frontend git API client functions"
```

---

### Task 13: Git Zustand Store

**Files:**
- Create: `frontend/src/stores/git.ts`

**Step 1: Create the store**

```typescript
// frontend/src/stores/git.ts
import { create } from 'zustand';
import type { GitBranch, GitCommit, GitFileStatus, GitStash, GutterChange } from '../types';
import { getGitBranches, getGitDiffLines, getGitLog, getGitStatus, gitStash } from '../api';

type GitState = {
  status: GitFileStatus[];
  branches: GitBranch[];
  commits: GitCommit[];
  stashes: GitStash[];
  gutterChanges: Record<string, GutterChange[]>; // keyed by file path
  loading: boolean;
  error: string | null;

  refresh: (projectPath: string) => Promise<void>;
  refreshGutter: (projectPath: string, filePath: string) => Promise<void>;
  loadMoreCommits: (projectPath: string) => Promise<void>;
  refreshStashes: (projectPath: string) => Promise<void>;
  clearError: () => void;
};

export const useGitStore = create<GitState>((set, get) => ({
  status: [],
  branches: [],
  commits: [],
  stashes: [],
  gutterChanges: {},
  loading: false,
  error: null,

  refresh: async (projectPath) => {
    set({ loading: true, error: null });
    try {
      const [status, branches, commits] = await Promise.all([
        getGitStatus(projectPath),
        getGitBranches(projectPath),
        getGitLog(projectPath, 20, 0),
      ]);
      const stashes = (await gitStash(projectPath, 'list')) as GitStash[] ?? [];
      set({ status, branches, commits, stashes, loading: false });
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Git refresh failed', loading: false });
    }
  },

  refreshGutter: async (projectPath, filePath) => {
    try {
      const changes = await getGitDiffLines(projectPath, filePath);
      set((state) => ({
        gutterChanges: { ...state.gutterChanges, [filePath]: changes },
      }));
    } catch {
      // Silently fail for gutter — non-critical
    }
  },

  loadMoreCommits: async (projectPath) => {
    const current = get().commits;
    try {
      const more = await getGitLog(projectPath, 20, current.length);
      set({ commits: [...current, ...more] });
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Failed to load commits' });
    }
  },

  refreshStashes: async (projectPath) => {
    try {
      const stashes = (await gitStash(projectPath, 'list')) as GitStash[] ?? [];
      set({ stashes });
    } catch {
      // silent
    }
  },

  clearError: () => set({ error: null }),
}));
```

**Step 2: Verify typecheck**

Run: `npm run typecheck`
Expected: PASS

**Step 3: Commit**

```bash
git add frontend/src/stores/git.ts
git commit -m "feat(store): add git zustand store with refresh and gutter support"
```

---

### Task 14: useGitStatus hook — polling + on-save refresh

**Files:**
- Create: `frontend/src/hooks/useGitStatus.ts`

**Step 1: Create the hook**

```typescript
// frontend/src/hooks/useGitStatus.ts
import { useCallback, useEffect, useRef } from 'react';
import { useGitStore } from '../stores/git';

const POLL_INTERVAL = 5000; // 5 seconds
const DEBOUNCE_MS = 1000;

export function useGitStatus(projectPath: string | null) {
  const refresh = useGitStore((s) => s.refresh);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const debouncedRefresh = useCallback(() => {
    if (!projectPath) return;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      refresh(projectPath);
    }, DEBOUNCE_MS);
  }, [projectPath, refresh]);

  // Periodic polling
  useEffect(() => {
    if (!projectPath) return;

    refresh(projectPath); // initial fetch

    intervalRef.current = setInterval(() => {
      refresh(projectPath);
    }, POLL_INTERVAL);

    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [projectPath, refresh]);

  // Return trigger for on-save
  return { triggerRefresh: debouncedRefresh };
}
```

**Step 2: Verify typecheck**

Run: `npm run typecheck`
Expected: PASS

**Step 3: Commit**

```bash
git add frontend/src/hooks/useGitStatus.ts
git commit -m "feat(hooks): add useGitStatus with polling and debounced refresh"
```

---

### Task 15: useGitGutter hook — line diff for active file

**Files:**
- Create: `frontend/src/hooks/useGitGutter.ts`

**Step 1: Create the hook**

```typescript
// frontend/src/hooks/useGitGutter.ts
import { useEffect } from 'react';
import { useGitStore } from '../stores/git';
import type { GutterChange } from '../types';

export function useGitGutter(projectPath: string | null, filePath: string | null): GutterChange[] {
  const refreshGutter = useGitStore((s) => s.refreshGutter);
  const gutterChanges = useGitStore((s) => s.gutterChanges);

  useEffect(() => {
    if (!projectPath || !filePath) return;
    refreshGutter(projectPath, filePath);
  }, [projectPath, filePath, refreshGutter]);

  if (!filePath) return [];
  return gutterChanges[filePath] ?? [];
}
```

**Step 2: Commit**

```bash
git add frontend/src/hooks/useGitGutter.ts
git commit -m "feat(hooks): add useGitGutter for editor line decorations"
```

---

### Task 16: ActivityBar — add Git icon

**Files:**
- Modify: `frontend/src/components/sidebar/ActivityBar.tsx`

**Step 1: Add git panel to panels array**

Add `{ id: 'git', icon: '🌿', label: 'Source Control' }` to the `panels` array.

**Step 2: Verify typecheck**

Run: `npm run typecheck`
Expected: PASS

**Step 3: Commit**

```bash
git add frontend/src/components/sidebar/ActivityBar.tsx
git commit -m "feat(ui): add git icon to activity bar"
```

---

### Task 17: GitPanel component — main Source Control sidebar

**Files:**
- Create: `frontend/src/components/git/GitPanel.tsx`
- Create: `frontend/src/components/git/GitStatusList.tsx`
- Create: `frontend/src/components/git/GitCommitBox.tsx`
- Create: `frontend/src/components/git/GitLogView.tsx`
- Create: `frontend/src/components/git/GitBranchPicker.tsx`
- Create: `frontend/src/components/git/GitStashPanel.tsx`

This is a large task. Each component should be created following the design spec wireframe. Key behaviors:

- **GitPanel.tsx**: Container that renders GitBranchPicker, GitCommitBox, GitStatusList (staged + unstaged sections), GitStashPanel, GitLogView
- **GitStatusList.tsx**: Renders file list with status icons (M/A/D/?), stage/unstage buttons
- **GitCommitBox.tsx**: Textarea for commit message + Commit button + "More" dropdown
- **GitLogView.tsx**: Scrollable commit list with "Load more" button
- **GitBranchPicker.tsx**: Current branch display + dropdown for switch/create/delete
- **GitStashPanel.tsx**: Collapsible stash list with pop/apply/drop actions

**Step 1: Create all components** (implementation details in code — follow the wireframe from spec section 2.4)

**Step 2: Wire GitPanel into IDEWorkspace sidebar**

In `IDEWorkspace.tsx`, add:
```tsx
{activePanel === 'git' && activeProject && (
  <GitPanel projectPath={activeProject.path} />
)}
```

**Step 3: Verify typecheck + build**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 4: Commit**

```bash
git add frontend/src/components/git/
git commit -m "feat(ui): add git panel with status, commit, branches, log, stash"
```

---

### Task 18: GitDiffView — Monaco diff editor tab

**Files:**
- Create: `frontend/src/components/git/GitDiffView.tsx`
- Modify: `frontend/src/components/editor/EditorArea.tsx` (support diff tabs)

**Step 1: Create GitDiffView using Monaco DiffEditor**

Uses `@monaco-editor/react`'s `DiffEditor` component. Left = HEAD content (read-only), Right = working content.

**Step 2: Extend EditorArea to handle diff tab type**

Add a `diffTab` concept — when user clicks a file in GitStatusList, open a diff tab instead of regular file tab.

**Step 3: Verify build**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 4: Commit**

```bash
git add frontend/src/components/git/GitDiffView.tsx frontend/src/components/editor/EditorArea.tsx
git commit -m "feat(ui): add git diff view with Monaco DiffEditor"
```

---

### Task 19: Git Gutter decorations in MonacoEditor

**Files:**
- Modify: `frontend/src/components/editor/MonacoEditor.tsx`

**Step 1: Add gutter decorations**

After Monaco editor mounts, use `useGitGutter` hook to get changes, then apply decorations via `editor.deltaDecorations()`:

```typescript
// Inside MonacoEditor component
const gutterChanges = useGitGutter(projectPath, filePath);

useEffect(() => {
  if (!editorRef.current) return;
  const decorations = gutterChanges.map((change) => ({
    range: new monaco.Range(change.startLine, 1, change.endLine, 1),
    options: {
      isWholeLine: true,
      linesDecorationsClassName: `git-gutter-${change.type}`,
    },
  }));
  decorationIds.current = editorRef.current.deltaDecorations(
    decorationIds.current,
    decorations
  );
}, [gutterChanges]);
```

**Step 2: Add CSS for gutter decorations**

In `ide.css`:
```css
.git-gutter-added {
  background: #a6e3a1;
  width: 3px !important;
  margin-left: 3px;
}
.git-gutter-modified {
  background: #89b4fa;
  width: 3px !important;
  margin-left: 3px;
}
.git-gutter-deleted {
  background: #f38ba8;
  width: 3px !important;
  margin-left: 3px;
}
```

**Step 3: Verify build**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 4: Commit**

```bash
git add frontend/src/components/editor/MonacoEditor.tsx frontend/src/styles/ide.css
git commit -m "feat(editor): add git gutter decorations (green/blue/red)"
```

---

### Task 20: Integration — wire useGitStatus into IDEWorkspace

**Files:**
- Modify: `frontend/src/apps/ide/IDEWorkspace.tsx`

**Step 1: Add useGitStatus hook call**

```typescript
const { triggerRefresh } = useGitStatus(activeProject?.path ?? null);
```

Wire `triggerRefresh` to fire after file save operations.

**Step 2: Add keyboard shortcut Ctrl+Shift+G**

```typescript
if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'G') {
  e.preventDefault();
  setActivePanel('git');
  if (!sidebarVisible) toggleSidebar();
}
```

**Step 3: Verify build**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 4: Commit**

```bash
git add frontend/src/apps/ide/IDEWorkspace.tsx
git commit -m "feat(ide): wire git status polling and Ctrl+Shift+G shortcut"
```

---

### Task 21: Phase 1 verification

**Step 1: Run all Go tests**

Run: `go test ./...`
Expected: PASS

**Step 2: Run frontend checks**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 3: Manual smoke test**

1. Start server: `go run ./cmd/server`
2. Open IDE mode in browser
3. Verify: Git icon in activity bar
4. Verify: Click git icon shows Source Control panel
5. Verify: Status shows modified/untracked files
6. Verify: Can stage/unstage files
7. Verify: Can commit
8. Verify: Git gutter shows colored bars in editor
9. Verify: Click file in status opens diff view

---

## Phase 2: Chat Backend + Chat UI

---

### Task 22: Chat Agent Discovery

**Files:**
- Create: `internal/chat/agents.go`
- Create: `internal/chat/agents_test.go`

**Step 1: Write the test**

```go
// internal/chat/agents_test.go
package chat

import "testing"

func TestDiscoverAgents(t *testing.T) {
	// Test with mock lookPath
	agents := discoverAgents(func(name string) (string, error) {
		if name == "opencode" {
			return "/usr/bin/opencode", nil
		}
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	})

	if len(agents) == 0 {
		t.Fatal("expected at least one agent when opencode is available")
	}
	if agents[0].ID != "opencode" {
		t.Errorf("expected opencode, got %s", agents[0].ID)
	}
}
```

**Step 2: Write the implementation**

```go
// internal/chat/agents.go
package chat

import "os/exec"

// Agent represents a CLI agent that can be harnessed.
type Agent struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Available bool   `json:"available"`
}

var knownAgents = []Agent{
	{ID: "opencode", Label: "OpenCode", Command: "opencode", Args: []string{}},
	{ID: "claude", Label: "Claude Code", Command: "claude", Args: []string{}},
}

// DiscoverAgents finds available CLI agents on the system.
func DiscoverAgents() []Agent {
	return discoverAgents(exec.LookPath)
}

func discoverAgents(lookPath func(string) (string, error)) []Agent {
	result := make([]Agent, 0, len(knownAgents))
	for _, agent := range knownAgents {
		a := agent
		if path, err := lookPath(agent.Command); err == nil {
			a.Command = path
			a.Available = true
		}
		result = append(result, a)
	}
	return result
}
```

**Step 3: Run test, commit**

```bash
git add internal/chat/
git commit -m "feat(chat): add agent discovery for opencode and claude"
```

---

### Task 23: Chat Session Manager

**Files:**
- Create: `internal/chat/manager.go`
- Create: `internal/chat/session.go`

**Step 1: Implement session manager**

Manager spawns agent as PTY subprocess (reuse go-pty pattern from `internal/terminal/session.go`). Each session has:
- PTY process (stdin/stdout)
- Session ID (UUID)
- Agent reference
- Read/Write/Close methods

**Step 2: Implement session with PTY I/O**

Session wraps the PTY, provides:
- `Write(input string)` — send to agent stdin
- `Read() <-chan string` — channel streaming agent stdout
- `Close()` — kill process
- `Reset()` — kill and respawn (for "New Chat")

**Step 3: Test + commit**

```bash
git add internal/chat/manager.go internal/chat/session.go
git commit -m "feat(chat): add session manager with PTY-based agent harness"
```

---

### Task 24: Chat Output Parser

**Files:**
- Create: `internal/chat/parser.go`

**Step 1: Implement parser**

- Strip ANSI escape codes (regex: `\x1b\[[0-9;]*[a-zA-Z]`)
- Detect message boundaries (newline patterns, prompt patterns)
- Pass through markdown content
- Detect file edit patterns → emit `agent_action` events

**Step 2: Commit**

```bash
git add internal/chat/parser.go
git commit -m "feat(chat): add output parser with ANSI stripping"
```

---

### Task 25: Chat WebSocket API

**Files:**
- Create: `internal/httpapi/chatapi.go`
- Modify: `internal/httpapi/router.go` (add chat routes + ChatManager dependency)

**Step 1: Implement chatapi.go**

Endpoints:
- `GET /api/chat/agents` — return discovered agents
- `POST /api/chat/sessions` — create session, spawn agent
- `DELETE /api/chat/sessions/:id` — kill session
- `GET /api/chat/sessions` — list active sessions
- `WS /ws/chat/:sessionId` — bidirectional WebSocket

WebSocket handler:
- Read client messages (message, message_with_context, new_chat, cancel)
- Write to agent PTY stdin
- Read agent PTY stdout → parse → stream to client
- Handle `new_chat` → session.Reset()
- Handle `cancel` → send SIGINT to agent process

**Step 2: Register routes**

**Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/httpapi/chatapi.go internal/httpapi/router.go
git commit -m "feat(api): add chat WebSocket and REST endpoints"
```

---

### Task 26: Frontend Chat Types & API

**Files:**
- Modify: `frontend/src/types.ts` (add chat types)
- Modify: `frontend/src/api.ts` (add chat API functions)

**Step 1: Add types**

```typescript
export type ChatAgent = {
  id: string;
  label: string;
  available: boolean;
};

export type CodeContext = {
  filePath: string;
  startLine: number;
  endLine: number;
  selectedCode: string;
  language: string;
};

export type ChatMessage = {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  context?: CodeContext;
  timestamp: number;
};

export type ChatSession = {
  id: string;
  agentId: string;
  title: string;
  messages: ChatMessage[];
  status: 'idle' | 'streaming' | 'error';
  createdAt: number;
};
```

**Step 2: Add API functions**

```typescript
export async function getChatAgents(): Promise<ChatAgent[]> { ... }
export async function createChatSession(agentId: string): Promise<{ id: string }> { ... }
export async function deleteChatSession(id: string): Promise<void> { ... }
export function createChatWebSocket(sessionId: string): WebSocket { ... }
```

**Step 3: Commit**

```bash
git add frontend/src/types.ts frontend/src/api.ts
git commit -m "feat(types): add chat types and API client"
```

---

### Task 27: Chat Zustand Store

**Files:**
- Create: `frontend/src/stores/chat.ts`

**Step 1: Create store**

State: sessions, activeSessionId, activeAgentId, agents, chatVisible.
Actions: createSession, sendMessage, receiveStream, resetSession, toggleChat, setAgent.

**Step 2: Commit**

```bash
git add frontend/src/stores/chat.ts
git commit -m "feat(store): add chat zustand store"
```

---

### Task 28: useChatSession hook

**Files:**
- Create: `frontend/src/hooks/useChatSession.ts`

**Step 1: Create hook**

Manages WebSocket connection lifecycle:
- Connect on session create
- Handle incoming messages (stream, stream_end, agent_action, error, session_reset)
- Provide `sendMessage(content, context?)` and `cancel()` functions
- Auto-reconnect on disconnect

**Step 2: Commit**

```bash
git add frontend/src/hooks/useChatSession.ts
git commit -m "feat(hooks): add useChatSession WebSocket hook"
```

---

### Task 29: ChatPanel component (right side panel)

**Files:**
- Create: `frontend/src/components/chat/ChatPanel.tsx`
- Create: `frontend/src/components/chat/ChatMessage.tsx`
- Create: `frontend/src/components/chat/ChatInput.tsx`
- Create: `frontend/src/components/chat/ChatSessionList.tsx`
- Create: `frontend/src/components/chat/AgentPicker.tsx`

**Step 1: Create all components**

Follow the wireframe from spec section 3.6:
- **ChatPanel**: Container with header (agent picker, new chat, sessions), message list, input
- **ChatMessage**: Renders markdown content, code blocks with syntax highlighting, code context (collapsible)
- **ChatInput**: Auto-resize textarea, Send button, Stop button (during streaming)
- **ChatSessionList**: Dropdown listing sessions with timestamps
- **AgentPicker**: Dropdown to select agent (OpenCode / Claude)

Note: For markdown rendering, use a lightweight approach — split by code fences, render code blocks with `<pre><code>`, rest as paragraphs. No heavy markdown library needed initially.

**Step 2: Commit**

```bash
git add frontend/src/components/chat/
git commit -m "feat(ui): add chat panel with messages, input, agent picker"
```

---

### Task 30: InlinePrompt component (code selection)

**Files:**
- Create: `frontend/src/components/chat/InlinePrompt.tsx`
- Modify: `frontend/src/components/editor/MonacoEditor.tsx` (add selection listener)

**Step 1: Create InlinePrompt**

Floating widget that appears below code selection after 500ms:
- Input field: "Ask about selection..."
- Quick action buttons: Explain, Refactor, Write Tests, Fix Bug
- On action → dispatch to chat store with CodeContext

**Step 2: Add selection listener to MonacoEditor**

Listen to `editor.onDidChangeCursorSelection`. After 500ms of stable selection (>0 chars), show InlinePrompt positioned below selection.

**Step 3: Commit**

```bash
git add frontend/src/components/chat/InlinePrompt.tsx frontend/src/components/editor/MonacoEditor.tsx
git commit -m "feat(ui): add inline prompt on code selection"
```

---

### Task 31: Wire ChatPanel into IDEWorkspace

**Files:**
- Modify: `frontend/src/apps/ide/IDEWorkspace.tsx`
- Modify: `frontend/src/stores/workspace.ts` (add chatVisible)
- Modify: `frontend/src/styles/ide.css` (add chat panel styles)

**Step 1: Add chatVisible to workspace store**

**Step 2: Add chat panel as right-side resizable panel in IDEWorkspace**

```tsx
{chatVisible && (
  <>
    <Separator className="resize-handle-h" style={{ cursor: 'col-resize' }} />
    <Panel defaultSize="25%" minSize="15%" maxSize="40%" className="ide-chat-area">
      <ChatPanel />
    </Panel>
  </>
)}
```

**Step 3: Add Ctrl+Shift+L shortcut**

**Step 4: Add CSS for chat panel**

**Step 5: Verify build**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 6: Commit**

```bash
git add frontend/src/apps/ide/IDEWorkspace.tsx frontend/src/stores/workspace.ts frontend/src/styles/ide.css
git commit -m "feat(ide): wire chat panel as right-side resizable panel"
```

---

### Task 32: Phase 2 verification

Same pattern as Task 21 — run all tests, typecheck, build, manual smoke test for chat functionality.

---

## Phase 3: Responsive Layout

---

### Task 33: useLayoutMode hook

**Files:**
- Create: `frontend/src/hooks/useLayoutMode.ts`

**Step 1: Create hook**

```typescript
// frontend/src/hooks/useLayoutMode.ts
import { useEffect, useState } from 'react';

export type LayoutMode = 'desktop' | 'tablet' | 'mobile';

const BP_DESKTOP = 1024;
const BP_TABLET = 768;

function getMode(width: number): LayoutMode {
  if (width >= BP_DESKTOP) return 'desktop';
  if (width >= BP_TABLET) return 'tablet';
  return 'mobile';
}

export function useLayoutMode(): LayoutMode {
  const [mode, setMode] = useState<LayoutMode>(() => getMode(window.innerWidth));

  useEffect(() => {
    const mql1 = window.matchMedia(`(min-width: ${BP_DESKTOP}px)`);
    const mql2 = window.matchMedia(`(min-width: ${BP_TABLET}px)`);

    function update() {
      setMode(getMode(window.innerWidth));
    }

    mql1.addEventListener('change', update);
    mql2.addEventListener('change', update);
    return () => {
      mql1.removeEventListener('change', update);
      mql2.removeEventListener('change', update);
    };
  }, []);

  return mode;
}
```

**Step 2: Commit**

```bash
git add frontend/src/hooks/useLayoutMode.ts
git commit -m "feat(hooks): add useLayoutMode responsive breakpoint hook"
```

---

### Task 34: Tablet layout — overlay sidebar & chat

**Files:**
- Modify: `frontend/src/apps/ide/IDEWorkspace.tsx`
- Modify: `frontend/src/styles/ide.css`

**Step 1: Conditionally render overlays in tablet mode**

When `layoutMode === 'tablet'`:
- Sidebar renders as fixed overlay (slide-in from left) with backdrop
- Chat renders as fixed overlay (slide-in from right) with backdrop
- Activity bar remains inline
- Editor + Terminal get full width

**Step 2: Add CSS for overlays**

```css
.sidebar-overlay {
  position: fixed;
  top: 0;
  left: 48px;
  bottom: 0;
  width: 300px;
  z-index: 100;
  background: var(--sidebar-bg);
  transform: translateX(-100%);
  transition: transform 0.2s ease;
}
.sidebar-overlay.open {
  transform: translateX(0);
}
.chat-overlay {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: 320px;
  z-index: 100;
  background: var(--sidebar-bg);
  transform: translateX(100%);
  transition: transform 0.2s ease;
}
.chat-overlay.open {
  transform: translateX(0);
}
.overlay-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.4);
  z-index: 99;
}
```

**Step 3: Add touch swipe detection**

Swipe from left edge → open sidebar. Swipe from right edge → open chat.

**Step 4: Verify build**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/apps/ide/IDEWorkspace.tsx frontend/src/styles/ide.css
git commit -m "feat(responsive): add tablet overlay layout for sidebar and chat"
```

---

### Task 35: Mobile layout — single panel + bottom nav

**Files:**
- Create: `frontend/src/components/shared/BottomNav.tsx`
- Create: `frontend/src/components/shared/MobileLayout.tsx`
- Modify: `frontend/src/apps/ide/IDEWorkspace.tsx`
- Modify: `frontend/src/styles/ide.css`

**Step 1: Create BottomNav**

```typescript
// 5 icons: Explorer | Git | Editor | Terminal | Chat
// Highlights active panel
// Fixed at bottom, 56px height
```

**Step 2: Create MobileLayout**

Renders only the active panel at full screen. Switches based on bottom nav selection.

**Step 3: Conditionally render in IDEWorkspace**

When `layoutMode === 'mobile'`, render `<MobileLayout />` instead of the desktop panel group.

**Step 4: Add CSS**

```css
.bottom-nav {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  height: 56px;
  display: flex;
  background: var(--activity-bg);
  border-top: 1px solid var(--border-color);
  z-index: 200;
}
.bottom-nav-btn {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--activity-fg);
  font-size: 0.7rem;
  gap: 2px;
}
.bottom-nav-btn.active {
  color: var(--activity-active);
}
.mobile-panel {
  height: calc(100vh - 56px);
  overflow: hidden;
}
```

**Step 5: Verify build**

Run: `npm run typecheck && npm run build`
Expected: PASS

**Step 6: Commit**

```bash
git add frontend/src/components/shared/ frontend/src/apps/ide/IDEWorkspace.tsx frontend/src/styles/ide.css
git commit -m "feat(responsive): add mobile single-panel layout with bottom nav"
```

---

### Task 36: Mobile touch interactions

**Files:**
- Modify: `frontend/src/components/editor/MonacoEditor.tsx` (long-press for inline prompt)
- Modify: `frontend/src/components/git/GitPanel.tsx` (pull-to-refresh)

**Step 1: Long-press on mobile**

Replace hover-based inline prompt trigger with long-press (500ms touch hold) on mobile.

**Step 2: Pull-to-refresh in git panel**

Detect pull-down gesture at top of git panel → trigger refresh.

**Step 3: Commit**

```bash
git add frontend/src/components/editor/MonacoEditor.tsx frontend/src/components/git/GitPanel.tsx
git commit -m "feat(responsive): add mobile touch interactions (long-press, pull-to-refresh)"
```

---

### Task 37: Phase 3 verification

**Step 1: Run all tests**

Run: `go test ./... && npm run typecheck && npm run build`
Expected: PASS

**Step 2: Manual responsive testing**

1. Desktop (>1024px): All panels visible, resizable
2. Resize to tablet (768-1023px): Sidebar/chat become overlays
3. Resize to mobile (<768px): Single panel + bottom nav
4. Test touch interactions on mobile viewport

**Step 3: Final commit**

```bash
git commit -m "feat: complete responsive layout implementation"
```

---

## Summary

| Phase | Tasks | Key Deliverables |
|-------|-------|-----------------|
| Phase 1 | Tasks 1–21 | Git backend, Git panel, Git gutter, Diff view |
| Phase 2 | Tasks 22–32 | Chat agent harness, Chat panel, Inline prompt |
| Phase 3 | Tasks 33–37 | Responsive breakpoints, Mobile layout, Touch |

Total: **37 tasks** across 3 phases.
