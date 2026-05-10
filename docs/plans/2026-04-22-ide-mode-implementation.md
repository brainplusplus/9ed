# IDE Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a full web IDE mode (`MODE=full`) to 9ed with Monaco editor, file tree, multi-project support, and activity bar — while keeping the existing terminal-only mode (`MODE=simple`) untouched.

**Architecture:** Monolithic extension. Two Vite entry points (terminal vs IDE) with conditional backend routing. New `internal/filesystem` package for file operations. Frontend uses Zustand for state, Monaco for editing, react-resizable-panels for layout. Existing terminal components reused as-is inside the IDE shell.

**Tech Stack:** Go 1.24, React 18, TypeScript, Vite 7, Monaco Editor, Zustand, react-resizable-panels, xterm.js (existing)

---

## Phase 1: Backend Foundation (Config + Filesystem API)

### Task 1: Extend Config with Mode and WorkspaceRoot

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `.env.example`

**Step 1: Write the failing tests**

Add to `internal/config/config_test.go`:

```go
func TestLoadFromEnvDefaultsToSimpleMode(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("BASIC_AUTH_USERNAME", "alice")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	t.Setenv("MODE", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.Mode != "simple" {
		t.Fatalf("expected default mode 'simple', got %q", cfg.Mode)
	}
}

func TestLoadFromEnvReadsFullMode(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("BASIC_AUTH_USERNAME", "alice")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	t.Setenv("MODE", "full")
	t.Setenv("WORKSPACE_ROOT", "/home/user/projects")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.Mode != "full" {
		t.Fatalf("expected mode 'full', got %q", cfg.Mode)
	}
	if cfg.WorkspaceRoot != "/home/user/projects" {
		t.Fatalf("expected workspace root '/home/user/projects', got %q", cfg.WorkspaceRoot)
	}
}

func TestLoadFromEnvRejectsInvalidMode(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("BASIC_AUTH_USERNAME", "alice")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	t.Setenv("MODE", "invalid")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v -run "TestLoadFromEnv(DefaultsToSimpleMode|ReadsFullMode|RejectsInvalidMode)"`
Expected: FAIL — `cfg.Mode` field does not exist

**Step 3: Implement Config changes**

Update `internal/config/config.go`:

```go
type Config struct {
	Port              string
	BasicAuthUsername string
	BasicAuthPassword string
	Mode              string // "simple" or "full"
	WorkspaceRoot     string // optional, root path for file access
}

func LoadFromEnv() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		Port:              strings.TrimSpace(os.Getenv("PORT")),
		BasicAuthUsername: os.Getenv("BASIC_AUTH_USERNAME"),
		BasicAuthPassword: os.Getenv("BASIC_AUTH_PASSWORD"),
		Mode:              strings.TrimSpace(strings.ToLower(os.Getenv("MODE"))),
		WorkspaceRoot:     strings.TrimSpace(os.Getenv("WORKSPACE_ROOT")),
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.Mode == "" {
		cfg.Mode = "simple"
	}

	if cfg.Mode != "simple" && cfg.Mode != "full" {
		return Config{}, fmt.Errorf("MODE must be 'simple' or 'full', got %q", cfg.Mode)
	}

	if strings.TrimSpace(cfg.BasicAuthUsername) == "" || strings.TrimSpace(cfg.BasicAuthPassword) == "" {
		return Config{}, errors.New("BASIC_AUTH_USERNAME and BASIC_AUTH_PASSWORD are required")
	}

	return cfg, nil
}
```

Add `"fmt"` to imports.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: ALL PASS

**Step 5: Update .env.example**

```env
PORT=8080
BASIC_AUTH_USERNAME=admin
BASIC_AUTH_PASSWORD=changeme
MODE=simple
WORKSPACE_ROOT=
```

**Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go .env.example
git commit -m "feat(config): add MODE and WORKSPACE_ROOT env vars"
```

---

### Task 2: Create filesystem package — path security

**Files:**
- Create: `internal/filesystem/security.go`
- Create: `internal/filesystem/security_test.go`

**Step 1: Write the failing tests**

Create `internal/filesystem/security_test.go`:

```go
package filesystem

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidatePathAllowsChildPaths(t *testing.T) {
	root := "/home/user/projects"
	if runtime.GOOS == "windows" {
		root = `C:\Users\user\projects`
	}

	child := filepath.Join(root, "myapp", "main.go")
	result, err := ValidatePath(root, child)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != child {
		t.Fatalf("expected %q, got %q", child, result)
	}
}

func TestValidatePathRejectsTraversal(t *testing.T) {
	root := "/home/user/projects"
	if runtime.GOOS == "windows" {
		root = `C:\Users\user\projects`
	}

	_, err := ValidatePath(root, filepath.Join(root, "..", "..", "etc", "passwd"))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestValidatePathAllowsRootItself(t *testing.T) {
	root := "/home/user/projects"
	if runtime.GOOS == "windows" {
		root = `C:\Users\user\projects`
	}

	result, err := ValidatePath(root, root)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != root {
		t.Fatalf("expected %q, got %q", root, result)
	}
}

func TestValidatePathWithEmptyRootAllowsAnything(t *testing.T) {
	result, err := ValidatePath("", "/any/path/at/all")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/filesystem/ -v`
Expected: FAIL — package does not exist

**Step 3: Implement security.go**

Create `internal/filesystem/security.go`:

```go
package filesystem

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidatePath ensures the given path is within the allowed root.
// If root is empty, all paths are allowed (full filesystem access).
func ValidatePath(root string, requestedPath string) (string, error) {
	cleaned := filepath.Clean(requestedPath)

	if root == "" {
		return cleaned, nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("invalid root path: %w", err)
	}

	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Ensure the path is within root (or is root itself)
	if !strings.HasPrefix(absPath, absRoot) {
		return "", fmt.Errorf("path %q is outside workspace root %q", requestedPath, root)
	}

	return absPath, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/filesystem/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/filesystem/
git commit -m "feat(filesystem): add path validation with traversal protection"
```

---

### Task 3: Create filesystem package — tree listing

**Files:**
- Create: `internal/filesystem/fs.go`
- Create: `internal/filesystem/fs_test.go`

**Step 1: Write the failing tests**

Create `internal/filesystem/fs_test.go`:

```go
package filesystem

import (
	"os"
	"path/filepath"
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
	_, err := ListDirectory("/nonexistent/path/that/does/not/exist")
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
	new_ := filepath.Join(dir, "new.txt")
	_ = os.WriteFile(old, []byte("data"), 0644)

	err := RenameEntry(old, new_)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old file should not exist")
	}
	data, _ := os.ReadFile(new_)
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/filesystem/ -v -run "TestListDirectory|TestReadFile|TestWriteFile|TestCreate|TestDelete|TestRename|TestCopy"`
Expected: FAIL — functions do not exist

**Step 3: Implement fs.go**

Create `internal/filesystem/fs.go`:

```go
package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DirEntry represents a file or directory in a listing.
type DirEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "file" or "dir"
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"` // unix timestamp
}

// FileContent represents the content of a read file.
type FileContent struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int64  `json:"size"`
}

// ListDirectory returns the immediate children of a directory.
func ListDirectory(dirPath string) ([]DirEntry, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	result := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		}

		result = append(result, DirEntry{
			Name:     entry.Name(),
			Type:     entryType,
			Size:     info.Size(),
			Modified: info.ModTime().Unix(),
		})
	}

	return result, nil
}

// ReadFile reads a file's content up to maxSize bytes.
func ReadFile(filePath string, maxSize int64) (FileContent, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return FileContent{}, err
	}

	if info.IsDir() {
		return FileContent{}, fmt.Errorf("%q is a directory", filePath)
	}

	if info.Size() > maxSize {
		return FileContent{}, fmt.Errorf("file size %d exceeds max %d", info.Size(), maxSize)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return FileContent{}, err
	}

	return FileContent{
		Content:  string(data),
		Encoding: "utf-8",
		Size:     info.Size(),
	}, nil
}

// WriteFile writes content to a file, creating or overwriting it.
func WriteFile(filePath string, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(content), 0644)
}

// CreateEntry creates a new file or directory.
func CreateEntry(path string, entryType string) error {
	switch entryType {
	case "file":
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		return f.Close()
	case "dir":
		return os.MkdirAll(path, 0755)
	default:
		return fmt.Errorf("unsupported entry type %q", entryType)
	}
}

// DeleteEntry removes a file or directory (recursively).
func DeleteEntry(path string) error {
	return os.RemoveAll(path)
}

// RenameEntry renames/moves a file or directory.
func RenameEntry(oldPath string, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// CopyEntry copies a file from src to dst.
func CopyEntry(src string, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/filesystem/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/filesystem/
git commit -m "feat(filesystem): add file operations — list, read, write, create, delete, rename, copy"
```

---

### Task 4: Create filesystem package — search

**Files:**
- Create: `internal/filesystem/search.go`
- Create: `internal/filesystem/search_test.go`

**Step 1: Write the failing tests**

Create `internal/filesystem/search_test.go`:

```go
package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchFindsMatchingLines(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO: fix this\nfunc main() {}\n"), 0644)

	results, err := Search(dir, "TODO", false, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Line != 2 {
		t.Fatalf("expected line 2, got %d", results[0].Line)
	}
}

func TestSearchRespectsMaxResults(t *testing.T) {
	dir := t.TempDir()
	content := "match\nmatch\nmatch\nmatch\nmatch\n"
	_ = os.WriteFile(filepath.Join(dir, "many.txt"), []byte(content), 0644)

	results, err := Search(dir, "match", false, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestSearchRecursesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	_ = os.Mkdir(sub, 0755)
	_ = os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("findme here\n"), 0644)

	results, err := Search(dir, "findme", false, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/filesystem/ -v -run "TestSearch"`
Expected: FAIL — `Search` function does not exist

**Step 3: Implement search.go**

Create `internal/filesystem/search.go`:

```go
package filesystem

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SearchResult represents a single search match.
type SearchResult struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Preview string `json:"preview"`
}

// Search recursively searches for a query string in files under root.
func Search(root string, query string, useRegex bool, maxResults int) ([]SearchResult, error) {
	var pattern *regexp.Regexp
	if useRegex {
		var err error
		pattern, err = regexp.Compile(query)
		if err != nil {
			return nil, err
		}
	}

	var results []SearchResult

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		if len(results) >= maxResults {
			return filepath.SkipAll
		}

		// Skip binary-looking files (simple heuristic: skip files > 1MB)
		if info.Size() > 1024*1024 {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			var col int
			var matched bool

			if pattern != nil {
				loc := pattern.FindStringIndex(line)
				if loc != nil {
					matched = true
					col = loc[0] + 1
				}
			} else {
				idx := strings.Index(line, query)
				if idx >= 0 {
					matched = true
					col = idx + 1
				}
			}

			if matched {
				relPath, _ := filepath.Rel(root, path)
				preview := line
				if len(preview) > 200 {
					preview = preview[:200]
				}

				results = append(results, SearchResult{
					Path:    relPath,
					Line:    lineNum,
					Column:  col,
					Preview: preview,
				})

				if len(results) >= maxResults {
					return filepath.SkipAll
				}
			}
		}

		return nil
	})

	return results, err
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/filesystem/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/filesystem/search.go internal/filesystem/search_test.go
git commit -m "feat(filesystem): add recursive text search with regex support"
```

---

### Task 5: Add File API routes to httpapi

**Files:**
- Create: `internal/httpapi/fileapi.go`
- Create: `internal/httpapi/fileapi_test.go`
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/types.go`

**Step 1: Write the failing tests**

Create `internal/httpapi/fileapi_test.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"9ed/internal/filesystem"
)

func TestFileTreeEndpoint(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hi"), 0644)

	api := &API{mode: "full", workspaceRoot: ""}
	handler := api.handleFileTree

	req := httptest.NewRequest(http.MethodGet, "/api/files/tree?path="+dir, nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []filesystem.DirEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
}

func TestFileTreeEndpointBlockedInSimpleMode(t *testing.T) {
	api := &API{mode: "simple", workspaceRoot: ""}
	handler := api.handleFileTree

	req := httptest.NewRequest(http.MethodGet, "/api/files/tree?path=/tmp", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 in simple mode, got %d", rec.Code)
	}
}

func TestFileContentReadEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	_ = os.WriteFile(path, []byte("hello world"), 0644)

	api := &API{mode: "full", workspaceRoot: ""}
	handler := api.handleFileContent

	req := httptest.NewRequest(http.MethodGet, "/api/files/content?path="+path, nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var fc filesystem.FileContent
	if err := json.NewDecoder(rec.Body).Decode(&fc); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if fc.Content != "hello world" {
		t.Fatalf("expected 'hello world', got %q", fc.Content)
	}
}

func TestFileContentWriteEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	api := &API{mode: "full", workspaceRoot: ""}
	handler := api.handleFileContent

	body := `{"content":"written via api"}`
	req := httptest.NewRequest(http.MethodPut, "/api/files/content?path="+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	data, _ := os.ReadFile(path)
	if string(data) != "written via api" {
		t.Fatalf("expected 'written via api', got %q", string(data))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/httpapi/ -v -run "TestFile"`
Expected: FAIL — fields and handlers do not exist

**Step 3: Update types.go with file API types**

Add to `internal/httpapi/types.go`:

```go
type writeFileRequest struct {
	Content string `json:"content"`
}

type createEntryRequest struct {
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "dir"
}

type renameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

type copyMoveRequest struct {
	SourcePath string `json:"sourcePath"`
	DestPath   string `json:"destPath"`
}

type configResponse struct {
	Mode          string `json:"mode"`
	WorkspaceRoot string `json:"workspaceRoot"`
}
```

**Step 4: Implement fileapi.go**

Create `internal/httpapi/fileapi.go` with handlers for:
- `handleFileTree` — GET /api/files/tree
- `handleFileContent` — GET (read) / PUT (write) /api/files/content
- `handleFileCreate` — POST /api/files/create
- `handleFileRename` — POST /api/files/rename
- `handleFileCopy` — POST /api/files/copy
- `handleFileMove` — POST /api/files/move
- `handleFileDelete` — DELETE /api/files
- `handleFileSearch` — GET /api/files/search
- `handleFileDownload` — GET /api/files/download
- `handleFileUpload` — POST /api/files/upload
- `handleConfig` — GET /api/config

Each handler checks `a.mode == "full"` and returns 404 if not. Each handler validates paths via `filesystem.ValidatePath(a.workspaceRoot, path)`.

**Step 5: Update router.go to add mode/workspaceRoot fields and register file routes**

Add `mode` and `workspaceRoot` fields to `API` struct. Update `New()` to accept them from `Dependencies`. Register file routes in `Handler()` conditionally.

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/httpapi/ -v`
Expected: ALL PASS

**Step 7: Commit**

```bash
git add internal/httpapi/
git commit -m "feat(httpapi): add file system API routes with mode gating"
```

---

### Task 6: Add CWD support to terminal session creation

**Files:**
- Modify: `internal/terminal/session.go`
- Modify: `internal/terminal/manager.go`
- Modify: `internal/httpapi/types.go`
- Modify: `internal/httpapi/router.go`

**Step 1: Update SpawnFunc to accept optional CWD**

In `internal/terminal/manager.go`, add `CWD` field to `ShellProfile`:

```go
type ShellProfile struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	CWD     string   `json:"cwd,omitempty"`
}
```

**Step 2: Update session.go to use CWD**

In `NewPTYSpawnFunc`, change:

```go
cmd.Dir = currentWorkingDirectory()
```

to:

```go
if profile.CWD != "" {
	cmd.Dir = profile.CWD
} else {
	cmd.Dir = currentWorkingDirectory()
}
```

**Step 3: Update createSessionRequest to include CWD**

In `internal/httpapi/types.go`:

```go
type createSessionRequest struct {
	ShellID string `json:"shellId"`
	CWD     string `json:"cwd,omitempty"`
}
```

**Step 4: Pass CWD through in router.go handleSessions**

In the session creation handler, pass `req.CWD` to the profile:

```go
session, err := a.sessions.Create(terminal.ShellProfile{
	ID:      profile.ID,
	Label:   profile.Label,
	Command: profile.Command,
	Args:    profile.Args,
	CWD:     req.CWD,
})
```

**Step 5: Run all tests**

Run: `go test ./... -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/terminal/ internal/httpapi/
git commit -m "feat(terminal): add CWD support for session creation"
```

---

### Task 7: Wire Mode into server.go

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/httpapi/router.go`

**Step 1: Pass Mode and WorkspaceRoot from Config to API**

Update `Dependencies` struct to include `Mode` and `WorkspaceRoot`. Update `server.New()` to pass them. Update `server.Handler()` to serve `ide.html` when `Mode == "full"` and `index.html` when `Mode == "simple"`.

**Step 2: Update spaHandler to accept mode parameter**

The SPA handler should serve from `dist/terminal/index.html` or `dist/ide/index.html` based on mode.

**Step 3: Run all Go tests**

Run: `go test ./... -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add internal/server/ internal/httpapi/
git commit -m "feat(server): wire MODE into server routing and SPA serving"
```

---

## Phase 2: Frontend Foundation (IDE Shell + Project Picker)

### Task 8: Install new frontend dependencies

**Step 1: Install packages**

Run:
```bash
npm install monaco-editor @monaco-editor/react zustand react-resizable-panels
```

**Step 2: Commit**

```bash
git add package.json package-lock.json
git commit -m "deps: add monaco-editor, zustand, react-resizable-panels"
```

---

### Task 9: Create Zustand workspace store

**Files:**
- Create: `frontend/src/stores/workspace.ts`
- Create: `frontend/src/types.ts` (extend)

**Step 1: Define types**

Add to `frontend/src/types.ts`:

```ts
export type FileTab = {
  id: string;
  path: string;
  name: string;
  content: string;
  language: string;
  modified: boolean;
};

export type Project = {
  id: string;
  path: string;
  name: string;
  openFiles: FileTab[];
  activeFileId: string | null;
  terminalSessions: string[];
};

export type ActivePanel = 'explorer' | 'search' | 'projects' | 'terminal';

export type AppConfig = {
  mode: 'simple' | 'full';
  workspaceRoot: string;
};
```

**Step 2: Create Zustand store**

Create `frontend/src/stores/workspace.ts` with actions:
- `addProject(path, name)`
- `removeProject(id)`
- `setActiveProject(id)`
- `setActivePanel(panel)`
- `openFile(projectId, file)`
- `closeFile(projectId, fileId)`
- `setActiveFile(projectId, fileId)`
- `updateFileContent(projectId, fileId, content)`
- `markFileSaved(projectId, fileId)`

**Step 3: Commit**

```bash
git add frontend/src/stores/ frontend/src/types.ts
git commit -m "feat(frontend): add Zustand workspace store and IDE types"
```

---

### Task 10: Create IDE entry point and project picker

**Files:**
- Create: `frontend/ide.html`
- Create: `frontend/src/apps/ide/IDEApp.tsx`
- Create: `frontend/src/apps/ide/ProjectPicker.tsx`
- Create: `frontend/src/apps/ide/main.tsx`
- Modify: `vite.config.ts` (multi-entry)

**Step 1: Create ide.html**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Web IDE</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/apps/ide/main.tsx"></script>
  </body>
</html>
```

**Step 2: Create IDE main.tsx bootstrap**

**Step 3: Create ProjectPicker component**

Fetches `GET /api/files/tree?path={root}` and renders an expandable folder tree. Click "Open" on a folder to set it as active project.

**Step 4: Create IDEApp component**

Shows ProjectPicker when no project is active, IDEWorkspace when a project is selected.

**Step 5: Update vite.config.ts for multi-entry**

```ts
build: {
  rollupOptions: {
    input: {
      terminal: resolve(__dirname, 'frontend/index.html'),
      ide: resolve(__dirname, 'frontend/ide.html'),
    },
  },
  outDir: resolve(__dirname, 'dist'),
  emptyOutDir: true,
}
```

**Step 6: Verify build**

Run: `npm run build`
Expected: Both entry points built to dist/

**Step 7: Commit**

```bash
git add frontend/ide.html frontend/src/apps/ vite.config.ts
git commit -m "feat(frontend): add IDE entry point with project picker"
```

---

### Task 11: Extend frontend API client

**Files:**
- Modify: `frontend/src/api.ts`

Add functions:
- `getConfig(): Promise<AppConfig>`
- `getFileTree(path: string): Promise<DirEntry[]>`
- `getFileContent(path: string): Promise<FileContent>`
- `saveFileContent(path: string, content: string): Promise<void>`
- `createEntry(path: string, type: string): Promise<void>`
- `renameEntry(oldPath: string, newPath: string): Promise<void>`
- `deleteEntry(path: string): Promise<void>`
- `copyEntry(src: string, dst: string): Promise<void>`
- `moveEntry(src: string, dst: string): Promise<void>`
- `searchFiles(root: string, query: string, regex: boolean, max: number): Promise<SearchResult[]>`
- `downloadFile(path: string): Promise<void>` (triggers browser download)
- `uploadFiles(path: string, files: FileList): Promise<void>`

**Commit:**

```bash
git add frontend/src/api.ts
git commit -m "feat(frontend): extend API client with file system operations"
```

---

## Phase 3: IDE Workspace UI

### Task 12: Activity Bar component

**Files:**
- Create: `frontend/src/components/sidebar/ActivityBar.tsx`
- Create: `frontend/src/styles/ide.css`

Icons for Explorer, Search, Projects, Terminal. Highlights active panel. Calls `setActivePanel()` from Zustand store.

---

### Task 13: File Tree component

**Files:**
- Create: `frontend/src/components/sidebar/FileTree.tsx`
- Create: `frontend/src/components/sidebar/FileTreeNode.tsx`

Lazy-loading tree. Click file → opens in editor. Right-click context menu for create/rename/delete/copy/move. Expand/collapse directories.

---

### Task 14: Monaco Editor integration

**Files:**
- Create: `frontend/src/components/editor/EditorArea.tsx`
- Create: `frontend/src/components/editor/EditorTabs.tsx`
- Create: `frontend/src/components/editor/MonacoEditor.tsx`

EditorTabs shows open files with modified indicator and close button. MonacoEditor wraps `@monaco-editor/react` with language auto-detection from file extension. Ctrl+S triggers save via API.

---

### Task 15: IDE Workspace layout (split panels)

**Files:**
- Create: `frontend/src/apps/ide/IDEWorkspace.tsx`

Uses `react-resizable-panels` for:
- Vertical split: ActivityBar (fixed) | Sidebar (resizable) | Main area
- Horizontal split in main area: Editor (top) | Terminal (bottom, collapsible)

Reuses existing `TerminalTabs` and `TerminalView` in the terminal panel.

---

### Task 16: Search Panel

**Files:**
- Create: `frontend/src/components/sidebar/SearchPanel.tsx`

Text input + results list. Calls `GET /api/files/search`. Click result → opens file at line.

---

### Task 17: Project List panel

**Files:**
- Create: `frontend/src/components/sidebar/ProjectList.tsx`

Shows all opened projects. Click to switch. "Open Folder" button to go back to project picker.

---

### Task 18: Move existing terminal components

**Files:**
- Move: `frontend/src/components/TerminalTabs.tsx` → `frontend/src/components/terminal/TerminalTabs.tsx`
- Move: `frontend/src/components/TerminalView.tsx` → `frontend/src/components/terminal/TerminalView.tsx`
- Move: `frontend/src/components/TopBar.tsx` → `frontend/src/components/terminal/TopBar.tsx`
- Update imports in `frontend/src/App.tsx`

Existing terminal app (`MODE=simple`) continues to work unchanged.

---

## Phase 4: Polish & Integration

### Task 19: Keyboard shortcuts

Implement `Ctrl+S`, `Ctrl+P`, `` Ctrl+` ``, `Ctrl+B` via `useEffect` keydown listeners.

### Task 20: Upload/Download UI

Add upload button in file tree toolbar. Download via right-click context menu.

### Task 21: Styling and responsive design

Create `frontend/src/styles/ide.css` with dark sidebar, editor theme matching terminal, responsive breakpoints.

### Task 22: End-to-end verification

Run:
```bash
go test ./...
go build ./...
npm run typecheck
npm run build
```

All must pass.

---

## Dependency Graph

```
Task 1 (config) ──┐
                   ├── Task 5 (file API routes) ── Task 7 (wire server)
Task 2 (security) ─┤
Task 3 (fs ops) ───┤
Task 4 (search) ───┘

Task 8 (npm deps) ── Task 9 (store) ── Task 10 (IDE entry) ── Task 11 (API client)
                                                                      │
                                                    ┌─────────────────┤
                                                    ▼                 ▼
                                              Task 12-17        Task 15 (layout)
                                              (components)            │
                                                    │                 ▼
                                                    └──────── Task 18-22 (polish)
```

Backend (Tasks 1-7) and Frontend foundation (Tasks 8-11) can be done in parallel.
