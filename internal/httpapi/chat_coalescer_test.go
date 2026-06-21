package httpapi

import (
	"sync"
	"testing"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
)

func TestCoalescerTextMerge(t *testing.T) {
	var mu sync.Mutex
	var flushed []chat.ChatEvent

	flushFn := func(events []chat.ChatEvent) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	}

	c := newChatStreamCoalescer(50*time.Millisecond, flushFn)
	defer c.stop()

	// Offer two text events that should merge.
	c.offer(chat.ChatEvent{Type: "text", Text: "hello "})
	c.offer(chat.ChatEvent{Type: "text", Text: "world"})

	// Wait for the coalesce window to fire.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("Expected 1 merged event, got %d", len(flushed))
	}
	if flushed[0].Text != "hello world" {
		t.Errorf("Expected 'hello world', got %q", flushed[0].Text)
	}
}

func TestCoalescerCriticalEventBypasses(t *testing.T) {
	var mu sync.Mutex
	var flushed []chat.ChatEvent

	flushFn := func(events []chat.ChatEvent) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	}

	c := newChatStreamCoalescer(50*time.Millisecond, flushFn)
	defer c.stop()

	// Offer a text event (coalescable).
	c.offer(chat.ChatEvent{Type: "text", Text: "hello"})

	// Offer a critical event (done) — should flush pending text immediately.
	// The critical event itself is returned to the caller (offer returns false),
	// it is NOT passed to flushFn. The caller handles it separately.
	c.offer(chat.ChatEvent{Type: "done", StopReason: "end_turn"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	// Should have 1 event: the text (flushed by critical event arrival).
	// The "done" event is NOT flushed by the coalescer — it's returned to caller.
	if len(flushed) != 1 {
		t.Fatalf("Expected 1 event (text flushed by critical), got %d", len(flushed))
	}
	if flushed[0].Type != "text" {
		t.Errorf("Expected first event 'text', got %q", flushed[0].Type)
	}
}

func TestCoalescerThinkingMerge(t *testing.T) {
	var mu sync.Mutex
	var flushed []chat.ChatEvent

	flushFn := func(events []chat.ChatEvent) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	}

	c := newChatStreamCoalescer(50*time.Millisecond, flushFn)
	defer c.stop()

	c.offer(chat.ChatEvent{Type: "thinking", Thinking: "step 1 "})
	c.offer(chat.ChatEvent{Type: "thinking", Thinking: "step 2"})

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("Expected 1 merged event, got %d", len(flushed))
	}
	if flushed[0].Thinking != "step 1 step 2" {
		t.Errorf("Expected 'step 1 step 2', got %q", flushed[0].Thinking)
	}
}

func TestCoalescerToolCallUpdateMerge(t *testing.T) {
	var mu sync.Mutex
	var flushed []chat.ChatEvent

	flushFn := func(events []chat.ChatEvent) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	}

	c := newChatStreamCoalescer(50*time.Millisecond, flushFn)
	defer c.stop()

	// Tool call updates with the same ToolCallID should merge.
	c.offer(chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc1", ToolStatus: "running"})
	c.offer(chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc1", ToolStatus: "completed", ToolContent: "result"})

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("Expected 1 merged event, got %d", len(flushed))
	}
	if flushed[0].ToolStatus != "completed" {
		t.Errorf("Expected 'completed', got %q", flushed[0].ToolStatus)
	}
	if flushed[0].ToolContent != "result" {
		t.Errorf("Expected 'result', got %q", flushed[0].ToolContent)
	}
}

func TestCoalescerDifferentToolCallIDsDoNotMerge(t *testing.T) {
	var mu sync.Mutex
	var flushed []chat.ChatEvent

	flushFn := func(events []chat.ChatEvent) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	}

	c := newChatStreamCoalescer(50*time.Millisecond, flushFn)
	defer c.stop()

	// Tool call updates with different ToolCallIDs should NOT merge.
	c.offer(chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc1", ToolStatus: "running"})
	c.offer(chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc2", ToolStatus: "running"})

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 2 {
		t.Fatalf("Expected 2 events (no merge), got %d", len(flushed))
	}
}

func TestCoalescerFlushImmediately(t *testing.T) {
	var mu sync.Mutex
	var flushed []chat.ChatEvent

	flushFn := func(events []chat.ChatEvent) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	}

	c := newChatStreamCoalescer(50*time.Millisecond, flushFn)

	c.offer(chat.ChatEvent{Type: "text", Text: "hello"})

	// Flush should send pending events immediately.
	c.flush()

	mu.Lock()
	if len(flushed) != 1 {
		t.Fatalf("Expected 1 event after flush, got %d", len(flushed))
	}
	if flushed[0].Text != "hello" {
		t.Errorf("Expected 'hello', got %q", flushed[0].Text)
	}
	mu.Unlock()

	c.stop()
}

func TestCoalescerStopFlushesPending(t *testing.T) {
	var mu sync.Mutex
	var flushed []chat.ChatEvent

	flushFn := func(events []chat.ChatEvent) {
		mu.Lock()
		flushed = append(flushed, events...)
		mu.Unlock()
	}

	c := newChatStreamCoalescer(10*time.Second, flushFn) // Long window

	c.offer(chat.ChatEvent{Type: "text", Text: "pending"})

	// Stop should flush all pending events.
	c.stop()

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("Expected 1 flushed event on stop, got %d", len(flushed))
	}
	if flushed[0].Text != "pending" {
		t.Errorf("Expected 'pending', got %q", flushed[0].Text)
	}
}

func TestCoalescerEmptyFlush(t *testing.T) {
	flushFn := func(events []chat.ChatEvent) {
		t.Error("flushFn should not be called on empty flush")
	}

	c := newChatStreamCoalescer(50*time.Millisecond, flushFn)
	c.flush() // Should be a no-op.
	c.stop()
}

func TestCanMergeEvents(t *testing.T) {
	tests := []struct {
		name string
		a    chat.ChatEvent
		b    chat.ChatEvent
		want bool
	}{
		{
			name: "text without toolCallID",
			a:    chat.ChatEvent{Type: "text", Text: "a"},
			b:    chat.ChatEvent{Type: "text", Text: "b"},
			want: true,
		},
		{
			name: "text with toolCallID",
			a:    chat.ChatEvent{Type: "text", Text: "a", ToolCallID: "tc1"},
			b:    chat.ChatEvent{Type: "text", Text: "b"},
			want: false,
		},
		{
			name: "tool_call_update same ID",
			a:    chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc1"},
			b:    chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc1"},
			want: true,
		},
		{
			name: "tool_call_update different ID",
			a:    chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc1"},
			b:    chat.ChatEvent{Type: "tool_call_update", ToolCallID: "tc2"},
			want: false,
		},
		{
			name: "different types (not called in practice — caller checks type equality first)",
			a:    chat.ChatEvent{Type: "text", Text: "a"},
			b:    chat.ChatEvent{Type: "thinking", Thinking: "b"},
			want: true, // canMergeEvents switches on a.Type ("text"), returns true since both have empty ToolCallID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canMergeEvents(&tt.a, &tt.b); got != tt.want {
				t.Errorf("canMergeEvents() = %v, want %v", got, tt.want)
			}
		})
	}
}
