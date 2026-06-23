package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/brainplusplus/9ed/internal/chat"
)

// timelineOut is a local decode target used to assert the JSON shape of the
// timeline response returned by computeTimelineState / sendTimelineResponse.
type timelineOut struct {
	Type        string `json:"type"`
	Epoch       string `json:"epoch"`
	Reset       bool   `json:"reset"`
	StaleCursor bool   `json:"staleCursor"`
	Gap         bool   `json:"gap"`
	Window      struct {
		MinSeq  int64 `json:"minSeq"`
		MaxSeq  int64 `json:"maxSeq"`
		NextSeq int64 `json:"nextSeq"`
	} `json:"window"`
	HasOlder  bool `json:"hasOlder"`
	HasNewer  bool `json:"hasNewer"`
	EndCursor int64
	Events    []json.RawMessage `json:"events"`
}

func TestComputeTimelineState_FullShape(t *testing.T) {
	now := int64(1000)
	records := []chat.EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text","text":"hi"}`, Seq: 3, Timestamp: now, Epoch: "ep-1"},
		{ID: "e2", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text","text":"there"}`, Seq: 5, Timestamp: now + 1, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{})

	out, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got timelineOut
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Type != "timeline" {
		t.Errorf("expected type 'timeline', got %q", got.Type)
	}
	if got.Epoch != "ep-1" {
		t.Errorf("expected epoch 'ep-1', got %q", got.Epoch)
	}
	if got.Window.MinSeq != 1 || got.Window.MaxSeq != 10 || got.Window.NextSeq != 11 {
		t.Errorf("window mismatch: got %+v", got.Window)
	}
	if len(got.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(got.Events))
	}
	// endCursor is the max seq of returned events.
	if ts.EndCursor != 5 {
		t.Errorf("expected endCursor 5, got %d", ts.EndCursor)
	}
}

func TestComputeTimelineState_StaleCursorDetection(t *testing.T) {
	records := []chat.EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 1, Epoch: "new-epoch"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 1, NextSeq: 2}

	ts := computeTimelineState(records, win, "new-epoch", timelineRequest{
		ClientEpoch: "stale-epoch",
	})

	if !ts.StaleCursor {
		t.Error("expected staleCursor true when client epoch != current epoch")
	}
	if !ts.Reset {
		t.Error("expected reset true when stale cursor detected")
	}
}

func TestComputeTimelineState_NoStaleCursorWhenEpochMatches(t *testing.T) {
	records := []chat.EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 1, Epoch: "same-epoch"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 1, NextSeq: 2}

	ts := computeTimelineState(records, win, "same-epoch", timelineRequest{
		ClientEpoch: "same-epoch",
	})

	if ts.StaleCursor {
		t.Error("expected staleCursor false when epochs match")
	}
	if ts.Reset {
		t.Error("expected reset false when no stale/gap")
	}
}

func TestComputeTimelineState_StaleCursorWhenClientEpochEmpty(t *testing.T) {
	// An empty client epoch is treated as "no epoch supplied" (not stale).
	records := []chat.EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 1, Epoch: "cur-epoch"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 1, NextSeq: 2}

	ts := computeTimelineState(records, win, "cur-epoch", timelineRequest{
		ClientEpoch: "",
	})

	if ts.StaleCursor {
		t.Error("expected staleCursor false when client epoch is empty (not supplied)")
	}
}

func TestComputeTimelineState_GapDetection(t *testing.T) {
	// afterSeq=2 but minSeq=5 -> gap (2 < 5-1=4).
	records := []chat.EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 5, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 2,
	})

	if !ts.Gap {
		t.Error("expected gap true when afterSeq < minSeq-1")
	}
	if !ts.Reset {
		t.Error("expected reset true when gap detected")
	}
}

func TestComputeTimelineState_NoGapWhenAfterSeqContiguous(t *testing.T) {
	// afterSeq=4, minSeq=5 -> contiguous (4 == 5-1), no gap.
	records := []chat.EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 5, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 4,
	})

	if ts.Gap {
		t.Error("expected gap false when afterSeq == minSeq-1 (contiguous)")
	}
	if ts.Reset {
		t.Error("expected reset false when contiguous")
	}
}

func TestComputeTimelineState_NoGapWhenAfterSeqZero(t *testing.T) {
	// afterSeq=0 means tail fetch; gap detection should not apply.
	records := []chat.EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 5, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 0,
	})

	if ts.Gap {
		t.Error("expected gap false when afterSeq == 0 (tail fetch)")
	}
}

func TestComputeTimelineState_HasOlderTrueWhenEventsBeforeRange(t *testing.T) {
	// Returned events start at seq 5; window minSeq is 1, so older events exist.
	records := []chat.EventRecord{
		{ID: "e5", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "ep-1"},
		{ID: "e6", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 6, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 4,
	})

	if !ts.HasOlder {
		t.Error("expected hasOlder true when events exist before returned range (minSeq=1 < firstSeq=5)")
	}
}

func TestComputeTimelineState_HasOlderFalseWhenRangeStartsAtMin(t *testing.T) {
	// Returned events start at seq 5 == minSeq, so no older events.
	records := []chat.EventRecord{
		{ID: "e5", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 5, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 4,
	})

	if ts.HasOlder {
		t.Error("expected hasOlder false when returned range starts at minSeq")
	}
}

func TestComputeTimelineState_HasNewerTrueWhenEventsAfterRange(t *testing.T) {
	// Returned events end at seq 6; window maxSeq is 10, so newer events exist.
	records := []chat.EventRecord{
		{ID: "e5", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "ep-1"},
		{ID: "e6", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 6, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 4,
	})

	if !ts.HasNewer {
		t.Error("expected hasNewer true when events exist after returned range (maxSeq=10 > lastSeq=6)")
	}
}

func TestComputeTimelineState_HasNewerFalseWhenRangeEndsAtMax(t *testing.T) {
	// Returned events end at seq 10 == maxSeq, so no newer events.
	records := []chat.EventRecord{
		{ID: "e10", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 10, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 9,
	})

	if ts.HasNewer {
		t.Error("expected hasNewer false when returned range ends at maxSeq")
	}
}

func TestComputeTimelineState_EndCursorIsMaxReturnedSeq(t *testing.T) {
	records := []chat.EventRecord{
		{ID: "e5", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "ep-1"},
		{ID: "e8", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 8, Epoch: "ep-1"},
	}
	win := seqWindow{MinSeq: 1, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "ep-1", timelineRequest{
		AfterSeq: 4,
	})

	if ts.EndCursor != 8 {
		t.Errorf("expected endCursor 8 (max returned seq), got %d", ts.EndCursor)
	}
}

func TestComputeTimelineState_EndCursorZeroWhenNoEvents(t *testing.T) {
	records := []chat.EventRecord{}
	win := seqWindow{MinSeq: 0, MaxSeq: 0, NextSeq: 1}

	ts := computeTimelineState(records, win, "", timelineRequest{})

	if ts.EndCursor != 0 {
		t.Errorf("expected endCursor 0 when no events, got %d", ts.EndCursor)
	}
	if len(ts.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(ts.Events))
	}
}

func TestComputeTimelineState_StaleCursorAndGapBothSet(t *testing.T) {
	// Both stale epoch and gap conditions can be true simultaneously.
	records := []chat.EventRecord{
		{ID: "e5", SessionID: "s1", Kind: "text", PayloadJSON: `{"type":"text"}`, Seq: 5, Epoch: "new-epoch"},
	}
	win := seqWindow{MinSeq: 5, MaxSeq: 10, NextSeq: 11}

	ts := computeTimelineState(records, win, "new-epoch", timelineRequest{
		ClientEpoch: "old-epoch",
		AfterSeq:    2,
	})

	if !ts.StaleCursor {
		t.Error("expected staleCursor true")
	}
	if !ts.Gap {
		t.Error("expected gap true")
	}
	if !ts.Reset {
		t.Error("expected reset true")
	}
}

func TestReplayMetaEnvelope_FullShape(t *testing.T) {
	meta := replayMetaEnvelope{
		Type:  "replay_meta",
		Epoch: "ep-1",
		Window: seqWindow{
			MinSeq:  1,
			MaxSeq:  10,
			NextSeq: 11,
		},
	}

	out, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got struct {
		Type  string `json:"type"`
		Epoch string `json:"epoch"`
		Window struct {
			MinSeq  int64 `json:"minSeq"`
			MaxSeq  int64 `json:"maxSeq"`
			NextSeq int64 `json:"nextSeq"`
		} `json:"window"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Type != "replay_meta" {
		t.Errorf("expected type 'replay_meta', got %q", got.Type)
	}
	if got.Epoch != "ep-1" {
		t.Errorf("expected epoch 'ep-1', got %q", got.Epoch)
	}
	if got.Window.MinSeq != 1 || got.Window.MaxSeq != 10 || got.Window.NextSeq != 11 {
		t.Errorf("window mismatch: got %+v", got.Window)
	}
}

func TestChatWSInbound_HasEpochField(t *testing.T) {
	// ADR-0002: client-supplied epoch for stale cursor detection.
	raw := `{"type":"fetch_timeline","afterSeq":5,"epoch":"client-epoch-123"}`
	var msg chatWSInbound
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Epoch != "client-epoch-123" {
		t.Errorf("expected Epoch 'client-epoch-123', got %q", msg.Epoch)
	}
	if msg.AfterSeq != 5 {
		t.Errorf("expected AfterSeq 5, got %d", msg.AfterSeq)
	}
}
