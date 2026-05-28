package main

import (
	"strings"
	"testing"
)

func TestTerminalToolsDescribeWorkflowChaining(t *testing.T) {
	tools := terminalTools()
	descriptions := make(map[string]string, len(tools))
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		description, _ := tool["description"].(string)
		descriptions[name] = description
	}

	if got := descriptions["active_terminal_run"]; !strings.Contains(got, "advances the current debugging or implementation workflow") {
		t.Fatalf("expected workflow guidance for run, got %q", got)
	}
	if got := descriptions["active_terminal_run"]; !strings.Contains(got, "Chain another targeted terminal or browser action") {
		t.Fatalf("expected chaining guidance for run, got %q", got)
	}
	if got := descriptions["active_terminal_start"]; !strings.Contains(got, "while you debug with browser MCP or inspect logs") {
		t.Fatalf("expected browser/terminal workflow guidance for start, got %q", got)
	}
	if got := descriptions["active_terminal_read"]; !strings.Contains(got, "gather logs after browser reproduction") {
		t.Fatalf("expected log observation guidance for read, got %q", got)
	}
}
