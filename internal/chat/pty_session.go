package chat

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"

	ptylib "github.com/aymanbagabas/go-pty"
)

// maxPendingBytes caps the carryover buffer used by detectTUIMode so a
// runaway stream of partial escape bytes can't grow it unboundedly. 16 bytes
// is more than enough for any CSI sequence we care about (longest is
// "\x1b[?1049h" = 7 bytes).
const maxPendingBytes = 16

// defaultInputLockTTL is the ADR-0005 default TTL for the per-pane input soft
// lock (2s). config.Config.PTYInputLockTTL overrides this at runtime.
const defaultInputLockTTL = 2 * time.Second

// tuiModeRegex matches DEC Private Mode Set/Reset sequences for the
// alternate-screen modes ADR-0005 cares about:
//   - 1049 (save cursor + enter/exit alternate screen)
//   - 1047 (enter/exit alternate screen, no cursor save)
//
// The trailing mode byte ([hl]) determines enter ('h') vs exit ('l'). The
// regex is anchored on the CSI private-marker prefix "\x1b[?" so it does not
// match unrelated CSI sequences like cursor movement (\x1b[A) or SGR
// (\x1b[31m). VAL-PTY-002: the legacy 47h/47l byte-matching dead code was
// removed entirely — only 1049 and 1047 are matched.
var tuiModeRegex = regexp.MustCompile(`\x1b\[\?(?:1049|1047)([hl])`)

// ptyRingBuffer is a fixed-size circular buffer for PTY output bytes
// (ADR-0005). Used for replay-on-subscribe so client B connecting mid-session
// sees recent output.
type ptyRingBuffer struct {
	mu    sync.Mutex
	data  []byte
	size  int
	start int
	total int
}

func newPtyRingBuffer(size int) *ptyRingBuffer {
	if size <= 0 {
		size = 256 * 1024 // 256KB default
	}
	return &ptyRingBuffer{data: make([]byte, size), size: size}
}

func (rb *ptyRingBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for len(p) > 0 {
		avail := rb.size - rb.total
		if avail == 0 {
			// Buffer full — evict oldest.
			evict := len(p)
			if evict > rb.size {
				evict = rb.size
			}
			rb.start = (rb.start + evict) % rb.size
			rb.total -= evict
			avail = evict
		}
		writeLen := len(p)
		if writeLen > avail {
			writeLen = avail
		}
		endPos := (rb.start + rb.total) % rb.size
		endAvail := rb.size - endPos
		if writeLen > endAvail {
			copy(rb.data[endPos:], p[:endAvail])
			copy(rb.data[0:], p[endAvail:writeLen])
		} else {
			copy(rb.data[endPos:endPos+writeLen], p[:writeLen])
		}
		rb.total += writeLen
		p = p[writeLen:]
	}
}

// Snapshot returns a copy of the buffer content in chronological order.
func (rb *ptyRingBuffer) Snapshot() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]byte, rb.total)
	if rb.total < rb.size {
		copy(out, rb.data[rb.start:rb.start+rb.total])
	} else {
		firstLen := rb.size - rb.start
		copy(out[:firstLen], rb.data[rb.start:])
		copy(out[firstLen:], rb.data[:rb.total-firstLen])
	}
	return out
}

// ptySession wraps the existing PTY-based agent spawn as a ChatSession.
type ptySession struct {
	id      string
	agentID string
	pty     ptylib.Pty
	cmd     *ptylib.Cmd
	events  chan ChatEvent
	done    chan struct{}
	closeMu sync.Once
	mu      sync.Mutex

	// ADR-0005: ring buffer for scrollback replay.
	ringBuffer *ptyRingBuffer
	// ADR-0005: TUI mode detection (alternate screen buffer).
	tuiMode bool
	// ADR-0005: carryover buffer for TUI detection across read boundaries.
	// Holds trailing bytes from the previous read that did not form a
	// complete escape sequence (e.g. "\x1b[?1049" waiting for the final
	// "h"/"l"). Always accessed under s.mu. Capped at maxPendingBytes.
	pending []byte
	// ADR-0005: collaborative soft lock for PTY input. Per-pane (keyed by
	// paneID) so the API is forward-compatible with multi-pane terminals.
	// Today paneID == sessionID, so there is at most one entry.
	inputLock *inputLock
}

func newPTYSession(agent AgentDescriptor, workDir string, ringBufferSize int, inputLockTTL time.Duration) (*ptySession, error) {
	pseudo, err := ptylib.New()
	if err != nil {
		return nil, fmt.Errorf("pty create: %w", err)
	}

	cmd := pseudo.Command(agent.Command, agent.Args...)
	if workDir != "" {
		cmd.Dir = workDir
	} else {
		cmd.Dir = currentWorkingDirectory()
	}
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		_ = pseudo.Close()
		return nil, fmt.Errorf("spawn %s: %w", agent.Command, err)
	}

	// ADR-0005: honor config-derived sizing. Fall back to ADR defaults
	// (1MB ring buffer, 2s lock TTL) when the caller passed zero values.
	if ringBufferSize <= 0 {
		ringBufferSize = 1048576 // 1MB default
	}
	if inputLockTTL <= 0 {
		inputLockTTL = 2 * time.Second
	}

	s := &ptySession{
		id:         uuid.NewString(),
		agentID:    agent.ID,
		pty:        pseudo,
		cmd:        cmd,
		events:     make(chan ChatEvent, 256),
		done:       make(chan struct{}),
		ringBuffer: newPtyRingBuffer(ringBufferSize),
		inputLock:  newInputLock(inputLockTTL),
	}

	if cmds := ptyAgentCommands(agent.ID); len(cmds) > 0 {
		s.events <- ChatEvent{Type: "commands", Commands: cmds}
	}

	go s.readLoop()
	go func() {
		_ = cmd.Wait()
	}()

	return s, nil
}

func ptyAgentCommands(agentID string) []CommandInfo {
	switch agentID {
	case "claude":
		return []CommandInfo{
			{Name: "compact", Description: "Compact conversation context"},
			{Name: "clear", Description: "Clear conversation"},
			{Name: "config", Description: "Show configuration"},
			{Name: "cost", Description: "Show token usage"},
			{Name: "doctor", Description: "Check health"},
			{Name: "help", Description: "Show commands"},
			{Name: "init", Description: "Initialize project"},
			{Name: "login", Description: "Login to account"},
			{Name: "logout", Description: "Logout"},
			{Name: "memory", Description: "Manage memory"},
			{Name: "model", Description: "Switch model"},
			{Name: "review", Description: "Review code"},
			{Name: "vim", Description: "Toggle vim mode"},
		}
	case "codex":
		return []CommandInfo{
			{Name: "help", Description: "Show commands"},
			{Name: "clear", Description: "Clear conversation"},
			{Name: "model", Description: "Switch model"},
			{Name: "approval", Description: "Change approval mode"},
		}
	default:
		return nil
	}
}

func (s *ptySession) ID() string                             { return s.id }
func (s *ptySession) AgentID() string                        { return s.agentID }
func (s *ptySession) WorkDir() string                        { return "" }
func (s *ptySession) Mode() SessionMode                      { return ModePTY }
func (s *ptySession) Events() <-chan ChatEvent               { return s.events }
func (s *ptySession) Done() <-chan struct{}                  { return s.done }
func (s *ptySession) Err() error                              { return nil }
func (s *ptySession) ACPSessionID() string                   { return "" }
func (s *ptySession) IsResumed() bool                        { return false }
func (s *ptySession) RespondPermission(_ PermissionResponse) {}
func (s *ptySession) SetAutoApprove(_ bool)                  {}
func (s *ptySession) SetUseActiveTerminal(_ bool, _ string)  {}
func (s *ptySession) UseActiveTerminalEnabled() bool         { return false }
func (s *ptySession) ActiveTerminalID() string               { return "" }
func (s *ptySession) SetUseActiveBrowser(_ bool, _ string)   {}
func (s *ptySession) UseActiveBrowserEnabled() bool          { return false }
func (s *ptySession) ActiveBrowserTabID() string             { return "" }

func (s *ptySession) SetConfigOption(_ context.Context, _, _ string) error {
	return nil
}

func (s *ptySession) Send(_ context.Context, message string, attachments []Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.pty.Write([]byte(formatMessageWithAttachments(message, attachments, true) + "\n"))
	return err
}

func (s *ptySession) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.pty.Write([]byte{0x03})
	return err
}

// inputLock tracks the soft lock state for collaborative PTY input (ADR-0005).
// The lock is per-pane (keyed by paneID) so the API is forward-compatible with
// multi-pane terminals. Today paneID == sessionID, so there is at most one
// entry in the map.
type inputLock struct {
	mu  sync.Mutex
	ttl time.Duration
	// panes maps paneID → the per-pane lock entry.
	panes map[string]*inputLockEntry
}

// inputLockEntry is the per-pane lock state.
type inputLockEntry struct {
	holderID  string // clientId of the lock holder, empty = no lock
	expiresAt time.Time
}

func newInputLock(ttl time.Duration) *inputLock {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	return &inputLock{ttl: ttl, panes: make(map[string]*inputLockEntry)}
}

// acquireInputLock attempts to acquire or renew the soft lock for a specific
// pane (ADR-0005). If holderID matches the current holder for paneID, the lock
// is renewed. If no lock is held or the lock has expired, it is acquired.
// Returns true if the caller now holds the lock for that pane.
func (s *ptySession) acquireInputLock(paneID, holderID string) bool {
	if s.inputLock == nil {
		return true // lock disabled
	}
	s.inputLock.mu.Lock()
	defer s.inputLock.mu.Unlock()

	now := time.Now()
	entry, ok := s.inputLock.panes[paneID]
	if !ok {
		entry = &inputLockEntry{}
		s.inputLock.panes[paneID] = entry
	}
	if entry.holderID == "" || now.After(entry.expiresAt) {
		// No active lock — acquire.
		entry.holderID = holderID
		entry.expiresAt = now.Add(s.inputLock.ttl)
		return true
	}
	if entry.holderID == holderID {
		// Same holder — renew.
		entry.expiresAt = now.Add(s.inputLock.ttl)
		return true
	}
	// Locked by someone else.
	return false
}

// InputLockHolder returns the clientId of the current lock holder for the
// given pane, or empty if no lock is active (ADR-0005).
func (s *ptySession) InputLockHolder(paneID string) string {
	if s.inputLock == nil {
		return ""
	}
	s.inputLock.mu.Lock()
	defer s.inputLock.mu.Unlock()
	entry, ok := s.inputLock.panes[paneID]
	if !ok {
		return ""
	}
	if time.Now().After(entry.expiresAt) {
		return ""
	}
	return entry.holderID
}

// ReleaseInputLock releases the soft lock for a pane if the caller is the
// holder (ADR-0005).
func (s *ptySession) ReleaseInputLock(paneID, holderID string) {
	if s.inputLock == nil {
		return
	}
	s.inputLock.mu.Lock()
	defer s.inputLock.mu.Unlock()
	entry, ok := s.inputLock.panes[paneID]
	if !ok {
		return
	}
	if entry.holderID == holderID {
		delete(s.inputLock.panes, paneID)
	}
}

func (s *ptySession) Close() error {
	s.closeMu.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		if s.pty != nil {
			_ = s.pty.Close()
		}
	})
	return nil
}

func (s *ptySession) readLoop() {
	defer close(s.done)

	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			raw := buf[:n]
			// ADR-0005: write raw bytes to ring buffer for scrollback replay.
			if s.ringBuffer != nil {
				s.ringBuffer.Write(raw)
			}
			// ADR-0005: detect TUI mode (alternate screen buffer enter/exit).
			s.detectTUIMode(raw)

			text := StripANSI(string(raw))
			if text != "" {
				select {
				case s.events <- ChatEvent{Type: "text", Text: text}:
				default:
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				select {
				case s.events <- ChatEvent{Type: "error", Error: err.Error()}:
				default:
				}
			}
			return
		}
	}
}

// PtySession is an exported wrapper for ptySession to allow type assertion
// from other packages (ADR-0005 soft lock access from httpapi).
type PtySession = ptySession

// AcquireInputLockPublic is the exported version of acquireInputLock.
func (s *ptySession) AcquireInputLockPublic(paneID, holderID string) bool {
	return s.acquireInputLock(paneID, holderID)
}

// InputLockHolderPublic is the exported version of InputLockHolder.
func (s *ptySession) InputLockHolderPublic(paneID string) string {
	return s.InputLockHolder(paneID)
}

// ReleaseInputLockPublic is the exported version of ReleaseInputLock.
func (s *ptySession) ReleaseInputLockPublic(paneID, holderID string) {
	s.ReleaseInputLock(paneID, holderID)
}

// RingBufferSnapshotPublic is the exported version of RingBufferSnapshot.
func (s *ptySession) RingBufferSnapshotPublic() []byte {
	return s.RingBufferSnapshot()
}

// IsTUIModePublic is the exported version of IsTUIMode.
func (s *ptySession) IsTUIModePublic() bool {
	return s.IsTUIMode()
}

// inputLockedEvent builds a dedicated input_locked ChatEvent (ADR-0005,
// VAL-PTY-004). Unlike the old piggyback on the generic error event, this is
// a first-class event type so clients can branch on data.type ===
// 'input_locked' without sniffing error text. TTL is expressed in
// milliseconds for JSON transport.
func inputLockedEvent(holderID string, ttl time.Duration) ChatEvent {
	return ChatEvent{
		Type:   "input_locked",
		Holder: holderID,
		TTL:    int(ttl / time.Millisecond),
	}
}

// InputLockedEventPublic is the exported version of inputLockedEvent, used by
// httpapi to emit the dedicated input_locked event when a client is rejected
// by the per-pane soft lock (VAL-PTY-004).
func InputLockedEventPublic(paneID, holderID string) ChatEvent {
	_ = paneID // paneID reserved for future multi-pane support; today unused in the event body
	return inputLockedEvent(holderID, defaultInputLockTTL)
}

// detectTUIMode scans PTY output for alternate-screen enter/exit escape
// sequences and updates s.tuiMode accordingly (ADR-0005, VAL-PTY-001,
// VAL-PTY-002).
//
// It uses a regex-based scan instead of the old fixed-offset byte matching so
// sequences embedded anywhere in the output are detected. A carryover buffer
// (s.pending, under s.mu) handles sequences that arrive split across read
// boundaries: the previous read's trailing partial bytes are prepended to the
// current read, and any new trailing partial (from the last ESC to end of
// data) is stored back into s.pending for the next read. The buffer is capped
// at maxPendingBytes.
func (s *ptySession) detectTUIMode(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prepend any carryover from the previous read.
	combined := data
	if len(s.pending) > 0 {
		combined = make([]byte, len(s.pending)+len(data))
		copy(combined, s.pending)
		copy(combined[len(s.pending):], data)
		s.pending = nil
	}

	// Find all escape-sequence matches. The trailing mode byte ([hl])
	// determines enter (h) vs exit (l).
	matches := tuiModeRegex.FindAllSubmatchIndex(combined, -1)
	for _, m := range matches {
		// m[2]:m[3] is the capture group index range for ([hl]).
		modeByte := combined[m[2]]
		if modeByte == 'h' {
			s.tuiMode = true
		} else if modeByte == 'l' {
			s.tuiMode = false
		}
	}

	// Store any trailing partial sequence back into s.pending for the next
	// read. We look for the last ESC (0x1b) in the combined buffer: any bytes
	// from there to the end might be the start of a sequence that continues
	// in the next read.
	lastESC := -1
	for i := len(combined) - 1; i >= 0; i-- {
		if combined[i] == 0x1b {
			lastESC = i
			break
		}
	}
	if lastESC >= 0 {
		tail := combined[lastESC:]
		// Only keep it if it doesn't already contain a complete sequence
		// (i.e. the regex didn't match it). If the regex matched, the tail
		// is a complete sequence and we don't need to carry it over.
		if len(tail) <= maxPendingBytes && !tuiModeRegex.Match(tail) {
			s.pending = append(s.pending, tail...)
		} else if len(tail) > maxPendingBytes {
			// Cap the carryover: keep only the last maxPendingBytes bytes.
			s.pending = append(s.pending, tail[len(tail)-maxPendingBytes:]...)
		}
	}
}

// RingBufferSnapshot returns the current ring buffer content for replay
// (ADR-0005). Called when a new subscriber joins to send recent PTY output.
func (s *ptySession) RingBufferSnapshot() []byte {
	if s.ringBuffer == nil {
		return nil
	}
	return s.ringBuffer.Snapshot()
}

// IsTUIMode reports whether the PTY is currently in TUI mode (alternate
// screen buffer active). Used to decide between ring buffer replay and
// xterm.js snapshot (ADR-0005).
func (s *ptySession) IsTUIMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tuiMode
}
