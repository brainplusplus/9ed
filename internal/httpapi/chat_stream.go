package httpapi

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/debug"
)

type chatEventPersister func(chat.ChatEvent)

type chatStreamRegistry struct {
	mu             sync.Mutex
	streams        map[string]*chatStream
	latestID       string
	coalesceWindow time.Duration
}

func newChatStreamRegistry() *chatStreamRegistry {
	return &chatStreamRegistry{streams: make(map[string]*chatStream)}
}

func (r *chatStreamRegistry) SetCoalesceWindow(d time.Duration) {
	r.coalesceWindow = d
}

func (r *chatStreamRegistry) GetOrCreate(sessionID string, session chat.ChatSession, persist chatEventPersister) *chatStream {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stream, ok := r.streams[sessionID]; ok {
		r.latestID = sessionID
		debug.Printf("[chat/stream] reuse session=%s", sessionID)
		return stream
	}

	var stream *chatStream
	stream = newChatStream(sessionID, session, persist, func() {
		r.mu.Lock()
		if r.streams[sessionID] == stream {
			delete(r.streams, sessionID)
		}
		r.mu.Unlock()
	}, r.coalesceWindow)
	r.streams[sessionID] = stream
	r.latestID = sessionID
	debug.Printf("[chat/stream] create session=%s agent=%s mode=%s record=%s", sessionID, session.AgentID(), session.Mode(), session.ACPSessionID())
	stream.Start()
	return stream
}

func (r *chatStreamRegistry) Touch(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.streams[sessionID]; !ok {
		return false
	}
	r.latestID = sessionID
	return true
}

// ReplaceSession invalidates any existing stream for sessionID and creates a
// fresh one bound to the new session. This is needed because the run()
// goroutine of the old stream listens on the old session's Events()/Done()
// channels. When a session is resumed (e.g. after toggling MCP options) and the
// ACP agent returns the same session ID, GetOrCreate would wrongly reuse the
// stale stream. ReplaceSession ensures the stream is rebound to the new
// session's channels.
func (r *chatStreamRegistry) ReplaceSession(sessionID string, session chat.ChatSession, persist chatEventPersister) *chatStream {
	r.mu.Lock()
	if old, ok := r.streams[sessionID]; ok {
		delete(r.streams, sessionID)
		r.mu.Unlock()
		old.invalidate()
		r.mu.Lock()
	}
	r.mu.Unlock()
	return r.GetOrCreate(sessionID, session, persist)
}

// Invalidate closes and removes the stream for sessionID if it exists,
// without emitting a session_closed event. Used when a session is being
// replaced by one with a different ID.
func (r *chatStreamRegistry) Invalidate(sessionID string) {
	r.mu.Lock()
	old, ok := r.streams[sessionID]
	if ok {
		delete(r.streams, sessionID)
	}
	r.mu.Unlock()
	if ok {
		old.invalidate()
	}
}

func (r *chatStreamRegistry) LatestID() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latestID != "" {
		if _, ok := r.streams[r.latestID]; ok {
			return r.latestID, true
		}
	}
	for id := range r.streams {
		return id, true
	}
	return "", false
}

type chatSubscriber struct {
	C        chan chat.ChatEvent // primary channel (backward compat, used as bulk)
	priority chan chat.ChatEvent // critical events (never drop) — ADR-0003
	// ADR-0005: clientID associates this subscriber with a logical client
	// (VAL-PTY-006) for routing tui_snapshot_request to the primary client.
	clientID string
}

// criticalEventTypes are event types that must never be dropped (ADR-0003).
// They go to the priority channel; if that channel is full, the agent turn is
// cancelled to apply backpressure.
var criticalEventTypes = map[string]bool{
	"permission_request": true,
	"error":               true,
	"done":                true,
	"terminal_execute":    true,
	"session_resumed":     true,
	"usage_update":        true,
}

func newChatSubscriber() *chatSubscriber {
	return &chatSubscriber{
		C:        make(chan chat.ChatEvent, 256),
		priority: make(chan chat.ChatEvent, 64),
	}
}

type chatStream struct {
	sessionID        string
	session          chat.ChatSession
	persist          chatEventPersister
	onDone           func()
	subscribers      map[*chatSubscriber]struct{}
	subscribersByClient map[string]*chatSubscriber // ADR-0005: clientID -> subscriber (VAL-PTY-006)
	turnDoneEmitted  bool
	turnObservations []chat.ChatEvent
	toolFallbackSeq  int
	toolFallback     *time.Timer
	toolFallbackText string
	turnRecoverySeq  int
	turnRecovery     *time.Timer
	turnRecoveryTool string
	turnWatchdogSeq  int
	turnWatchdog     *time.Timer
	coalescer        *chatStreamCoalescer
	mu               sync.Mutex
	done             chan struct{}
	closeOnce        sync.Once
	// ADR-0005: TUI snapshot coordinator (VAL-PTY-006). Tracks the
	// in-flight tui_snapshot_request so responses can be de-duplicated
	// and a timeout can fall back to ring buffer replay.
	snapshot *tuiSnapshotState
}

var interactiveToolDoneTimeout = 3500 * time.Millisecond
var interactiveToolUnsureTimeout = 9 * time.Second
var interactiveTurnInactivityTimeout = 45 * time.Second

func newChatStream(sessionID string, session chat.ChatSession, persist chatEventPersister, onDone func(), coalesceWindow time.Duration) *chatStream {
	s := &chatStream{
		sessionID:   sessionID,
		session:     session,
		persist:     persist,
		onDone:      onDone,
		subscribers: make(map[*chatSubscriber]struct{}),
		subscribersByClient: make(map[string]*chatSubscriber),
		done:        make(chan struct{}),
	}
	if coalesceWindow > 0 {
		s.coalescer = newChatStreamCoalescer(coalesceWindow, func(batch []chat.ChatEvent) {
			for _, evt := range batch {
				s.publishDirect(evt)
			}
		})
	}
	return s
}

func (s *chatStream) Start() {
	go s.run()
}

// invalidate signals the run() goroutine to exit immediately without emitting
// a session_closed done event. Subscribers' channels are closed so any pending
// WebSocket handlers exit promptly. Called by ReplaceSession when a new session
// replaces an existing one with the same ID.
func (s *chatStream) invalidate() {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return
	default:
	}
	if s.coalescer != nil {
		s.coalescer.stop()
	}
	s.cancelToolFallbackLocked()
	s.cancelTurnRecoveryLocked()
	s.cancelTurnWatchdogLocked()
	s.closeOnce.Do(func() {
		close(s.done)
	})
	for sub := range s.subscribers {
		delete(s.subscribers, sub)
		if sub.clientID != "" { delete(s.subscribersByClient, sub.clientID) }
		close(sub.C)
		close(sub.priority)
	}
	s.mu.Unlock()
	debug.Printf("[chat/stream] invalidated session=%s", s.sessionID)
}

func (s *chatStream) StartTurn() {
	s.mu.Lock()
	s.turnDoneEmitted = false
	s.turnObservations = nil
	s.cancelToolFallbackLocked()
	s.cancelTurnRecoveryLocked()
	s.cancelTurnWatchdogLocked()
	s.mu.Unlock()
	s.armTurnWatchdog(interactiveTurnInactivityTimeout)
}

func (s *chatStream) Subscribe() *chatSubscriber {
	sub := newChatSubscriber()
	s.mu.Lock()
	select {
	case <-s.done:
		close(sub.C)
	default:
		s.subscribers[sub] = struct{}{}
		debug.Printf("[chat/stream] subscribe session=%s subscribers=%d", s.sessionID, len(s.subscribers))
	}
	s.mu.Unlock()
	return sub
}

func (s *chatStream) Unsubscribe(sub *chatSubscriber) {
	s.mu.Lock()
	if _, ok := s.subscribers[sub]; ok {
		delete(s.subscribers, sub)
		if sub.clientID != "" {
			delete(s.subscribersByClient, sub.clientID)
		}
		close(sub.C)
		close(sub.priority)
		debug.Printf("[chat/stream] unsubscribe session=%s subscribers=%d", s.sessionID, len(s.subscribers))
	}
	s.mu.Unlock()
}

func (s *chatStream) run() {
	defer func() {
		// ADR-0006: flush any pending coalesced events before closing.
		if s.coalescer != nil {
			s.coalescer.stop()
		}
		s.mu.Lock()
		s.cancelToolFallbackLocked()
		s.cancelTurnRecoveryLocked()
		s.cancelTurnWatchdogLocked()
		subscriberCount := len(s.subscribers)
		s.closeOnce.Do(func() {
			close(s.done)
		})
		for sub := range s.subscribers {
			delete(s.subscribers, sub)
			close(sub.C)
			close(sub.priority)
		}
		s.mu.Unlock()
		debug.Printf("[chat/stream] closed session=%s subscribersClosed=%d", s.sessionID, subscriberCount)
		if s.onDone != nil {
			s.onDone()
		}
	}()

	for {
		select {
		case <-s.done:
			debug.Printf("[chat/stream] invalidated session=%s", s.sessionID)
			return
		case <-s.session.Done():
			debug.Printf("[chat/stream] session done session=%s", s.sessionID)
			s.mu.Lock()
			doneEmitted := s.turnDoneEmitted
			s.mu.Unlock()
			if !doneEmitted {
				s.publish(chat.ChatEvent{Type: "done", StopReason: "session_closed"})
			}
			return
		case evt, ok := <-s.session.Events():
			if !ok {
				debug.Printf("[chat/stream] events closed session=%s", s.sessionID)
				return
			}
			s.publish(evt)
		}
	}
}

// publish routes an event through the coalescer if one is configured, or
// publishes directly if coalescing is disabled (ADR-0006).
func (s *chatStream) publish(evt chat.ChatEvent) {
	if s.coalescer != nil {
		if s.coalescer.offer(evt) {
			return
		}
	}
	s.publishDirect(evt)
}

func (s *chatStream) publishDirect(evt chat.ChatEvent) {
	if s.shouldIgnoreLateTurnEvent(evt) {
		debug.Printf("[chat/stream] drop late turn event session=%s type=%s tool=%s", s.sessionID, evt.Type, evt.ToolTitle)
		return
	}
	if evt.Type == "text" || evt.Type == "done" || evt.Type == "error" || evt.Type == "usage_update" {
		debug.Printf("[chat/stream] publish session=%s type=%s textChars=%d stop=%q err=%q", s.sessionID, evt.Type, len(evt.Text), evt.StopReason, evt.Error)
	}
	s.handleToolFallbackTrigger(evt)
	s.handleTurnRecoveryTrigger(evt)
	s.handleTurnWatchdogTrigger(evt)
	if s.persist != nil {
		s.persist(evt)
	}

	s.mu.Lock()
	if evt.Type == "done" || evt.Type == "error" {
		s.turnObservations = nil
		s.cancelToolFallbackLocked()
		s.cancelTurnRecoveryLocked()
		s.cancelTurnWatchdogLocked()
		s.turnDoneEmitted = true
	}
	defer s.mu.Unlock()
	isCritical := criticalEventTypes[evt.Type]

	// ADR-0003: track whether ANY priority overflow occurred during this
	// fan-out. Backpressure-to-agent triggers on any priority overflow (not
	// gated on len(subscribers)==0). Critical events that overflow are
	// buffered (already persisted above) and retried for delivery after the
	// agent is cancelled.
	var backpressureNeeded bool
	var bufferedCritical []chat.ChatEvent
	var bulkDropped bool

	for sub := range s.subscribers {
		if isCritical {
			// ADR-0003: critical events go to priority channel (never drop).
			select {
			case sub.priority <- evt:
			default:
				// Priority channel full — do NOT drop the subscriber. Set the
				// backpressure flag, buffer the critical event for retry after
				// Cancel, and keep the subscriber alive.
				backpressureNeeded = true
				bufferedCritical = append(bufferedCritical, evt)
				debug.Printf("[chat/stream] priority overflow session=%s subscriber kept, backpressure flagged", s.sessionID)
			}
		} else {
			// ADR-0003: bulk events go to main channel (drop oldest if full).
			// The subscriber is NEVER dropped on bulk overflow — drop-oldest
			// only; if still full after drop, drop the new event (subscriber
			// stays alive). The client detects the seq gap via the seq_gap
			// signal below and re-fetches missing events (ADR-0002 catch-up).
			select {
			case sub.C <- evt:
			default:
				// Channel full — drop oldest to make room for newest.
				select {
				case <-sub.C:
				default:
				}
				select {
				case sub.C <- evt:
					// Made room by dropping oldest — signal the client that a
					// bulk event was dropped so it can re-fetch (ADR-0003).
					bulkDropped = true
				default:
					// Still full after dropping one — drop the new event. The
					// subscriber stays alive. The client will detect the gap
					// via the seq_gap signal and re-fetch (ADR-0002 catch-up).
					bulkDropped = true
					debug.Printf("[chat/stream] bulk overflow session=%s new event dropped, subscriber kept", s.sessionID)
				}
			}
		}
	}

	// ADR-0003: emit a seq_gap signal to subscribers when a bulk event was
	// dropped, so the client knows to re-fetch missing events via ADR-0002
	// catch-up. The signal is transient (not persisted) and best-effort: it
	// goes to the priority channel non-blocking; if the priority channel is
	// also full the client will still detect the gap on its next fetch.
	if bulkDropped {
		s.deliverSeqGapLocked()
	}

	// ADR-0003: if any priority overflow occurred, apply backpressure to the
	// agent. Cancel the agent turn, retry delivery of buffered critical
	// events, and emit a done event with stopReason=client_backpressure so
	// the client can re-prompt and re-fetch the timeline tail. This triggers
	// on ANY priority overflow (not gated on len(subscribers)==0).
	if backpressureNeeded && evt.Type != "done" && evt.Type != "error" {
		// Snapshot the buffered critical events so the goroutine can safely
		// reference them after the lock is released.
		retryEvents := bufferedCritical
		go func() {
			_ = s.session.Cancel()
			// Retry delivery of buffered critical events to subscribers. These
			// were already persisted above, so this path does NOT re-persist.
			// If priority channels are still full, the events are dropped here
			// (they are persisted) — the client re-fetches them via catch-up.
			s.redeliverCritical(retryEvents)
			// Emit the terminal done event. It is delivered with drop-oldest
			// semantics on the priority channel so it always gets through to
			// the (slow) client, even if the channel is full. Dropped pending
			// priority events are persisted and re-fetched via ADR-0002.
			s.publishBackpressureDone()
		}()
	}
}

// deliverSeqGapLocked emits a transient seq_gap signal to every subscriber's
// priority channel (non-blocking). The caller must hold s.mu. The signal tells
// the client that a bulk event was dropped due to overflow so it can re-fetch
// the missing events via ADR-0002 catch-up. It is NOT persisted.
func (s *chatStream) deliverSeqGapLocked() {
	signal := chat.ChatEvent{Type: "seq_gap"}
	for sub := range s.subscribers {
		select {
		case sub.priority <- signal:
		default:
			// Priority channel full — skip; client will still detect the gap
			// on its next fetch_timeline call.
		}
	}
}

// redeliverCritical attempts to re-deliver buffered critical events to all
// subscribers' priority channels (non-blocking). It does NOT persist (the
// events were already persisted in the original publishDirect call). If a
// priority channel is still full, the event is skipped — the client will
// detect the gap via the subsequent client_backpressure done event and
// re-fetch via ADR-0002 catch-up.
func (s *chatStream) redeliverCritical(events []chat.ChatEvent) {
	if len(events) == 0 {
		return
	}
	s.mu.Lock()
	for _, evt := range events {
		for sub := range s.subscribers {
			select {
			case sub.priority <- evt:
			default:
				// Still full — client will re-fetch via catch-up.
			}
		}
	}
	s.mu.Unlock()
}

// publishBackpressureDone emits the terminal done event with
// stopReason=client_backpressure. It persists the event (so it is part of the
// timeline the client re-fetches) and delivers it to every subscriber's
// priority channel with drop-oldest semantics: if the priority channel is
// full, the oldest pending priority event is dropped to make room. This
// guarantees the terminal signal always reaches the slow client. Dropped
// pending events are persisted and re-fetched via ADR-0002 catch-up.
func (s *chatStream) publishBackpressureDone() {
	evt := chat.ChatEvent{Type: "done", StopReason: "client_backpressure"}
	if s.persist != nil {
		s.persist(evt)
	}
	s.mu.Lock()
	if s.turnDoneEmitted {
		// A terminal event was already emitted for this turn; do not emit a
		// second one. The client already received (or will re-fetch) the
		// prior terminal event.
		s.mu.Unlock()
		return
	}
	s.turnDoneEmitted = true
	for sub := range s.subscribers {
		select {
		case sub.priority <- evt:
		default:
			// Priority channel full — drop the oldest pending priority event
			// to make room for the terminal done. The dropped event is
			// persisted and the client re-fetches it via ADR-0002 catch-up.
			select {
			case <-sub.priority:
			default:
			}
			select {
			case sub.priority <- evt:
			default:
				// Extremely unlikely: still full after dropping one. Drop the
				// event itself — the client will re-fetch via catch-up since
				// it is persisted.
			}
		}
	}
	s.mu.Unlock()
}

func (s *chatStream) handleToolFallbackTrigger(evt chat.ChatEvent) {
	if shouldScheduleToolFallback(evt) {
		s.scheduleToolFallback(toolFallbackText(evt))
		return
	}
	if shouldCancelToolFallback(evt) {
		s.mu.Lock()
		s.cancelToolFallbackLocked()
		s.mu.Unlock()
	}
}

func shouldScheduleToolFallback(evt chat.ChatEvent) bool {
	// Keep tool results inside the agent loop instead of synthesizing assistant
	// replies from MCP observations. The UI can render the tool steps directly,
	// which feels closer to Codex/Claude-style agents and avoids "too fast" fake
	// conclusions when local tools finish instantly.
	return false
}

func shouldScheduleLegacyToolFallback(evt chat.ChatEvent) bool {
	return evt.Type == "tool_call_update" &&
		strings.EqualFold(evt.ToolStatus, "completed") &&
		isInteractiveMCPTool(evt.ToolTitle) &&
		(strings.TrimSpace(evt.ToolContent) != "" || strings.TrimSpace(evt.ToolRawInput) != "") &&
		!terminalObservationStillRunning(evt)
}

func terminalObservationStillRunning(evt chat.ChatEvent) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(evt.ToolTitle)), "active_terminal_") &&
		strings.Contains(strings.ToLower(evt.ToolContent), "terminal status: still running")
}

func shouldCancelToolFallback(evt chat.ChatEvent) bool {
	switch evt.Type {
	case "text", "tool_call", "plan", "done", "error", "terminal_execute":
		return true
	case "tool_call_update":
		return !strings.EqualFold(evt.ToolStatus, "completed")
	default:
		return false
	}
}

func isInteractiveMCPTool(title string) bool {
	value := strings.TrimSpace(strings.ToLower(title))
	return value == "active_terminal_run" ||
		value == "active_terminal_start" ||
		value == "active_terminal_read" ||
		strings.HasPrefix(value, "9ed_browser_") ||
		strings.HasPrefix(value, "9ed-active-browser_") ||
		strings.HasPrefix(value, "active_browser_") ||
		strings.HasPrefix(value, "browser_")
}

func isBrowserMCPTool(title string) bool {
	value := strings.TrimSpace(strings.ToLower(title))
	return strings.HasPrefix(value, "9ed_browser_") ||
		strings.HasPrefix(value, "9ed-active-browser_") ||
		strings.HasPrefix(value, "active_browser_") ||
		strings.HasPrefix(value, "browser_")
}

func (s *chatStream) handleTurnRecoveryTrigger(evt chat.ChatEvent) {
	if shouldScheduleTurnRecovery(evt) {
		s.recordTurnObservationLocked(evt)
		s.scheduleTurnRecoveryLocked(evt.ToolTitle, turnRecoveryDelay(evt))
		return
	}
	if shouldCancelTurnRecovery(evt) {
		s.mu.Lock()
		s.cancelTurnRecoveryLocked()
		s.mu.Unlock()
	}
}

func (s *chatStream) handleTurnWatchdogTrigger(evt chat.ChatEvent) {
	if shouldRefreshTurnWatchdog(evt) {
		s.armTurnWatchdog(interactiveTurnInactivityTimeout)
		return
	}
	if shouldCancelTurnWatchdog(evt) {
		s.mu.Lock()
		s.cancelTurnWatchdogLocked()
		s.mu.Unlock()
	}
}

func shouldRefreshTurnWatchdog(evt chat.ChatEvent) bool {
	switch evt.Type {
	case "done", "error":
		return false
	default:
		return true
	}
}

func shouldCancelTurnWatchdog(evt chat.ChatEvent) bool {
	switch evt.Type {
	case "done", "error":
		return true
	default:
		return false
	}
}

func shouldScheduleTurnRecovery(evt chat.ChatEvent) bool {
	return evt.Type == "tool_call_update" &&
		(strings.EqualFold(evt.ToolStatus, "completed") || strings.EqualFold(evt.ToolStatus, "failed")) &&
		isInteractiveMCPTool(evt.ToolTitle) &&
		!terminalObservationStillRunning(evt)
}

func shouldCancelTurnRecovery(evt chat.ChatEvent) bool {
	switch evt.Type {
	case "text", "thinking", "tool_call", "plan", "done", "error", "terminal_execute", "permission_request":
		return true
	case "tool_call_update":
		if !isInteractiveMCPTool(evt.ToolTitle) {
			return true
		}
		return !strings.EqualFold(evt.ToolStatus, "completed") && !strings.EqualFold(evt.ToolStatus, "failed")
	default:
		return false
	}
}

func (s *chatStream) scheduleTurnRecoveryLocked(title string, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelTurnRecoveryLocked()
	s.turnRecoveryTool = title
	s.turnRecoverySeq++
	seq := s.turnRecoverySeq
	s.turnRecovery = time.AfterFunc(timeout, func() {
		s.mu.Lock()
		if seq != s.turnRecoverySeq || s.turnDoneEmitted {
			s.mu.Unlock()
			return
		}
		tool := s.turnRecoveryTool
		observations := append([]chat.ChatEvent(nil), s.turnObservations...)
		s.turnRecovery = nil
		s.turnRecoveryTool = ""
		s.mu.Unlock()

		debug.Printf("[chat/stream] recovering stalled turn after completed tool session=%s tool=%s", s.sessionID, tool)
		text := synthesizeTurnRecoveryText(observations)
		if text == "" {
			debug.Printf("[chat/stream] recovery skipped because latest tool still needs agent continuation session=%s tool=%s", s.sessionID, tool)
			return
		}
		_ = s.session.Cancel()
		s.publish(chat.ChatEvent{Type: "text", Text: text})
		s.publish(chat.ChatEvent{Type: "done", StopReason: "tool_completion_timeout_stream"})
	})
}

func (s *chatStream) cancelTurnRecoveryLocked() {
	s.turnRecoverySeq++
	s.turnRecoveryTool = ""
	if s.turnRecovery != nil {
		s.turnRecovery.Stop()
		s.turnRecovery = nil
	}
}

func (s *chatStream) armTurnWatchdog(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnDoneEmitted {
		return
	}
	s.cancelTurnWatchdogLocked()
	s.turnWatchdogSeq++
	seq := s.turnWatchdogSeq
	s.turnWatchdog = time.AfterFunc(timeout, func() {
		s.mu.Lock()
		if seq != s.turnWatchdogSeq || s.turnDoneEmitted {
			s.mu.Unlock()
			return
		}
		observations := append([]chat.ChatEvent(nil), s.turnObservations...)
		s.turnWatchdog = nil
		s.mu.Unlock()

		debug.Printf("[chat/stream] inactivity watchdog fired session=%s timeout=%s", s.sessionID, timeout)
		_ = s.session.Cancel()
		if text := synthesizeTurnRecoveryText(observations); text != "" {
			s.publish(chat.ChatEvent{Type: "text", Text: text})
		}
		s.publish(chat.ChatEvent{Type: "done", StopReason: "turn_inactivity_timeout_stream"})
	})
}

func (s *chatStream) cancelTurnWatchdogLocked() {
	s.turnWatchdogSeq++
	if s.turnWatchdog != nil {
		s.turnWatchdog.Stop()
		s.turnWatchdog = nil
	}
}

func turnRecoveryDelay(evt chat.ChatEvent) time.Duration {
	title := strings.TrimSpace(strings.ToLower(evt.ToolTitle))
	if strings.HasPrefix(title, "active_terminal_") {
		if terminalObservationSufficient(evt) {
			return interactiveToolDoneTimeout
		}
		return interactiveToolUnsureTimeout
	}
	if isBrowserMCPTool(title) {
		if browserObservationSufficient(evt) {
			return interactiveToolDoneTimeout
		}
		return interactiveToolUnsureTimeout
	}
	return interactiveToolDoneTimeout
}

func terminalObservationSufficient(evt chat.ChatEvent) bool {
	if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(evt.ToolTitle)), "active_terminal_") {
		return false
	}
	return strings.Contains(strings.ToLower(evt.ToolContent), "decision: sufficient_to_answer=true")
}

func browserObservationSufficient(evt chat.ChatEvent) bool {
	if !isBrowserMCPTool(evt.ToolTitle) {
		return false
	}
	return strings.Contains(strings.ToLower(evt.ToolContent), "decision: sufficient_to_answer=true")
}

func (s *chatStream) shouldIgnoreLateTurnEvent(evt chat.ChatEvent) bool {
	s.mu.Lock()
	done := s.turnDoneEmitted
	s.mu.Unlock()
	if !done {
		return false
	}
	switch evt.Type {
	case "tool_call", "tool_call_update", "text", "thinking", "diff", "plan", "permission_request", "terminal_execute":
		return true
	default:
		return false
	}
}

func (s *chatStream) recordTurnObservationLocked(evt chat.ChatEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.turnObservations {
		if s.turnObservations[i].ToolCallID != "" && s.turnObservations[i].ToolCallID == evt.ToolCallID {
			s.turnObservations[i] = evt
			return
		}
	}
	s.turnObservations = append(s.turnObservations, evt)
	const maxTurnObservations = 8
	if len(s.turnObservations) > maxTurnObservations {
		s.turnObservations = append([]chat.ChatEvent(nil), s.turnObservations[len(s.turnObservations)-maxTurnObservations:]...)
	}
}

func (s *chatStream) scheduleToolFallback(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelToolFallbackLocked()
	s.toolFallbackText = text
	s.toolFallbackSeq++
	seq := s.toolFallbackSeq
	s.toolFallback = time.AfterFunc(8*time.Second, func() {
		s.mu.Lock()
		if seq != s.toolFallbackSeq || s.turnDoneEmitted || s.toolFallbackText == "" {
			s.mu.Unlock()
			return
		}
		fallback := s.toolFallbackText
		s.toolFallbackText = ""
		s.toolFallback = nil
		s.mu.Unlock()
		debug.Printf("[chat/stream] tool fallback session=%s", s.sessionID)
		s.publish(chat.ChatEvent{Type: "text", Text: fallback})
		s.publish(chat.ChatEvent{Type: "done", StopReason: "tool_observation_fallback"})
	})
}

func (s *chatStream) cancelToolFallbackLocked() {
	s.toolFallbackSeq++
	s.toolFallbackText = ""
	if s.toolFallback != nil {
		s.toolFallback.Stop()
		s.toolFallback = nil
	}
}

func (s *chatStream) consumeToolFallback() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	fallback := s.toolFallbackText
	s.cancelToolFallbackLocked()
	return fallback
}

var fallbackWhitespace = regexp.MustCompile(`[ \t]+`)
var fallbackNumeric = regexp.MustCompile(`^\d+$`)

func toolFallbackText(evt chat.ChatEvent) string {
	title := strings.TrimSpace(evt.ToolTitle)
	content := strings.TrimSpace(evt.ToolContent)
	if strings.HasPrefix(strings.ToLower(title), "active_terminal_") {
		return terminalFallbackText(content, evt.ToolRawInput)
	}
	return browserFallbackText(title, content)
}

func terminalFallbackText(content, rawInput string) string {
	command := extractAfterLinePrefix(content, "Terminal command:")
	if command == "" {
		command = commandFromToolRawInput(rawInput)
	}
	status := extractAfterLinePrefix(content, "Terminal status:")
	output := extractAfterMarker(content, "Output:")
	if output == "" {
		output = content
	}
	if summary := summarizeProcessNameCommand(command, output); summary != "" {
		return summary
	}
	if summary := summarizePortProcess(output); summary != "" {
		return summary
	}
	if status == "" {
		status = "completed"
	}
	return "Output terminal terakhir berstatus `" + status + "`.\n\n```text\n" + trimFallbackOutput(output) + "\n```"
}

func commandFromToolRawInput(rawInput string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(rawInput), &payload) != nil {
		return ""
	}
	if command, ok := payload["command"].(string); ok {
		return strings.TrimSpace(command)
	}
	return ""
}

func browserFallbackText(title, content string) string {
	line := firstUsefulLine(content)
	if line == "" {
		line = "aksi browser selesai"
	}
	return "Saya sudah menjalankan " + title + ". Hasil terakhir: " + line
}

func summarizePortProcess(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if fields[2] != "0" && strings.ToLower(fields[3]) != "idle" && fallbackNumeric.MatchString(fields[0]) {
			return "Setelah saya cek, port " + fields[0] + " sedang dipakai oleh proses `" + fields[3] + "` dengan PID `" + fields[2] + "`."
		}
	}
	return ""
}

type processRow struct {
	Name string
	ID   string
}

func synthesizeTurnRecoveryText(observations []chat.ChatEvent) string {
	if summary := summarizeBrowserObservationChain(observations); summary != "" {
		return summary
	}
	if summary := summarizeTerminalObservationChain(observations); summary != "" {
		return summary
	}
	for i := len(observations) - 1; i >= 0; i-- {
		if browserEventNeedsAgentContinuation(observations[i]) {
			return ""
		}
		if shouldScheduleLegacyToolFallback(observations[i]) {
			return toolFallbackText(observations[i])
		}
	}
	return ""
}

type browserObservation struct {
	Action string
	URL    string
	Title  string
	Text   string
	HTML   string
	Path   string
	Count  int
	Bytes  int
	Cut    bool
}

func summarizeBrowserObservationChain(observations []chat.ChatEvent) string {
	found := false
	var latest browserObservation
	var latestEvent chat.ChatEvent
	for i := len(observations) - 1; i >= 0; i-- {
		evt := observations[i]
		if !isBrowserMCPTool(evt.ToolTitle) {
			continue
		}
		if !strings.EqualFold(evt.ToolStatus, "completed") && !strings.EqualFold(evt.ToolStatus, "failed") {
			continue
		}
		obs := parseBrowserObservation(evt)
		if !found {
			latest = obs
			latestEvent = evt
			found = true
		}
		if obs.Action == "inspect" && (obs.URL != "" || obs.Title != "" || obs.Text != "") {
			latest = obs
			latestEvent = evt
			break
		}
	}
	if !found {
		return ""
	}
	if browserObservationNeedsAgentContinuation(latestEvent, latest) {
		return ""
	}
	if summary := renderBrowserObservationSummary(latest); summary != "" {
		return summary
	}
	return ""
}

func browserObservationNeedsAgentContinuation(evt chat.ChatEvent, obs browserObservation) bool {
	if browserObservationSufficient(evt) {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(obs.Action)) {
	case "goto", "navigate", "scroll":
		return true
	default:
		return false
	}
}

func browserEventNeedsAgentContinuation(evt chat.ChatEvent) bool {
	if !isBrowserMCPTool(evt.ToolTitle) {
		return false
	}
	if !strings.EqualFold(evt.ToolStatus, "completed") && !strings.EqualFold(evt.ToolStatus, "failed") {
		return false
	}
	return browserObservationNeedsAgentContinuation(evt, parseBrowserObservation(evt))
}

func parseBrowserObservation(evt chat.ChatEvent) browserObservation {
	obs := browserObservation{
		Action: browserActionFromObservation(evt),
	}
	payload := parseBrowserPayload(evt.ToolContent)
	if len(payload) == 0 {
		return obs
	}
	if url, ok := payload["url"].(string); ok {
		obs.URL = strings.TrimSpace(url)
	}
	if title, ok := payload["title"].(string); ok {
		obs.Title = strings.TrimSpace(title)
	}
	if text, ok := payload["text"].(string); ok {
		obs.Text = strings.TrimSpace(text)
	}
	if html, ok := payload["html"].(string); ok {
		obs.HTML = strings.TrimSpace(html)
	}
	if path, ok := payload["path"].(string); ok {
		obs.Path = strings.TrimSpace(path)
	}
	if count, ok := toIntFromAny(payload["count"]); ok {
		obs.Count = count
	}
	if bytes, ok := toIntFromAny(payload["htmlBytes"]); ok {
		obs.Bytes = bytes
	}
	if truncated, ok := payload["truncated"].(bool); ok {
		obs.Cut = truncated
	}
	return obs
}

func browserActionFromObservation(evt chat.ChatEvent) string {
	if action := browserActionFromRawInput(evt.ToolRawInput); action != "" {
		return action
	}
	value := strings.TrimSpace(strings.ToLower(evt.ToolTitle))
	value = strings.TrimPrefix(value, "9ed-active-browser_")
	value = strings.TrimPrefix(value, "9ed_browser_")
	value = strings.TrimPrefix(value, "active_browser_")
	value = strings.TrimPrefix(value, "browser_")
	return value
}

func browserActionFromRawInput(rawInput string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(rawInput), &payload) != nil {
		return ""
	}
	action, ok := payload["action"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(action))
}

func parseBrowserPayload(content string) map[string]any {
	candidate := extractAfterMarker(content, "Output:")
	if candidate == "" {
		candidate = content
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return nil
	}
	if parsed := parseJSONObject(candidate); len(parsed) > 0 {
		return parsed
	}
	if start := strings.Index(candidate, "{"); start >= 0 {
		if end := strings.LastIndex(candidate, "}"); end > start {
			if parsed := parseJSONObject(candidate[start : end+1]); len(parsed) > 0 {
				return parsed
			}
		}
	}
	return nil
}

func parseJSONObject(candidate string) map[string]any {
	var parsed map[string]any
	if json.Unmarshal([]byte(candidate), &parsed) != nil {
		return nil
	}
	return parsed
}

func renderBrowserObservationSummary(obs browserObservation) string {
	switch obs.Action {
	case "inspect", "goto", "navigate", "click", "type", "press", "scroll":
		if summary := renderBrowserPageState(obs); summary != "" {
			return summary
		}
	case "screenshot":
		if obs.Path != "" {
			return "Screenshot browser tersimpan di `" + obs.Path + "`."
		}
	case "page_source", "source":
		if obs.Bytes > 0 {
			if obs.Cut {
				return "Page source browser berhasil diambil (`" + intToString(obs.Bytes) + "` bytes, terpotong sesuai limit)."
			}
			return "Page source browser berhasil diambil (`" + intToString(obs.Bytes) + "` bytes)."
		}
		if obs.HTML != "" {
			return "Page source browser berhasil diambil."
		}
	case "console_logs", "console":
		if obs.Count > 0 {
			return "Browser memiliki `" + intToString(obs.Count) + "` entri console terbaru yang siap dirangkum."
		}
	case "network_requests", "network":
		if obs.Count > 0 {
			return "Browser memiliki `" + intToString(obs.Count) + "` entri network terbaru yang siap dirangkum."
		}
	}
	if obs.Title != "" || obs.URL != "" || obs.Text != "" {
		return renderBrowserPageState(obs)
	}
	return ""
}

func renderBrowserPageState(obs browserObservation) string {
	location := ""
	if obs.Title != "" && obs.URL != "" {
		location = "`" + obs.Title + "` (" + obs.URL + ")"
	} else if obs.Title != "" {
		location = "`" + obs.Title + "`"
	} else if obs.URL != "" {
		location = obs.URL
	}
	preview := firstUsefulLine(obs.Text)
	if preview != "" {
		preview = truncateText(preview, 220)
	}
	if location != "" && preview != "" {
		return "Halaman browser saat ini " + location + ". Ringkasan teks: \"" + preview + "\"."
	}
	if location != "" {
		return "Halaman browser saat ini " + location + "."
	}
	if preview != "" {
		return "Ringkasan teks halaman browser: \"" + preview + "\"."
	}
	return ""
}

func truncateText(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func toIntFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed), true
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed, true
		}
	default:
		return 0, false
	}
	return 0, false
}

func intToString(value int) string {
	return strconv.Itoa(value)
}

func summarizeTerminalObservationChain(observations []chat.ChatEvent) string {
	var latestCompleted *chat.ChatEvent
	port := ""
	for i := len(observations) - 1; i >= 0; i-- {
		evt := observations[i]
		if !isInteractiveMCPTool(evt.ToolTitle) {
			continue
		}
		if !strings.EqualFold(evt.ToolStatus, "completed") && !strings.EqualFold(evt.ToolStatus, "failed") {
			continue
		}
		if latestCompleted == nil {
			copyEvt := evt
			latestCompleted = &copyEvt
		}
		if port == "" {
			port = detectPortFromObservation(evt)
		}
	}
	if latestCompleted == nil {
		return ""
	}

	command := extractAfterLinePrefix(latestCompleted.ToolContent, "Terminal command:")
	if command == "" {
		command = commandFromToolRawInput(latestCompleted.ToolRawInput)
	}
	output := extractAfterMarker(latestCompleted.ToolContent, "Output:")
	if output == "" {
		output = latestCompleted.ToolContent
	}
	if summary := summarizeProcessTableCommand(command, output, port); summary != "" {
		return summary
	}
	if summary := summarizeNetstatObservation(command, output, port); summary != "" {
		return summary
	}
	return ""
}

func detectPortFromObservation(evt chat.ChatEvent) string {
	command := extractAfterLinePrefix(evt.ToolContent, "Terminal command:")
	if command == "" {
		command = commandFromToolRawInput(evt.ToolRawInput)
	}
	if port := detectPort(command); port != "" {
		return port
	}
	output := extractAfterMarker(evt.ToolContent, "Output:")
	if output == "" {
		output = evt.ToolContent
	}
	return detectPort(output)
}

var portPattern = regexp.MustCompile(`(?i)(?::|localport\s+)(\d{2,5})`)

func detectPort(value string) string {
	match := portPattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func summarizeNetstatObservation(command, output, port string) string {
	if port == "" {
		port = detectPort(command)
	}
	var ownerPIDs []string
	seen := map[string]struct{}{}
	if pid := extractColonValue(output, "OwningProcess"); fallbackNumeric.MatchString(pid) {
		seen[pid] = struct{}{}
		ownerPIDs = append(ownerPIDs, pid)
	}
	for _, line := range strings.Split(trimFallbackOutput(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid := strings.TrimSpace(fields[len(fields)-1])
		if !fallbackNumeric.MatchString(pid) {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		ownerPIDs = append(ownerPIDs, pid)
	}
	if len(ownerPIDs) == 0 {
		return ""
	}
	if port != "" && len(ownerPIDs) == 1 {
		return "Untuk port `" + port + "`, output terminal menunjukkan PID terkait `" + ownerPIDs[0] + "`."
	}
	if port != "" {
		return "Untuk port `" + port + "`, output terminal menunjukkan PID terkait " + joinQuotedValues(ownerPIDs) + "."
	}
	return ""
}

func extractColonValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), key) {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

var getProcessIDPattern = regexp.MustCompile(`(?i)get-process\s+-id\s+([0-9,\s]+)`)

func summarizeProcessNameCommand(command, output string) string {
	if command == "" || output == "" {
		return ""
	}
	match := getProcessIDPattern.FindStringSubmatch(command)
	if len(match) < 2 {
		return ""
	}
	ids := splitProcessIDs(match[1])
	name := lastPlainProcessName(output)
	if name == "" {
		return ""
	}
	if len(ids) == 1 {
		return "Setelah saya cek, PID `" + ids[0] + "` adalah proses `" + name + "`."
	}
	return "Setelah saya cek, salah satu proses yang terdeteksi adalah `" + name + "`. Output terakhir menunjukkan:\n```text\n" + trimFallbackOutput(output) + "\n```"
}

func summarizeProcessTableCommand(command, output, port string) string {
	if !strings.Contains(strings.ToLower(command), "get-process") {
		return ""
	}
	rows := parseProcessRows(output)
	if len(rows) == 0 {
		return ""
	}
	subject := "Proses yang dicek"
	if port != "" {
		subject = "Untuk port `" + port + "`, proses yang dicek"
	}
	if len(rows) == 1 {
		return subject + " adalah `" + rows[0].Name + "` (PID `" + rows[0].ID + "`)."
	}
	return subject + " adalah " + formatProcessRows(rows) + "."
}

func parseProcessRows(output string) []processRow {
	lines := strings.Split(trimFallbackOutput(output), "\n")
	rows := make([]processRow, 0, len(lines))
	seen := map[string]struct{}{}
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" ||
			strings.Contains(line, "Terminal observation complete") ||
			strings.Contains(line, "Next step:") ||
			strings.HasPrefix(line, "Terminal command:") ||
			strings.HasPrefix(line, "Terminal status:") ||
			strings.HasPrefix(line, "Output:") ||
			strings.EqualFold(line, "name id sessionid") ||
			strings.HasPrefix(line, "----") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var row processRow
		switch {
		case fallbackNumeric.MatchString(fields[1]):
			row = processRow{Name: fields[0], ID: fields[1]}
		case fallbackNumeric.MatchString(fields[0]):
			row = processRow{Name: fields[1], ID: fields[0]}
		default:
			continue
		}
		if row.Name == "" || row.ID == "" {
			continue
		}
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		rows = append(rows, row)
	}
	return rows
}

func formatProcessRows(rows []processRow) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, "`"+row.Name+"` (PID `"+row.ID+"`)")
	}
	if len(parts) == 1 {
		return parts[0]
	}
	if len(parts) == 2 {
		return parts[0] + " dan " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", dan " + parts[len(parts)-1]
}

func joinQuotedValues(values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, "`"+value+"`")
	}
	if len(parts) == 1 {
		return parts[0]
	}
	if len(parts) == 2 {
		return parts[0] + " dan " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", dan " + parts[len(parts)-1]
}

func splitProcessIDs(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && fallbackNumeric.MatchString(part) {
			out = append(out, part)
		}
	}
	return out
}

func lastPlainProcessName(output string) string {
	lines := strings.Split(trimFallbackOutput(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" ||
			strings.Contains(line, "Terminal observation complete") ||
			strings.Contains(line, "Next step:") ||
			strings.Contains(line, "ProcessName") ||
			strings.HasPrefix(line, "Terminal command:") ||
			strings.HasPrefix(line, "Terminal status:") ||
			strings.HasPrefix(line, "Output:") ||
			strings.HasPrefix(line, "--") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 1 && !fallbackNumeric.MatchString(fields[0]) {
			return fields[0]
		}
		if len(fields) >= 2 && fallbackNumeric.MatchString(fields[1]) {
			return fields[0]
		}
		if len(fields) >= 2 && fallbackNumeric.MatchString(fields[0]) {
			return fields[1]
		}
	}
	return ""
}

func extractAfterLinePrefix(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func extractAfterMarker(content, marker string) string {
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(content[idx+len(marker):])
}

func firstUsefulLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "observation complete") || strings.HasPrefix(line, "Next step:") {
			continue
		}
		return fallbackWhitespace.ReplaceAllString(line, " ")
	}
	return ""
}

func trimFallbackOutput(output string) string {
	output = strings.TrimSpace(output)
	const maxLen = 4000
	if len(output) > maxLen {
		return "...(truncated)\n" + output[len(output)-maxLen:]
	}
	return output
}
