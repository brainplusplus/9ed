package chat

import (
	"testing"
)

// TestSetUseActiveBrowserUpdatesSessionOpts verifies that SetUseActiveBrowser
// keeps s.sessionOpts in sync with the live toggle state. tryRestart reads
// s.sessionOpts when recreating the adapter (via activeMCPServersForOptions),
// so a stale sessionOpts would silently drop the browser MCP server on
// auto-restart. This validates the "SetUseActiveBrowser updates both
// s.useActiveBrowser and s.sessionOpts.UseActiveBrowser" expected behavior.
func TestSetUseActiveBrowserUpdatesSessionOpts(t *testing.T) {
	s := &acpSession{
		sessionOpts: SessionOptions{
			UseActiveBrowser:   false,
			ActiveBrowserTabID: "",
		},
	}

	s.SetUseActiveBrowser(true, "tab-99")

	if !s.useActiveBrowser {
		t.Errorf("s.useActiveBrowser = false, want true")
	}
	if s.activeBrowserTabID != "tab-99" {
		t.Errorf("s.activeBrowserTabID = %q, want %q", s.activeBrowserTabID, "tab-99")
	}
	if !s.sessionOpts.UseActiveBrowser {
		t.Errorf("s.sessionOpts.UseActiveBrowser = false, want true (stale opts would drop browser MCP on restart)")
	}
	if s.sessionOpts.ActiveBrowserTabID != "tab-99" {
		t.Errorf("s.sessionOpts.ActiveBrowserTabID = %q, want %q", s.sessionOpts.ActiveBrowserTabID, "tab-99")
	}

	// Toggling back off should also propagate.
	s.SetUseActiveBrowser(false, "")
	if s.useActiveBrowser {
		t.Errorf("s.useActiveBrowser = true, want false")
	}
	if s.sessionOpts.UseActiveBrowser {
		t.Errorf("s.sessionOpts.UseActiveBrowser = true, want false")
	}
	if s.sessionOpts.ActiveBrowserTabID != "" {
		t.Errorf("s.sessionOpts.ActiveBrowserTabID = %q, want empty", s.sessionOpts.ActiveBrowserTabID)
	}
}

// TestSetUseActiveTerminalUpdatesSessionOpts verifies that SetUseActiveTerminal
// keeps s.sessionOpts in sync with the live toggle state, mirroring the
// browser toggle fix. This validates the "SetUseActiveTerminal updates both
// s.useActiveTerminal and s.sessionOpts.UseActiveTerminal" expected behavior.
func TestSetUseActiveTerminalUpdatesSessionOpts(t *testing.T) {
	s := &acpSession{
		sessionOpts: SessionOptions{
			UseActiveTerminal: false,
			ActiveTerminalID:  "",
		},
	}

	s.SetUseActiveTerminal(true, "term-42")

	if !s.useActiveTerminal {
		t.Errorf("s.useActiveTerminal = false, want true")
	}
	if s.activeTerminalID != "term-42" {
		t.Errorf("s.activeTerminalID = %q, want %q", s.activeTerminalID, "term-42")
	}
	if !s.sessionOpts.UseActiveTerminal {
		t.Errorf("s.sessionOpts.UseActiveTerminal = false, want true (stale opts would drop terminal MCP on restart)")
	}
	if s.sessionOpts.ActiveTerminalID != "term-42" {
		t.Errorf("s.sessionOpts.ActiveTerminalID = %q, want %q", s.sessionOpts.ActiveTerminalID, "term-42")
	}

	// Toggling back off should also propagate.
	s.SetUseActiveTerminal(false, "")
	if s.useActiveTerminal {
		t.Errorf("s.useActiveTerminal = true, want false")
	}
	if s.sessionOpts.UseActiveTerminal {
		t.Errorf("s.sessionOpts.UseActiveTerminal = true, want false")
	}
	if s.sessionOpts.ActiveTerminalID != "" {
		t.Errorf("s.sessionOpts.ActiveTerminalID = %q, want empty", s.sessionOpts.ActiveTerminalID)
	}
}

// TestSetUseActiveBrowserTrimsTabID ensures whitespace-only tab IDs are
// normalized so tryRestart does not pass a blank-but-nonempty ID.
func TestSetUseActiveBrowserTrimsTabID(t *testing.T) {
	s := &acpSession{sessionOpts: SessionOptions{}}

	s.SetUseActiveBrowser(true, "  tab-x  ")

	if s.activeBrowserTabID != "tab-x" {
		t.Errorf("s.activeBrowserTabID = %q, want %q (trimmed)", s.activeBrowserTabID, "tab-x")
	}
	if s.sessionOpts.ActiveBrowserTabID != "tab-x" {
		t.Errorf("s.sessionOpts.ActiveBrowserTabID = %q, want %q (trimmed)", s.sessionOpts.ActiveBrowserTabID, "tab-x")
	}
}

// TestSetUseActiveTerminalTrimsTerminalID ensures whitespace-only terminal IDs
// are normalized.
func TestSetUseActiveTerminalTrimsTerminalID(t *testing.T) {
	s := &acpSession{sessionOpts: SessionOptions{}}

	s.SetUseActiveTerminal(true, "  \tterm-y  ")

	if s.activeTerminalID != "term-y" {
		t.Errorf("s.activeTerminalID = %q, want %q (trimmed)", s.activeTerminalID, "term-y")
	}
	if s.sessionOpts.ActiveTerminalID != "term-y" {
		t.Errorf("s.sessionOpts.ActiveTerminalID = %q, want %q (trimmed)", s.sessionOpts.ActiveTerminalID, "term-y")
	}
}
