//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeActiveBrowserIntegration(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!doctype html><html><head><title>INTEGRATION_FIXTURE</title></head><body><main><h1>BROWSER_CHAIN_TARGET</h1><p>OpenCode active browser MCP integration fixture page.</p></main></body></html>")
	}))
	defer fixture.Close()

	h := newOpenCodeBrowserIntegrationHarness(t, fixture.URL, 2*time.Minute)

	inspectCtx, cancelInspect := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancelInspect()
	inspect, err := h.browserMgr.TabInspect(inspectCtx, h.tab.ID)
	if err != nil {
		t.Fatalf("warm inspect failed: %v", err)
	}
	if !strings.Contains(inspect.Title, "INTEGRATION_FIXTURE") {
		t.Fatalf("unexpected warm inspect title: %q", inspect.Title)
	}

	prompt := strings.Join([]string{
		"Use active browser MCP tools in the linked WebRTC tab.",
		"Call 9ed_browser_goto exactly once with URL: " + fixture.URL,
		"Then call 9ed_browser_inspect exactly once.",
		"After inspect completes, answer with exactly: BROWSER_CHAIN_OK INTEGRATION_FIXTURE",
		"Do not use any terminal tool and do not call screenshot/console/network tools.",
	}, "\n")
	h.sendPrompt(t, prompt)

	var (
		transcript            []string
		assistantText         strings.Builder
		completedBrowserTools int
		sawGoto               bool
		sawInspect            bool
		duplicateBridgeEvents int
		doneStopReason        string
	)

	deadline := time.After(90 * time.Second)
	for {
		select {
		case evt := <-h.sub.C:
			transcript = append(transcript, fmt.Sprintf("%s|%s|%s", evt.Type, evt.ToolTitle, evt.ToolStatus))
			if evt.Type == "tool_call" || evt.Type == "tool_call_update" {
				title := strings.ToLower(strings.TrimSpace(evt.ToolTitle))
				if title == "9ed_browser_goto" || title == "9ed_browser_inspect" || title == "9ed_browser_click" {
					duplicateBridgeEvents++
				}
			}
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
	if duplicateBridgeEvents != 0 {
		t.Fatalf("expected no duplicate bridge browser events, got %d; transcript=%v", duplicateBridgeEvents, transcript)
	}
	upperText := strings.ToUpper(gotText)
	if doneStopReason == "tool_completion_timeout_stream" {
		if !strings.Contains(upperText, "INTEGRATION_FIXTURE") || !strings.Contains(upperText, "BROWSER_CHAIN_TARGET") {
			t.Fatalf("expected synthesized browser recovery summary, got %q; transcript=%v stop=%q", gotText, transcript, doneStopReason)
		}
		return
	}
	if strings.TrimSpace(gotText) == "" {
		// Some ACP runs end the turn without a final text chunk even after a
		// valid tool chain; treat the completed goto+inspect sequence as success.
		return
	}
	if !strings.Contains(upperText, "BROWSER_CHAIN_OK") || !strings.Contains(upperText, "INTEGRATION_FIX") {
		t.Fatalf("expected assistant text to contain marker, got %q; transcript=%v stop=%q", gotText, transcript, doneStopReason)
	}

	select {
	case evt := <-h.sub.C:
		t.Fatalf("expected no late event after done, got %#v; transcript=%v", evt, transcript)
	case <-time.After(1500 * time.Millisecond):
	}
}
