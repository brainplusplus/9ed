package chat

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	ptylib "github.com/aymanbagabas/go-pty"
)

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
	tuiMode    bool
	// ADR-0005: collaborative soft lock for PTY input.
	inputLock  *inputLock
}

func newPTYSession(agent AgentDescriptor, workDir string) (*ptySession, error) {
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

	s := &ptySession{
		id:         uuid.NewString(),
		agentID:    agent.ID,
		pty:        pseudo,
		cmd:        cmd,
		events:     make(chan ChatEvent, 256),
		done:       make(chan struct{}),
		ringBuffer: newPtyRingBuffer(256 * 1024), // ADR-0005: 256KB ring buffer
		inputLock:  newInputLock(2 * time.Second), // ADR-0005: 2s soft lock TTL
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
type inputLock struct {
	mu        sync.Mutex
	holderID  string // clientId of the lock holder, empty = no lock
	expiresAt time.Time
	ttl       time.Duration
}

func newInputLock(ttl time.Duration) *inputLock {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	return &inputLock{ttl: ttl}
}

// acquireInputLock attempts to acquire or renew the soft lock.
// If holderID matches the current holder, the lock is renewed.
// If no lock is held or the lock has expired, it is acquired.
// Returns true if the caller now holds the lock.
func (s *ptySession) acquireInputLock(holderID string) bool {
	if s.inputLock == nil {
		return true // lock disabled
	}
	s.inputLock.mu.Lock()
	defer s.inputLock.mu.Unlock()

	now := time.Now()
	if s.inputLock.holderID == "" || now.After(s.inputLock.expiresAt) {
		// No active lock — acquire.
		s.inputLock.holderID = holderID
		s.inputLock.expiresAt = now.Add(s.inputLock.ttl)
		return true
	}
	if s.inputLock.holderID == holderID {
		// Same holder — renew.
		s.inputLock.expiresAt = now.Add(s.inputLock.ttl)
		return true
	}
	// Locked by someone else.
	return false
}

// InputLockHolder returns the clientId of the current lock holder, or empty
// if no lock is active (ADR-0005).
func (s *ptySession) InputLockHolder() string {
	if s.inputLock == nil {
		return ""
	}
	s.inputLock.mu.Lock()
	defer s.inputLock.mu.Unlock()
	if time.Now().After(s.inputLock.expiresAt) {
		return ""
	}
	return s.inputLock.holderID
}

// ReleaseInputLock releases the soft lock if the caller is the holder.
func (s *ptySession) ReleaseInputLock(holderID string) {
	if s.inputLock == nil {
		return
	}
	s.inputLock.mu.Lock()
	defer s.inputLock.mu.Unlock()
	if s.inputLock.holderID == holderID {
		s.inputLock.holderID = ""
		s.inputLock.expiresAt = time.Time{}
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
func (s *ptySession) AcquireInputLockPublic(holderID string) bool {
	return s.acquireInputLock(holderID)
}

// InputLockHolderPublic is the exported version of InputLockHolder.
func (s *ptySession) InputLockHolderPublic() string {
	return s.InputLockHolder()
}

// ReleaseInputLockPublic is the exported version of ReleaseInputLock.
func (s *ptySession) ReleaseInputLockPublic(holderID string) {
	s.ReleaseInputLock(holderID)
}

// RingBufferSnapshotPublic is the exported version of RingBufferSnapshot.
func (s *ptySession) RingBufferSnapshotPublic() []byte {
	return s.RingBufferSnapshot()
}

// IsTUIModePublic is the exported version of IsTUIMode.
func (s *ptySession) IsTUIModePublic() bool {
	return s.IsTUIMode()
}
// (ADR-0005). \033[?1049h = enter TUI, \033[?1049l = exit TUI.
func (s *ptySession) detectTUIMode(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(data)-6; i++ {
		if data[i] == 0x1b && data[i+1] == '[' {
			// Look for ?1049h or ?1049l
			if i+6 < len(data) && data[i+2] == '?' {
				seq := string(data[i+3 : i+7])
				if seq == "1049" || seq == "1047" || seq == "47h" || seq == "47l" {
					if i+7 < len(data) {
						mode := data[i+7]
						if mode == 'h' {
							s.tuiMode = true
						} else if mode == 'l' {
							s.tuiMode = false
						}
					}
				}
			}
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
