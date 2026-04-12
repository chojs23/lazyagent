package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/chojs23/lazyagent/internal/model"
)

type Store struct {
	db *sql.DB
}

type Queries struct {
	db dbtx
}

type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func Open(dbPath string) (*Store, error) {
	dsn := dbPath + "?_busy_timeout=5000&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Read() *Queries { return &Queries{db: s.db} }

func (s *Store) WithTx(ctx context.Context, fn func(*Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(&Queries{db: tx}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) HealthCheck(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "SELECT 1")
	return err
}

func (s *Store) init(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA cache_size = -64000",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 30000000",
	}
	for _, p := range pragmas {
		if _, err := s.db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	ddl := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			transcript_path TEXT,
			metadata TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			parent_session_id TEXT REFERENCES sessions(id),
			project_id INTEGER NOT NULL REFERENCES projects(id),
			slug TEXT,
			status TEXT DEFAULT 'active',
			runtime TEXT NOT NULL DEFAULT 'claude',
			started_at INTEGER NOT NULL,
			stopped_at INTEGER,
			transcript_path TEXT,
			metadata TEXT,
			event_count INTEGER NOT NULL DEFAULT 0,
			agent_count INTEGER NOT NULL DEFAULT 0,
			last_activity INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			parent_agent_id TEXT REFERENCES agents(id),
			name TEXT,
			description TEXT,
			agent_type TEXT,
			agent_class TEXT DEFAULT 'claude-code',
			transcript_path TEXT,
			metadata TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id TEXT NOT NULL REFERENCES agents(id),
			session_id TEXT NOT NULL REFERENCES sessions(id),
			type TEXT NOT NULL,
			subtype TEXT,
			tool_name TEXT,
			timestamp INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			payload TEXT NOT NULL,
			tool_use_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS pending_agent_spawns (
			tool_use_id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			owner_agent_id TEXT,
			name TEXT,
			description TEXT,
			agent_type TEXT,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pending_agent_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			owner_agent_id TEXT,
			name TEXT,
			description TEXT,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects(slug)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_transcript_path ON projects(transcript_path)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session_agent ON events(session_id, agent_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_type ON events(type, subtype)`,
		`CREATE INDEX IF NOT EXISTS idx_events_tool_use_id ON events(tool_use_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_session ON agents(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_parent ON agents(parent_agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pending_agent_queue_session ON pending_agent_queue(session_id, id)`,
	}
	for _, stmt := range ddl {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ddl: %w", err)
		}
	}

	// migrations for existing databases
	migrations := []string{
		`ALTER TABLE sessions ADD COLUMN runtime TEXT NOT NULL DEFAULT 'claude'`,
		`ALTER TABLE sessions ADD COLUMN parent_session_id TEXT REFERENCES sessions(id)`,
		`ALTER TABLE projects ADD COLUMN directory TEXT`,
		`ALTER TABLE agents ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`,
	}
	for _, m := range migrations {
		s.db.ExecContext(ctx, m) // ignore errors (column may already exist)
	}

	// idempotent index creation for new columns
	s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_directory ON projects(directory) WHERE directory IS NOT NULL`)

	// Backfill: mark agents as stopped when evidence of completion exists.
	// This covers agents created before the status column was added.
	// 1) Claude subagents with a SubagentStop event
	s.db.ExecContext(ctx, `
		UPDATE agents SET status = 'stopped'
		WHERE status = 'active' AND id IN (
			SELECT DISTINCT agent_id FROM events WHERE subtype = 'SubagentStop'
		)`)
	// 2) OpenCode agents whose session is already stopped
	s.db.ExecContext(ctx, `
		UPDATE agents SET status = 'stopped'
		WHERE status = 'active' AND id IN (
			SELECT a.id FROM agents a
			JOIN sessions s ON a.id = s.id
			WHERE s.status = 'stopped'
		)`)

	return nil
}

// ── Projects ──

func (q *Queries) CreateProject(ctx context.Context, slug, name, directory, transcriptPath string) (int64, error) {
	now := nowMillis()
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO projects (slug, name, directory, transcript_path, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		slug, name, nullIfEmpty(directory), nullIfEmpty(transcriptPath), now, now)
	if err != nil {
		return 0, fmt.Errorf("create project: %w", err)
	}
	return res.LastInsertId()
}

func (q *Queries) GetProjectBySlug(ctx context.Context, slug string) (*model.Project, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, slug, name, COALESCE(directory,''), COALESCE(transcript_path,''), COALESCE(metadata,''), 0, created_at, updated_at FROM projects WHERE slug = ?`, slug)
	return scanProject(row)
}

func (q *Queries) GetProjectByDirectory(ctx context.Context, dir string) (*model.Project, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, slug, name, COALESCE(directory,''), COALESCE(transcript_path,''), COALESCE(metadata,''), 0, created_at, updated_at FROM projects WHERE directory = ?`, dir)
	return scanProject(row)
}

func (q *Queries) GetProjectByTranscriptPath(ctx context.Context, path string) (*model.Project, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, slug, name, COALESCE(directory,''), COALESCE(transcript_path,''), COALESCE(metadata,''), 0, created_at, updated_at FROM projects WHERE transcript_path = ?`, path)
	return scanProject(row)
}

func (q *Queries) UpdateProjectDirectory(ctx context.Context, projectID int64, dir string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE projects SET directory=?, updated_at=? WHERE id=?`, dir, nowMillis(), projectID)
	return err
}

func (q *Queries) IsSlugAvailable(ctx context.Context, slug string) (bool, error) {
	var id int64
	err := q.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE slug = ?`, slug).Scan(&id)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (q *Queries) UpdateProjectName(ctx context.Context, projectID int64, name string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE projects SET name=?, updated_at=? WHERE id=?`, name, nowMillis(), projectID)
	return err
}

func (q *Queries) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.name, COALESCE(p.directory,''), COALESCE(p.transcript_path,''), COALESCE(p.metadata,''),
			COUNT(DISTINCT s.id), p.created_at, p.updated_at
		FROM projects p
		LEFT JOIN sessions s ON s.project_id = p.id
		GROUP BY p.id
		ORDER BY p.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Directory, &p.TranscriptPath, &p.Metadata, &p.SessionCount, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (q *Queries) DeleteProject(ctx context.Context, projectID int64) error {
	// cascade: events → agents → sessions → project
	_, err := q.db.ExecContext(ctx, `DELETE FROM events WHERE session_id IN (SELECT id FROM sessions WHERE project_id = ?)`, projectID)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `DELETE FROM agents WHERE session_id IN (SELECT id FROM sessions WHERE project_id = ?)`, projectID)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `DELETE FROM sessions WHERE project_id = ?`, projectID)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID)
	return err
}

// ── Sessions ──

func (q *Queries) UpsertSession(ctx context.Context, id string, parentSessionID string, projectID int64, slug string, runtime string, metadata map[string]any, timestamp int64, transcriptPath string) error {
	now := nowMillis()
	metaJSON, _ := jsonString(metadata)
	if runtime == "" {
		runtime = "claude"
	}
	_, err := q.db.ExecContext(ctx, `
		INSERT INTO sessions (id, parent_session_id, project_id, slug, status, runtime, started_at, transcript_path, metadata, created_at, updated_at)
		VALUES (?,?,?,?,'active',?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			parent_session_id = COALESCE(excluded.parent_session_id, sessions.parent_session_id),
			slug = COALESCE(excluded.slug, sessions.slug),
			runtime = excluded.runtime,
			transcript_path = COALESCE(excluded.transcript_path, sessions.transcript_path),
			metadata = COALESCE(excluded.metadata, sessions.metadata),
			updated_at = ?`,
		id, nullIfEmpty(parentSessionID), projectID, nullIfEmpty(slug), runtime, timestamp, nullIfEmpty(transcriptPath), metaJSON, now, now, now)
	return err
}

func (q *Queries) GetSessionByID(ctx context.Context, id string) (*model.Session, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT s.id, COALESCE(s.parent_session_id,''), s.project_id, COALESCE(p.slug,''), COALESCE(p.name,''),
			COALESCE(s.slug,''), COALESCE(s.status,''), COALESCE(s.runtime,'claude'), s.started_at, COALESCE(s.stopped_at,0),
			COALESCE(s.transcript_path,''), COALESCE(s.metadata,''),
			s.event_count, s.agent_count, COALESCE(s.last_activity,0), s.created_at, s.updated_at
		FROM sessions s LEFT JOIN projects p ON p.id = s.project_id
		WHERE s.id = ?`, id)
	return scanSession(row)
}

func (q *Queries) ListSessionsForProject(ctx context.Context, projectID int64) ([]model.Session, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT s.id, COALESCE(s.parent_session_id,''), s.project_id, COALESCE(p.slug,''), COALESCE(p.name,''),
			COALESCE(s.slug,''), COALESCE(s.status,''), COALESCE(s.runtime,'claude'), s.started_at, COALESCE(s.stopped_at,0),
			COALESCE(s.transcript_path,''), COALESCE(s.metadata,''),
			s.event_count, s.agent_count, COALESCE(s.last_activity,0), s.created_at, s.updated_at
		FROM sessions s LEFT JOIN projects p ON p.id = s.project_id
		WHERE s.project_id = ? AND (s.parent_session_id IS NULL OR s.parent_session_id = '')
		ORDER BY s.started_at DESC, s.created_at DESC, s.id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

func (q *Queries) ListRecentSessions(ctx context.Context, limit int) ([]model.Session, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT s.id, COALESCE(s.parent_session_id,''), s.project_id, COALESCE(p.slug,''), COALESCE(p.name,''),
			COALESCE(s.slug,''), COALESCE(s.status,''), COALESCE(s.runtime,'claude'), s.started_at, COALESCE(s.stopped_at,0),
			COALESCE(s.transcript_path,''), COALESCE(s.metadata,''),
			s.event_count, s.agent_count, COALESCE(s.last_activity,0), s.created_at, s.updated_at
		FROM sessions s JOIN projects p ON p.id = s.project_id
		WHERE (s.parent_session_id IS NULL OR s.parent_session_id = '')
		ORDER BY s.started_at DESC, s.created_at DESC, s.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

// HasActiveChildSessions returns true if any direct child session of the given
// parent is still in "active" status.
func (q *Queries) HasActiveChildSessions(ctx context.Context, parentSessionID string) (bool, error) {
	var exists bool
	err := q.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM sessions WHERE parent_session_id = ? AND status = 'active')`,
		parentSessionID).Scan(&exists)
	return exists, err
}

// HasSessionIdleEvent returns true if the given session has a recorded idle
// signal: either a deprecated "Stop" (session.idle) event or a "SessionStatus"
// event with status_type "idle" in its payload.
func (q *Queries) HasSessionIdleEvent(ctx context.Context, sessionID string) (bool, error) {
	var exists bool
	err := q.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM events WHERE session_id = ? AND (
				subtype = 'Stop'
				OR (subtype = 'SessionStatus' AND json_extract(payload, '$.status_type') = 'idle')
			)
		)`,
		sessionID).Scan(&exists)
	return exists, err
}

func (q *Queries) UpdateSessionStatus(ctx context.Context, id, status string) error {
	var stoppedAt any
	if status == "stopped" {
		stoppedAt = nowMillis()
	}
	_, err := q.db.ExecContext(ctx, `UPDATE sessions SET status=?, stopped_at=?, updated_at=? WHERE id=?`,
		status, stoppedAt, nowMillis(), id)
	return err
}

func (q *Queries) UpdateSessionSlug(ctx context.Context, id, slug string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE sessions SET slug=?, updated_at=? WHERE id=?`, slug, nowMillis(), id)
	return err
}

func (q *Queries) UpdateSessionProject(ctx context.Context, id string, projectID int64) error {
	_, err := q.db.ExecContext(ctx, `UPDATE sessions SET project_id=?, updated_at=? WHERE id=?`, projectID, nowMillis(), id)
	return err
}

func (q *Queries) DeleteSession(ctx context.Context, id string) error {
	// Delete child sessions first to satisfy the parent_session_id FK constraint.
	children, err := q.listChildSessionIDs(ctx, id)
	if err != nil {
		return err
	}
	for _, childID := range children {
		if err := q.DeleteSession(ctx, childID); err != nil {
			return err
		}
	}

	if _, err := q.db.ExecContext(ctx, `DELETE FROM events WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := q.db.ExecContext(ctx, `DELETE FROM agents WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := q.db.ExecContext(ctx, `DELETE FROM pending_agent_spawns WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := q.db.ExecContext(ctx, `DELETE FROM pending_agent_queue WHERE session_id = ?`, id); err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (q *Queries) listChildSessionIDs(ctx context.Context, parentID string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT id FROM sessions WHERE parent_session_id = ?`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (q *Queries) ClearSessionEvents(ctx context.Context, id string) error {
	// Use a recursive CTE to clear events and agents for the entire session
	// tree, matching the recursive scope used by ListEventsForSessionTree and
	// ListAgentsForSessionTree. Without this, child-session data would remain
	// visible in the TUI after clearing the parent.
	const treeCTE = `WITH RECURSIVE session_tree(id) AS (
		SELECT ?1
		UNION ALL
		SELECT s.id FROM sessions s
		JOIN session_tree st ON s.parent_session_id = st.id
	) `

	if _, err := q.db.ExecContext(ctx, treeCTE+`DELETE FROM events WHERE session_id IN (SELECT id FROM session_tree)`, id); err != nil {
		return err
	}
	if _, err := q.db.ExecContext(ctx, treeCTE+`DELETE FROM agents WHERE session_id IN (SELECT id FROM session_tree)`, id); err != nil {
		return err
	}
	now := nowMillis()
	_, err := q.db.ExecContext(ctx, treeCTE+`UPDATE sessions SET event_count=0, agent_count=0, last_activity=NULL, updated_at=? WHERE id IN (SELECT id FROM session_tree)`, id, now)
	return err
}

// ReapStaleSessions marks active sessions as stopped if they have no recent
// activity and no active child sessions. This handles cases where the runtime
// (OpenCode/Claude) was killed without sending a stop event. It also marks
// the corresponding root agents as stopped.
func (q *Queries) ReapStaleSessions(ctx context.Context, maxIdleMs int64) (int, error) {
	cutoff := nowMillis() - maxIdleMs
	// Find stale active sessions that have no active children.
	rows, err := q.db.QueryContext(ctx, `
		SELECT s.id FROM sessions s
		WHERE s.status = 'active'
			AND COALESCE(s.last_activity, s.started_at) < ?
			AND NOT EXISTS (
				SELECT 1 FROM sessions c
				WHERE c.parent_session_id = s.id AND c.status = 'active'
			)`, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	now := nowMillis()
	for _, id := range ids {
		if _, err := q.db.ExecContext(ctx, `UPDATE sessions SET status='stopped', stopped_at=?, updated_at=? WHERE id=?`, now, now, id); err != nil {
			return 0, err
		}
		if _, err := q.db.ExecContext(ctx, `UPDATE agents SET status='stopped', updated_at=? WHERE id=? AND status='active'`, now, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// ── Agents ──

func (q *Queries) UpsertAgent(ctx context.Context, id, sessionID, parentAgentID, name, description, agentType, transcriptPath string) error {
	existing, err := q.GetAgentByID(ctx, id)
	if err != nil {
		return err
	}
	now := nowMillis()
	_, err = q.db.ExecContext(ctx, `
		INSERT INTO agents (id, session_id, parent_agent_id, name, description, agent_type, transcript_path, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			parent_agent_id = COALESCE(agents.parent_agent_id, excluded.parent_agent_id),
			name = COALESCE(excluded.name, agents.name),
			description = COALESCE(excluded.description, agents.description),
			agent_type = COALESCE(excluded.agent_type, agents.agent_type),
			transcript_path = COALESCE(excluded.transcript_path, agents.transcript_path),
			updated_at = ?`,
		id, sessionID, nullIfEmpty(parentAgentID), nullIfEmpty(name), nullIfEmpty(description),
		nullIfEmpty(agentType), nullIfEmpty(transcriptPath), now, now, now)
	if err != nil {
		return err
	}
	if existing == nil {
		_, err = q.db.ExecContext(ctx, `UPDATE sessions SET agent_count = agent_count + 1 WHERE id = ?`, sessionID)
	}
	return err
}

func (q *Queries) UpdateAgentStatus(ctx context.Context, id, status string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE agents SET status=?, updated_at=? WHERE id=?`,
		status, nowMillis(), id)
	return err
}

func (q *Queries) UpdateAgentClass(ctx context.Context, id, agentClass string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE agents SET agent_class=?, updated_at=? WHERE id=?`,
		agentClass, nowMillis(), id)
	return err
}

func (q *Queries) GetAgentByID(ctx context.Context, id string) (*model.Agent, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, session_id, COALESCE(parent_agent_id,''), COALESCE(name,''), COALESCE(description,''),
			COALESCE(agent_type,''), COALESCE(agent_class,''), COALESCE(status,'active'), COALESCE(transcript_path,''), COALESCE(metadata,''),
			created_at, updated_at
		FROM agents WHERE id = ?`, id)
	var a model.Agent
	err := row.Scan(&a.ID, &a.SessionID, &a.ParentAgentID, &a.Name, &a.Description,
		&a.AgentType, &a.AgentClass, &a.Status, &a.TranscriptPath, &a.Metadata, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (q *Queries) ListAgentsForSession(ctx context.Context, sessionID string) ([]model.Agent, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, session_id, COALESCE(parent_agent_id,''), COALESCE(name,''), COALESCE(description,''),
			COALESCE(agent_type,''), COALESCE(agent_class,''), COALESCE(status,'active'), COALESCE(transcript_path,''), COALESCE(metadata,''),
			created_at, updated_at
		FROM agents WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Agent
	for rows.Next() {
		var a model.Agent
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ParentAgentID, &a.Name, &a.Description,
			&a.AgentType, &a.AgentClass, &a.Status, &a.TranscriptPath, &a.Metadata, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAgentsForSessionTree returns agents from the session and all its
// descendant sessions (recursive). Child-session agents get their
// parent_agent_id set to their parent session's ID so they render as a
// proper tree in the agents pane.
func (q *Queries) ListAgentsForSessionTree(ctx context.Context, sessionID string) ([]model.Agent, error) {
	rows, err := q.db.QueryContext(ctx, `
		WITH RECURSIVE session_tree(id, parent) AS (
			SELECT ?1, ''
			UNION ALL
			SELECT s.id, s.parent_session_id FROM sessions s
			JOIN session_tree st ON s.parent_session_id = st.id
		)
		SELECT a.id, a.session_id,
			CASE WHEN a.session_id != ?1 THEN COALESCE(
				(SELECT parent FROM session_tree WHERE id = a.session_id), ?1
			) ELSE COALESCE(a.parent_agent_id,'') END,
			COALESCE(a.name,''), COALESCE(a.description,''),
			COALESCE(a.agent_type,''), COALESCE(a.agent_class,''), COALESCE(a.status,'active'), COALESCE(a.transcript_path,''), COALESCE(a.metadata,''),
			a.created_at, a.updated_at
		FROM agents a
		WHERE a.session_id IN (SELECT id FROM session_tree)
		ORDER BY a.created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Agent
	for rows.Next() {
		var a model.Agent
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ParentAgentID, &a.Name, &a.Description,
			&a.AgentType, &a.AgentClass, &a.Status, &a.TranscriptPath, &a.Metadata, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (q *Queries) CountEventsForSessionTree(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := q.db.QueryRowContext(ctx, `
		WITH RECURSIVE session_tree(id) AS (
			SELECT ?1
			UNION ALL
			SELECT s.id FROM sessions s
			JOIN session_tree st ON s.parent_session_id = st.id
		)
		SELECT COUNT(*) FROM events
		WHERE session_id IN (SELECT id FROM session_tree)`, sessionID).Scan(&count)
	return count, err
}

// appendEventFilterConditions keeps the event filter predicate assembly in one
// place so tree count and list queries cannot drift on agent, type, subtype,
// or payload search handling.
func appendEventFilterConditions(parts []string, args []any, f model.EventFilter) ([]string, []any) {
	if len(f.AgentIDs) > 0 {
		placeholders := make([]string, len(f.AgentIDs))
		for i, id := range f.AgentIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		parts = append(parts, fmt.Sprintf("AND agent_id IN (%s)", strings.Join(placeholders, ",")))
	}
	if f.Type != "" {
		parts = append(parts, "AND type = ?")
		args = append(args, f.Type)
	}
	if f.Subtype != "" {
		parts = append(parts, "AND subtype = ?")
		args = append(args, f.Subtype)
	}
	if f.Search != "" {
		parts = append(parts, "AND payload LIKE ?")
		args = append(args, "%"+f.Search+"%")
	}
	return parts, args
}

// CountFilteredEventsForSessionTree counts events in the session tree that
// match the given filter conditions. When no filter fields are set this
// behaves identically to CountEventsForSessionTree.
func (q *Queries) CountFilteredEventsForSessionTree(ctx context.Context, sessionID string, f model.EventFilter) (int, error) {
	cte := `WITH RECURSIVE session_tree(id) AS (
		SELECT ?1
		UNION ALL
		SELECT s.id FROM sessions s
		JOIN session_tree st ON s.parent_session_id = st.id
	) `
	parts := []string{cte + "SELECT COUNT(*) FROM events WHERE session_id IN (SELECT id FROM session_tree)"}
	args := []any{sessionID}
	parts, args = appendEventFilterConditions(parts, args, f)

	var count int
	err := q.db.QueryRowContext(ctx, strings.Join(parts, " "), args...).Scan(&count)
	return count, err
}

func (q *Queries) ListEventsForSessionTree(ctx context.Context, sessionID string, f model.EventFilter) ([]model.Event, error) {
	cte := `WITH RECURSIVE session_tree(id) AS (
		SELECT ?1
		UNION ALL
		SELECT s.id FROM sessions s
		JOIN session_tree st ON s.parent_session_id = st.id
	) `
	parts := []string{cte + "SELECT id, agent_id, session_id, type, COALESCE(subtype,''), COALESCE(tool_name,''), COALESCE(tool_use_id,''), timestamp, created_at, payload FROM events WHERE session_id IN (SELECT id FROM session_tree)"}
	args := []any{sessionID}
	parts, args = appendEventFilterConditions(parts, args, f)
	parts = append(parts, "ORDER BY timestamp ASC")
	if f.Limit > 0 {
		parts = append(parts, "LIMIT ?")
		args = append(args, f.Limit)
		if f.Offset > 0 {
			parts = append(parts, "OFFSET ?")
			args = append(args, f.Offset)
		}
	}

	rows, err := q.db.QueryContext(ctx, strings.Join(parts, " "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (q *Queries) UpdateAgentType(ctx context.Context, id, agentType string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE agents SET agent_type=?, updated_at=? WHERE id=?`, agentType, nowMillis(), id)
	return err
}

func (q *Queries) UpdateAgentName(ctx context.Context, id, name string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE agents SET name=?, updated_at=? WHERE id=?`, name, nowMillis(), id)
	return err
}

// ── Events ──

func (q *Queries) InsertEvent(ctx context.Context, event model.Event) (int64, error) {
	now := nowMillis()
	res, err := q.db.ExecContext(ctx, `
		INSERT INTO events (agent_id, session_id, type, subtype, tool_name, timestamp, created_at, payload, tool_use_id)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		event.AgentID, event.SessionID, event.Type,
		nullIfEmpty(event.Subtype), nullIfEmpty(event.ToolName),
		event.Timestamp, now, event.Payload, nullIfEmpty(event.ToolUseID))
	if err != nil {
		return 0, err
	}
	_, err = q.db.ExecContext(ctx, `
		UPDATE sessions SET event_count = event_count + 1,
			last_activity = MAX(COALESCE(last_activity,0), ?), updated_at = ?
		WHERE id = ?`, event.Timestamp, now, event.SessionID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (q *Queries) CountEventsForSession(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := q.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE session_id = ?", sessionID).Scan(&count)
	return count, err
}

func (q *Queries) ListEventsForSession(ctx context.Context, sessionID string, f model.EventFilter) ([]model.Event, error) {
	parts := []string{"SELECT id, agent_id, session_id, type, COALESCE(subtype,''), COALESCE(tool_name,''), COALESCE(tool_use_id,''), timestamp, created_at, payload FROM events WHERE session_id = ?"}
	args := []any{sessionID}

	if len(f.AgentIDs) > 0 {
		placeholders := make([]string, len(f.AgentIDs))
		for i, id := range f.AgentIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		parts = append(parts, fmt.Sprintf("AND agent_id IN (%s)", strings.Join(placeholders, ",")))
	}
	if f.Type != "" {
		parts = append(parts, "AND type = ?")
		args = append(args, f.Type)
	}
	if f.Subtype != "" {
		parts = append(parts, "AND subtype = ?")
		args = append(args, f.Subtype)
	}
	if f.Search != "" {
		parts = append(parts, "AND payload LIKE ?")
		args = append(args, "%"+f.Search+"%")
	}
	parts = append(parts, "ORDER BY timestamp ASC")
	if f.Limit > 0 {
		parts = append(parts, "LIMIT ?")
		args = append(args, f.Limit)
		if f.Offset > 0 {
			parts = append(parts, "OFFSET ?")
			args = append(args, f.Offset)
		}
	}

	rows, err := q.db.QueryContext(ctx, strings.Join(parts, " "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (q *Queries) ListEventsForAgent(ctx context.Context, agentID string) ([]model.Event, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, agent_id, session_id, type, COALESCE(subtype,''), COALESCE(tool_name,''), COALESCE(tool_use_id,''),
			timestamp, created_at, payload
		FROM events WHERE agent_id = ? ORDER BY timestamp ASC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (q *Queries) GetEventByID(ctx context.Context, id int64) (*model.Event, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, agent_id, session_id, type, COALESCE(subtype,''), COALESCE(tool_name,''), COALESCE(tool_use_id,''),
			timestamp, created_at, payload
		FROM events WHERE id = ?`, id)
	var e model.Event
	err := row.Scan(&e.ID, &e.AgentID, &e.SessionID, &e.Type, &e.Subtype, &e.ToolName, &e.ToolUseID,
		&e.Timestamp, &e.CreatedAt, &e.Payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (q *Queries) GetEventThread(ctx context.Context, eventID int64) ([]model.Event, error) {
	ev, err := q.GetEventByID(ctx, eventID)
	if err != nil || ev == nil {
		return nil, err
	}

	// subagent events: return all events for that agent
	if ev.AgentID != ev.SessionID {
		return q.ListEventsForAgent(ctx, ev.AgentID)
	}

	// root agent: find turn boundaries
	var startTS int64
	err = q.db.QueryRowContext(ctx, `
		SELECT timestamp FROM events
		WHERE session_id = ? AND subtype = 'UserPromptSubmit' AND timestamp <= ?
		ORDER BY timestamp DESC LIMIT 1`, ev.SessionID, ev.Timestamp).Scan(&startTS)
	if err == sql.ErrNoRows {
		startTS = 0
	} else if err != nil {
		return nil, err
	}

	var endTS int64
	err = q.db.QueryRowContext(ctx, `
		SELECT timestamp FROM events
		WHERE session_id = ? AND timestamp > ?
			AND (subtype = 'UserPromptSubmit' OR subtype = 'Stop' OR subtype = 'SubagentStop')
		ORDER BY timestamp ASC LIMIT 1`, ev.SessionID, ev.Timestamp).Scan(&endTS)
	if err == sql.ErrNoRows {
		// no end boundary: return everything from start
		rows, err := q.db.QueryContext(ctx, `
			SELECT id, agent_id, session_id, type, COALESCE(subtype,''), COALESCE(tool_name,''), COALESCE(tool_use_id,''),
				timestamp, created_at, payload
			FROM events WHERE session_id = ? AND timestamp >= ? ORDER BY timestamp ASC`, ev.SessionID, startTS)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanEvents(rows)
	} else if err != nil {
		return nil, err
	}

	rows, err := q.db.QueryContext(ctx, `
		SELECT id, agent_id, session_id, type, COALESCE(subtype,''), COALESCE(tool_name,''), COALESCE(tool_use_id,''),
			timestamp, created_at, payload
		FROM events WHERE session_id = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC`,
		ev.SessionID, startTS, endTS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ── Pending Agent ──

func (q *Queries) AddPendingAgentSpawn(ctx context.Context, spawn model.PendingAgentSpawn) error {
	if spawn.ToolUseID == "" {
		return nil
	}
	now := nowMillis()
	_, err := q.db.ExecContext(ctx, `
		INSERT INTO pending_agent_spawns (tool_use_id, session_id, owner_agent_id, name, description, agent_type, created_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(tool_use_id) DO UPDATE SET
			session_id = excluded.session_id,
			owner_agent_id = COALESCE(excluded.owner_agent_id, pending_agent_spawns.owner_agent_id),
			name = COALESCE(excluded.name, pending_agent_spawns.name),
			description = COALESCE(excluded.description, pending_agent_spawns.description),
			agent_type = COALESCE(excluded.agent_type, pending_agent_spawns.agent_type),
			created_at = excluded.created_at`,
		spawn.ToolUseID, spawn.SessionID, nullIfEmpty(spawn.OwnerAgentID),
		nullIfEmpty(spawn.Name), nullIfEmpty(spawn.Description), nullIfEmpty(spawn.AgentType), now)
	if err != nil {
		return err
	}
	if spawn.Name == "" && spawn.Description == "" {
		return nil
	}
	_, err = q.db.ExecContext(ctx, `
		INSERT INTO pending_agent_queue (session_id, owner_agent_id, name, description, created_at)
		VALUES (?,?,?,?,?)`,
		spawn.SessionID, nullIfEmpty(spawn.OwnerAgentID), nullIfEmpty(spawn.Name), nullIfEmpty(spawn.Description), now)
	return err
}

func (q *Queries) PopPendingAgentQueue(ctx context.Context, sessionID string) (*model.PendingAgentSpawn, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, session_id, COALESCE(owner_agent_id,''), COALESCE(name,''), COALESCE(description,''), created_at
		FROM pending_agent_queue WHERE session_id = ? ORDER BY id ASC LIMIT 1`, sessionID)
	var queueID int64
	s := &model.PendingAgentSpawn{}
	err := row.Scan(&queueID, &s.SessionID, &s.OwnerAgentID, &s.Name, &s.Description, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = q.db.ExecContext(ctx, `DELETE FROM pending_agent_queue WHERE id = ?`, queueID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (q *Queries) TakePendingAgentSpawn(ctx context.Context, toolUseID string) (*model.PendingAgentSpawn, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT tool_use_id, session_id, COALESCE(owner_agent_id,''), COALESCE(name,''), COALESCE(description,''),
			COALESCE(agent_type,''), created_at
		FROM pending_agent_spawns WHERE tool_use_id = ?`, toolUseID)
	s := &model.PendingAgentSpawn{}
	err := row.Scan(&s.ToolUseID, &s.SessionID, &s.OwnerAgentID, &s.Name, &s.Description, &s.AgentType, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = q.db.ExecContext(ctx, `DELETE FROM pending_agent_spawns WHERE tool_use_id = ?`, toolUseID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ── Utility ──

func (q *Queries) ClearAllData(ctx context.Context) error {
	for _, t := range []string{"events", "agents", "pending_agent_spawns", "pending_agent_queue", "sessions", "projects"} {
		if _, err := q.db.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return err
		}
	}
	return nil
}

// ── scan helpers ──

func scanProject(row *sql.Row) (*model.Project, error) {
	var p model.Project
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.Directory, &p.TranscriptPath, &p.Metadata, &p.SessionCount, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanSession(row *sql.Row) (*model.Session, error) {
	var s model.Session
	err := row.Scan(&s.ID, &s.ParentSessionID, &s.ProjectID, &s.ProjectSlug, &s.ProjectName,
		&s.Slug, &s.Status, &s.Runtime, &s.StartedAt, &s.StoppedAt,
		&s.TranscriptPath, &s.Metadata, &s.EventCount, &s.AgentCount,
		&s.LastActivity, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func scanSessions(rows *sql.Rows) ([]model.Session, error) {
	var out []model.Session
	for rows.Next() {
		var s model.Session
		if err := rows.Scan(&s.ID, &s.ParentSessionID, &s.ProjectID, &s.ProjectSlug, &s.ProjectName,
			&s.Slug, &s.Status, &s.Runtime, &s.StartedAt, &s.StoppedAt,
			&s.TranscriptPath, &s.Metadata, &s.EventCount, &s.AgentCount,
			&s.LastActivity, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanEvents(rows *sql.Rows) ([]model.Event, error) {
	var out []model.Event
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.ID, &e.AgentID, &e.SessionID, &e.Type, &e.Subtype, &e.ToolName, &e.ToolUseID,
			&e.Timestamp, &e.CreatedAt, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func jsonString(v map[string]any) (any, error) {
	if len(v) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}
