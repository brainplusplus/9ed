package chat

import (
	"testing"
)

// newTUIOnlySession builds a ptySession with enough state for TUI detection
// tests, without spawning a real PTY subprocess. The pending carryover buffer
// lives under s.mu.
func newTUIOnlySession() *ptySession {
	return &ptySession{
		id:        "test-tui",
		agentID:   "test",
		ringBuffer: newPtyRingBuffer(1024),
		inputLock: newInputLock(defaultInputLockTTL),
	}
}

// TestDetectTUI_EnterAlternateScreen1049 verifies the canonical "enter TUI"
// sequence \x1b[?1049h sets tuiMode=true when it arrives in a single read.
func TestDetectTUI_EnterAlternateScreen1049(t *testing.T) {
	s := newTUIOnlySession()
	s.detectTUIMode([]byte("\x1b[?1049h"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true after \\x1b[?1049h")
	}
}

// TestDetectTUI_ExitAlternateScreen1049 verifies the canonical "exit TUI"
// sequence \x1b[?1049l sets tuiMode=false.
func TestDetectTUI_ExitAlternateScreen1049(t *testing.T) {
	s := newTUIOnlySession()
	s.mu.Lock()
	s.tuiMode = true
	s.mu.Unlock()

	s.detectTUIMode([]byte("\x1b[?1049l"))
	if s.IsTUIMode() {
		t.Fatalf("expected tuiMode=false after \\x1b[?1049l")
	}
}

// TestDetectTUI_1047Sequence verifies the 1047 alternate-screen sequence is
// matched (older xterm variant).
func TestDetectTUI_1047Sequence(t *testing.T) {
	s := newTUIOnlySession()
	s.detectTUIMode([]byte("\x1b[?1047h"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true after \\x1b[?1047h")
	}

	s2 := newTUIOnlySession()
	s2.mu.Lock()
	s2.tuiMode = true
	s2.mu.Unlock()
	s2.detectTUIMode([]byte("\x1b[?1047l"))
	if s2.IsTUIMode() {
		t.Fatalf("expected tuiMode=false after \\x1b[?1047l")
	}
}

// TestDetectTUI_SplitAcrossReads (VAL-PTY-001): a sequence that arrives split
// across two reads (\x1b[?1049 in read 1, h in read 2) must still set
// tuiMode=true via the carryover buffer.
func TestDetectTUI_SplitAcrossReads(t *testing.T) {
	s := newTUIOnlySession()

	// First read contains the escape sequence without the final mode byte.
	s.detectTUIMode([]byte("\x1b[?1049"))
	if s.IsTUIMode() {
		t.Fatalf("tuiMode should not yet be set after partial read")
	}

	// Second read delivers the trailing 'h' that completes the sequence.
	s.detectTUIMode([]byte("h"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true after split-across-reads \\x1b[?1049h")
	}
}

// TestDetectTUI_SplitAcrossReadsAtESC (VAL-PTY-001): a sequence that arrives
// split right at the ESC byte (\x1b in read 1, [?1049h in read 2) is detected
// via the carryover buffer.
func TestDetectTUI_SplitAcrossReadsAtESC(t *testing.T) {
	s := newTUIOnlySession()

	s.detectTUIMode([]byte("some output\x1b"))
	if s.IsTUIMode() {
		t.Fatalf("tuiMode should not yet be set after ESC-only read")
	}

	s.detectTUIMode([]byte("[?1049h"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true after split-across-reads at ESC")
	}
}

// TestDetectTUI_SplitAcrossReadsExit verifies exit sequences split across
// reads are also detected.
func TestDetectTUI_SplitAcrossReadsExit(t *testing.T) {
	s := newTUIOnlySession()
	s.mu.Lock()
	s.tuiMode = true
	s.mu.Unlock()

	s.detectTUIMode([]byte("\x1b[?1049"))
	s.detectTUIMode([]byte("l"))
	if s.IsTUIMode() {
		t.Fatalf("expected tuiMode=false after split-across-reads \\x1b[?1049l")
	}
}

// TestDetectTUI_MultipleSequencesInOneRead verifies that multiple escape
// sequences in a single read are all parsed.
func TestDetectTUI_MultipleSequencesInOneRead(t *testing.T) {
	s := newTUIOnlySession()

	// Enter then immediately exit TUI in the same read.
	s.detectTUIMode([]byte("\x1b[?1049h\x1b[?1049l"))
	if s.IsTUIMode() {
		t.Fatalf("expected tuiMode=false after enter+exit in one read")
	}

	// Exit then enter again.
	s.detectTUIMode([]byte("\x1b[?1049l\x1b[?1049h"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true after exit+enter in one read")
	}
}

// TestDetectTUI_SequenceEmbeddedInOutput verifies that escape sequences
// embedded in regular output bytes are still detected.
func TestDetectTUI_SequenceEmbeddedInOutput(t *testing.T) {
	s := newTUIOnlySession()

	// Sequence embedded in regular terminal output.
	s.detectTUIMode([]byte("vim starting\r\n\x1b[?1049h\r\nclearing screen"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true when sequence is embedded in output")
	}
}

// TestDetectTUI_NoSequences verifies that regular output without escape
// sequences does not change tuiMode.
func TestDetectTUI_NoSequences(t *testing.T) {
	s := newTUIOnlySession()
	s.detectTUIMode([]byte("regular terminal output\r\nwith newlines and stuff"))
	if s.IsTUIMode() {
		t.Fatalf("tuiMode should remain false for regular output")
	}

	// Now enter TUI mode then send more regular output.
	s.detectTUIMode([]byte("\x1b[?1049h"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true after enter")
	}
	s.detectTUIMode([]byte("more output\r\n"))
	if !s.IsTUIMode() {
		t.Fatalf("tuiMode should remain true after regular output")
	}
}

// TestDetectTUI_PendingCapped (VAL-PTY-001): the carryover buffer is capped
// at a small size so a runaway stream of ESC bytes can't grow it unboundedly.
func TestDetectTUI_PendingCapped(t *testing.T) {
	s := newTUIOnlySession()

	// Send a chunk of ESC bytes well beyond the pending cap.
	huge := make([]byte, 256)
	for i := range huge {
		huge[i] = 0x1b
	}
	s.detectTUIMode(huge)

	s.mu.Lock()
	pendingLen := len(s.pending)
	s.mu.Unlock()
	if pendingLen > maxPendingBytes {
		t.Fatalf("pending buffer grew to %d bytes (cap %d)", pendingLen, maxPendingBytes)
	}
}

// TestDetectTUI_Non1049_SequenceIgnored verifies that other escape sequences
// (e.g. cursor movement) do not flip TUI mode.
func TestDetectTUI_Non1049_SequenceIgnored(t *testing.T) {
	s := newTUIOnlySession()

	// Cursor-up sequence: \x1b[A — should NOT change TUI mode.
	s.detectTUIMode([]byte("\x1b[A"))
	if s.IsTUIMode() {
		t.Fatalf("cursor-up sequence should not set tuiMode")
	}

	// SGR (color): \x1b[31m — should NOT change TUI mode.
	s.detectTUIMode([]byte("\x1b[31mhello\x1b[0m"))
	if s.IsTUIMode() {
		t.Fatalf("SGR color sequence should not set tuiMode")
	}

	// \x1b[?25h (show cursor) — private mode but NOT 1049/1047/47.
	s.detectTUIMode([]byte("\x1b[?25h"))
	if s.IsTUIMode() {
		t.Fatalf("cursor-show sequence should not set tuiMode")
	}
}

// TestDetectTUI_1049And1047Interleaved verifies that 1049 and 1047 are tracked
// together. A 1047l exit with a prior 1049h enter should... well, the ADR
// treats them as the same alternate-screen signal. We only assert the simple
// enter/exit semantics here.
func TestDetectTUI_1049And1047Interleaved(t *testing.T) {
	s := newTUIOnlySession()
	s.detectTUIMode([]byte("\x1b[?1049h"))
	if !s.IsTUIMode() {
		t.Fatalf("expected tuiMode=true after 1049h")
	}
	s.detectTUIMode([]byte("\x1b[?1047l"))
	if s.IsTUIMode() {
		t.Fatalf("expected tuiMode=false after 1047l")
	}
}
