package config

import (
	"testing"
)

func TestLoadFromEnvUsesDefaultPortWhenUnset(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("BASIC_AUTH_USERNAME", "alice")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.Port != "8080" {
		t.Fatalf("expected default port 8080, got %q", cfg.Port)
	}
}

func TestLoadFromEnvRejectsMissingCredentials(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("BASIC_AUTH_USERNAME", "")
	t.Setenv("BASIC_AUTH_PASSWORD", "")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestLoadFromEnvDefaultsToSimpleMode(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("BASIC_AUTH_USERNAME", "alice")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	t.Setenv("MODE", "")
	t.Setenv("WORKSPACE_ROOT", "")
	t.Setenv("TUNNEL_ENGINE", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.Mode != "simple" {
		t.Fatalf("expected default mode 'simple', got %q", cfg.Mode)
	}

	if cfg.WorkspaceRoot != "" {
		t.Fatalf("expected empty workspace root, got %q", cfg.WorkspaceRoot)
	}

	if cfg.TunnelEngine != "cloudflare" {
		t.Fatalf("expected default tunnel engine 'cloudflare', got %q", cfg.TunnelEngine)
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

func TestLoadFromEnvReadsConfiguredValues(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("BASIC_AUTH_USERNAME", "bob")
	t.Setenv("BASIC_AUTH_PASSWORD", "hunter2")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Fatalf("expected configured port, got %q", cfg.Port)
	}
	if cfg.BasicAuthUsername != "bob" {
		t.Fatalf("expected configured username, got %q", cfg.BasicAuthUsername)
	}
	if cfg.BasicAuthPassword != "hunter2" {
		t.Fatalf("expected configured password, got %q", cfg.BasicAuthPassword)
	}
}

func TestLoadFromEnvDefaultsBrowserEnabled(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("BASIC_AUTH_USERNAME", "alice")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	t.Setenv("USE_BROWSER", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if !cfg.UseBrowser {
		t.Fatal("expected UseBrowser to default to true when USE_BROWSER is empty")
	}
}

func TestLoadFromEnvEnablesBrowser(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("BASIC_AUTH_USERNAME", "alice")
	t.Setenv("BASIC_AUTH_PASSWORD", "secret")
	t.Setenv("USE_BROWSER", "true")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if !cfg.UseBrowser {
		t.Fatal("expected UseBrowser to be true when USE_BROWSER=true")
	}
}
