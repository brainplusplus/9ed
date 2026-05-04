package chat

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

type ChatStore struct {
	db *sql.DB
}

type SessionRecord struct {
	ID        string `json:"id"`
	AgentID   string `json:"agentId"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type RecentProject struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	LastOpened int64  `json:"lastOpened"`
}

type MessageRecord struct {
	ID           string `json:"id"`
	SessionID    string `json:"sessionId"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	ContextFile  string `json:"contextFile,omitempty"`
	ContextStart int    `json:"contextStartLine,omitempty"`
	ContextEnd   int    `json:"contextEndLine,omitempty"`
	ContextCode  string `json:"contextCode,omitempty"`
	ContextLang  string `json:"contextLanguage,omitempty"`
	Timestamp    int64  `json:"timestamp"`
}

func DefaultDBPath() string {
	var home string
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	} else {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".go-webttyd", "ide.db")
}

func NewChatStore(dbPath string) (*ChatStore, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}

	if err := createTables(db); err != nil {
		db.Close()
		return nil, err
	}

	return &ChatStore{db: db}, nil
}

func createTables(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS chat_sessions (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    context_file TEXT,
    context_start_line INTEGER,
    context_end_line INTEGER,
    context_code TEXT,
    context_language TEXT,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON chat_messages(session_id, timestamp);

CREATE TABLE IF NOT EXISTS recent_projects (
    path TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    last_opened INTEGER NOT NULL
);
`
	_, err := db.Exec(schema)
	return err
}

func (s *ChatStore) Close() error {
	return s.db.Close()
}

func (s *ChatStore) CreateSession(id, agentId, title string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"INSERT INTO chat_sessions (id, agent_id, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		id, agentId, title, now, now,
	)
	return err
}

func (s *ChatStore) ListSessions(limit int) ([]SessionRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		"SELECT id, agent_id, title, created_at, updated_at FROM chat_sessions ORDER BY updated_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionRecord
	for rows.Next() {
		var r SessionRecord
		if err := rows.Scan(&r.ID, &r.AgentID, &r.Title, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, r)
	}
	return sessions, rows.Err()
}

func (s *ChatStore) DeleteSession(id string) error {
	_, err := s.db.Exec("DELETE FROM chat_sessions WHERE id = ?", id)
	return err
}

func (s *ChatStore) UpdateSessionTitle(id, title string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"UPDATE chat_sessions SET title = ?, updated_at = ? WHERE id = ?",
		title, now, id,
	)
	return err
}

func (s *ChatStore) AddMessage(msg MessageRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_messages (id, session_id, role, content, context_file, context_start_line, context_end_line, context_code, context_language, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.SessionID, msg.Role, msg.Content,
		nullString(msg.ContextFile), nullInt(msg.ContextStart), nullInt(msg.ContextEnd),
		nullString(msg.ContextCode), nullString(msg.ContextLang), msg.Timestamp,
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		"UPDATE chat_sessions SET updated_at = ? WHERE id = ?",
		msg.Timestamp, msg.SessionID,
	)
	return err
}

func (s *ChatStore) GetMessages(sessionId string) ([]MessageRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, role, content, 
		        COALESCE(context_file, ''), COALESCE(context_start_line, 0), COALESCE(context_end_line, 0),
		        COALESCE(context_code, ''), COALESCE(context_language, ''), timestamp
		 FROM chat_messages WHERE session_id = ? ORDER BY timestamp ASC`,
		sessionId,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageRecord
	for rows.Next() {
		var m MessageRecord
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content,
			&m.ContextFile, &m.ContextStart, &m.ContextEnd,
			&m.ContextCode, &m.ContextLang, &m.Timestamp); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *ChatStore) ListRecentProjects(limit int) ([]RecentProject, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		"SELECT path, name, last_opened FROM recent_projects ORDER BY last_opened DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []RecentProject
	for rows.Next() {
		var p RecentProject
		if err := rows.Scan(&p.Path, &p.Name, &p.LastOpened); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *ChatStore) SaveRecentProject(path, name string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO recent_projects (path, name, last_opened) VALUES (?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET name = excluded.name, last_opened = excluded.last_opened`,
		path, name, now,
	)
	return err
}

func (s *ChatStore) RemoveRecentProject(path string) error {
	_, err := s.db.Exec("DELETE FROM recent_projects WHERE path = ?", path)
	return err
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}
