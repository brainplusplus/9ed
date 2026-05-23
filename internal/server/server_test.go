package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/brainplusplus/9ed/internal/config"
)

func TestServerHandlerRequiresBasicAuthForSPA(t *testing.T) {
	root := setupTestDist(t)
	withWorkingDirectory(t, root)

	srv := New(config.Config{
		Port:              "8080",
		BasicAuthUsername: "alice",
		BasicAuthPassword: "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestServerHandlerServesSPAAndStaticAssetsWhenAuthenticated(t *testing.T) {
	root := setupTestDist(t)
	withWorkingDirectory(t, root)

	srv := New(config.Config{
		Port:              "8080",
		BasicAuthUsername: "alice",
		BasicAuthPassword: "secret",
	})

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexReq.SetBasicAuth("alice", "secret")
	indexRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(indexRec, indexReq)

	if indexRec.Code != http.StatusOK {
		t.Fatalf("expected index request to return 200, got %d", indexRec.Code)
	}
	if body := indexRec.Body.String(); body != "<html><body>app</body></html>" {
		t.Fatalf("expected index body to match test asset, got %q", body)
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	assetReq.SetBasicAuth("alice", "secret")
	assetRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(assetRec, assetReq)

	if assetRec.Code != http.StatusOK {
		t.Fatalf("expected asset request to return 200, got %d", assetRec.Code)
	}
	if body := assetRec.Body.String(); body != "console.log('ok');" {
		t.Fatalf("expected asset body to match test asset, got %q", body)
	}
}

func TestServerHandlerReturns404ForMissingAssetInsteadOfSPAHTML(t *testing.T) {
	root := setupTestDist(t)
	withWorkingDirectory(t, root)

	srv := New(config.Config{
		Port:              "8080",
		BasicAuthUsername: "alice",
		BasicAuthPassword: "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	req.SetBasicAuth("alice", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing asset, got %d with body %q", rec.Code, rec.Body.String())
	}
}

func setupTestDist(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html><body>app</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile for index.html returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte("console.log('ok');"), 0o644); err != nil {
		t.Fatalf("WriteFile for app.js returned error: %v", err)
	}

	return root
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restoring working directory failed: %v", err)
		}
	})
}
