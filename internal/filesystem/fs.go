package filesystem

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

type DirEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

type FileContent struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int64  `json:"size"`

	// Version uniquely identifies the file's current bytes. Multi-client
	// editors should pass this back in WriteFileAtomic.IfMatch to detect
	// conflicting writes from another device.
	Version string `json:"version,omitempty"`
}

// ErrVersionMismatch is returned by WriteFileAtomic when the on-disk file's
// version does not match the caller-provided IfMatch version.
var ErrVersionMismatch = errors.New("file version mismatch")

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
		Version:  computeFileVersion(data, info),
	}, nil
}

// FileVersion returns a deterministic identifier for the bytes currently on
// disk at filePath, or empty string with an error if the file is unreadable.
// Used to support optimistic concurrency in WriteFileAtomic.
func FileVersion(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a directory", filePath)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return computeFileVersion(data, info), nil
}

func computeFileVersion(data []byte, info os.FileInfo) string {
	sum := sha256.Sum256(data)
	// Include size + mtime nanoseconds as a coarse sanity check, but rely
	// on the content hash for correctness — mtime resolution differs across
	// filesystems (NTFS = 100ns, ext4 = ns, FAT32 = 2s).
	return fmt.Sprintf("sha256:%s:%d", hex.EncodeToString(sum[:]), info.Size())
}

// WriteFile writes the content atomically: write to a sibling temp file,
// fsync, then rename over the destination. This guarantees that readers see
// either the old or the new bytes, never a partial write — important when
// multiple clients edit the same workspace from different devices.
func WriteFile(filePath string, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return atomicWrite(filePath, []byte(content))
}

// WriteFileAtomic is a stricter variant of WriteFile. When ifMatch is non-empty,
// the existing file's FileVersion must equal ifMatch or the write is rejected
// with ErrVersionMismatch. Pass ifMatch == "" to skip the precondition check
// (equivalent to plain WriteFile).
//
// When ifMatch is the literal string "new", the destination must NOT exist —
// useful when a client believes they are creating a new file and wants to
// avoid clobbering an existing one created by another device.
func WriteFileAtomic(filePath string, content string, ifMatch string) (string, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	// Precondition checks against the on-disk file (best-effort: filesystem
	// access between this check and the rename is not transactional, but the
	// window is small and the rename is atomic).
	if ifMatch == "new" {
		if _, err := os.Stat(filePath); err == nil {
			return "", ErrVersionMismatch
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	} else if ifMatch != "" {
		current, err := FileVersion(filePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Caller expected a specific version, but file is gone.
				return "", ErrVersionMismatch
			}
			return "", err
		}
		if current != ifMatch {
			return "", ErrVersionMismatch
		}
	}

	if err := atomicWrite(filePath, []byte(content)); err != nil {
		return "", err
	}

	// Compute new version from the bytes we just wrote so callers can update
	// their cache without a follow-up read.
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}
	return computeFileVersion([]byte(content), info), nil
}

func atomicWrite(filePath string, data []byte) error {
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	// Preserve the destination's mode if it already exists.
	if existing, err := os.Stat(filePath); err == nil {
		_ = os.Chmod(tmpPath, existing.Mode().Perm())
	}

	if err := renameWithRetry(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// renameWithRetry wraps os.Rename with a short backoff loop. On Windows,
// concurrent renames against the same destination, antivirus scanners,
// and indexer file locks all produce transient "Access is denied" or
// "The process cannot access the file" errors that resolve in milliseconds.
// Retrying makes WriteFile robust under multi-client concurrent saves.
func renameWithRetry(src, dst string) error {
	const maxAttempts = 8
	delays := []int{1, 2, 5, 10, 20, 40, 80, 160} // ms

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if err := os.Rename(src, dst); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxAttempts-1 {
			sleepMs(delays[i])
		}
	}
	return lastErr
}

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

func DeleteEntry(path string) error {
	return os.RemoveAll(path)
}

func RenameEntry(oldPath string, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func CopyEntry(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}
