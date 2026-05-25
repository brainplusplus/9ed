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
