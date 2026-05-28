package main

import "testing"

func TestMapToolActionSupportsCanonicalAndAliases(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want string
	}{
		{name: "goto canonical", tool: "9ed_browser_goto", want: "goto"},
		{name: "goto legacy", tool: "active_browser_goto", want: "goto"},
		{name: "goto alias", tool: "browser_navigate", want: "goto"},
		{name: "click", tool: "active_browser_click", want: "click"},
		{name: "type", tool: "active_browser_type", want: "type"},
		{name: "press", tool: "active_browser_press", want: "press"},
		{name: "scroll", tool: "active_browser_scroll", want: "scroll"},
		{name: "inspect", tool: "active_browser_inspect", want: "inspect"},
		{name: "screenshot", tool: "active_browser_screenshot", want: "screenshot"},
		{name: "console logs", tool: "active_browser_console_logs", want: "console_logs"},
		{name: "network requests", tool: "active_browser_network_requests", want: "network_requests"},
		{name: "unknown", tool: "active_browser_unknown", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapToolAction(tt.tool); got != tt.want {
				t.Fatalf("mapToolAction(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

func TestBrowserToolsIncludeTelemetryTools(t *testing.T) {
	tools := browserTools()
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		names[name] = true
	}

	for _, name := range []string{
		"9ed_browser_goto",
		"9ed_browser_click",
		"9ed_browser_type",
		"9ed_browser_press",
		"9ed_browser_scroll",
		"9ed_browser_inspect",
		"9ed_browser_screenshot",
		"9ed_browser_console_logs",
		"9ed_browser_network_requests",
	} {
		if !names[name] {
			t.Fatalf("expected tool %q to be registered", name)
		}
	}
}
