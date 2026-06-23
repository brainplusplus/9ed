package bininstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// extractTarGz extracts a tar.gz/tar.xz/tgz archive, finding the named binary
// inside it and copying it to dest.
func extractTar(archive, dest, binaryName string) error {
	// Use system tar to extract into a temp dir, then find the binary.
	tmpDir, err := os.MkdirTemp(filepath.Dir(dest), "extract-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command("tar", "-xf", archive, "-C", tmpDir)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return err
	}

	// Find the binary inside the extracted tree.
	srcPath := filepath.Join(tmpDir, binaryName)
	if _, err := os.Stat(srcPath); err != nil {
		// Search recursively (archives often have bin/ subdirectories).
		found, findErr := findFile(tmpDir, binaryName)
		if findErr != nil {
			return fmt.Errorf("%s not found after extraction: %w", binaryName, findErr)
		}
		srcPath = found
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read extracted binary: %w", err)
	}
	return os.WriteFile(dest, data, 0o755)
}

// extractZip extracts a zip archive, finding the named binary inside it and
// copying it to dest.
func extractZip(archive, dest, binaryName string) error {
	// Use system tar (bsdtar on macOS, tar on Linux, or PowerShell tar on
	// Windows 10+) to extract zip files.
	tmpDir, err := os.MkdirTemp(filepath.Dir(dest), "extract-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command("tar", "-xf", archive, "-C", tmpDir)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return err
	}

	// Find the binary inside the extracted tree.
	srcPath := filepath.Join(tmpDir, binaryName)
	if _, err := os.Stat(srcPath); err != nil {
		// Search recursively (archives often have bin/ subdirectories).
		found, findErr := findFile(tmpDir, binaryName)
		if findErr != nil {
			return fmt.Errorf("%s not found after extraction: %w", binaryName, findErr)
		}
		srcPath = found
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read extracted binary: %w", err)
	}
	return os.WriteFile(dest, data, 0o755)
}

// findFile searches for a file named filename within root recursively.
func findFile(root, filename string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == filename {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("file %q not found in %s", filename, root)
	}
	return found, nil
}
