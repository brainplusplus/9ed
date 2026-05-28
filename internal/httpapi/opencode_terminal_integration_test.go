//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/chat/acp"
	"github.com/brainplusplus/9ed/internal/terminal"
)

func TestOpenCodeActiveTerminalIntegration(t *testing.T) {
	repoRoot := integrationRepoRoot(t)
	goCmd, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go not found: %v", err)
	}
	opencodeCmd, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatalf("opencode not found: %v", err)
	}
	shellCmd, shellArgs := integrationShell(t)

	termMgr := terminal.NewManager(terminal.NewPTYSpawnFunc())
	term, err := termMgr.Create(terminal.ShellProfile{
		ID:      "pwsh",
		Label:   "PowerShell 7",
		Command: shellCmd,
		Args:    shellArgs,
		CWD:     repoRoot,
	})
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer func() { _ = termMgr.Remove(term.ID) }()

	chatMgr := chat.NewSessionManager()
	api := New(Dependencies{
		Sessions:           termMgr,
		ChatSessionManager: chatMgr,
		TerminalMCPToken:   randomIntegrationToken(),
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	chat.SetActiveTerminalMCPServers([]acp.MCPServer{{
		Name:    "9ed-active-terminal",
		Command: goCmd,
		Args:    []string{"run", filepath.Join(repoRoot, "cmd", "active-terminal-mcp")},
		Env: []acp.EnvVariable{
			{Name: "NINE_ED_MCP_ENDPOINT", Value: server.URL + "/api/chat/terminal/run"},
			{Name: "NINE_ED_MCP_TOKEN", Value: api.terminalMCPToken},
		},
	}})
	defer chat.SetActiveTerminalMCPServers(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	agent := chat.AgentDescriptor{
		ID:          "opencode",
		Label:       "OpenCode",
		Command:     opencodeCmd,
		Available:   true,
		SupportsACP: true,
		ACPArgs:     []string{"acp"},
	}
	session, err := chatMgr.Create(ctx, agent, repoRoot, chat.SessionOptions{
		UseActiveTerminal: true,
		ActiveTerminalID:  term.ID,
	})
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	defer chatMgr.Remove(session.ID())

	stream := api.chatStreams.GetOrCreate(session.ID(), session, nil)
	api.chatStreams.Touch(session.ID())
	stream.StartTurn()
	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	prompt := strings.Join([]string{
		"Use MCP tool active_terminal_run exactly two times in the same active terminal.",
		"First command: Write-Output $PID",
		"Second command: Get-Process -Id <the PID from the first command> | Select-Object -First 1 ProcessName, Id",
		"After the second command, answer with exactly: CHAIN_OK <ProcessName> <PID>",
		"Do not use any browser tool. Do not use active_terminal_read unless the terminal is still running.",
	}, "\n")
	if err := session.Send(ctx, prompt, nil); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	var (
		transcript     []string
		assistantText  strings.Builder
		completedTools int
		doneStopReason string
	)

	deadline := time.After(90 * time.Second)
	for {
		select {
		case evt := <-sub.C:
			transcript = append(transcript, fmt.Sprintf("%s|%s|%s", evt.Type, evt.ToolTitle, evt.ToolStatus))
			if evt.Type == "tool_call_update" && evt.ToolTitle == "active_terminal_run" && evt.ToolStatus == "completed" {
				completedTools++
			}
			if evt.Type == "text" {
				assistantText.WriteString(evt.Text)
			}
			if evt.Type == "done" {
				doneStopReason = evt.StopReason
				goto VERIFY
			}
		case <-deadline:
			t.Fatalf("timed out waiting for done event; transcript=%v text=%q", transcript, assistantText.String())
		}
	}

VERIFY:
	gotText := assistantText.String()
	if completedTools < 2 {
		t.Fatalf("expected at least 2 completed terminal tools, got %d; transcript=%v text=%q stop=%q", completedTools, transcript, gotText, doneStopReason)
	}
	if !strings.Contains(strings.ToUpper(gotText), "CHAIN_OK") {
		t.Fatalf("expected assistant text to contain CHAIN_OK, got %q; transcript=%v stop=%q", gotText, transcript, doneStopReason)
	}
	if !strings.Contains(strings.ToLower(gotText), "pwsh") {
		t.Fatalf("expected assistant text to mention pwsh, got %q; transcript=%v stop=%q", gotText, transcript, doneStopReason)
	}

	select {
	case evt := <-sub.C:
		t.Fatalf("expected no late event after done, got %#v; transcript=%v", evt, transcript)
	case <-time.After(1500 * time.Millisecond):
	}
}

func integrationRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func integrationShell(t *testing.T) (string, []string) {
	t.Helper()
	if cmd, err := exec.LookPath("pwsh"); err == nil {
		return cmd, []string{"-NoLogo", "-NoProfile"}
	}
	if cmd, err := exec.LookPath("powershell"); err == nil {
		return cmd, []string{"-NoLogo", "-NoProfile"}
	}
	t.Fatal("no PowerShell executable found on PATH")
	return "", nil
}

func randomIntegrationToken() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("integration-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}
