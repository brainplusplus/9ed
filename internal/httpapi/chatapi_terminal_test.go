package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/terminal"
)

func TestTerminalShellWaitingForInputRecognizesPromptLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "powershell prompt", raw: "PS D:\\golang\\go-webttyd> ", want: true},
		{name: "windows cmd prompt", raw: "C:\\golang\\go-webttyd> ", want: true},
		{name: "unix prompt", raw: "brain@box:~/repo$ ", want: true},
		{name: "powershell command echo", raw: "PS D:\\golang\\go-webttyd> Get-Process -Id 1", want: false},
		{name: "plain greater-than output", raw: "RemoteAddress >", want: false},
		{name: "server log", raw: "ready on http://127.0.0.1:8183", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := terminalShellWaitingForInput(tc.raw); got != tc.want {
				t.Fatalf("terminalShellWaitingForInput(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestTerminalCommandEnvelopeWrapsPowerShellWithHiddenCompletionMarker(t *testing.T) {
	t.Parallel()

	wrapped, marker := terminalCommandEnvelope(terminal.ShellProfile{ID: "pwsh"}, `Get-Process -Id 1`)
	if marker == "" {
		t.Fatal("expected completion marker")
	}
	if !strings.Contains(wrapped, `Get-Process -Id 1`) {
		t.Fatalf("wrapped command missing original command: %q", wrapped)
	}
	if !strings.Contains(wrapped, `9ed-terminal-done;`) {
		t.Fatalf("wrapped command missing hidden marker write: %q", wrapped)
	}
}

func TestTerminalCommandEnvelopeLeavesUnknownShellUntouched(t *testing.T) {
	t.Parallel()

	command := "npm run start"
	wrapped, marker := terminalCommandEnvelope(terminal.ShellProfile{ID: "cmd"}, command)
	if wrapped != command {
		t.Fatalf("expected untouched command, got %q", wrapped)
	}
	if marker != "" {
		t.Fatalf("expected empty marker, got %q", marker)
	}
}

func TestStripTerminalCompletionMarkerRemovesHiddenMarker(t *testing.T) {
	t.Parallel()

	marker := terminalCompletionMarkerPrefix + "token-1\a"
	raw := "line 1\r\n" + marker + "\r\nPS D:\\repo> "
	cleaned := stripTerminalCompletionMarker(raw, marker)
	if strings.Contains(cleaned, marker) {
		t.Fatalf("expected marker removed, got %q", cleaned)
	}
	if !strings.Contains(trimTerminalOutput(cleaned), "PS D:\\repo>") {
		t.Fatalf("expected prompt to remain, got %q", cleaned)
	}
}

func TestTerminalLiveObservationStatusDistinguishesPromptStreamingAndQuiet(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)

	status, _ := terminalLiveObservationStatus("PS D:\\repo> ", now, now)
	if status != "waiting for input" {
		t.Fatalf("expected waiting for input, got %q", status)
	}

	status, _ = terminalLiveObservationStatus("server log line", now.Add(-time.Second), now)
	if status != "streaming output (process still running)" {
		t.Fatalf("expected streaming status, got %q", status)
	}

	status, _ = terminalLiveObservationStatus("server log line", now.Add(-10*time.Second), now)
	if status != "still running (quiet)" {
		t.Fatalf("expected quiet running status, got %q", status)
	}
}
