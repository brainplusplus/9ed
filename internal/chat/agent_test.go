package chat

import (
	"encoding/json"
	"strings"
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

func TestDecorateActiveToolMessageAddsWorkflowGuidance(t *testing.T) {
	got := decorateActiveToolMessage("debug why page A errors", true, true)

	if !strings.Contains(got, "debug why page A errors") {
		t.Fatalf("expected original prompt to be preserved, got %q", got)
	}
	if !strings.Contains(got, "whether the need comes from the user's words or from your own analysis") {
		t.Fatalf("expected workflow-based browser guidance, got %q", got)
	}
	if !strings.Contains(got, "Do not use 9ed_browser_screenshot just to decide what to click") {
		t.Fatalf("expected screenshot guardrail, got %q", got)
	}
	if !strings.Contains(got, "Chain terminal commands when the last result reveals the next necessary diagnostic or fix step") {
		t.Fatalf("expected terminal chaining guidance, got %q", got)
	}
}
