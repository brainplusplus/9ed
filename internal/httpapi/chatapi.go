package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/chat/agentconfig"
	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/brainplusplus/9ed/internal/terminal"
)

type chatCreateRequest struct {
	AgentID            string `json:"agentId"`
	WorkDir            string `json:"workDir,omitempty"`
	ResumeID           string `json:"resumeId,omitempty"`
	ACPSessionID       string `json:"acpSessionId,omitempty"`
	UseActiveTerminal  bool   `json:"useActiveTerminal,omitempty"`
	ActiveTerminalID   string `json:"activeTerminalId,omitempty"`
	UseActiveBrowser   bool   `json:"useActiveBrowser,omitempty"`
	ActiveBrowserTabID string `json:"activeBrowserTabId,omitempty"`
}

type chatCreateResponse struct {
	ID           string `json:"id"`
	Mode         string `json:"mode"`
	IsResumed    bool   `json:"isResumed"`
	ResumedFrom  string `json:"resumedFrom,omitempty"`
	WorkDir      string `json:"workDir,omitempty"`
	ACPSessionID string `json:"acpSessionId,omitempty"`
}

type chatSessionInfo struct {
	ID           string `json:"id"`
	AgentID      string `json:"agentId"`
	Mode         string `json:"mode"`
	WorkDir      string `json:"workDir,omitempty"`
	ACPSessionID string `json:"acpSessionId,omitempty"`
	IsResumed    bool   `json:"isResumed"`
}

type chatRestoreResponse struct {
	Found            bool   `json:"found"`
	SessionID        string `json:"sessionId,omitempty"`
	LiveSessionID    string `json:"liveSessionId,omitempty"`
	AgentID          string `json:"agentId,omitempty"`
	WorkDir          string `json:"workDir,omitempty"`
	ACPSessionID     string `json:"acpSessionId,omitempty"`
	Status           string `json:"status,omitempty"`
	Title            string `json:"title,omitempty"`
	IsLive           bool   `json:"isLive"`
	CanResume        bool   `json:"canResume"`
	AgentSupportsACP bool   `json:"agentSupportsAcp,omitempty"`
	AgentAvailable   bool   `json:"agentAvailable,omitempty"`
	ResumeError      string `json:"resumeError,omitempty"`
}

type chatResumeRequest struct {
	SessionID          string `json:"sessionId"`
	AgentID            string `json:"agentId"`
	WorkDir            string `json:"workDir"`
	ACPSessionID       string `json:"acpSessionId"`
	UseActiveTerminal  bool   `json:"useActiveTerminal,omitempty"`
	ActiveTerminalID   string `json:"activeTerminalId,omitempty"`
	UseActiveBrowser   bool   `json:"useActiveBrowser,omitempty"`
	ActiveBrowserTabID string `json:"activeBrowserTabId,omitempty"`
}

var discoverAgentDescriptors = chat.DiscoverAgentDescriptors

type chatWSInbound struct {
	Type        string           `json:"type"`
	Content     string           `json:"content,omitempty"`
	Context     json.RawMessage  `json:"context,omitempty"`
	ConfigID    string           `json:"configId,omitempty"`
	Value       string           `json:"value,omitempty"`
	Attachments []chatAttachment `json:"attachments,omitempty"`

	PermissionID       string `json:"permissionId,omitempty"`
	OptionID           string `json:"optionId,omitempty"`
	Cancelled          bool   `json:"cancelled,omitempty"`
	AutoApprove        bool   `json:"autoApprove,omitempty"`
	UseActiveTerminal  bool   `json:"useActiveTerminal,omitempty"`
	ActiveTerminalID   string `json:"activeTerminalId,omitempty"`
	UseActiveBrowser   bool   `json:"useActiveBrowser,omitempty"`
	ActiveBrowserTabID string `json:"activeBrowserTabId,omitempty"`
}

type chatAttachment struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Name string `json:"name"`
}

type chatTerminalRunRequest struct {
	SessionID string `json:"sessionId"`
	Action    string `json:"action,omitempty"`
	Command   string `json:"command,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

type chatBrowserRunRequest struct {
	SessionID string   `json:"sessionId"`
	Action    string   `json:"action"`
	URL       string   `json:"url,omitempty"`
	Selector  string   `json:"selector,omitempty"`
	Text      string   `json:"text,omitempty"`
	Key       string   `json:"key,omitempty"`
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	DeltaX    float64  `json:"deltaX,omitempty"`
	DeltaY    float64  `json:"deltaY,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	TimeoutMS int      `json:"timeoutMs,omitempty"`
}

func summarizeBrowserRunRequest(req chatBrowserRunRequest) string {
	parts := make([]string, 0, 8)
	if url := strings.TrimSpace(req.URL); url != "" {
		parts = append(parts, "url="+truncateBrowserLogValue(url, 120))
	}
	if selector := strings.TrimSpace(req.Selector); selector != "" {
		parts = append(parts, "selector="+truncateBrowserLogValue(selector, 120))
	}
	if req.Text != "" {
		parts = append(parts, "textBytes="+strconv.Itoa(len(req.Text)))
		parts = append(parts, "text="+truncateBrowserLogValue(req.Text, 80))
	}
	if key := strings.TrimSpace(req.Key); key != "" {
		parts = append(parts, "key="+truncateBrowserLogValue(key, 40))
	}
	if req.X != nil && req.Y != nil {
		parts = append(parts, fmt.Sprintf("point=%.1f,%.1f", *req.X, *req.Y))
	}
	if req.DeltaX != 0 || req.DeltaY != 0 {
		parts = append(parts, fmt.Sprintf("delta=%.1f,%.1f", req.DeltaX, req.DeltaY))
	}
	if req.Limit > 0 {
		parts = append(parts, "limit="+strconv.Itoa(req.Limit))
	}
	if req.TimeoutMS > 0 {
		parts = append(parts, "timeoutMs="+strconv.Itoa(req.TimeoutMS))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func truncateBrowserLogValue(value string, maxLen int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func browserToolRawInput(req chatBrowserRunRequest) string {
	payload := map[string]any{
		"action": req.Action,
	}
	if strings.TrimSpace(req.URL) != "" {
		payload["url"] = strings.TrimSpace(req.URL)
	}
	if strings.TrimSpace(req.Selector) != "" {
		payload["selector"] = strings.TrimSpace(req.Selector)
	}
	if req.Text != "" {
		payload["text"] = req.Text
	}
	if strings.TrimSpace(req.Key) != "" {
		payload["key"] = strings.TrimSpace(req.Key)
	}
	if req.X != nil {
		payload["x"] = *req.X
	}
	if req.Y != nil {
		payload["y"] = *req.Y
	}
	if req.DeltaX != 0 {
		payload["deltaX"] = req.DeltaX
	}
	if req.DeltaY != 0 {
		payload["deltaY"] = req.DeltaY
	}
	if req.Limit > 0 {
		payload["limit"] = req.Limit
	}
	if req.TimeoutMS > 0 {
		payload["timeoutMs"] = req.TimeoutMS
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func browserActionContext(parent context.Context, timeoutMS int) (context.Context, context.CancelFunc) {
	if timeoutMS <= 0 {
		timeoutMS = 15000
	}
	if timeoutMS > 60000 {
		timeoutMS = 60000
	}
	return context.WithTimeout(parent, time.Duration(timeoutMS)*time.Millisecond)
}

func browserToolOutcome(action string, detail string) string {
	action = strings.TrimSpace(strings.ToLower(action))
	detail = strings.TrimSpace(detail)
	switch action {
	case "goto", "navigate":
		if detail != "" {
			return "Opened " + detail
		}
		return "Opened page"
	case "click":
		if detail != "" {
			return "Clicked " + detail
		}
		return "Clicked element"
	case "type":
		if detail != "" {
			return "Typed " + detail
		}
		return "Typed text"
	case "press":
		if detail != "" {
			return "Pressed " + detail
		}
		return "Pressed key"
	case "scroll":
		return "Scrolled page"
	case "inspect":
		if detail != "" {
			return "Inspected " + detail
		}
		return "Inspected page"
	case "screenshot":
		if detail != "" {
			return "Captured " + detail
		}
		return "Captured screenshot"
	case "console_logs", "console":
		if detail != "" {
			return "Read " + detail
		}
		return "Read console logs"
	case "network_requests", "network":
		if detail != "" {
			return "Read " + detail
		}
		return "Read network requests"
	default:
		if detail != "" {
			return detail
		}
		return "Browser action completed"
	}
}

func terminalToolRawInput(req chatTerminalRunRequest) string {
	payload := map[string]any{"action": req.Action}
	if req.Command != "" {
		payload["command"] = req.Command
	}
	if req.TimeoutMS > 0 {
		payload["timeoutMs"] = req.TimeoutMS
	}
	if req.MaxBytes > 0 {
		payload["maxBytes"] = req.MaxBytes
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func terminalToolStart(req chatTerminalRunRequest) string {
	if req.Action == "read" {
		return "Reading recent active terminal output"
	}
	if req.Action == "start" {
		return "Starting long-running command in active terminal:\n" + req.Command
	}
	return "Running command in active terminal:\n" + req.Command
}

var terminalANSIPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var terminalOSCSequencePattern = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
var powershellPromptPattern = regexp.MustCompile(`^PS\s+.+>\s*$`)
var windowsDrivePromptPattern = regexp.MustCompile(`^[A-Za-z]:\\.*>\s*$`)
var unixPromptPattern = regexp.MustCompile(`^(?:.+@.+:.+[#$%]|.+[#$%])\s*$`)

func runTerminalCommand(ctx context.Context, session *terminal.ManagedSession, command string, timeoutMS int, expectPrompt bool) (string, error) {
	if timeoutMS <= 0 {
		timeoutMS = 10000
	}
	if timeoutMS > 60000 {
		timeoutMS = 60000
	}

	baselineSnapshot := session.Snapshot(20000)
	output, unsubscribe := session.Subscribe(false)
	defer unsubscribe()

	executedCommand, completionMarker := terminalCommandEnvelope(session.Profile, command)
	if _, err := session.Write([]byte(executedCommand + "\r")); err != nil {
		return "", err
	}

	var collected strings.Builder
	deadline := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer deadline.Stop()
	quiet := time.NewTimer(1200 * time.Millisecond)
	defer quiet.Stop()
	if !quiet.Stop() {
		<-quiet.C
	}

	resetQuiet := func(duration time.Duration) {
		if !quiet.Stop() {
			select {
			case <-quiet.C:
			default:
			}
		}
		quiet.Reset(duration)
	}

	sawOutput := false
	for {
		select {
		case <-ctx.Done():
			return terminalRunResult(command, collected.String(), "cancelled"), ctx.Err()
		case data, ok := <-output:
			if !ok {
				return terminalRunResult(command, stripTerminalCompletionMarker(collected.String(), completionMarker), "terminal closed"), nil
			}
			if len(data) == 0 {
				continue
			}
			sawOutput = true
			collected.Write(data)
			if terminalOutputContainsCompletionMarker(collected.String(), completionMarker) {
				return terminalRunResult(command, stripTerminalCompletionMarker(collected.String(), completionMarker), "completed"), nil
			}
			if terminalOutputLooksComplete(collected.String()) {
				resetQuiet(350 * time.Millisecond)
			} else {
				resetQuiet(1500 * time.Millisecond)
			}
		case <-quiet.C:
			snapshot := session.Snapshot(30000)
			if terminalOutputContainsCompletionMarker(collected.String(), completionMarker) {
				return terminalRunResult(command, stripTerminalCompletionMarker(collected.String(), completionMarker), "completed"), nil
			}
			if terminalShellWaitingForInput(snapshot) && (sawOutput || terminalSnapshotChanged(baselineSnapshot, snapshot)) {
				return terminalRunResult(command, stripTerminalCompletionMarker(collected.String(), completionMarker), "completed"), nil
			}
			if sawOutput {
				resetQuiet(1500 * time.Millisecond)
			} else {
				resetQuiet(900 * time.Millisecond)
			}
		case <-deadline.C:
			status := "still running after timeout"
			snapshot := session.Snapshot(30000)
			if terminalOutputContainsCompletionMarker(collected.String(), completionMarker) || terminalShellWaitingForInput(snapshot) {
				status = "completed"
			} else if !expectPrompt {
				status, _ = terminalLiveObservationStatus(snapshot, session.LastOutputAt(), time.Now())
			}
			return terminalRunResult(command, stripTerminalCompletionMarker(collected.String(), completionMarker), status), nil
		}
	}
}

const interactiveToolMinInProgress = 450 * time.Millisecond
const terminalCompletionMarkerPrefix = "\x1b]9ed-terminal-done;"
const terminalRecentOutputWindow = 2 * time.Second

func waitForInteractiveToolFloor(ctx context.Context, startedAt time.Time) {
	remaining := interactiveToolMinInProgress - time.Since(startedAt)
	if remaining <= 0 {
		return
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func terminalReadResult(session *terminal.ManagedSession, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 20000
	}
	if maxBytes > 100000 {
		maxBytes = 100000
	}
	snapshot := session.Snapshot(maxBytes)
	status, decision := terminalLiveObservationStatus(snapshot, session.LastOutputAt(), time.Now())
	return "Terminal tool result.\nTerminal status: " + status + "\n" + decision + "\n\nOutput:\n" + trimTerminalOutput(snapshot)
}

func terminalRunResult(command, rawOutput, status string) string {
	output := trimTerminalOutput(rawOutput)
	if output == "" {
		output = "(no terminal output captured)"
	}
	return fmt.Sprintf("Terminal tool result.\nTerminal command: %s\nTerminal status: %s\n%s\n\nOutput:\n%s", command, status, terminalDecisionHint(command, output, status), output)
}

func terminalDecisionHint(command, output, status string) string {
	lowerStatus := strings.ToLower(status)
	if strings.Contains(lowerStatus, "still running") || strings.Contains(lowerStatus, "streaming output") {
		return "Decision: command is still running. Do not send another terminal command yet. Use active_terminal_read to observe more output or wait until it reports waiting for input."
	}
	if strings.Contains(lowerStatus, "cancelled") || strings.Contains(lowerStatus, "closed") {
		return "Decision: the command did not complete normally. Explain the partial output or run one targeted recovery command if needed."
	}
	if summary := summarizeProcessNameCommand(command, output); summary != "" {
		return "Decision: sufficient_to_answer=true. Answer the user now using this result; do not run tasklist/Get-Process again just to confirm the same PID.\nSuggested final answer: " + summary
	}
	if summary := summarizePortProcess(output); summary != "" {
		return "Decision: sufficient_to_answer=true. Answer the user now using this result; do not run another terminal command for the same port unless the user asks for more detail.\nSuggested final answer: " + summary
	}
	return "Decision: if this output contains the requested fact, answer now. Run another command only for missing information, not for redundant confirmation."
}

func terminalLiveObservationStatus(snapshot string, lastOutputAt, now time.Time) (string, string) {
	if terminalShellWaitingForInput(snapshot) {
		return "waiting for input", "Decision: the shell is idle and ready for another command. If this output already answers the user, respond now instead of reading again."
	}
	if !lastOutputAt.IsZero() && now.Sub(lastOutputAt) <= terminalRecentOutputWindow {
		return "streaming output (process still running)", "Decision: terminal output is actively moving and the shell is not idle yet. Do not send another terminal command; use this as live observation or call active_terminal_read again if you need a fresher tail."
	}
	return "still running (quiet)", "Decision: the shell has not clearly returned to an idle prompt yet. The process still appears active even if it is currently quiet. Do not send another terminal command until active_terminal_read reports waiting for input."
}

func trimTerminalOutput(raw string) string {
	text := stripTerminalControlSequences(raw)
	text = strings.ReplaceAll(text, terminalCompletionMarkerPrefix, "")
	text = strings.ReplaceAll(text, "\x07", "")
	text = strings.ReplaceAll(text, "\x1b\\", "")
	text = strings.ReplaceAll(text, "\u0007", "")
	text = strings.ReplaceAll(text, "\u001b\\", "")
	text = strings.ReplaceAll(text, "\u001b", "")
	text = strings.ReplaceAll(text, "\u009c", "")
	text = strings.ReplaceAll(text, "\u0000", "")
	text = strings.ReplaceAll(text, "\u200b", "")
	text = strings.ReplaceAll(text, "\u200c", "")
	text = strings.ReplaceAll(text, "\u200d", "")
	text = strings.ReplaceAll(text, "\ufeff", "")
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.ReplaceAll(text, "\x1a", "")
	text = strings.ReplaceAll(text, "\x08", "")
	text = strings.ReplaceAll(text, "\x0c", "")
	text = strings.ReplaceAll(text, "\x0e", "")
	text = strings.ReplaceAll(text, "\x0f", "")
	text = strings.ReplaceAll(text, "\x7f", "")
	text = strings.ReplaceAll(text, "\u009b", "")
	text = strings.ReplaceAll(text, "\u0085", "")
	text = strings.ReplaceAll(text, "\u2028", "\n")
	text = strings.ReplaceAll(text, "\u2029", "\n")
	text = strings.ReplaceAll(text, "\u000b", "\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	const maxLen = 30000
	if len(text) > maxLen {
		return "...(truncated)\n" + text[len(text)-maxLen:]
	}
	return text
}

func terminalOutputLooksComplete(raw string) bool {
	return terminalShellWaitingForInput(raw)
}

func stripTerminalControlSequences(raw string) string {
	text := terminalOSCSequencePattern.ReplaceAllString(raw, "")
	return terminalANSIPattern.ReplaceAllString(text, "")
}

func terminalShellWaitingForInput(raw string) bool {
	line := terminalLastMeaningfulLine(raw)
	if line == "" {
		return false
	}
	if powershellPromptPattern.MatchString(line) {
		return true
	}
	if windowsDrivePromptPattern.MatchString(line) {
		return true
	}
	return unixPromptPattern.MatchString(line)
}

func terminalLastMeaningfulLine(raw string) string {
	text := trimTerminalOutput(raw)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func terminalSnapshotChanged(before, after string) bool {
	return trimTerminalOutput(before) != trimTerminalOutput(after)
}

func terminalCommandEnvelope(profile terminal.ShellProfile, command string) (string, string) {
	token := fmt.Sprintf("t%d", time.Now().UnixNano())
	switch strings.ToLower(strings.TrimSpace(profile.ID)) {
	case "pwsh", "powershell":
		marker := terminalCompletionMarkerPrefix + token + "\a"
		wrapped := fmt.Sprintf("& { %s }; [Console]::Out.Write(\"`e]9ed-terminal-done;%s`a\")", command, token)
		return wrapped, marker
	case "bash", "zsh", "sh", "git-bash":
		marker := terminalCompletionMarkerPrefix + token + "\a"
		wrapped := fmt.Sprintf("{ %s; }; printf '\\033]9ed-terminal-done;%s\\a'", command, token)
		return wrapped, marker
	default:
		return command, ""
	}
}

func terminalOutputContainsCompletionMarker(raw, marker string) bool {
	return marker != "" && strings.Contains(raw, marker)
}

func stripTerminalCompletionMarker(raw, marker string) string {
	if marker == "" {
		return raw
	}
	return strings.ReplaceAll(raw, marker, "")
}

func (a *API) handleChatTerminalRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if a.terminalMCPToken == "" || r.Header.Get("X-9ed-MCP-Token") != a.terminalMCPToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req chatTerminalRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action == "" {
		req.Action = "run"
	}
	req.Command = strings.TrimSpace(req.Command)
	if (req.Action == "run" || req.Action == "start") && req.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		if latestID, ok := a.chatStreams.LatestID(); ok {
			req.SessionID = latestID
		}
	}
	if req.SessionID == "" {
		http.Error(w, "no active chat stream", http.StatusBadRequest)
		return
	}
	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}
	session, ok := a.chatSessionManager.Get(req.SessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if !session.UseActiveTerminalEnabled() {
		http.Error(w, "active terminal integration is disabled for this chat session", http.StatusConflict)
		return
	}
	terminalID := strings.TrimSpace(session.ActiveTerminalID())
	if terminalID == "" {
		http.Error(w, "no active terminal is linked to this chat session", http.StatusConflict)
		return
	}
	term, ok := a.sessions.Get(terminalID)
	if !ok {
		http.Error(w, "linked terminal session was closed", http.StatusGone)
		return
	}

	stream := a.chatStreams.GetOrCreate(req.SessionID, session, a.newChatEventPersister(req.SessionID))
	a.chatStreams.Touch(req.SessionID)
	toolName := "active_terminal_run"
	if req.Action == "read" {
		toolName = "active_terminal_read"
	} else if req.Action == "start" {
		toolName = "active_terminal_start"
	}
	toolCallID := fmt.Sprintf("active-terminal-%d", time.Now().UnixNano())
	rawInput := terminalToolRawInput(req)
	stream.publish(chat.ChatEvent{
		Type:         "tool_call",
		ToolCallID:   toolCallID,
		ToolTitle:    toolName,
		ToolKind:     "execute",
		ToolStatus:   "pending",
		ToolRawInput: rawInput,
	})
	stream.publish(chat.ChatEvent{
		Type:         "tool_call_update",
		ToolCallID:   toolCallID,
		ToolTitle:    toolName,
		ToolStatus:   "in_progress",
		ToolContent:  terminalToolStart(req),
		ToolRawInput: rawInput,
	})
	phaseStartedAt := time.Now()

	var result string
	var err error
	switch req.Action {
	case "run":
		result, err = runTerminalCommand(r.Context(), term, req.Command, req.TimeoutMS, true)
	case "start":
		result, err = runTerminalCommand(r.Context(), term, req.Command, req.TimeoutMS, false)
	case "read":
		result = terminalReadResult(term, req.MaxBytes)
	default:
		err = fmt.Errorf("unsupported terminal action: %s", req.Action)
	}
	if err != nil {
		waitForInteractiveToolFloor(r.Context(), phaseStartedAt)
		stream.publish(chat.ChatEvent{
			Type:         "tool_call_update",
			ToolCallID:   toolCallID,
			ToolTitle:    toolName,
			ToolStatus:   "failed",
			ToolContent:  err.Error(),
			ToolRawInput: rawInput,
		})
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	waitForInteractiveToolFloor(r.Context(), phaseStartedAt)
	stream.publish(chat.ChatEvent{
		Type:         "tool_call_update",
		ToolCallID:   toolCallID,
		ToolTitle:    toolName,
		ToolStatus:   "completed",
		ToolContent:  result,
		ToolRawInput: rawInput,
	})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(result))
}

func (a *API) handleChatBrowserRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if a.browserMCPToken == "" || r.Header.Get("X-9ed-MCP-Token") != a.browserMCPToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if a.browser == nil {
		http.Error(w, "browser is disabled", http.StatusServiceUnavailable)
		return
	}

	var req chatBrowserRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.SessionID == "" {
		if latestID, ok := a.chatStreams.LatestID(); ok {
			req.SessionID = latestID
		}
	}
	if req.SessionID == "" {
		http.Error(w, "no active chat stream", http.StatusBadRequest)
		return
	}
	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}
	session, ok := a.chatSessionManager.Get(req.SessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if !session.UseActiveBrowserEnabled() {
		http.Error(w, "active browser integration is disabled for this chat session", http.StatusConflict)
		return
	}
	tabID := session.ActiveBrowserTabID()
	if tabID == "" {
		http.Error(w, "no active browser tab is linked to this chat session", http.StatusConflict)
		return
	}
	tab, ok := a.browser.Tab(tabID)
	if !ok {
		http.Error(w, "linked browser tab was closed", http.StatusGone)
		return
	}
	if tab.Transport != string(browser.TransportWebRTC) {
		http.Error(w, "browser control requires a WebRTC browser tab", http.StatusConflict)
		return
	}
	actionCtx, cancelAction := browserActionContext(r.Context(), req.TimeoutMS)
	defer cancelAction()

	startedAt := time.Now()
	action := req.Action
	toolName := strings.TrimSpace(r.Header.Get("X-9ed-MCP-Tool-Name"))
	if toolName == "" {
		toolName = action
	}
	payloadSummary := summarizeBrowserRunRequest(req)
	stream := a.chatStreams.GetOrCreate(req.SessionID, session, a.newChatEventPersister(req.SessionID))
	a.chatStreams.Touch(req.SessionID)
	toolCallID := fmt.Sprintf("active-browser-%d", time.Now().UnixNano())
	toolRawInput := browserToolRawInput(req)
	stream.publish(chat.ChatEvent{
		Type:         "tool_call",
		ToolCallID:   toolCallID,
		ToolTitle:    toolName,
		ToolKind:     "browser",
		ToolStatus:   "pending",
		ToolRawInput: toolRawInput,
	})
	stream.publish(chat.ChatEvent{
		Type:         "tool_call_update",
		ToolCallID:   toolCallID,
		ToolTitle:    toolName,
		ToolStatus:   "in_progress",
		ToolContent:  browserToolOutcome(action, payloadSummary),
		ToolRawInput: toolRawInput,
	})
	phaseStartedAt := time.Now()
	debug.BrowserMCPLog(
		"bridge",
		"info",
		"tool=%s session=%s tab=%s action=%s payload=%s",
		toolName,
		req.SessionID,
		tabID,
		action,
		payloadSummary,
	)
	logOutcome := func(err error, outcome string) {
		waitForInteractiveToolFloor(actionCtx, phaseStartedAt)
		if err != nil {
			stream.publish(chat.ChatEvent{
				Type:         "tool_call_update",
				ToolCallID:   toolCallID,
				ToolTitle:    toolName,
				ToolStatus:   "failed",
				ToolContent:  err.Error(),
				ToolRawInput: toolRawInput,
			})
			status := a.browser.AutomationStatus()
			debug.BrowserMCPLog(
				"server",
				"error",
				"tool=%s session=%s tab=%s action=%s payload=%s duration=%s err=%v automation_running=%t automation_last_error=%q",
				toolName,
				req.SessionID,
				tabID,
				action,
				payloadSummary,
				time.Since(startedAt).Round(time.Millisecond),
				err,
				status.Running,
				status.LastError,
			)
			return
		}
		stream.publish(chat.ChatEvent{
			Type:         "tool_call_update",
			ToolCallID:   toolCallID,
			ToolTitle:    toolName,
			ToolStatus:   "completed",
			ToolContent:  browserToolOutcome(action, outcome),
			ToolRawInput: toolRawInput,
		})
		debug.BrowserMCPLog(
			"server",
			"info",
			"tool=%s session=%s tab=%s action=%s payload=%s duration=%s outcome=%s",
			toolName,
			req.SessionID,
			tabID,
			action,
			payloadSummary,
			time.Since(startedAt).Round(time.Millisecond),
			outcome,
		)
	}

	switch req.Action {
	case "goto", "navigate":
		result, err := a.browser.TabNavigate(actionCtx, tabID, req.URL)
		if err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		logOutcome(nil, "url="+truncateBrowserLogValue(result.URL, 120))
		writeJSON(w, http.StatusOK, result)
	case "click":
		if err := a.browser.TabClick(actionCtx, tabID, req.Selector, req.X, req.Y); err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		result, inspectErr := a.browser.TabInspect(actionCtx, tabID)
		if inspectErr != nil {
			logOutcome(nil, "clicked")
			writeJSON(w, http.StatusOK, map[string]string{"status": "clicked"})
			return
		}
		logOutcome(nil, "clicked title="+truncateBrowserLogValue(result.Title, 80))
		writeJSON(w, http.StatusOK, result)
	case "type":
		if err := a.browser.TabType(actionCtx, tabID, req.Selector, req.Text); err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		result, inspectErr := a.browser.TabInspect(actionCtx, tabID)
		if inspectErr != nil {
			logOutcome(nil, "typed")
			writeJSON(w, http.StatusOK, map[string]string{"status": "typed"})
			return
		}
		logOutcome(nil, "typed title="+truncateBrowserLogValue(result.Title, 80))
		writeJSON(w, http.StatusOK, result)
	case "press":
		if err := a.browser.TabPress(actionCtx, tabID, req.Key); err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		result, inspectErr := a.browser.TabInspect(actionCtx, tabID)
		if inspectErr != nil {
			logOutcome(nil, "pressed")
			writeJSON(w, http.StatusOK, map[string]string{"status": "pressed"})
			return
		}
		logOutcome(nil, "pressed title="+truncateBrowserLogValue(result.Title, 80))
		writeJSON(w, http.StatusOK, result)
	case "scroll":
		if err := a.browser.TabScroll(actionCtx, tabID, req.DeltaX, req.DeltaY); err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		result, inspectErr := a.browser.TabInspect(actionCtx, tabID)
		if inspectErr != nil {
			logOutcome(nil, "scrolled")
			writeJSON(w, http.StatusOK, map[string]string{"status": "scrolled"})
			return
		}
		logOutcome(nil, "scrolled title="+truncateBrowserLogValue(result.Title, 80))
		writeJSON(w, http.StatusOK, result)
	case "inspect":
		result, err := a.browser.TabInspect(actionCtx, tabID)
		if err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		logOutcome(nil, "title="+truncateBrowserLogValue(result.Title, 80))
		writeJSON(w, http.StatusOK, result)
	case "screenshot":
		data, err := a.browser.TabScreenshot(actionCtx, tabID)
		if err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		path, err := saveBrowserCapture("active-browser", data)
		if err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logOutcome(nil, "path="+truncateBrowserLogValue(path, 120))
		writeJSON(w, http.StatusOK, map[string]string{"path": path, "mimeType": "image/png"})
	case "console_logs", "console":
		logs, err := a.browser.TabConsoleLogs(tabID, req.Limit)
		if err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		logOutcome(nil, fmt.Sprintf("entries=%d", len(logs)))
		writeJSON(w, http.StatusOK, map[string]any{"entries": logs, "count": len(logs)})
	case "network_requests", "network":
		entries, err := a.browser.TabNetworkRequests(tabID, req.Limit)
		if err != nil {
			logOutcome(err, "")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		logOutcome(nil, fmt.Sprintf("entries=%d", len(entries)))
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "count": len(entries)})
	default:
		logOutcome(fmt.Errorf("unsupported browser action: %s", req.Action), "")
		http.Error(w, "unsupported browser action: "+req.Action, http.StatusBadRequest)
	}
}

func (a *API) isDuplicateTerminalRun(sessionID, command string) bool {
	const window = 10 * time.Second
	key := sessionID + "\x00" + command
	now := time.Now()

	a.terminalRunMu.Lock()
	defer a.terminalRunMu.Unlock()

	for k, ts := range a.terminalRuns {
		if now.Sub(ts) > window {
			delete(a.terminalRuns, k)
		}
	}
	if ts, ok := a.terminalRuns[key]; ok && now.Sub(ts) <= window {
		return true
	}
	a.terminalRuns[key] = now
	return false
}

func (a *API) handleChatAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	configs := agentconfig.DetectAll()
	writeJSON(w, http.StatusOK, configs)
}

func (a *API) handleChatSessions(w http.ResponseWriter, r *http.Request) {
	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions := a.chatSessionManager.List()
		infos := make([]chatSessionInfo, 0, len(sessions))
		for _, s := range sessions {
			infos = append(infos, chatSessionInfo{
				ID:           s.ID(),
				AgentID:      s.AgentID(),
				Mode:         string(s.Mode()),
				WorkDir:      s.WorkDir(),
				ACPSessionID: s.ACPSessionID(),
				IsResumed:    s.IsResumed(),
			})
		}
		writeJSON(w, http.StatusOK, infos)

	case http.MethodPost:
		var req chatCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		agent, ok := findAgentDescriptor(req.AgentID)
		if !ok {
			http.Error(w, "unknown or unavailable agent", http.StatusBadRequest)
			return
		}

		workDir := req.WorkDir
		if workDir == "" {
			workDir = a.workspaceRoot
		}

		var session chat.ChatSession
		var err error
		resumedFrom := ""
		opts := chat.SessionOptions{
			UseActiveTerminal:  req.UseActiveTerminal,
			ActiveTerminalID:   req.ActiveTerminalID,
			UseActiveBrowser:   req.UseActiveBrowser,
			ActiveBrowserTabID: req.ActiveBrowserTabID,
		}
		previousLiveID := ""
		if req.ResumeID != "" {
			if liveID, ok := a.chatSessionManager.LiveIDForRecordID(req.ResumeID); ok {
				previousLiveID = liveID
			}
		}

		if req.ResumeID != "" && req.ACPSessionID != "" && agent.SupportsACP {
			session, err = a.chatSessionManager.Resume(context.Background(), agent, workDir, req.ACPSessionID, opts)
			if err != nil {
				session, err = a.chatSessionManager.Create(context.Background(), agent, workDir, opts)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				resumedFrom = req.ResumeID
			}
		} else {
			session, err = a.chatSessionManager.Create(context.Background(), agent, workDir, opts)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if previousLiveID != "" && previousLiveID != session.ID() {
			a.chatSessionManager.Remove(previousLiveID)
		}

		if a.chatStore != nil {
			if resumedFrom != "" && session.ID() != req.ResumeID {
				_ = a.chatStore.UpdateSessionACP(req.ResumeID, session.ACPSessionID(), workDir)
				a.chatSessionManager.LinkRecordID(session.ID(), req.ResumeID)
			} else {
				record, _ := a.chatStore.GetSession(session.ID())
				if record == nil {
					_ = a.chatStore.CreateSessionFull(
						session.ID(),
						session.AgentID(),
						"",
						workDir,
						session.ACPSessionID(),
					)
				} else {
					_ = a.chatStore.UpdateSessionACP(session.ID(), session.ACPSessionID(), workDir)
				}
				a.chatSessionManager.LinkRecordID(session.ID(), session.ID())
			}
		}

		writeJSON(w, http.StatusCreated, chatCreateResponse{
			ID:           session.ID(),
			Mode:         string(session.Mode()),
			IsResumed:    resumedFrom != "",
			ResumedFrom:  resumedFrom,
			WorkDir:      session.WorkDir(),
			ACPSessionID: session.ACPSessionID(),
		})

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleChatSessionByID(w http.ResponseWriter, r *http.Request) {
	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/chat/sessions/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodDelete {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	a.chatSessionManager.Remove(id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) newChatEventPersister(sessionID string) chatEventPersister {
	if a.chatStore == nil {
		debug.Printf("[chat/persist] disabled: no chat store session=%s", sessionID)
		return nil
	}

	persistRecordID := a.chatSessionManager.RecordIDFor(sessionID)
	if persistRecordID == sessionID {
		persistRecordID = a.chatStore.ResolveRecordID(sessionID)
	}
	if persistRecordID == "" {
		debug.Printf("[chat/persist] disabled: empty record id for live session=%s", sessionID)
		return nil
	}
	if record, err := a.chatStore.GetSession(persistRecordID); err != nil {
		debug.Printf("[chat/persist] record lookup failed live=%s record=%s err=%v", sessionID, persistRecordID, err)
	} else if record == nil {
		if live, ok := a.chatSessionManager.Get(sessionID); ok {
			if err := a.chatStore.CreateSessionFull(persistRecordID, live.AgentID(), "", live.WorkDir(), live.ACPSessionID()); err != nil {
				debug.Printf("[chat/persist] record create failed live=%s record=%s agent=%s workDir=%q err=%v", sessionID, persistRecordID, live.AgentID(), live.WorkDir(), err)
			} else {
				debug.Printf("[chat/persist] record created live=%s record=%s agent=%s workDir=%q", sessionID, persistRecordID, live.AgentID(), live.WorkDir())
			}
		} else {
			debug.Printf("[chat/persist] record missing and live session unavailable live=%s record=%s", sessionID, persistRecordID)
		}
	}
	debug.Printf("[chat/persist] attached live=%s record=%s", sessionID, persistRecordID)

	persistSeq, _ := a.chatStore.NextEventSeq(persistRecordID)
	var mu sync.Mutex
	var assistantText strings.Builder
	assistantMessageID := ""
	assistantSaved := false
	saveAssistantDraft := func() {
		if strings.TrimSpace(assistantText.String()) == "" {
			return
		}
		now := time.Now().UnixMilli()
		if assistantMessageID == "" {
			assistantMessageID = fmt.Sprintf("%s-assistant-%d", persistRecordID, now)
		}
		if err := a.chatStore.UpsertMessage(chat.MessageRecord{
			ID:        assistantMessageID,
			SessionID: persistRecordID,
			Role:      "assistant",
			Content:   assistantText.String(),
			Timestamp: now,
		}); err != nil {
			debug.Printf("[chat/persist] assistant upsert failed live=%s record=%s msg=%s chars=%d err=%v", sessionID, persistRecordID, assistantMessageID, assistantText.Len(), err)
		} else {
			debug.Printf("[chat/persist] assistant upserted live=%s record=%s msg=%s chars=%d", sessionID, persistRecordID, assistantMessageID, assistantText.Len())
		}
	}

	return func(evt chat.ChatEvent) {
		mu.Lock()
		defer mu.Unlock()
		if evt.Type == "text" || evt.Type == "done" || evt.Type == "error" || evt.Type == "usage_update" {
			debug.Printf("[chat/persist] event live=%s record=%s type=%s textChars=%d stop=%q err=%q", sessionID, persistRecordID, evt.Type, len(evt.Text), evt.StopReason, evt.Error)
		}

		switch evt.Type {
		case "text":
			if assistantSaved {
				assistantText.Reset()
				assistantMessageID = ""
				assistantSaved = false
			}
			assistantText.WriteString(evt.Text)
			saveAssistantDraft()
		case "done":
			if !assistantSaved {
				saveAssistantDraft()
				assistantSaved = true
			}
		}

		shouldPersist := false
		switch evt.Type {
		case "text", "thinking", "tool_call", "tool_call_update", "diff",
			"plan", "title", "done", "error", "session_info", "usage_update":
			shouldPersist = true
		case "commands":
			shouldPersist = true
			commandsJSON, _ := json.Marshal(evt.Commands)
			_ = a.chatStore.SaveSnapshot(chat.SessionSnapshot{
				SessionID:    persistRecordID,
				CommandsJSON: string(commandsJSON),
			})
		case "config_options":
			shouldPersist = true
			configJSON, _ := json.Marshal(evt.ConfigOptions)
			snap, _ := a.chatStore.GetSnapshot(persistRecordID)
			commandsJSON := ""
			if snap != nil {
				commandsJSON = snap.CommandsJSON
			}
			_ = a.chatStore.SaveSnapshot(chat.SessionSnapshot{
				SessionID:      persistRecordID,
				CommandsJSON:   commandsJSON,
				ConfigOptsJSON: string(configJSON),
			})
		}
		if !shouldPersist {
			return
		}
		payload, _ := json.Marshal(evt)
		now := time.Now().UnixMilli()
		for attempt := 0; attempt < 2; attempt++ {
			evtID := fmt.Sprintf("%s-e-%d", persistRecordID, persistSeq)
			record := chat.EventRecord{
				ID:          evtID,
				SessionID:   persistRecordID,
				Kind:        evt.Type,
				PayloadJSON: string(payload),
				Seq:         persistSeq,
				Timestamp:   now,
			}
			persistSeq++
			if err := a.chatStore.AppendEvent(record); err == nil {
				if evt.Type == "text" || evt.Type == "done" || evt.Type == "error" {
					debug.Printf("[chat/persist] event appended live=%s record=%s seq=%d type=%s", sessionID, persistRecordID, record.Seq, evt.Type)
				}
				break
			} else {
				debug.Printf("[chat/persist] event append failed live=%s record=%s seq=%d type=%s attempt=%d err=%v", sessionID, persistRecordID, record.Seq, evt.Type, attempt+1, err)
			}
			nextSeq, err := a.chatStore.NextEventSeq(persistRecordID)
			if err != nil {
				break
			}
			persistSeq = nextSeq
		}
		if evt.Title != "" && (evt.Type == "title" || evt.Type == "session_info") {
			_ = a.chatStore.UpdateSessionTitle(persistRecordID, evt.Title)
		}
	}
}

func (a *API) handleChatWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/chat/")
	session, ok := a.chatSessionManager.Get(sessionID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream := a.chatStreams.GetOrCreate(sessionID, session, a.newChatEventPersister(sessionID))
	a.chatStreams.Touch(sessionID)
	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-sub.C:
				if !ok {
					return
				}
				if err := conn.WriteJSON(evt); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		var msg chatWSInbound
		if err := conn.ReadJSON(&msg); err != nil {
			cancel()
			return
		}

		switch msg.Type {
		case "message":
			a.chatStreams.Touch(sessionID)
			stream.StartTurn()
			content := msg.Content
			if msg.Context != nil && len(msg.Context) > 0 {
				content = formatContextMessage(msg.Content, msg.Context)
			}
			attachments := make([]chat.Attachment, 0, len(msg.Attachments))
			for _, attachment := range msg.Attachments {
				attachments = append(attachments, chat.Attachment{
					Type: attachment.Type,
					Path: attachment.Path,
					Name: attachment.Name,
				})
			}
			if err := session.Send(ctx, content, attachments); err != nil {
				stream.publish(chat.ChatEvent{Type: "error", Error: err.Error()})
			}

		case "cancel":
			if err := session.Cancel(); err != nil {
				stream.publish(chat.ChatEvent{Type: "error", Error: err.Error()})
			}

		case "set_config_option":
			if err := session.SetConfigOption(ctx, msg.ConfigID, msg.Value); err != nil {
				stream.publish(chat.ChatEvent{Type: "error", Error: err.Error()})
			}

		case "permission_response":
			session.RespondPermission(chat.PermissionResponse{
				PermissionID: msg.PermissionID,
				OptionID:     msg.OptionID,
				Cancelled:    msg.Cancelled,
			})

		case "set_auto_approve":
			session.SetAutoApprove(msg.AutoApprove)

		case "set_use_active_terminal":
			session.SetUseActiveTerminal(msg.UseActiveTerminal, msg.ActiveTerminalID)

		case "set_use_active_browser":
			session.SetUseActiveBrowser(msg.UseActiveBrowser, msg.ActiveBrowserTabID)

		default:
			stream.publish(chat.ChatEvent{Type: "error", Error: "unsupported message type: " + msg.Type})
		}
	}
}

func (a *API) handleChatRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	workDir := r.URL.Query().Get("workDir")
	preferredSessionID := r.URL.Query().Get("sessionId")
	if workDir == "" {
		http.Error(w, "workDir parameter is required", http.StatusBadRequest)
		return
	}

	resp := chatRestoreResponse{}

	if a.chatStore == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	var (
		record *chat.SessionRecord
		err    error
	)
	if preferredSessionID != "" {
		record, err = a.chatStore.GetSessionForProject(preferredSessionID, workDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if record == nil {
			writeJSON(w, http.StatusOK, chatRestoreResponse{
				Found:       false,
				SessionID:   preferredSessionID,
				ResumeError: fmt.Sprintf("session %s not found for project", preferredSessionID),
			})
			return
		}
	} else {
		record, err = a.chatStore.GetLastSessionForProject(workDir)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if record == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Found = true
	resp.SessionID = record.ID
	resp.AgentID = record.AgentID
	resp.WorkDir = record.WorkDir
	resp.ACPSessionID = record.ACPSessionID
	resp.Status = record.Status
	resp.Title = record.Title

	if a.chatSessionManager != nil {
		if liveID, ok := a.chatSessionManager.LiveIDForRecordID(record.ID); ok {
			resp.LiveSessionID = liveID
			resp.IsLive = true
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	if a.chatSessionManager != nil && a.chatSessionManager.IsLive(record.ID) {
		resp.IsLive = true
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if record.ACPSessionID != "" && record.AgentID != "" {
		agent, ok := findAgentDescriptor(record.AgentID)
		resp.AgentSupportsACP = agent.SupportsACP
		resp.AgentAvailable = agent.Available
		if ok && agent.Available && agent.SupportsACP {
			resp.CanResume = true
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleChatResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	var req chatResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.AgentID == "" {
		http.Error(w, "sessionId and agentId are required", http.StatusBadRequest)
		return
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = a.workspaceRoot
	}

	agent, ok := findAnyAgentDescriptor(req.AgentID)
	if !ok {
		writeJSON(w, http.StatusOK, chatRestoreResponse{
			Found:       true,
			SessionID:   req.SessionID,
			CanResume:   false,
			ResumeError: "unknown agent: " + req.AgentID,
		})
		return
	}
	if !agent.Available {
		writeJSON(w, http.StatusOK, chatRestoreResponse{
			Found:       true,
			SessionID:   req.SessionID,
			CanResume:   false,
			ResumeError: "agent is not available: " + req.AgentID,
		})
		return
	}

	var session chat.ChatSession
	var err error
	resumed := true
	resumeErr := ""
	opts := chat.SessionOptions{
		UseActiveTerminal:  req.UseActiveTerminal,
		ActiveTerminalID:   req.ActiveTerminalID,
		UseActiveBrowser:   req.UseActiveBrowser,
		ActiveBrowserTabID: req.ActiveBrowserTabID,
	}
	previousLiveID := ""
	if liveID, ok := a.chatSessionManager.LiveIDForRecordID(req.SessionID); ok {
		previousLiveID = liveID
	}
	if agent.SupportsACP && req.ACPSessionID != "" {
		session, err = a.chatSessionManager.Resume(context.Background(), agent, workDir, req.ACPSessionID, opts)
		if err != nil {
			resumeErr = err.Error()
		}
	} else if agent.SupportsACP {
		resumeErr = "missing ACP session id; creating replacement session"
		err = fmt.Errorf("%s", resumeErr)
	} else {
		resumeErr = "agent does not support ACP session/resume"
		err = fmt.Errorf("%s", resumeErr)
	}
	if err != nil {
		session, err = a.chatSessionManager.Create(context.Background(), agent, workDir, opts)
		if err != nil {
			writeJSON(w, http.StatusOK, chatRestoreResponse{
				Found:       true,
				SessionID:   req.SessionID,
				CanResume:   false,
				ResumeError: resumeErr + "; fallback create failed: " + err.Error(),
			})
			return
		}
		resumed = false
	}
	if previousLiveID != "" && previousLiveID != session.ID() {
		a.chatSessionManager.Remove(previousLiveID)
	}

	if a.chatStore != nil {
		if session.ID() != req.SessionID {
			_ = a.chatStore.UpdateSessionACP(req.SessionID, session.ACPSessionID(), workDir)
			a.chatSessionManager.LinkRecordID(session.ID(), req.SessionID)
		} else {
			record, _ := a.chatStore.GetSession(session.ID())
			if record == nil {
				_ = a.chatStore.CreateSessionFull(
					session.ID(),
					session.AgentID(),
					"",
					workDir,
					session.ACPSessionID(),
				)
			} else {
				_ = a.chatStore.UpdateSessionACP(session.ID(), session.ACPSessionID(), workDir)
			}
			a.chatSessionManager.LinkRecordID(session.ID(), session.ID())
		}
	}

	writeJSON(w, http.StatusCreated, chatCreateResponse{
		ID:           session.ID(),
		Mode:         string(session.Mode()),
		IsResumed:    resumed,
		ResumedFrom:  req.SessionID,
		WorkDir:      session.WorkDir(),
		ACPSessionID: session.ACPSessionID(),
	})
}

type chatHistoryMessageRequest struct {
	SessionID    string `json:"sessionId"`
	AgentID      string `json:"agentId"`
	Title        string `json:"title"`
	WorkDir      string `json:"workDir,omitempty"`
	ACPSessionID string `json:"acpSessionId,omitempty"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	ContextFile  string `json:"contextFile,omitempty"`
	ContextStart int    `json:"contextStartLine,omitempty"`
	ContextEnd   int    `json:"contextEndLine,omitempty"`
	ContextCode  string `json:"contextCode,omitempty"`
	ContextLang  string `json:"contextLanguage,omitempty"`
}

func (a *API) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	if a.chatStore == nil {
		http.Error(w, "chat history not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		workDir := r.URL.Query().Get("workDir")
		var (
			sessions []chat.SessionRecord
			err      error
		)
		if workDir != "" {
			sessions, err = a.chatStore.SessionsForProject(workDir, 50)
		} else {
			sessions, err = a.chatStore.ListSessions(50)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sessions == nil {
			sessions = []chat.SessionRecord{}
		}
		writeJSON(w, http.StatusOK, sessions)

	case http.MethodPost:
		var req chatHistoryMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.SessionID == "" || req.Role == "" || req.Content == "" {
			http.Error(w, "sessionId, role, and content are required", http.StatusBadRequest)
			return
		}

		sessions, _ := a.chatStore.ListSessions(0)
		sessionExists := false
		for _, s := range sessions {
			if s.ID == req.SessionID {
				sessionExists = true
				break
			}
		}

		if !sessionExists {
			agentId := req.AgentID
			if agentId == "" {
				agentId = "unknown"
			}
			title := req.Title
			if title == "" {
				title = truncate(req.Content, 50)
			}
			if err := a.chatStore.CreateSessionFull(req.SessionID, agentId, title, req.WorkDir, req.ACPSessionID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if req.Title != "" {
				_ = a.chatStore.UpdateSessionTitle(req.SessionID, req.Title)
			}
			if req.WorkDir != "" || req.ACPSessionID != "" {
				record, _ := a.chatStore.GetSession(req.SessionID)
				workDir := req.WorkDir
				acpSessionID := req.ACPSessionID
				if record != nil {
					if workDir == "" {
						workDir = record.WorkDir
					}
					if acpSessionID == "" {
						acpSessionID = record.ACPSessionID
					}
				}
				_ = a.chatStore.UpdateSessionACP(req.SessionID, acpSessionID, workDir)
			}
		}

		now := time.Now().UnixMilli()
		msgID := fmt.Sprintf("%s-%d", req.SessionID, now)
		msg := chat.MessageRecord{
			ID:           msgID,
			SessionID:    req.SessionID,
			Role:         req.Role,
			Content:      req.Content,
			ContextFile:  req.ContextFile,
			ContextStart: req.ContextStart,
			ContextEnd:   req.ContextEnd,
			ContextCode:  req.ContextCode,
			ContextLang:  req.ContextLang,
			Timestamp:    now,
		}
		if err := a.chatStore.AddMessage(msg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleChatHistoryByID(w http.ResponseWriter, r *http.Request) {
	if a.chatStore == nil {
		http.Error(w, "chat history not available", http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/chat/history/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("transcript") == "true" {
			events, err := a.chatStore.GetEvents(sessionID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if events == nil {
				events = []chat.EventRecord{}
			}
			snap, _ := a.chatStore.GetSnapshot(sessionID)
			snapshot := snap
			if snapshot == nil {
				snapshot = &chat.SessionSnapshot{SessionID: sessionID}
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"events":   events,
				"snapshot": snapshot,
			})
			return
		}

		messages, err := a.chatStore.GetMessages(sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if messages == nil {
			messages = []chat.MessageRecord{}
		}
		writeJSON(w, http.StatusOK, messages)

	case http.MethodDelete:
		if err := a.chatStore.DeleteSession(sessionID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleChatStateByID(w http.ResponseWriter, r *http.Request) {
	if a.chatStore == nil {
		http.Error(w, "chat history not available", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/chat/state/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	session, err := a.chatStore.GetSession(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if session == nil {
		http.NotFound(w, r)
		return
	}
	events, err := a.chatStore.GetEvents(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	messages, err := a.chatStore.GetMessages(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snapshot, err := a.chatStore.GetSnapshot(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":  session,
		"messages": messages,
		"events":   events,
		"snapshot": snapshot,
	})
}

func (a *API) handleChatInstallACP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID string `json:"agentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	path, err := chat.InstallACPAdapter(req.AgentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func findAgentDescriptor(id string) (chat.AgentDescriptor, bool) {
	agents := discoverAgentDescriptors()
	for _, a := range agents {
		if a.ID == id && a.Available {
			return a, true
		}
	}
	return chat.AgentDescriptor{}, false
}

func findAnyAgentDescriptor(id string) (chat.AgentDescriptor, bool) {
	agents := discoverAgentDescriptors()
	for _, a := range agents {
		if a.ID == id {
			return a, true
		}
	}
	return chat.AgentDescriptor{}, false
}

func formatContextMessage(content string, ctx json.RawMessage) string {
	if ctx == nil || len(ctx) == 0 {
		return content
	}

	var context struct {
		FilePath     string `json:"filePath"`
		StartLine    int    `json:"startLine"`
		EndLine      int    `json:"endLine"`
		SelectedCode string `json:"selectedCode"`
		Language     string `json:"language"`
	}

	if err := json.Unmarshal(ctx, &context); err != nil {
		return content
	}

	var sb strings.Builder
	if context.FilePath != "" {
		sb.WriteString(fmt.Sprintf("File: %s", context.FilePath))
		if context.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(" (lines %d-%d)", context.StartLine, context.EndLine))
		}
		sb.WriteString("\n")
	}
	if context.SelectedCode != "" {
		sb.WriteString("```")
		if context.Language != "" {
			sb.WriteString(context.Language)
		}
		sb.WriteString("\n")
		sb.WriteString(context.SelectedCode)
		sb.WriteString("\n```\n\n")
	}
	sb.WriteString(content)
	return sb.String()
}
