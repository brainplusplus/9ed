//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/chat/acp"
)

func TestOpenCodeActiveBrowserIntegration(t *testing.T) {
	repoRoot := integrationRepoRoot(t)
	goCmd, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go not found: %v", err)
	}
	opencodeCmd, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatalf("opencode not found: %v", err)
	}

	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!doctype html><html><head><title>INTEGRATION_FIXTURE</title></head><body><main><h1>BROWSER_CHAIN_TARGET</h1><p>OpenCode active browser MCP integration fixture page.</p></main></body></html>")
	}))
	defer fixture.Close()

	browserMgr := browser.NewManager()
	defer browserMgr.Close()

	tab, err := browserMgr.CreateTabWithTransport(fixture.URL, browser.TransportWebRTC)
	if err != nil {
		t.Fatalf("create browser tab: %v", err)
	}
	defer func() { _ = browserMgr.DeleteTab(tab.ID) }()

	inspectCtx, cancelInspect := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelInspect()
	inspect, err := browserMgr.TabInspect(inspectCtx, tab.ID)
	if err != nil {
		t.Fatalf("warm inspect failed: %v", err)
	}
	if !strings.Contains(inspect.Title, "INTEGRATION_FIXTURE") {
		t.Fatalf("unexpected warm inspect title: %q", inspect.Title)
	}

	chatMgr := chat.NewSessionManager()
	api := New(Dependencies{
		ChatSessionManager: chatMgr,
		Browser:            browserMgr,
		BrowserMCPToken:    randomIntegrationToken(),
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	chat.SetActiveBrowserMCPServers([]acp.MCPServer{{
		Name:    "9ed-active-browser",
		Command: goCmd,
		Args:    []string{"run", filepath.Join(repoRoot, "cmd", "active-browser-mcp")},
		Env: []acp.EnvVariable{
			{Name: "NINE_ED_BROWSER_MCP_ENDPOINT", Value: server.URL + "/api/chat/browser/run"},
			{Name: "NINE_ED_BROWSER_MCP_TOKEN", Value: api.browserMCPToken},
		},
	}})
	defer chat.SetActiveBrowserMCPServers(nil)

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
		UseActiveBrowser:   true,
		ActiveBrowserTabID: tab.ID,
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
		"Use active browser MCP tools in the linked WebRTC tab.",
		"Call 9ed_browser_goto exactly once with URL: " + fixture.URL,
		"Then call 9ed_browser_inspect exactly once.",
		"After inspect completes, answer with exactly: BROWSER_CHAIN_OK INTEGRATION_FIXTURE",
		"Do not use any terminal tool and do not call screenshot/console/network tools.",
	}, "\n")
	if err := session.Send(ctx, prompt, nil); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	var (
		transcript            []string
		assistantText         strings.Builder
		completedBrowserTools int
		sawGoto               bool
		sawInspect            bool
		doneStopReason        string
	)

	deadline := time.After(90 * time.Second)
	for {
		select {
		case evt := <-sub.C:
			transcript = append(transcript, fmt.Sprintf("%s|%s|%s", evt.Type, evt.ToolTitle, evt.ToolStatus))
			if evt.Type == "tool_call_update" && evt.ToolStatus == "completed" && isBrowserMCPTool(evt.ToolTitle) {
				completedBrowserTools++
				toolTitle := strings.ToLower(strings.TrimSpace(evt.ToolTitle))
				if strings.Contains(toolTitle, "goto") || strings.Contains(toolTitle, "navigate") {
					sawGoto = true
				}
				if strings.Contains(toolTitle, "inspect") {
					sawInspect = true
				}
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
	if completedBrowserTools < 2 || !sawGoto || !sawInspect {
		t.Fatalf("expected goto+inspect browser chain, got completed=%d sawGoto=%t sawInspect=%t transcript=%v text=%q stop=%q", completedBrowserTools, sawGoto, sawInspect, transcript, gotText, doneStopReason)
	}
	if doneStopReason == "tool_completion_timeout_stream" {
		t.Fatalf("expected natural done event, got stall recovery done; transcript=%v text=%q", transcript, gotText)
	}
	upperText := strings.ToUpper(gotText)
	if !strings.Contains(upperText, "BROWSER_CHAIN_OK") || !strings.Contains(upperText, "INTEGRATION_FIX") {
		t.Fatalf("expected assistant text to contain marker, got %q; transcript=%v stop=%q", gotText, transcript, doneStopReason)
	}

	select {
	case evt := <-sub.C:
		t.Fatalf("expected no late event after done, got %#v; transcript=%v", evt, transcript)
	case <-time.After(1500 * time.Millisecond):
	}
}
