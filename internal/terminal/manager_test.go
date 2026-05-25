package terminal

import (
	"errors"
	"io"
	"testing"
)

func TestManagerCreateAndRemoveSession(t *testing.T) {
	manager := NewManager(func(profile ShellProfile) (PtySession, error) {
		return &fakeSession{id: "session-1", profile: profile}, nil
	})

	session, err := manager.Create(ShellProfile{ID: "bash", Label: "Bash", Command: "bash"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	got, ok := manager.Get(session.ID)
	if !ok {
		t.Fatal("expected session to be stored")
	}
	if got.ID != session.ID {
		t.Fatalf("expected session %q, got %q", session.ID, got.ID)
	}

	if err := manager.Remove(session.ID); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	if _, ok := manager.Get(session.ID); ok {
		t.Fatal("expected session to be removed")
	}
}

func TestManagerReturnsSpawnError(t *testing.T) {
	wantErr := errors.New("spawn failed")
	manager := NewManager(func(profile ShellProfile) (PtySession, error) {
		return nil, wantErr
	})

	_, err := manager.Create(ShellProfile{ID: "bash", Label: "Bash", Command: "bash"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
}

type fakeSession struct {
	id         string
	profile    ShellProfile
	closed     chan struct{}
	closedFlag bool
}

func (f *fakeSession) ID() string            { return f.id }
func (f *fakeSession) Profile() ShellProfile { return f.profile }
func (f *fakeSession) Read(p []byte) (int, error) {
	if f.closed == nil {
		f.closed = make(chan struct{})
	}
	<-f.closed
	return 0, io.EOF
}
func (f *fakeSession) Write(p []byte) (int, error)           { return len(p), nil }
func (f *fakeSession) Resize(cols uint16, rows uint16) error { return nil }
func (f *fakeSession) Close() error {
	if f.closed == nil {
		f.closed = make(chan struct{})
	}
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	f.closedFlag = true
	return nil
}
