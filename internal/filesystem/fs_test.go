package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestListDirectoryReturnsEntries(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0644)
	_ = os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	entries, err := ListDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names["hello.txt"] || !names["subdir"] {
		t.Fatalf("expected hello.txt and subdir, got %v", entries)
	}
}

func TestListDirectoryDistinguishesTypes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0644)
	_ = os.Mkdir(filepath.Join(dir, "folder"), 0755)

	entries, err := ListDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, e := range entries {
		if e.Name == "file.txt" && e.Type != "file" {
			t.Fatalf("expected type 'file' for file.txt, got %q", e.Type)
		}
		if e.Name == "folder" && e.Type != "dir" {
			t.Fatalf("expected type 'dir' for folder, got %q", e.Type)
		}
	}
}

func TestListDirectoryReturnsErrorForNonexistent(t *testing.T) {
	_, err := ListDirectory(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestReadFileReturnsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(path, []byte("hello world"), 0644)

	result, err := ReadFile(path, 10*1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result.Content)
	}
}

func TestWriteFileCreatesAndWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	err := WriteFile(path, "new content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Fatalf("expected 'new content', got %q", string(data))
	}
}

func TestCreateFileCreatesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	err := CreateEntry(path, "file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected file, got directory")
	}
}

func TestCreateDirCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "newdir")

	err := CreateEntry(path, "dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory, got file")
	}
}

func TestDeleteEntryRemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doomed.txt")
	_ = os.WriteFile(path, []byte("bye"), 0644)

	err := DeleteEntry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should have been deleted")
	}
}

func TestRenameEntryMovesFile(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	_ = os.WriteFile(old, []byte("data"), 0644)

	err := RenameEntry(old, newPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old file should not exist")
	}
	data, _ := os.ReadFile(newPath)
	if string(data) != "data" {
		t.Fatalf("expected 'data', got %q", string(data))
	}
}

func TestCopyEntryCopiesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	_ = os.WriteFile(src, []byte("copy me"), 0644)

	err := CopyEntry(src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcData, _ := os.ReadFile(src)
	dstData, _ := os.ReadFile(dst)
	if string(srcData) != string(dstData) {
		t.Fatal("copy content mismatch")
	}
}

func TestReadFileReturnsVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.txt")
	_ = os.WriteFile(path, []byte("hello"), 0644)

	first, err := ReadFile(path, 1024)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.HasPrefix(first.Version, "sha256:") {
		t.Fatalf("expected sha256 prefixed version, got %q", first.Version)
	}

	// Re-read returns the same version when content unchanged.
	second, err := ReadFile(path, 1024)
	if err != nil {
		t.Fatalf("re-read failed: %v", err)
	}
	if first.Version != second.Version {
		t.Fatalf("expected stable version, got %q vs %q", first.Version, second.Version)
	}

	// Modify the file: version must change.
	if err := os.WriteFile(path, []byte("changed"), 0644); err != nil {
		t.Fatalf("modify failed: %v", err)
	}
	third, err := ReadFile(path, 1024)
	if err != nil {
		t.Fatalf("post-modify read failed: %v", err)
	}
	if first.Version == third.Version {
		t.Errorf("expected version change after modification, got same %q", third.Version)
	}
}

func TestWriteFileAtomicNoPrecondition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	v, err := WriteFileAtomic(path, "first", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if v == "" {
		t.Fatal("expected non-empty version")
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "first" {
		t.Fatalf("expected 'first', got %q (err=%v)", string(got), err)
	}
}

func TestWriteFileAtomicIfMatchSucceedsWhenVersionMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.txt")
	_ = os.WriteFile(path, []byte("v1"), 0644)

	v1, err := FileVersion(path)
	if err != nil {
		t.Fatalf("FileVersion failed: %v", err)
	}

	v2, err := WriteFileAtomic(path, "v2", v1)
	if err != nil {
		t.Fatalf("expected success when version matches, got %v", err)
	}
	if v2 == v1 {
		t.Fatal("expected new version to differ from old")
	}

	got, _ := os.ReadFile(path)
	if string(got) != "v2" {
		t.Fatalf("expected 'v2' on disk, got %q", string(got))
	}
}

func TestWriteFileAtomicIfMatchRejectsStaleVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.txt")
	_ = os.WriteFile(path, []byte("from-device-A"), 0644)
	staleVersion, _ := FileVersion(path)

	// Simulate device B saving the file behind device A's back.
	_ = os.WriteFile(path, []byte("from-device-B"), 0644)

	// Device A tries to save with its old version → must be rejected.
	_, err := WriteFileAtomic(path, "from-device-A-edits", staleVersion)
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("expected ErrVersionMismatch, got %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "from-device-B" {
		t.Errorf("disk content was clobbered, got %q", string(got))
	}
}

func TestWriteFileAtomicIfMatchNewRejectsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claim.txt")
	_ = os.WriteFile(path, []byte("already there"), 0644)

	_, err := WriteFileAtomic(path, "I claim this", "new")
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("expected ErrVersionMismatch for ifMatch=new on existing file, got %v", err)
	}
}

func TestWriteFileAtomicIfMatchNewSucceedsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.txt")

	v, err := WriteFileAtomic(path, "first write", "new")
	if err != nil {
		t.Fatalf("expected creation to succeed, got %v", err)
	}
	if v == "" {
		t.Fatal("expected non-empty version")
	}
}

func TestWriteFileLeavesNoTempArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.txt")

	if err := WriteFile(path, "ok"); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".clean.txt.tmp") {
			t.Errorf("temp artifact left behind: %s", e.Name())
		}
	}
}

func TestWriteFilePreservesModeOnRewrite(t *testing.T) {
	if os.Getenv("CI_SKIP_MODE_TEST") != "" {
		t.Skip("explicit skip")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mode.txt")
	if err := os.WriteFile(path, []byte("v1"), 0600); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	if err := WriteFile(path, "v2"); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	// On Windows, perms reduce to 0666/0444; we only verify on POSIX.
	if os.PathSeparator == '/' {
		if info.Mode().Perm() != 0o600 {
			t.Errorf("expected mode 0600 preserved, got %o", info.Mode().Perm())
		}
	}
}

func TestWriteFileAtomicIsLastWriteWinsUnderConcurrency(t *testing.T) {
	// Without IfMatch, concurrent writes may interleave but must never produce
	// partial bytes — the atomic rename guarantees one of the writers' full
	// content is on disk at any moment.
	dir := t.TempDir()
	path := filepath.Join(dir, "race.txt")
	if err := os.WriteFile(path, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	const writers = 8
	payloads := make([]string, writers)
	for i := range payloads {
		payloads[i] = strings.Repeat(string(rune('A'+i)), 1024)
	}

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			if _, err := WriteFileAtomic(path, payloads[i], ""); err != nil {
				t.Errorf("WriteFileAtomic %d failed: %v", i, err)
			}
		}()
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("final read failed: %v", err)
	}
	matched := false
	for _, p := range payloads {
		if string(got) == p {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("on-disk content does not match any complete payload — got partial write of %d bytes", len(got))
	}
}
