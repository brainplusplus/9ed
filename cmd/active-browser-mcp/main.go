package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func mcpDebugEnabled() bool {
	if !envFlagEnabled("DEBUG", false) {
		return false
	}
	return envFlagEnabled("DEBUG_BROWSER_MCP", true) || envFlagEnabled("DEBUG_BROWSER_AUTOMATION", false) || envFlagEnabled("NINE_ED_BROWSER_MCP_DEBUG", false)
}

func debugf(format string, args ...any) {
	if mcpDebugEnabled() {
		log.Printf("[active-browser-mcp] "+format, args...)
	}
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			debugf("discarding invalid rpc payload: %v", err)
			continue
		}
		if req.ID == nil {
			debugf("ignoring notification method=%s", strings.TrimSpace(req.Method))
			continue
		}
		debugf("rpc request method=%s id=%v", strings.TrimSpace(req.Method), req.ID)
		writeResponse(req.ID, handle(req))
	}
	if err := scanner.Err(); err != nil {
		debugf("stdin scanner stopped: %v", err)
	}
}

func handle(req rpcRequest) any {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "9ed-active-browser",
				"version": "0.1.0",
			},
		}
	case "tools/list":
		return map[string]any{"tools": browserTools()}
	case "tools/call":
		return callTool(req.Params)
	default:
		return map[string]any{}
	}
}

func browserTools() []map[string]any {
	timeout := numberProp("Maximum time to wait for this browser action, in milliseconds. Default 15000, max 60000.")
	maxBytes := numberProp("Maximum source bytes to return for page source (default 200000, max 600000).")
	return []map[string]any{
		{
			"name":        "9ed_browser_goto",
			"description": "Navigate the active 9ed WebRTC browser tab to a URL. After navigation, use the returned page state to decide the next minimal action or answer.",
			"inputSchema": schema(map[string]any{"url": stringProp("URL to open"), "timeoutMs": timeout}, []string{"url"}),
		},
		{
			"name":        "9ed_browser_click",
			"description": "Click an element in the active 9ed WebRTC browser tab by selector, or by x/y viewport coordinates. After a successful click that reaches the requested target, answer naturally instead of re-clicking or over-inspecting.",
			"inputSchema": schema(map[string]any{"selector": stringProp("CSS selector"), "x": numberProp("Viewport x coordinate"), "y": numberProp("Viewport y coordinate"), "timeoutMs": timeout}, []string{}),
		},
		{
			"name":        "9ed_browser_type",
			"description": "Type text into the active 9ed WebRTC browser tab. Provide selector to fill a specific element.",
			"inputSchema": schema(map[string]any{"selector": stringProp("CSS selector"), "text": stringProp("Text to type"), "timeoutMs": timeout}, []string{"text"}),
		},
		{
			"name":        "9ed_browser_press",
			"description": "Press a keyboard key in the active 9ed WebRTC browser tab, e.g. Enter, Escape, Tab.",
			"inputSchema": schema(map[string]any{"key": stringProp("Keyboard key"), "timeoutMs": timeout}, []string{"key"}),
		},
		{
			"name":        "9ed_browser_scroll",
			"description": "Scroll the active 9ed WebRTC browser tab.",
			"inputSchema": schema(map[string]any{"deltaX": numberProp("Horizontal scroll delta"), "deltaY": numberProp("Vertical scroll delta"), "timeoutMs": timeout}, []string{}),
		},
		{
			"name":        "9ed_browser_inspect",
			"description": "Read URL, title, and visible body text from the active 9ed WebRTC browser tab. Use this to choose the next action; if the page state already satisfies the user, answer without more browser calls.",
			"inputSchema": schema(map[string]any{"timeoutMs": timeout}, []string{}),
		},
		{
			"name":        "9ed_browser_screenshot",
			"description": "Capture a screenshot of the active 9ed WebRTC browser tab and return the local image path. Use this only when visual evidence is required (UI verification, debug artifact, or image analysis). Prefer inspect/page source for normal navigation and text extraction tasks.",
			"inputSchema": schema(map[string]any{"timeoutMs": timeout}, []string{}),
		},
		{
			"name":        "9ed_browser_page_source",
			"description": "Read current page HTML source from the active 9ed WebRTC browser tab. Use this to inspect DOM/page structure when selectors are uncertain.",
			"inputSchema": schema(map[string]any{"maxBytes": maxBytes, "timeoutMs": timeout}, []string{}),
		},
		{
			"name":        "9ed_browser_console_logs",
			"description": "Read recent JavaScript console logs from the active 9ed WebRTC browser tab.",
			"inputSchema": schema(map[string]any{"limit": numberProp("Maximum number of recent logs to return (default 60, max 400)")}, []string{}),
		},
		{
			"name":        "9ed_browser_network_requests",
			"description": "Read recent network request/response events from the active 9ed WebRTC browser tab.",
			"inputSchema": schema(map[string]any{"limit": numberProp("Maximum number of recent events to return (default 60, max 400)")}, []string{}),
		},
	}
}

func callTool(raw json.RawMessage) any {
	var params toolCallParams
	_ = json.Unmarshal(raw, &params)
	debugf("tool call name=%s args=%s", strings.TrimSpace(params.Name), summarizeJSON(params.Arguments))
	action := mapToolAction(params.Name)
	if action == "" {
		debugf("tool call rejected: unknown tool %s", strings.TrimSpace(params.Name))
		return toolText("unknown browser tool")
	}
	var args map[string]any
	if len(params.Arguments) > 0 {
		_ = json.Unmarshal(params.Arguments, &args)
	}
	if args == nil {
		args = map[string]any{}
	}
	args["action"] = action
	body, err := sendAction(strings.TrimSpace(params.Name), args)
	if err != nil {
		debugf("tool call name=%s failed: %v", strings.TrimSpace(params.Name), err)
		return toolText("browser action failed: " + err.Error())
	}
	if body == "" {
		body = "Browser action completed."
	}
	debugf("tool call name=%s ok response=%s", strings.TrimSpace(params.Name), summarizeText(body))
	return toolText("Browser tool result.\n" + browserDecisionHint(action) + "\n\n" + body)
}

func browserDecisionHint(action string) string {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "inspect", "console_logs", "network_requests", "screenshot", "page_source", "source":
		if action == "screenshot" {
			return "Decision: sufficient_to_answer=true. Only use screenshot when visual proof/debug is required. If text/DOM evidence is enough, avoid more screenshots and answer now."
		}
		return "Decision: sufficient_to_answer=true. If this observation already satisfies the user's request, answer now and avoid extra browser tool calls."
	case "goto", "navigate", "click", "type", "press", "scroll":
		return "Decision: sufficient_to_answer=false. Continue with one minimal follow-up browser observation/action only if needed to answer the user."
	default:
		return "Decision: if this page state satisfies the user's request, answer now. Call another 9ed_browser_* tool only when a necessary page action or missing observation remains."
	}
}

func mapToolAction(name string) string {
	switch name {
	case "9ed_browser_goto", "9ed_browser_navigate", "active_browser_goto", "active_browser_navigate", "browser_goto", "browser_navigate":
		return "goto"
	case "9ed_browser_click", "active_browser_click", "browser_click":
		return "click"
	case "9ed_browser_type", "active_browser_type", "browser_type":
		return "type"
	case "9ed_browser_press", "active_browser_press", "browser_press":
		return "press"
	case "9ed_browser_scroll", "active_browser_scroll", "browser_scroll":
		return "scroll"
	case "9ed_browser_inspect", "active_browser_inspect", "browser_inspect":
		return "inspect"
	case "9ed_browser_screenshot", "active_browser_screenshot", "browser_screenshot":
		return "screenshot"
	case "9ed_browser_page_source", "9ed_browser_source", "active_browser_page_source", "browser_page_source", "active_browser_source", "browser_source":
		return "page_source"
	case "9ed_browser_console_logs", "9ed_browser_console", "active_browser_console_logs", "browser_console_logs", "active_browser_console":
		return "console_logs"
	case "9ed_browser_network_requests", "9ed_browser_network", "active_browser_network_requests", "browser_network_requests", "active_browser_network":
		return "network_requests"
	default:
		return ""
	}
}

func sendAction(toolName string, payload map[string]any) (string, error) {
	endpoint := os.Getenv("NINE_ED_BROWSER_MCP_ENDPOINT")
	token := os.Getenv("NINE_ED_BROWSER_MCP_TOKEN")
	if endpoint == "" || token == "" {
		return "", fmt.Errorf("missing MCP endpoint configuration")
	}
	body, _ := json.Marshal(payload)
	startedAt := time.Now()
	debugf("dispatch action=%v endpoint=%s payload=%s", payload["action"], endpoint, summarizeBytes(body))
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		debugf("request build failed action=%v: %v", payload["action"], err)
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-9ed-MCP-Token", token)
	if toolName != "" {
		req.Header.Set("X-9ed-MCP-Tool-Name", toolName)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		debugf("dispatch failed action=%v duration=%s err=%v", payload["action"], time.Since(startedAt).Round(time.Millisecond), err)
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 300 {
		debugf("dispatch failed action=%v status=%s duration=%s body=%s", payload["action"], resp.Status, time.Since(startedAt).Round(time.Millisecond), summarizeBytes(data))
		return "", fmt.Errorf("server returned %s: %s", resp.Status, string(data))
	}
	debugf("dispatch ok action=%v status=%s duration=%s body=%s", payload["action"], resp.Status, time.Since(startedAt).Round(time.Millisecond), summarizeBytes(data))
	return string(data), nil
}

func summarizeJSON(raw json.RawMessage) string {
	return summarizeBytes(raw)
}

func summarizeBytes(raw []byte) string {
	return summarizeText(string(raw))
}

func summarizeText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "<empty>"
	}
	const maxLen = 240
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func envFlagEnabled(key string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return defaultValue
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func schema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func stringProp(description string) map[string]string {
	return map[string]string{"type": "string", "description": description}
}

func numberProp(description string) map[string]string {
	return map[string]string{"type": "number", "description": description}
}

func toolText(text string) any {
	return map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": text,
		}},
	}
}

func writeResponse(id any, result any) {
	data, _ := json.Marshal(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
	fmt.Println(string(data))
}
