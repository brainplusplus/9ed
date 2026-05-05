package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

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
}

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
	}, nil
}

func WriteFile(filePath string, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(content), 0644)
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
