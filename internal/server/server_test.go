package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

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
	if body := indexRec.Body.String(); body != "<html><body>ide</body></html>" {
		t.Fatalf("expected IDE body to match test asset, got %q", body)
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

func TestEmbeddedSPAHandlerServesEmbeddedAssets(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": {
			Data: []byte("<html><body>terminal</body></html>"),
			Mode: fs.ModePerm,
		},
		"ide.html": {
			Data: []byte("<html><body>ide</body></html>"),
			Mode: fs.ModePerm,
		},
		"assets/app.js": {
			Data: []byte("console.log('embedded');"),
			Mode: fs.ModePerm,
		},
	}
	handler := embeddedSPAHandler(assets)

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRec := httptest.NewRecorder()
	handler.ServeHTTP(indexRec, indexReq)

	if indexRec.Code != http.StatusOK {
		t.Fatalf("expected embedded index request to return 200, got %d", indexRec.Code)
	}
	if body := indexRec.Body.String(); body != "<html><body>ide</body></html>" {
		t.Fatalf("expected embedded IDE body, got %q", body)
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	assetRec := httptest.NewRecorder()
	handler.ServeHTTP(assetRec, assetReq)

	if assetRec.Code != http.StatusOK {
		t.Fatalf("expected embedded asset request to return 200, got %d", assetRec.Code)
	}
	if body := assetRec.Body.String(); body != "console.log('embedded');" {
		t.Fatalf("expected embedded asset body, got %q", body)
	}
	if got := assetRec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("expected embedded asset cache header, got %q", got)
	}
}

func TestServerHandlerExemptsActiveTerminalMCPFromBasicAuth(t *testing.T) {
	root := setupTestDist(t)
	withWorkingDirectory(t, root)

	srv := New(config.Config{
		Port:              "8080",
		BasicAuthUsername: "alice",
		BasicAuthPassword: "secret",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/chat/terminal/run", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from MCP token check, got %d", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("expected MCP endpoint to bypass Basic Auth challenge, got %q", got)
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
	if err := os.WriteFile(filepath.Join(dist, "ide.html"), []byte("<html><body>ide</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile for ide.html returned error: %v", err)
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
