package chat

import (
	"io"
	"os"
	"sync"
	"time"

	ptylib "github.com/aymanbagabas/go-pty"
	"github.com/google/uuid"
)

type Session struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	agent   Agent
	pty     ptylib.Pty
	cmd     *ptylib.Cmd
	output  chan string
	done    chan struct{}
	closeMu sync.Once
	mu      sync.Mutex
}

func newSession(agent Agent) (*Session, error) {
	s := &Session{
		ID:      uuid.NewString(),
		AgentID: agent.ID,
		agent:   agent,
		output:  make(chan string, 256),
		done:    make(chan struct{}),
	}
	if err := s.spawn(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Session) spawn() error {
	pseudo, err := ptylib.New()
	if err != nil {
		return err
	}

	cmd := pseudo.Command(s.agent.Command, s.agent.Args...)
	cmd.Dir = currentWorkingDirectory()
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		_ = pseudo.Close()
		return err
	}

	s.pty = pseudo
	s.cmd = cmd

	go s.readLoop()
	go func() {
		_ = cmd.Wait()
	}()

	return nil
}

func (s *Session) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			select {
			case s.output <- string(buf[:n]):
			default:
				// drop if channel full
			}
		}
		if err != nil {
			if err != io.EOF {
				select {
				case s.output <- "":
				default:
				}
			}
			close(s.done)
			return
		}
	}
}

func (s *Session) Write(input string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.pty.Write([]byte(input))
	return err
}

func (s *Session) Output() <-chan string {
	return s.output
}

func (s *Session) Done() <-chan struct{} {
	return s.done
}

func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Send Ctrl+C (ETX)
	_, err := s.pty.Write([]byte{0x03})
	return err
}

func (s *Session) Reset() (*Session, error) {
	s.kill()
	// Small delay to let process exit
	time.Sleep(100 * time.Millisecond)

	newSess, err := newSession(s.agent)
	if err != nil {
		return nil, err
	}
	return newSess, nil
}

func (s *Session) Close() error {
	s.kill()
	return nil
}

func (s *Session) kill() {
	s.closeMu.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		if s.pty != nil {
			_ = s.pty.Close()
		}
	})
}

func currentWorkingDirectory() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}
