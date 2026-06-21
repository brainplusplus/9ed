package httpapi

import (
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
)

// chatStreamCoalescer batches bulk events (text, thinking, tool_call_update)
// within a configurable window (default 60ms) before they are persisted and
// fanned out to subscribers (ADR-0006).
//
// Critical events (permission_request, error, done, etc.) bypass the coalescer
// and are flushed immediately.
type chatStreamCoalescer struct {
	mu       sync.Mutex
	window   time.Duration
	pending  []chat.ChatEvent
	timer    *time.Timer
	flushFn  func([]chat.ChatEvent)
	flushing bool
}

// coalescableEventTypes are event types whose payloads can be merged within a
// coalesce window. Adjacent events of the same type are concatenated/collapsed.
var coalescableEventTypes = map[string]bool{
	"text":            true,
	"thinking":        true,
	"tool_call_update": true,
}

func newChatStreamCoalescer(window time.Duration, flushFn func([]chat.ChatEvent)) *chatStreamCoalescer {
	return &chatStreamCoalescer{
		window:  window,
		flushFn: flushFn,
	}
}

// offer adds an event to the coalescer. If the event is critical (not
// coalescable), pending events are flushed first, then the critical event is
// flushed immediately. Returns true if the event was accepted by the coalescer
// (deferred), false if it was already flushed (critical event).
func (c *chatStreamCoalescer) offer(evt chat.ChatEvent) bool {
	if !coalescableEventTypes[evt.Type] {
		// Critical event: flush pending first, then pass through immediately.
		c.flush()
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to merge with the last pending event of the same type.
	if len(c.pending) > 0 {
		last := &c.pending[len(c.pending)-1]
		if last.Type == evt.Type && canMergeEvents(last, &evt) {
			mergeEvents(last, &evt)
			c.armTimerLocked()
			return true
		}
	}

	c.pending = append(c.pending, evt)
	c.armTimerLocked()
	return true
}

// flush sends all pending events to the flush function and stops the timer.
func (c *chatStreamCoalescer) flush() {
	c.mu.Lock()
	if len(c.pending) == 0 {
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		c.mu.Unlock()
		return
	}
	pending := c.pending
	c.pending = nil
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()

	if c.flushFn != nil {
		c.flushFn(pending)
	}
}

// stop cancels any pending timer and flushes remaining events. Called when the
// stream is closing.
func (c *chatStreamCoalescer) stop() {
	c.flush()
}

func (c *chatStreamCoalescer) armTimerLocked() {
	if c.timer != nil {
		c.timer.Reset(c.window)
		return
	}
	c.timer = time.AfterFunc(c.window, func() {
		c.flush()
	})
}

// canMergeEvents reports whether two adjacent events of the same type can be
// merged into one. Text/thinking events merge if they don't carry tool call
// metadata. tool_call_update events merge only if they reference the same
// tool call ID.
func canMergeEvents(a, b *chat.ChatEvent) bool {
	switch a.Type {
	case "text":
		// Text events merge if neither has tool call metadata (plain assistant text).
		return a.ToolCallID == "" && b.ToolCallID == ""
	case "thinking":
		return a.ToolCallID == "" && b.ToolCallID == ""
	case "tool_call_update":
		// Merge updates for the same tool call (collapse pending→running→completed).
		return a.ToolCallID != "" && a.ToolCallID == b.ToolCallID
	default:
		return false
	}
}

// mergeEvents merges event b into event a (a is the accumulator).
func mergeEvents(a, b *chat.ChatEvent) {
	switch a.Type {
	case "text":
		a.Text += b.Text
	case "thinking":
		a.Thinking += b.Thinking
	case "tool_call_update":
		// Collapse: take the latest status/content/raw input.
		if b.ToolStatus != "" {
			a.ToolStatus = b.ToolStatus
		}
		if b.ToolContent != "" {
			a.ToolContent = b.ToolContent
		}
		if b.ToolRawInput != "" {
			a.ToolRawInput = b.ToolRawInput
		}
		if b.ToolTitle != "" {
			a.ToolTitle = b.ToolTitle
		}
	}
}
