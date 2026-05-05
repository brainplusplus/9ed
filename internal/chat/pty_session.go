package chat

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/google/uuid"

	ptylib "github.com/aymanbagabas/go-pty"
)

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
		id:      uuid.NewString(),
		agentID: agent.ID,
		pty:     pseudo,
		cmd:     cmd,
		events:  make(chan ChatEvent, 256),
		done:    make(chan struct{}),
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

func (s *ptySession) ID() string        { return s.id }
func (s *ptySession) AgentID() string    { return s.agentID }
func (s *ptySession) Mode() SessionMode  { return ModePTY }
func (s *ptySession) Events() <-chan ChatEvent { return s.events }
func (s *ptySession) Done() <-chan struct{}    { return s.done }
func (s *ptySession) RespondPermission(_ PermissionResponse) {}
func (s *ptySession) SetAutoApprove(_ bool) {}

func (s *ptySession) SetConfigOption(_ context.Context, _, _ string) error {
	return nil
}

func (s *ptySession) Send(_ context.Context, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.pty.Write([]byte(message + "\n"))
	return err
}

func (s *ptySession) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.pty.Write([]byte{0x03})
	return err
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
			text := StripANSI(string(buf[:n]))
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
