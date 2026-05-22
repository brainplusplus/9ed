package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempStore(t *testing.T) *ChatStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewChatStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewChatStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewChatStore_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "nested", "chat.db")
	store, err := NewChatStore(dbPath)
	if err != nil {
		t.Fatalf("NewChatStore: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestCreateAndListSessions(t *testing.T) {
	store := tempStore(t)

	if err := store.CreateSession("s1", "opencode", "First chat"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.CreateSession("s2", "claude", "Second chat"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "s2" {
		t.Errorf("expected most recent first, got %s", sessions[0].ID)
	}
}

func TestUpsertMessageUpdatesExistingMessage(t *testing.T) {
	store := tempStore(t)
	if err := store.CreateSession("s1", "opencode", "Upsert"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.UpsertMessage(MessageRecord{
		ID:        "m1",
		SessionID: "s1",
		Role:      "assistant",
		Content:   "hel",
		Timestamp: 1,
	}); err != nil {
		t.Fatalf("UpsertMessage first: %v", err)
	}
	if err := store.UpsertMessage(MessageRecord{
		ID:        "m1",
		SessionID: "s1",
		Role:      "assistant",
		Content:   "hello",
		Timestamp: 2,
	}); err != nil {
		t.Fatalf("UpsertMessage second: %v", err)
	}

	messages, err := store.GetMessages("s1")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "hello" || messages[0].Timestamp != 2 {
		t.Fatalf("expected updated single message, got %#v", messages)
	}
}

func TestDeleteSession_CascadesMessages(t *testing.T) {
	store := tempStore(t)

	store.CreateSession("s1", "opencode", "Test")
	store.AddMessage(MessageRecord{
		ID: "m1", SessionID: "s1", Role: "user", Content: "hello", Timestamp: time.Now().UnixMilli(),
	})

	if err := store.DeleteSession("s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	msgs, err := store.GetMessages("s1")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after cascade delete, got %d", len(msgs))
	}
}

func TestUpdateSessionTitle(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Old title")

	if err := store.UpdateSessionTitle("s1", "New title"); err != nil {
		t.Fatalf("UpdateSessionTitle: %v", err)
	}

	sessions, _ := store.ListSessions(10)
	if sessions[0].Title != "New title" {
		t.Errorf("expected 'New title', got %q", sessions[0].Title)
	}
}

func TestAddAndGetMessages(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Test")

	now := time.Now().UnixMilli()
	store.AddMessage(MessageRecord{
		ID: "m1", SessionID: "s1", Role: "user", Content: "hello",
		ContextFile: "main.go", ContextStart: 10, ContextEnd: 20,
		ContextCode: "func main() {}", ContextLang: "go",
		Timestamp: now,
	})
	store.AddMessage(MessageRecord{
		ID: "m2", SessionID: "s1", Role: "assistant", Content: "hi there",
		Timestamp: now + 1000,
	})

	msgs, err := store.GetMessages("s1")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	if msgs[0].ContextFile != "main.go" {
		t.Errorf("expected context file 'main.go', got %q", msgs[0].ContextFile)
	}
	if msgs[1].ContextFile != "" {
		t.Errorf("expected empty context file, got %q", msgs[1].ContextFile)
	}
}

func TestListSessions_RespectsLimit(t *testing.T) {
	store := tempStore(t)
	for i := 0; i < 10; i++ {
		store.CreateSession("s"+string(rune('a'+i)), "opencode", "Session")
		time.Sleep(time.Millisecond)
	}

	sessions, _ := store.ListSessions(3)
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}
}

func TestGetLastSessionForProject_ReturnsLatestWithinProject(t *testing.T) {
	store := tempStore(t)

	if err := store.CreateSessionFull("other-1", "opencode", "Other", "/other", ""); err != nil {
		t.Fatalf("CreateSessionFull other-1: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := store.CreateSessionFull("repo-1", "claude", "Repo 1", "/repo", ""); err != nil {
		t.Fatalf("CreateSessionFull repo-1: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := store.CreateSessionFull("repo-2", "claude", "Repo 2", "/repo", ""); err != nil {
		t.Fatalf("CreateSessionFull repo-2: %v", err)
	}

	record, err := store.GetLastSessionForProject("/repo")
	if err != nil {
		t.Fatalf("GetLastSessionForProject: %v", err)
	}
	if record == nil {
		t.Fatal("expected record, got nil")
	}
	if record.ID != "repo-2" {
		t.Fatalf("expected latest repo session repo-2, got %q", record.ID)
	}
}

func TestSessionsForProject_FiltersOtherProjects(t *testing.T) {
	store := tempStore(t)

	if err := store.CreateSessionFull("repo-1", "claude", "Repo 1", "/repo", ""); err != nil {
		t.Fatalf("CreateSessionFull repo-1: %v", err)
	}
	if err := store.CreateSessionFull("other-1", "opencode", "Other 1", "/other", ""); err != nil {
		t.Fatalf("CreateSessionFull other-1: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := store.CreateSessionFull("repo-2", "claude", "Repo 2", "/repo", ""); err != nil {
		t.Fatalf("CreateSessionFull repo-2: %v", err)
	}

	sessions, err := store.SessionsForProject("/repo", 10)
	if err != nil {
		t.Fatalf("SessionsForProject: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 repo sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "repo-2" || sessions[1].ID != "repo-1" {
		t.Fatalf("expected repo sessions ordered repo-2, repo-1; got %q, %q", sessions[0].ID, sessions[1].ID)
	}
}

func TestAppendAndGetEvents(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Events test")

	now := time.Now().UnixMilli()

	events := []EventRecord{
		{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{"text":"hello"}`, Seq: 1, Timestamp: now},
		{ID: "e2", SessionID: "s1", Kind: "tool_call", PayloadJSON: `{"toolCallId":"tc1","toolTitle":"Read file"}`, Seq: 2, Timestamp: now + 100},
		{ID: "e3", SessionID: "s1", Kind: "tool_call_update", PayloadJSON: `{"toolCallId":"tc1","toolStatus":"completed"}`, Seq: 3, Timestamp: now + 200},
		{ID: "e4", SessionID: "s1", Kind: "plan", PayloadJSON: `{"planEntries":[{"content":"step1"}]}`, Seq: 4, Timestamp: now + 300},
		{ID: "e5", SessionID: "s1", Kind: "thinking", PayloadJSON: `{"thinking":"hmm"}`, Seq: 5, Timestamp: now + 400},
		{ID: "e6", SessionID: "s1", Kind: "diff", PayloadJSON: `{"diffPath":"main.go","diffOldText":"old","diffNewText":"new"}`, Seq: 6, Timestamp: now + 500},
		{ID: "e7", SessionID: "s1", Kind: "title", PayloadJSON: `{"title":"New Title"}`, Seq: 7, Timestamp: now + 600},
	}

	for _, e := range events {
		if err := store.AppendEvent(e); err != nil {
			t.Fatalf("AppendEvent %s: %v", e.ID, err)
		}
	}

	result, err := store.GetEvents("s1")
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(result) != 7 {
		t.Fatalf("expected 7 events, got %d", len(result))
	}
	if result[0].Kind != "text" {
		t.Errorf("expected first event kind 'text', got %q", result[0].Kind)
	}
	if result[3].Kind != "plan" {
		t.Errorf("expected 4th event kind 'plan', got %q", result[3].Kind)
	}
	if result[6].PayloadJSON != `{"title":"New Title"}` {
		t.Errorf("expected title payload, got %q", result[6].PayloadJSON)
	}
}

func TestGetEvents_Empty(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Empty")

	result, err := store.GetEvents("s1")
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 events for empty session, got %d", len(result))
	}
}

func TestSaveAndGetSnapshot(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Snap test")

	snap := SessionSnapshot{
		SessionID:      "s1",
		CommandsJSON:   `[{"name":"commit","description":"Create commit"}]`,
		ConfigOptsJSON: `[{"id":"model","name":"Model","currentValue":"opus"}]`,
	}
	if err := store.SaveSnapshot(snap); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	result, err := store.GetSnapshot("s1")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if result == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if result.CommandsJSON != snap.CommandsJSON {
		t.Errorf("expected commands %q, got %q", snap.CommandsJSON, result.CommandsJSON)
	}
	if result.ConfigOptsJSON != snap.ConfigOptsJSON {
		t.Errorf("expected configOpts %q, got %q", snap.ConfigOptsJSON, result.ConfigOptsJSON)
	}
}

func TestGetSnapshot_NotFound(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "No snap")

	result, err := store.GetSnapshot("s1")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for missing snapshot, got %+v", result)
	}
}

func TestSaveSnapshot_Upserts(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Upsert snap")

	snap1 := SessionSnapshot{
		SessionID:      "s1",
		CommandsJSON:   `[{"name":"old"}]`,
		ConfigOptsJSON: ``,
	}
	store.SaveSnapshot(snap1)

	snap2 := SessionSnapshot{
		SessionID:      "s1",
		CommandsJSON:   `[{"name":"new"}]`,
		ConfigOptsJSON: `[{"id":"model"}]`,
	}
	store.SaveSnapshot(snap2)

	result, _ := store.GetSnapshot("s1")
	if result == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if result.CommandsJSON != `[{"name":"new"}]` {
		t.Errorf("expected upserted commands, got %q", result.CommandsJSON)
	}
	if result.ConfigOptsJSON != `[{"id":"model"}]` {
		t.Errorf("expected upserted configOpts, got %q", result.ConfigOptsJSON)
	}
}

func TestNextEventSeq(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Seq test")

	seq, err := store.NextEventSeq("s1")
	if err != nil {
		t.Fatalf("NextEventSeq empty: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected seq 1 for empty, got %d", seq)
	}

	store.AppendEvent(EventRecord{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{}`, Seq: 1, Timestamp: 1})

	seq, err = store.NextEventSeq("s1")
	if err != nil {
		t.Fatalf("NextEventSeq after insert: %v", err)
	}
	if seq != 2 {
		t.Errorf("expected seq 2 after one insert, got %d", seq)
	}
}

func TestDeleteSession_CascadesEventsAndSnapshots(t *testing.T) {
	store := tempStore(t)
	store.CreateSession("s1", "opencode", "Cascade test")
	store.AppendEvent(EventRecord{ID: "e1", SessionID: "s1", Kind: "text", PayloadJSON: `{}`, Seq: 1, Timestamp: 1})
	store.SaveSnapshot(SessionSnapshot{SessionID: "s1", CommandsJSON: `[]`, ConfigOptsJSON: `[]`})

	if err := store.DeleteSession("s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	events, _ := store.GetEvents("s1")
	if len(events) != 0 {
		t.Errorf("expected 0 events after cascade, got %d", len(events))
	}
	snap, _ := store.GetSnapshot("s1")
	if snap != nil {
		t.Errorf("expected nil snapshot after cascade, got %+v", snap)
	}
}

func TestAppendAndLoadRichTranscript(t *testing.T) {
	store := tempStore(t)
	if err := store.CreateSessionFull("s-rich", "opencode", "Rich", "/repo", "acp-1"); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}

	events := []EventRecord{
		{ID: "e1", SessionID: "s-rich", Kind: "user", PayloadJSON: `{"role":"user","content":"create file .env.local.example"}`, Timestamp: 1000, Seq: 1},
		{ID: "e2", SessionID: "s-rich", Kind: "tool_call", PayloadJSON: `{"toolCall":{"toolCallId":"tc1","title":".env.local.example","kind":"edit","status":"completed"},"diffs":[{"path":".env.local.example","oldText":"","newText":"PORT=8080"}]}`, Timestamp: 1001, Seq: 2},
		{ID: "e3", SessionID: "s-rich", Kind: "assistant", PayloadJSON: `{"role":"assistant","content":"Done."}`, Timestamp: 1002, Seq: 3},
	}

	for _, event := range events {
		if err := store.AppendEvent(event); err != nil {
			t.Fatalf("AppendEvent(%s): %v", event.ID, err)
		}
	}

	got, err := store.GetEvents("s-rich")
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[1].Kind != "tool_call" {
		t.Fatalf("expected second event kind tool_call, got %q", got[1].Kind)
	}
	if got[1].PayloadJSON == "" {
		t.Fatal("expected tool_call payload json to be persisted")
	}
}

func TestSaveAndLoadSessionSnapshot(t *testing.T) {
	store := tempStore(t)
	if err := store.CreateSessionFull("s-snap", "opencode", "Snapshot", "/repo", "acp-1"); err != nil {
		t.Fatalf("CreateSessionFull: %v", err)
	}

	commands := []CommandInfo{{Name: "help", Description: "Show commands"}}
	configOptions := []ConfigOptionInfo{{ID: "model", Name: "Model", Type: "string", CurrentValue: "gpt-5", Options: []ConfigValueInfo{{Value: "gpt-5", Name: "GPT-5"}}}}
	commandsJSON, _ := json.Marshal(commands)
	configJSON, _ := json.Marshal(configOptions)

	snapshot := SessionSnapshot{
		SessionID:      "s-snap",
		CommandsJSON:   string(commandsJSON),
		ConfigOptsJSON: string(configJSON),
		UpdatedAt:      12345,
	}
	if err := store.SaveSnapshot(snapshot); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	got, err := store.GetSnapshot("s-snap")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if got == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if got.CommandsJSON != string(commandsJSON) {
		t.Fatalf("expected commands json %q, got %q", string(commandsJSON), got.CommandsJSON)
	}
	if got.ConfigOptsJSON != string(configJSON) {
		t.Fatalf("expected config json %q, got %q", string(configJSON), got.ConfigOptsJSON)
	}
}
