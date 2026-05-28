//go:build integration

package httpapi

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeActiveBrowserIntegrationDetikArticle(t *testing.T) {
	if strings.TrimSpace(os.Getenv("NINE_ED_RUN_LIVE_DETIK_TEST")) != "1" {
		t.Skip("set NINE_ED_RUN_LIVE_DETIK_TEST=1 to run live detik.com integration test")
	}

	h := newOpenCodeBrowserIntegrationHarness(t, "https://www.detik.com", 3*time.Minute)

	prompt := strings.Join([]string{
		"Gunakan browser MCP pada tab aktif.",
		"Buka https://www.detik.com lalu pilih satu artikel berita dengan klik link artikel.",
		"Setelah goto selesai, lakukan setidaknya satu browser click untuk membuka artikel sebelum memakai inspect, network, atau screenshot.",
		"Gunakan selector artikel yang sederhana seperti a[href*=\"/d-\"] atau a[href*=\"/berita/d-\"] bila perlu.",
		"Jika klik gagal, coba selector alternatif yang lebih sederhana dan jangan ulang selector gagal yang sama.",
		"Jangan klik login, daftar, atau menu non-berita.",
		"Setelah berhasil masuk halaman artikel, jawab PERSIS: DETIK_ARTICLE_OK <FINAL_URL>",
		"<FINAL_URL> harus domain *.detik.com dan path mengandung \"/d-\" atau \"/berita/\".",
		"Jangan gunakan tool terminal.",
	}, "\n")
	h.sendPrompt(t, prompt)

	var (
		transcript            []string
		assistantText         strings.Builder
		doneStopReason        string
		gotoCompleted         bool
		clickCompleted        bool
		inspectCompleted      bool
		pageSourceCompleted   bool
		screenshotBeforeClick bool
		clickFailedCount      int
		duplicateBridgeEvents int
		successByTools        bool
		articleURL            string
	)
	detikArticlePattern := regexp.MustCompile(`https?://[a-z0-9.-]*detik\.com/[^\s"')]+`)

	deadline := time.After(210 * time.Second)
	for {
		select {
		case evt := <-h.sub.C:
			transcript = append(transcript, fmt.Sprintf("%s|%s|%s", evt.Type, evt.ToolTitle, evt.ToolStatus))
			if evt.Type == "tool_call" || evt.Type == "tool_call_update" {
				title := strings.ToLower(strings.TrimSpace(evt.ToolTitle))
				if strings.HasPrefix(title, "9ed_browser_") {
					duplicateBridgeEvents++
				}
			}
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
				case strings.Contains(title, "screenshot"):
					if !clickCompleted {
						screenshotBeforeClick = true
					}
				case strings.Contains(title, "page_source") || strings.Contains(title, "source"):
					if evt.ToolStatus == "completed" {
						pageSourceCompleted = true
					}
				}
				if evt.ToolStatus == "completed" {
					if matched := detikArticlePattern.FindString(evt.ToolContent); matched != "" {
						if isLikelyDetikArticleURL(matched) {
							articleURL = matched
						}
					}
					if tabState, ok := h.browserMgr.Tab(h.tab.ID); ok && isLikelyDetikArticleURL(tabState.URL) {
						articleURL = tabState.URL
					}
				}
				if gotoCompleted && clickCompleted && articleURL != "" {
					successByTools = true
					_ = h.session.Cancel()
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
	if screenshotBeforeClick {
		t.Fatalf("expected click before any screenshot; transcript=%v text=%q stop=%q", transcript, gotText, doneStopReason)
	}
	if !successByTools && !inspectCompleted && !pageSourceCompleted {
		t.Fatalf("expected at least one completed inspect or page_source; transcript=%v text=%q stop=%q", transcript, gotText, doneStopReason)
	}
	if duplicateBridgeEvents != 0 {
		t.Fatalf("expected no duplicate bridge browser events, got %d; transcript=%v", duplicateBridgeEvents, transcript)
	}
	recoveredByTimeout := doneStopReason == "tool_completion_timeout_stream" || doneStopReason == "turn_inactivity_timeout_stream"
	if successByTools {
		if articleURL == "" {
			t.Fatalf("expected article URL from tool output, transcript=%v stop=%q", transcript, doneStopReason)
		}
	} else {
		upperText := strings.ToUpper(gotText)
		if recoveredByTimeout {
			if !strings.Contains(upperText, "DETIK") {
				t.Fatalf("expected timeout recovery summary to mention detik context, got %q; transcript=%v stop=%q", gotText, transcript, doneStopReason)
			}
			return
		}
		if !strings.Contains(upperText, "DETIK_ARTICLE_OK") {
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
	case evt := <-h.sub.C:
		t.Fatalf("expected no late event after done, got %#v; transcript=%v", evt, transcript)
	case <-time.After(1500 * time.Millisecond):
	}
}

func isLikelyDetikArticleURL(rawURL string) bool {
	lowerURL := strings.ToLower(strings.TrimSpace(rawURL))
	if lowerURL == "" || !strings.Contains(lowerURL, "detik.com/") {
		return false
	}
	return strings.Contains(lowerURL, "/d-") || strings.Contains(lowerURL, "/berita/")
}
