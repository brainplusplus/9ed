package chat

import (
	"encoding/json"
	"testing"

	"github.com/brainplusplus/9ed/internal/chat/acp"
)

func TestCommandFromToolCallRequiresExecutableKind(t *testing.T) {
	raw, err := json.Marshal(map[string]string{"command": "Get-ChildItem"})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	if got := commandFromToolCall(acp.ToolCallUpdate{
		Kind:     acp.ToolKind("other"),
		RawInput: raw,
	}); got != "" {
		t.Fatalf("expected non-executable tool kind to be ignored, got %q", got)
	}

	if got := commandFromToolCall(acp.ToolCallUpdate{
		Kind:     acp.ToolKindExecute,
		RawInput: raw,
	}); got != "Get-ChildItem" {
		t.Fatalf("expected execute tool command, got %q", got)
	}
}

func TestApplyRememberedToolMetaFillsCompletedUpdate(t *testing.T) {
	session := &acpSession{
		toolMeta: make(map[string]acpToolMeta),
	}
	raw, err := json.Marshal(map[string]string{"selector": `a[href*="/d-"]`})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	session.rememberToolMeta("tool-1", "9ed-active-browser_9ed_browser_click", acp.ToolKind("browser"), raw)

	update := acp.ToolCallStatusUpdate{
		ToolCallID: "tool-1",
		Status:     acp.ToolStatusCompleted,
	}
	session.applyRememberedToolMeta(&update)

	if update.Title != "9ed-active-browser_9ed_browser_click" {
		t.Fatalf("expected remembered title, got %q", update.Title)
	}
	if string(update.Kind) != "browser" {
		t.Fatalf("expected remembered kind, got %q", update.Kind)
	}
	if string(update.RawInput) != string(raw) {
		t.Fatalf("expected remembered raw input, got %s", string(update.RawInput))
	}
}
