package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
		return map[string]any{
			"tools": []map[string]any{{
				"name":        "active_terminal_run",
				"description": "Send exactly one command to the user's active visible 9ed terminal. Use this instead of internal shell/execute tools when terminal integration is enabled. The tool completes after sending; do not call it again unless another command is actually needed.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{
							"type":        "string",
							"description": "Command formatted for the active terminal shell.",
						},
					},
					"required": []string{"command"},
				},
			}},
		}
	case "tools/call":
		return callTool(req.Params)
	default:
		return map[string]any{}
	}
}

func callTool(raw json.RawMessage) any {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(raw, &params)
	if params.Name != "active_terminal_run" {
		return toolText("unknown tool")
	}
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(params.Arguments, &args)
	if args.Command == "" {
		return toolText("command is required")
	}
	if err := sendCommand(args.Command); err != nil {
		return toolText("failed to send command: " + err.Error())
	}
	return toolText("Command sent to the active terminal. This tool call is complete. Do not wait for terminal output or repeat the same command in this turn; end your response unless another distinct command is required.")
}

func sendCommand(command string) error {
	endpoint := os.Getenv("NINE_ED_MCP_ENDPOINT")
	token := os.Getenv("NINE_ED_MCP_TOKEN")
	if endpoint == "" || token == "" {
		return fmt.Errorf("missing MCP endpoint configuration")
	}
	body, _ := json.Marshal(map[string]string{"command": command})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-9ed-MCP-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return nil
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
