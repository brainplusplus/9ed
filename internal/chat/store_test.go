package chat

import (
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
