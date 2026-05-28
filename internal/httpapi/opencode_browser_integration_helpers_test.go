//go:build integration

package httpapi

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/chat/acp"
)

type openCodeBrowserIntegrationHarness struct {
	browserMgr *browser.Manager
	tab        browser.Tab
	session    chat.ChatSession
	stream     *chatStream
	sub        *chatSubscriber
	ctx        context.Context
}

func newOpenCodeBrowserIntegrationHarness(t *testing.T, initialURL string, timeout time.Duration) *openCodeBrowserIntegrationHarness {
	t.Helper()

	repoRoot := integrationRepoRoot(t)
	goCmd := requireCommand(t, "go")
	opencodeCmd := requireCommand(t, "opencode")

	browserMgr := browser.NewManager()
	tab, err := browserMgr.CreateTabWithTransport(initialURL, browser.TransportWebRTC)
	if err != nil {
		browserMgr.Close()
		t.Fatalf("create browser tab: %v", err)
	}

	chatMgr := chat.NewSessionManager()
	api := New(Dependencies{
		ChatSessionManager: chatMgr,
		Browser:            browserMgr,
		BrowserMCPToken:    randomIntegrationToken(),
	})
	server := httptest.NewServer(api.Handler())

	chat.SetActiveBrowserMCPServers([]acp.MCPServer{{
		Name:    "9ed-active-browser",
		Command: goCmd,
		Args:    []string{"run", filepath.Join(repoRoot, "cmd", "active-browser-mcp")},
		Env: []acp.EnvVariable{
			{Name: "NINE_ED_BROWSER_MCP_ENDPOINT", Value: server.URL + "/api/chat/browser/run"},
			{Name: "NINE_ED_BROWSER_MCP_TOKEN", Value: api.browserMCPToken},
		},
	}})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
		cancel()
		chat.SetActiveBrowserMCPServers(nil)
		server.Close()
		_ = browserMgr.DeleteTab(tab.ID)
		browserMgr.Close()
		t.Fatalf("create chat session: %v", err)
	}

	stream := api.chatStreams.GetOrCreate(session.ID(), session, nil)
	api.chatStreams.Touch(session.ID())
	sub := stream.Subscribe()

	h := &openCodeBrowserIntegrationHarness{
		browserMgr: browserMgr,
		tab:        tab,
		session:    session,
		stream:     stream,
		sub:        sub,
		ctx:        ctx,
	}

	t.Cleanup(func() {
		stream.Unsubscribe(sub)
		chatMgr.Remove(session.ID())
		cancel()
		chat.SetActiveBrowserMCPServers(nil)
		server.Close()
		_ = browserMgr.DeleteTab(tab.ID)
		browserMgr.Close()
	})

	return h
}

func (h *openCodeBrowserIntegrationHarness) sendPrompt(t *testing.T, prompt string) {
	t.Helper()
	h.stream.StartTurn()
	if err := h.session.Send(h.ctx, prompt, nil); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
}

func requireCommand(t *testing.T, name string) string {
	t.Helper()
	cmd, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s not found: %v", name, err)
	}
	return cmd
}
