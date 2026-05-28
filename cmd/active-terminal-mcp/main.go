package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		writeResponse(req.ID, handle(req))
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
				"name":    "9ed-active-terminal",
				"version": "0.1.0",
			},
		}
	case "tools/list":
		return map[string]any{"tools": terminalTools()}
	case "tools/call":
		return callTool(req.Params)
	default:
		return map[string]any{}
	}
}

func terminalTools() []map[string]any {
	return []map[string]any{{
		"name":        "active_terminal_run",
		"description": "Run exactly one command in the user's active visible 9ed terminal and return observed output. Use this for build/test/git/search/diagnostic commands expected to finish and return the shell to idle. Prefer one information-dense command that advances the current debugging or implementation workflow. Chain another targeted terminal or browser action when the output reveals the next necessary step; answer when the current task is satisfied. Avoid redundant confirmation commands when the result already proves the point.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Command formatted for the active terminal shell.",
				},
				"timeoutMs": map[string]any{
					"type":        "number",
					"description": "Maximum time to wait for the command to finish before returning partial output. Default 10000, max 60000.",
				},
			},
			"required": []string{"command"},
		},
	}, {
		"name":        "active_terminal_start",
		"description": "Start exactly one long-running command in the user's active visible 9ed terminal and return startup output without waiting for the process to exit. Use this for commands like npm run start, go run, dev servers, log tails, watchers, and anything expected to keep running while you debug with browser MCP or inspect logs. After calling this tool, do not send another terminal command to the same terminal until active_terminal_read reports waiting for input again.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Command formatted for the active terminal shell.",
				},
				"timeoutMs": map[string]any{
					"type":        "number",
					"description": "Maximum time to collect startup output before returning. Default 10000, max 60000.",
				},
			},
			"required": []string{"command"},
		},
	}, {
		"name":        "active_terminal_read",
		"description": "Read recent output from the active visible 9ed terminal without sending a new command. This reports whether the shell appears to be waiting for input again or whether the command still looks active. Use it to observe long-running commands, gather logs after browser reproduction, or check whether the shell is ready for the next terminal command. Do not use it after a completed command just to re-check the same fact.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"maxBytes": map[string]any{
					"type":        "number",
					"description": "Maximum recent output bytes to read. Default 20000, max 100000.",
				},
			},
			"required": []string{},
		},
	}}
}

func callTool(raw json.RawMessage) any {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(raw, &params)
	if params.Name != "active_terminal_run" && params.Name != "active_terminal_start" && params.Name != "active_terminal_read" {
		return toolText("unknown tool")
	}
	var args struct {
		Command   string `json:"command"`
		TimeoutMS int    `json:"timeoutMs"`
		MaxBytes  int    `json:"maxBytes"`
	}
	_ = json.Unmarshal(params.Arguments, &args)
	if params.Name == "active_terminal_read" {
		body, err := sendTerminalRequest(map[string]any{
			"action":   "read",
			"maxBytes": args.MaxBytes,
		})
		if err != nil {
			return toolText("failed to read terminal output: " + err.Error())
		}
		return toolText(body)
	}
	if args.Command == "" {
		return toolText("command is required")
	}
	action := "run"
	if params.Name == "active_terminal_start" {
		action = "start"
	}
	body, err := sendTerminalRequest(map[string]any{
		"action":    action,
		"command":   args.Command,
		"timeoutMs": args.TimeoutMS,
	})
	if err != nil {
		return toolText("failed to run command: " + err.Error())
	}
	return toolText(body)
}

func sendTerminalRequest(payload map[string]any) (string, error) {
	endpoint := os.Getenv("NINE_ED_MCP_ENDPOINT")
	token := os.Getenv("NINE_ED_MCP_TOKEN")
	if endpoint == "" || token == "" {
		return "", fmt.Errorf("missing MCP endpoint configuration")
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-9ed-MCP-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("server returned %s: %s", resp.Status, string(data))
	}
	if len(data) == 0 {
		return "Terminal action completed with no output.", nil
	}
	return string(data), nil
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
