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
	ID            string `json:"id"`
	AgentID       string `json:"agentId"`
	Title         string `json:"title"`
	WorkDir       string `json:"workDir,omitempty"`
	ACPSessionID  string `json:"acpSessionId,omitempty"`
	Status        string `json:"status"` // "active", "closed"
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// SessionStatus constants.
const (
	SessionStatusActive  = "active"
	SessionStatusClosed  = "closed"
)

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

// EventRecord stores a single rich ACP transcript event for session persistence.
// The Kind field matches ChatEvent.Type values: text, thinking, tool_call,
// tool_call_update, diff, plan, commands, config_options, title, done.
// PayloadJSON carries the full event-specific data as a JSON blob.
type EventRecord struct {
	ID          string `json:"id"`
	SessionID   string `json:"sessionId"`
	Kind        string `json:"kind"`
	PayloadJSON string `json:"payloadJson"`
	Seq         int64  `json:"seq"`
	Timestamp   int64  `json:"timestamp"`
}

// SessionSnapshot stores the latest session UI state (commands, configOptions)
// so restored sessions can render footer controls before live ACP reconnects.
type SessionSnapshot struct {
	SessionID      string `json:"sessionId"`
	CommandsJSON   string `json:"commandsJson,omitempty"`
	ConfigOptsJSON string `json:"configOptsJson,omitempty"`
	UpdatedAt      int64  `json:"updatedAt"`
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
	return filepath.Join(home, ".9ed", "ide.db")
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

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
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

	if err := migrateSchema(db); err != nil {
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
    work_dir TEXT NOT NULL DEFAULT '',
    acp_session_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
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

CREATE TABLE IF NOT EXISTS workspace_state (
    project_path TEXT PRIMARY KEY,
    state_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    seq INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_events_session_seq ON chat_events(session_id, seq ASC);

CREATE TABLE IF NOT EXISTS chat_session_snapshots (
    session_id TEXT PRIMARY KEY,
    commands_json TEXT NOT NULL DEFAULT '',
    config_opts_json TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
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

func (s *ChatStore) CreateSessionFull(id, agentId, title, workDir, acpSessionID string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"INSERT INTO chat_sessions (id, agent_id, title, work_dir, acp_session_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, agentId, title, workDir, acpSessionID, SessionStatusActive, now, now,
	)
	return err
}

func (s *ChatStore) UpdateSessionStatus(id, status string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"UPDATE chat_sessions SET status = ?, updated_at = ? WHERE id = ?",
		status, now, id,
	)
	return err
}

func (s *ChatStore) GetSession(id string) (*SessionRecord, error) {
	row := s.db.QueryRow(
		"SELECT id, agent_id, title, work_dir, acp_session_id, status, created_at, updated_at FROM chat_sessions WHERE id = ?",
		id,
	)
	var r SessionRecord
	if err := row.Scan(&r.ID, &r.AgentID, &r.Title, &r.WorkDir, &r.ACPSessionID, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *ChatStore) GetLastSessionForProject(workDir string) (*SessionRecord, error) {
	row := s.db.QueryRow(
		"SELECT id, agent_id, title, work_dir, acp_session_id, status, created_at, updated_at FROM chat_sessions WHERE work_dir = ? ORDER BY updated_at DESC LIMIT 1",
		workDir,
	)
	var r SessionRecord
	if err := row.Scan(&r.ID, &r.AgentID, &r.Title, &r.WorkDir, &r.ACPSessionID, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *ChatStore) GetSessionForProject(id, workDir string) (*SessionRecord, error) {
	row := s.db.QueryRow(
		"SELECT id, agent_id, title, work_dir, acp_session_id, status, created_at, updated_at FROM chat_sessions WHERE id = ? AND work_dir = ? LIMIT 1",
		id, workDir,
	)
	var r SessionRecord
	if err := row.Scan(&r.ID, &r.AgentID, &r.Title, &r.WorkDir, &r.ACPSessionID, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *ChatStore) ListSessions(limit int) ([]SessionRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		"SELECT id, agent_id, title, work_dir, acp_session_id, status, created_at, updated_at FROM chat_sessions ORDER BY updated_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionRecord
	for rows.Next() {
		var r SessionRecord
		if err := rows.Scan(&r.ID, &r.AgentID, &r.Title, &r.WorkDir, &r.ACPSessionID, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
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

// UpdateSessionACP re-links a session's ACP session ID and workDir after resume.
func (s *ChatStore) UpdateSessionACP(id, acpSessionID, workDir string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"UPDATE chat_sessions SET acp_session_id = ?, work_dir = ?, status = ?, updated_at = ? WHERE id = ?",
		acpSessionID, workDir, SessionStatusActive, now, id,
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

func (s *ChatStore) GetWorkspaceState(projectPath string) (string, error) {
	var stateJSON string
	err := s.db.QueryRow(
		"SELECT state_json FROM workspace_state WHERE project_path = ?",
		projectPath,
	).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return stateJSON, err
}

func (s *ChatStore) SaveWorkspaceState(projectPath, stateJSON string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO workspace_state (project_path, state_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(project_path) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at`,
		projectPath, stateJSON, now,
	)
	return err
}

func (s *ChatStore) AppendEvent(evt EventRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_events (id, session_id, kind, payload_json, seq, timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
		evt.ID, evt.SessionID, evt.Kind, evt.PayloadJSON, evt.Seq, evt.Timestamp,
	)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"UPDATE chat_sessions SET updated_at = ? WHERE id = ?",
		evt.Timestamp, evt.SessionID,
	)
	return err
}

func (s *ChatStore) GetEvents(sessionID string) ([]EventRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, kind, payload_json, seq, timestamp FROM chat_events WHERE session_id = ? ORDER BY seq ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventRecord
	for rows.Next() {
		var e EventRecord
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Kind, &e.PayloadJSON, &e.Seq, &e.Timestamp); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *ChatStore) SaveSnapshot(snap SessionSnapshot) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO chat_session_snapshots (session_id, commands_json, config_opts_json, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET commands_json = excluded.commands_json, config_opts_json = excluded.config_opts_json, updated_at = excluded.updated_at`,
		snap.SessionID, snap.CommandsJSON, snap.ConfigOptsJSON, now,
	)
	return err
}

func (s *ChatStore) GetSnapshot(sessionID string) (*SessionSnapshot, error) {
	row := s.db.QueryRow(
		`SELECT session_id, COALESCE(commands_json, ''), COALESCE(config_opts_json, ''), updated_at FROM chat_session_snapshots WHERE session_id = ?`,
		sessionID,
	)
	var snap SessionSnapshot
	if err := row.Scan(&snap.SessionID, &snap.CommandsJSON, &snap.ConfigOptsJSON, &snap.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

func (s *ChatStore) NextEventSeq(sessionID string) (int64, error) {
	var maxSeq sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(seq) FROM chat_events WHERE session_id = ?`, sessionID,
	).Scan(&maxSeq)
	if err != nil {
		return 0, err
	}
	if maxSeq.Valid {
		return maxSeq.Int64 + 1, nil
	}
	return 1, nil
}

func (s *ChatStore) ResolveRecordID(liveSessionID string) string {
	record, err := s.GetSession(liveSessionID)
	if err == nil && record != nil {
		return record.ID
	}
	return liveSessionID
}

func (s *ChatStore) SessionsForProject(workDir string, limit int) ([]SessionRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		"SELECT id, agent_id, title, work_dir, acp_session_id, status, created_at, updated_at FROM chat_sessions WHERE work_dir = ? ORDER BY updated_at DESC LIMIT ?",
		workDir, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionRecord
	for rows.Next() {
		var r SessionRecord
		if err := rows.Scan(&r.ID, &r.AgentID, &r.Title, &r.WorkDir, &r.ACPSessionID, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, r)
	}
	return sessions, rows.Err()
}

func migrateSchema(db *sql.DB) error {
	var cols []string
	rows, err := db.Query("PRAGMA table_info(chat_sessions)")
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			rows.Close()
			return err
		}
		cols = append(cols, name)
	}
	rows.Close()

	hasWorkDir := false
	hasACP := false
	hasStatus := false
	for _, c := range cols {
		switch c {
		case "work_dir":
			hasWorkDir = true
		case "acp_session_id":
			hasACP = true
		case "status":
			hasStatus = true
		}
	}

	if !hasWorkDir {
		if _, err := db.Exec("ALTER TABLE chat_sessions ADD COLUMN work_dir TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if !hasACP {
		if _, err := db.Exec("ALTER TABLE chat_sessions ADD COLUMN acp_session_id TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if !hasStatus {
		if _, err := db.Exec("ALTER TABLE chat_sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'active'"); err != nil {
			return err
		}
	}

	var idxCols []string
	idxRows, err := db.Query("PRAGMA index_list(chat_sessions)")
	if err != nil {
		return err
	}
	for idxRows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			idxRows.Close()
			return err
		}
		idxCols = append(idxCols, name)
	}
	idxRows.Close()

	hasWorkDirIdx := false
	for _, idx := range idxCols {
		if idx == "idx_sessions_workdir" {
			hasWorkDirIdx = true
		}
	}
	if !hasWorkDirIdx {
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_workdir ON chat_sessions(work_dir, updated_at DESC)"); err != nil {
			return err
		}
	}

	return nil
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
