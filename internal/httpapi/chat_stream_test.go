package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
)

func TestChatStreamBroadcastsToSubscribersAndPersistsOnce(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	var persisted []chat.ChatEvent
	stream := newChatStream("session-1", session, func(evt chat.ChatEvent) {
		persisted = append(persisted, evt)
	}, nil)
	stream.Start()

	subA := stream.Subscribe()
	defer stream.Unsubscribe(subA)
	subB := stream.Subscribe()
	defer stream.Unsubscribe(subB)

	want := chat.ChatEvent{Type: "text", Text: "hello"}
	session.events <- want

	if got := readChatEvent(t, subA.C); got.Type != want.Type || got.Text != want.Text {
		t.Fatalf("subscriber A got %#v, want %#v", got, want)
	}
	if got := readChatEvent(t, subB.C); got.Type != want.Type || got.Text != want.Text {
		t.Fatalf("subscriber B got %#v, want %#v", got, want)
	}
	if len(persisted) != 1 || persisted[0].Text != want.Text {
		t.Fatalf("persisted events = %#v, want one %#v", persisted, want)
	}
}

func TestChatStreamEmitsSessionClosedDoneAfterNewTurn(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{Type: "done", StopReason: "end_turn"})
	if got := readChatEvent(t, sub.C); got.Type != "done" || got.StopReason != "end_turn" {
		t.Fatalf("expected prior turn done event, got %#v", got)
	}

	stream.StartTurn()
	close(session.done)

	if got := readChatEvent(t, sub.C); got.Type != "done" || got.StopReason != "session_closed" {
		t.Fatalf("expected session_closed done after turn reset, got %#v", got)
	}
}

func TestChatStreamWatchdogRecoversTurnWithoutToolEvents(t *testing.T) {
	originalInactivity := interactiveTurnInactivityTimeout
	interactiveTurnInactivityTimeout = 40 * time.Millisecond
	defer func() {
		interactiveTurnInactivityTimeout = originalInactivity
	}()

	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 2),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.StartTurn()
	done := readChatEventTimeout(t, sub.C, 250*time.Millisecond)
	if done.Type != "done" || done.StopReason != "turn_inactivity_timeout_stream" {
		t.Fatalf("expected inactivity recovery done, got %#v", done)
	}
	if session.cancelCalls != 1 {
		t.Fatalf("expected cancel to be invoked once, got %d", session.cancelCalls)
	}
}

func TestChatStreamRegistryLatestTracksTouchedStream(t *testing.T) {
	registry := newChatStreamRegistry()
	sessionA := &fakeChatSession{events: make(chan chat.ChatEvent), done: make(chan struct{})}
	sessionB := &fakeChatSession{events: make(chan chat.ChatEvent), done: make(chan struct{})}

	registry.GetOrCreate("session-a", sessionA, nil)
	registry.GetOrCreate("session-b", sessionB, nil)

	if latest, ok := registry.LatestID(); !ok || latest != "session-b" {
		t.Fatalf("expected latest session-b after create, got %q ok=%v", latest, ok)
	}
	if !registry.Touch("session-a") {
		t.Fatal("expected touch session-a to succeed")
	}
	if latest, ok := registry.LatestID(); !ok || latest != "session-a" {
		t.Fatalf("expected latest session-a after touch, got %q ok=%v", latest, ok)
	}
	if registry.Touch("missing") {
		t.Fatal("expected touch missing to fail")
	}

	close(sessionA.done)
	close(sessionB.done)
}

func TestChatStreamDoesNotSynthesizeTerminalFallbackAfterToolCompletion(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: check\nTerminal status: completed\n\nOutput:\n8183 Established 874616 server",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected tool update first, got %#v", got)
	}
	assertNoChatEvent(t, sub.C, 300*time.Millisecond)
}

func TestChatStreamKeepsBareDoneWithoutSyntheticFallbackText(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: check\nTerminal status: completed\n\nOutput:\n8183 Established 874616 server",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected tool update first, got %#v", got)
	}
	stream.publish(chat.ChatEvent{Type: "done", StopReason: "end_turn"})
	done := readChatEvent(t, sub.C)
	if done.Type != "done" || done.StopReason != "end_turn" {
		t.Fatalf("expected original done, got %#v", done)
	}
	assertNoChatEvent(t, sub.C, 200*time.Millisecond)
}

func TestChatStreamThinkingDoesNotTriggerSyntheticFallbackText(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: check\nTerminal status: completed\n\nOutput:\n8183 Established 874616 server",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected tool update first, got %#v", got)
	}
	stream.publish(chat.ChatEvent{Type: "thinking"})
	if got := readChatEvent(t, sub.C); got.Type != "thinking" {
		t.Fatalf("expected thinking event, got %#v", got)
	}
	assertNoChatEvent(t, sub.C, 300*time.Millisecond)
}

func TestChatStreamRecoversDoneAfterCompletedInteractiveToolChainStall(t *testing.T) {
	originalTimeout := interactiveToolDoneTimeout
	originalUnsureTimeout := interactiveToolUnsureTimeout
	interactiveToolDoneTimeout = 40 * time.Millisecond
	interactiveToolUnsureTimeout = 40 * time.Millisecond
	defer func() {
		interactiveToolDoneTimeout = originalTimeout
		interactiveToolUnsureTimeout = originalUnsureTimeout
	}()

	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolCallID:  "tc-netstat",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: netstat -ano | Select-String \":8183\"\nTerminal status: completed\n\nOutput:\nTCP    127.0.0.1:8183    127.0.0.1:59105    ESTABLISHED    30792",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected tool update first, got %#v", got)
	}
	stream.publish(chat.ChatEvent{
		Type:       "tool_call",
		ToolCallID: "tc-process",
		ToolTitle:  "active_terminal_run",
		ToolKind:   "execute",
		ToolStatus: "pending",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call" {
		t.Fatalf("expected second tool call, got %#v", got)
	}
	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolCallID:  "tc-process",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: Get-Process -Id 821452, 30792, 125156 | Format-Table Name, Id, SessionId -AutoSize\nTerminal status: completed\n\nOutput:\nName                Id SessionId\n----                -- ---------\nactive-terminal-mcp 125156         1\nchrome             30792         1\nserver            821452         1",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected second tool update, got %#v", got)
	}
	text := readChatEventTimeout(t, sub.C, 300*time.Millisecond)
	if text.Type != "text" || !strings.Contains(text.Text, "Untuk port `8183`, proses yang dicek adalah") || !strings.Contains(text.Text, "`server` (PID `821452`)") {
		t.Fatalf("expected synthesized recovery text, got %#v", text)
	}
	done := readChatEventTimeout(t, sub.C, 300*time.Millisecond)
	if done.Type != "done" || done.StopReason != "tool_completion_timeout_stream" {
		t.Fatalf("expected stream recovery done, got %#v", done)
	}
	if session.cancelCalls != 1 {
		t.Fatalf("expected cancel to be invoked once, got %d", session.cancelCalls)
	}
}

func TestChatStreamThinkingCancelsDoneRecovery(t *testing.T) {
	originalTimeout := interactiveToolDoneTimeout
	originalUnsureTimeout := interactiveToolUnsureTimeout
	interactiveToolDoneTimeout = 40 * time.Millisecond
	interactiveToolUnsureTimeout = 40 * time.Millisecond
	defer func() {
		interactiveToolDoneTimeout = originalTimeout
		interactiveToolUnsureTimeout = originalUnsureTimeout
	}()

	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: Get-Process -Id 152164 | Format-Table Name, Id, SessionId -AutoSize\nTerminal status: completed\n\nOutput:\nName       Id SessionId\n----       -- ---------\nserver 152164         1",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected tool update first, got %#v", got)
	}
	stream.publish(chat.ChatEvent{Type: "thinking"})
	if got := readChatEvent(t, sub.C); got.Type != "thinking" {
		t.Fatalf("expected thinking event, got %#v", got)
	}
	assertNoChatEvent(t, sub.C, 120*time.Millisecond)
	if session.cancelCalls != 0 {
		t.Fatalf("expected no cancel after continued progress, got %d", session.cancelCalls)
	}
}

func TestChatStreamUnsureTerminalObservationWaitsLongerBeforeRecovery(t *testing.T) {
	originalTimeout := interactiveToolDoneTimeout
	originalUnsureTimeout := interactiveToolUnsureTimeout
	interactiveToolDoneTimeout = 30 * time.Millisecond
	interactiveToolUnsureTimeout = 140 * time.Millisecond
	defer func() {
		interactiveToolDoneTimeout = originalTimeout
		interactiveToolUnsureTimeout = originalUnsureTimeout
	}()

	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolCallID:  "tc-netstat",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: Get-NetTCPConnection -LocalPort 8183 | Format-List\nTerminal status: completed\nDecision: if this output contains the requested fact, answer now. Run another command only for missing information, not for redundant confirmation.\n\nOutput:\nLocalPort : 8183\nOwningProcess : 956544",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected tool update first, got %#v", got)
	}
	assertNoChatEvent(t, sub.C, 80*time.Millisecond)
	text := readChatEventTimeout(t, sub.C, 200*time.Millisecond)
	if text.Type != "text" || !strings.Contains(text.Text, "port `8183`") || !strings.Contains(text.Text, "`956544`") {
		t.Fatalf("expected delayed recovery summary, got %#v", text)
	}
}

func TestTurnRecoveryDelayUsesBrowserDecisionHint(t *testing.T) {
	doneDelay := turnRecoveryDelay(chat.ChatEvent{
		ToolTitle:   "9ed_browser_inspect",
		ToolContent: "Browser tool result.\nDecision: sufficient_to_answer=true.",
	})
	if doneDelay != interactiveToolDoneTimeout {
		t.Fatalf("expected done timeout for sufficient browser observation, got %s", doneDelay)
	}

	unsureDelay := turnRecoveryDelay(chat.ChatEvent{
		ToolTitle:   "9ed_browser_goto",
		ToolContent: "Browser tool result.\nDecision: sufficient_to_answer=false.",
	})
	if unsureDelay != interactiveToolUnsureTimeout {
		t.Fatalf("expected unsure timeout for incomplete browser observation, got %s", unsureDelay)
	}
}

func TestChatStreamUnsureBrowserObservationWaitsLongerBeforeRecovery(t *testing.T) {
	originalTimeout := interactiveToolDoneTimeout
	originalUnsureTimeout := interactiveToolUnsureTimeout
	interactiveToolDoneTimeout = 30 * time.Millisecond
	interactiveToolUnsureTimeout = 140 * time.Millisecond
	defer func() {
		interactiveToolDoneTimeout = originalTimeout
		interactiveToolUnsureTimeout = originalUnsureTimeout
	}()

	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:       "tool_call_update",
		ToolCallID: "browser-goto",
		ToolTitle:  "9ed_browser_goto",
		ToolStatus: "completed",
		ToolContent: strings.Join([]string{
			"Browser tool result.",
			"Decision: sufficient_to_answer=false. Continue with one minimal follow-up browser observation/action only if needed to answer the user.",
			"",
			`{"url":"https://example.com/docs","title":"Integration Fixture","text":"BROWSER_CHAIN_TARGET appears on this page."}`,
		}, "\n"),
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected browser tool update first, got %#v", got)
	}
	assertNoChatEvent(t, sub.C, 80*time.Millisecond)
	text := readChatEventTimeout(t, sub.C, 240*time.Millisecond)
	if text.Type != "text" || !strings.Contains(text.Text, "Integration Fixture") || !strings.Contains(text.Text, "BROWSER_CHAIN_TARGET") {
		t.Fatalf("expected delayed browser recovery summary, got %#v", text)
	}
	done := readChatEventTimeout(t, sub.C, 200*time.Millisecond)
	if done.Type != "done" || done.StopReason != "tool_completion_timeout_stream" {
		t.Fatalf("expected stream recovery done, got %#v", done)
	}
}

func TestChatStreamRecoversDoneAfterCompletedBrowserToolChainStall(t *testing.T) {
	originalTimeout := interactiveToolDoneTimeout
	originalUnsureTimeout := interactiveToolUnsureTimeout
	interactiveToolDoneTimeout = 40 * time.Millisecond
	interactiveToolUnsureTimeout = 40 * time.Millisecond
	defer func() {
		interactiveToolDoneTimeout = originalTimeout
		interactiveToolUnsureTimeout = originalUnsureTimeout
	}()

	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 6),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{
		Type:       "tool_call_update",
		ToolCallID: "browser-goto",
		ToolTitle:  "9ed_browser_goto",
		ToolStatus: "completed",
		ToolContent: strings.Join([]string{
			"Browser tool result.",
			"Decision: sufficient_to_answer=false. Continue with one minimal follow-up browser observation/action only if needed to answer the user.",
			"",
			`{"url":"https://example.com/docs","title":"Setup Page","text":"Setup page for integration test."}`,
		}, "\n"),
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected first browser tool update, got %#v", got)
	}
	stream.publish(chat.ChatEvent{
		Type:       "tool_call",
		ToolCallID: "browser-inspect",
		ToolTitle:  "9ed_browser_inspect",
		ToolKind:   "browser",
		ToolStatus: "pending",
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call" {
		t.Fatalf("expected inspect tool call, got %#v", got)
	}
	stream.publish(chat.ChatEvent{
		Type:       "tool_call_update",
		ToolCallID: "browser-inspect",
		ToolTitle:  "9ed_browser_inspect",
		ToolStatus: "completed",
		ToolContent: strings.Join([]string{
			"Browser tool result.",
			"Decision: sufficient_to_answer=true. If this observation already satisfies the user's request, answer now and avoid extra browser tool calls.",
			"",
			`{"url":"https://example.com/docs","title":"Integration Fixture","text":"BROWSER_CHAIN_TARGET is visible in the hero section."}`,
		}, "\n"),
	})
	if got := readChatEvent(t, sub.C); got.Type != "tool_call_update" {
		t.Fatalf("expected inspect tool update, got %#v", got)
	}
	text := readChatEventTimeout(t, sub.C, 300*time.Millisecond)
	if text.Type != "text" || !strings.Contains(text.Text, "Integration Fixture") || !strings.Contains(text.Text, "BROWSER_CHAIN_TARGET") {
		t.Fatalf("expected synthesized browser recovery text, got %#v", text)
	}
	done := readChatEventTimeout(t, sub.C, 300*time.Millisecond)
	if done.Type != "done" || done.StopReason != "tool_completion_timeout_stream" {
		t.Fatalf("expected stream recovery done, got %#v", done)
	}
	if session.cancelCalls != 1 {
		t.Fatalf("expected cancel to be invoked once, got %d", session.cancelCalls)
	}
}

func TestChatStreamDropsLateToolEventsAfterDone(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	stream := newChatStream("session-1", session, nil, nil)
	stream.Start()

	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	stream.publish(chat.ChatEvent{Type: "done", StopReason: "tool_completion_timeout_stream"})
	done := readChatEvent(t, sub.C)
	if done.Type != "done" {
		t.Fatalf("expected done event, got %#v", done)
	}

	stream.publish(chat.ChatEvent{
		Type:        "tool_call_update",
		ToolCallID:  "late-tool",
		ToolTitle:   "active_terminal_run",
		ToolStatus:  "completed",
		ToolContent: "Terminal command: Get-Process -Id 956544\nTerminal status: completed\n\nOutput:\nserver",
	})
	assertNoChatEvent(t, sub.C, 80*time.Millisecond)
}

func TestChatStreamFallbackSummarizesProcessNameCommand(t *testing.T) {
	text := terminalFallbackText(
		"Terminal command: (Get-Process -Id 164496).ProcessName\nTerminal status: completed\n\nOutput:\nserver",
		"",
	)
	if !strings.Contains(text, "PID `164496`") || !strings.Contains(text, "`server`") {
		t.Fatalf("expected PID/process fallback, got %q", text)
	}
}

func TestTerminalDecisionHintMarksProcessResultSufficient(t *testing.T) {
	text := terminalDecisionHint(
		"(Get-Process -Id 164496).ProcessName",
		"server",
		"completed",
	)
	if !strings.Contains(text, "sufficient_to_answer=true") || !strings.Contains(text, "do not run tasklist/Get-Process again") {
		t.Fatalf("expected sufficient decision hint, got %q", text)
	}
}

func readChatEvent(t *testing.T, ch <-chan chat.ChatEvent) chat.ChatEvent {
	t.Helper()
	return readChatEventTimeout(t, ch, time.Second)
}

func readChatEventTimeout(t *testing.T, ch <-chan chat.ChatEvent, timeout time.Duration) chat.ChatEvent {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(timeout):
		t.Fatal("timed out waiting for chat event")
		return chat.ChatEvent{}
	}
}

func assertNoChatEvent(t *testing.T, ch <-chan chat.ChatEvent, timeout time.Duration) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("expected no chat event, got %#v", evt)
	case <-time.After(timeout):
	}
}

type fakeChatSession struct {
	events      chan chat.ChatEvent
	done        chan struct{}
	cancelCalls int
}

func (s *fakeChatSession) ID() string                                            { return "session-1" }
func (s *fakeChatSession) AgentID() string                                       { return "opencode" }
func (s *fakeChatSession) WorkDir() string                                       { return "" }
func (s *fakeChatSession) Mode() chat.SessionMode                                { return chat.ModeACP }
func (s *fakeChatSession) Events() <-chan chat.ChatEvent                         { return s.events }
func (s *fakeChatSession) Done() <-chan struct{}                                 { return s.done }
func (s *fakeChatSession) Send(context.Context, string, []chat.Attachment) error { return nil }
func (s *fakeChatSession) Cancel() error                                         { s.cancelCalls++; return nil }
func (s *fakeChatSession) Close() error                                          { close(s.done); return nil }
func (s *fakeChatSession) SetConfigOption(context.Context, string, string) error { return nil }
func (s *fakeChatSession) ACPSessionID() string                                  { return "" }
func (s *fakeChatSession) IsResumed() bool                                       { return false }
func (s *fakeChatSession) RespondPermission(chat.PermissionResponse)             {}
func (s *fakeChatSession) SetAutoApprove(bool)                                   {}
func (s *fakeChatSession) SetUseActiveTerminal(bool, string)                     {}
func (s *fakeChatSession) UseActiveTerminalEnabled() bool                        { return false }
func (s *fakeChatSession) ActiveTerminalID() string                              { return "" }
func (s *fakeChatSession) SetUseActiveBrowser(bool, string)                      {}
func (s *fakeChatSession) UseActiveBrowserEnabled() bool                         { return false }
func (s *fakeChatSession) ActiveBrowserTabID() string                            { return "" }
