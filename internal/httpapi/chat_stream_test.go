package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
)

func TestChatStreamBroadcastsToSubscribersAndPersistsOnce(t *testing.T) {
	session := &fakeChatSession{
		events: make(chan chat.ChatEvent, 4),
		done:   make(chan struct{}),
	}
	var persisted []chat.ChatEvent
	stream := newChatStream("session-1", session, func(evt chat.ChatEvent) {
		persisted = append(persisted, evt)
	}, nil)
	stream.Start()

	subA := stream.Subscribe()
	defer stream.Unsubscribe(subA)
	subB := stream.Subscribe()
	defer stream.Unsubscribe(subB)

	want := chat.ChatEvent{Type: "text", Text: "hello"}
	session.events <- want

	if got := readChatEvent(t, subA.C); got.Type != want.Type || got.Text != want.Text {
		t.Fatalf("subscriber A got %#v, want %#v", got, want)
	}
	if got := readChatEvent(t, subB.C); got.Type != want.Type || got.Text != want.Text {
		t.Fatalf("subscriber B got %#v, want %#v", got, want)
	}
	if len(persisted) != 1 || persisted[0].Text != want.Text {
		t.Fatalf("persisted events = %#v, want one %#v", persisted, want)
	}
}

func readChatEvent(t *testing.T, ch <-chan chat.ChatEvent) chat.ChatEvent {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for chat event")
		return chat.ChatEvent{}
	}
}

type fakeChatSession struct {
	events chan chat.ChatEvent
	done   chan struct{}
}

func (s *fakeChatSession) ID() string                                            { return "session-1" }
func (s *fakeChatSession) AgentID() string                                       { return "opencode" }
func (s *fakeChatSession) WorkDir() string                                       { return "" }
func (s *fakeChatSession) Mode() chat.SessionMode                                { return chat.ModeACP }
func (s *fakeChatSession) Events() <-chan chat.ChatEvent                         { return s.events }
func (s *fakeChatSession) Done() <-chan struct{}                                 { return s.done }
func (s *fakeChatSession) Send(context.Context, string) error                    { return nil }
func (s *fakeChatSession) Cancel() error                                         { return nil }
func (s *fakeChatSession) Close() error                                          { close(s.done); return nil }
func (s *fakeChatSession) SetConfigOption(context.Context, string, string) error { return nil }
func (s *fakeChatSession) ACPSessionID() string                                  { return "" }
func (s *fakeChatSession) IsResumed() bool                                       { return false }
func (s *fakeChatSession) RespondPermission(chat.PermissionResponse)             {}
func (s *fakeChatSession) SetAutoApprove(bool)                                   {}
