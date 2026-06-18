package acpinstall

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoinRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	bad := []string{"../evil", "../../evil", `..\\evil`}
	abs := filepath.Join(string(os.PathSeparator), "abs", "evil")
	if filepath.IsAbs(abs) {
		bad = append(bad, abs)
	}
	for _, name := range bad {
		if _, err := safeJoin(base, name); err == nil {
			t.Errorf("safeJoin(%q) returned nil error, expected traversal rejection", name)
		}
	}
}

func TestExtractTarGz(t *testing.T) {
	dest := t.TempDir()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("hello")
	if err := tw.WriteHeader(&tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatalf("WriteHeader failed: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close failed: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close failed: %v", err)
	}

	zr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader failed: %v", err)
	}
	defer zr.Close()
	if err := extractTar(zr, dest); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("expected hello, got %q", string(got))
	}
}

func TestExtractZip(t *testing.T) {
	dest := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("nested/tool.txt")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := io.WriteString(w, "ziphello"); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close failed: %v", err)
	}

	if err := extractZip(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "nested", "tool.txt"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "ziphello" {
		t.Errorf("expected ziphello, got %q", string(got))
	}
}

func TestVersionedBinaryDirStable(t *testing.T) {
	dir := withTempAdaptersDir(t)
	a, err := versionedBinaryDir("opencode", "1.17.7", "https://example.com/opencode.zip")
	if err != nil {
		t.Fatalf("versionedBinaryDir failed: %v", err)
	}
	b, err := versionedBinaryDir("opencode", "1.17.7", "https://example.com/opencode.zip")
	if err != nil {
		t.Fatalf("versionedBinaryDir failed: %v", err)
	}
	if a != b {
		t.Errorf("expected stable dir, got %q and %q", a, b)
	}
	if !filepath.HasPrefix(a, filepath.Join(dir, "binary", "opencode")) {
		t.Errorf("expected dir under binary/opencode, got %q", a)
	}
}
