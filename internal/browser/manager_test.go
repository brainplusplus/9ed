package browser

import "testing"

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "localhost port", raw: "localhost:3000", want: "http://localhost:3000"},
		{name: "loopback port", raw: "127.0.0.1:5173", want: "http://127.0.0.1:5173"},
		{name: "host port", raw: "example.com:8080", want: "http://example.com:8080"},
		{name: "http", raw: "http://127.0.0.1:5173/app", want: "http://127.0.0.1:5173/app"},
		{name: "bare host", raw: "example.com", want: "https://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeURL(tt.raw)
			if err != nil {
				t.Fatalf("NormalizeURL() error = %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("NormalizeURL() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestNormalizeURLRejectsUnsupportedScheme(t *testing.T) {
	if _, err := NormalizeURL("file:///tmp/secret.txt"); err == nil {
		t.Fatal("NormalizeURL() expected unsupported scheme error")
	}
}

func TestManagerTabsAndProxyTarget(t *testing.T) {
	manager := NewManager()
	tab, err := manager.CreateTab("localhost:3000")
	if err != nil {
		t.Fatalf("CreateTab() error = %v", err)
	}

	target, err := manager.ProxyTarget(tab.ID, "/dashboard", "x=1")
	if err != nil {
		t.Fatalf("ProxyTarget() error = %v", err)
	}
	if target.String() != "http://localhost:3000/dashboard?x=1" {
		t.Fatalf("ProxyTarget() = %q", target.String())
	}

	if _, err := manager.NavigateTab(tab.ID, "https://example.com/docs"); err != nil {
		t.Fatalf("NavigateTab() error = %v", err)
	}
	if len(manager.ListTabs()) != 1 {
		t.Fatalf("ListTabs() length = %d", len(manager.ListTabs()))
	}
	if err := manager.DeleteTab(tab.ID); err != nil {
		t.Fatalf("DeleteTab() error = %v", err)
	}
}
