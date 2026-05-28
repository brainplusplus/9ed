//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/browser"
	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/chat/acp"
)

func TestOpenCodeActiveBrowserIntegrationDetikArticle(t *testing.T) {
	if strings.TrimSpace(os.Getenv("NINE_ED_RUN_LIVE_DETIK_TEST")) != "1" {
		t.Skip("set NINE_ED_RUN_LIVE_DETIK_TEST=1 to run live detik.com integration test")
	}

	repoRoot := integrationRepoRoot(t)
	goCmd, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go not found: %v", err)
	}
	opencodeCmd, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatalf("opencode not found: %v", err)
	}

	browserMgr := browser.NewManager()
	defer browserMgr.Close()

	tab, err := browserMgr.CreateTabWithTransport("https://www.detik.com", browser.TransportWebRTC)
	if err != nil {
		t.Fatalf("create detik browser tab: %v", err)
	}
	defer func() { _ = browserMgr.DeleteTab(tab.ID) }()

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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
		"Gunakan browser MCP pada tab aktif.",
		"Buka https://www.detik.com lalu pilih satu artikel berita dengan klik link artikel.",
		"Jika klik gagal, coba selector alternatif yang lebih sederhana dan jangan ulang selector gagal yang sama.",
		"Jangan klik login, daftar, atau menu non-berita.",
		"Setelah berhasil masuk halaman artikel, jawab PERSIS: DETIK_ARTICLE_OK <FINAL_URL>",
		"<FINAL_URL> harus domain *.detik.com dan path mengandung \"/d-\" atau \"/berita/\".",
		"Jangan gunakan tool terminal.",
	}, "\n")
	if err := session.Send(ctx, prompt, nil); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	var (
		transcript       []string
		assistantText    strings.Builder
		doneStopReason   string
		gotoCompleted    bool
		clickCompleted   bool
		inspectCompleted bool
		clickFailedCount int
		successByTools   bool
		articleURL       string
	)
	detikArticlePattern := regexp.MustCompile(`https?://[a-z0-9.-]*detik\.com/[^\s"')]+`)

	deadline := time.After(210 * time.Second)
	for {
		select {
		case evt := <-sub.C:
			transcript = append(transcript, fmt.Sprintf("%s|%s|%s", evt.Type, evt.ToolTitle, evt.ToolStatus))
			if evt.Type == "tool_call_update" && isBrowserMCPTool(evt.ToolTitle) {
				title := strings.ToLower(strings.TrimSpace(evt.ToolTitle))
				switch {
				case strings.Contains(title, "goto") || strings.Contains(title, "navigate"):
					if evt.ToolStatus == "completed" {
						gotoCompleted = true
					}
				case strings.Contains(title, "click"):
					if evt.ToolStatus == "completed" {
						clickCompleted = true
					}
					if evt.ToolStatus == "failed" {
						clickFailedCount++
					}
				case strings.Contains(title, "inspect"):
					if evt.ToolStatus == "completed" {
						inspectCompleted = true
					}
				}
				if evt.ToolStatus == "completed" {
					if matched := detikArticlePattern.FindString(evt.ToolContent); matched != "" {
						if isLikelyDetikArticleURL(matched) {
							articleURL = matched
						}
					}
					if tabState, ok := browserMgr.Tab(tab.ID); ok && isLikelyDetikArticleURL(tabState.URL) {
						articleURL = tabState.URL
					}
				}
				if gotoCompleted && clickCompleted && articleURL != "" {
					successByTools = true
					_ = session.Cancel()
					doneStopReason = "integration_success_by_tools"
					goto VERIFY
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
	if !gotoCompleted {
		t.Fatalf("expected at least one completed goto/navigate; transcript=%v text=%q stop=%q", transcript, gotText, doneStopReason)
	}
	if !clickCompleted {
		t.Fatalf("expected at least one completed click; clickFailures=%d transcript=%v text=%q stop=%q", clickFailedCount, transcript, gotText, doneStopReason)
	}
	if !inspectCompleted {
		t.Fatalf("expected at least one completed inspect; transcript=%v text=%q stop=%q", transcript, gotText, doneStopReason)
	}
	if !successByTools && doneStopReason == "tool_completion_timeout_stream" {
		t.Fatalf("expected natural done event, got stall recovery done; transcript=%v text=%q", transcript, gotText)
	}
	if successByTools {
		if articleURL == "" {
			t.Fatalf("expected article URL from tool output, transcript=%v stop=%q", transcript, doneStopReason)
		}
	} else {
		if !strings.Contains(strings.ToUpper(gotText), "DETIK_ARTICLE_OK") {
			t.Fatalf("expected assistant text marker, got %q; transcript=%v stop=%q", gotText, transcript, doneStopReason)
		}
		urlMatch := detikArticlePattern.FindString(gotText)
		if urlMatch == "" {
			t.Fatalf("expected detik article URL in assistant text, got %q; transcript=%v", gotText, transcript)
		}
		if !isLikelyDetikArticleURL(urlMatch) {
			t.Fatalf("expected article-like detik URL, got %q; transcript=%v", urlMatch, transcript)
		}
	}

	if successByTools {
		return
	}

	select {
	case evt := <-sub.C:
		t.Fatalf("expected no late event after done, got %#v; transcript=%v", evt, transcript)
	case <-time.After(1500 * time.Millisecond):
	}
}

func isLikelyDetikArticleURL(rawURL string) bool {
	lowerURL := strings.ToLower(strings.TrimSpace(rawURL))
	if lowerURL == "" || !strings.Contains(lowerURL, "detik.com/") {
		return false
	}
	return strings.Contains(lowerURL, "/d-")
}
