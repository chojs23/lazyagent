package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chojs23/lazyagent/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestIngestSessionStartCreatesProjectAndSession(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	payload := map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "sess-1",
		"transcript_path": "/home/user/.claude/projects/-home-user-my-app/session.jsonl",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}

	result, err := IngestClaudeEvent(ctx, st, payload, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "sess-1" {
		t.Fatalf("got sessionID=%q", result.SessionID)
	}
	if result.ProjectID == 0 {
		t.Fatal("expected non-zero projectID")
	}
	if result.EventID == 0 {
		t.Fatal("expected non-zero eventID")
	}

	session, err := st.Read().GetSessionByID(ctx, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Status != "active" {
		t.Fatalf("got status=%q", session.Status)
	}
	if session.EventCount != 1 {
		t.Fatalf("got eventCount=%d", session.EventCount)
	}
}

func TestIngestAgentSpawnAndSubagentNaming(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. SessionStart
	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "sess-1",
		"transcript_path": "/home/user/.claude/projects/-home-user-my-app/s.jsonl",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}, "")

	// 2. PreToolUse Agent — stash name
	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-1",
		"tool_name":       "Agent",
		"tool_use_id":     "tu-1",
		"tool_input":      map[string]any{"name": "planner", "description": "Plans things", "subagent_type": "Plan"},
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	}, "")

	// 3. Subagent event arrives (agent_id = subagent)
	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "Notification",
		"session_id":      "sess-1",
		"agent_id":        "agent-sub-1",
		"meta":            map[string]any{"timestamp": float64(1712700002000)},
	}, "")

	// Verify subagent got named from queue
	agent, err := st.Read().GetAgentByID(ctx, "agent-sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent == nil {
		t.Fatal("subagent not found")
	}
	if agent.Name != "planner" {
		t.Fatalf("got agent name=%q, want planner", agent.Name)
	}
	if agent.Description != "Plans things" {
		t.Fatalf("got agent description=%q", agent.Description)
	}

	// 4. PostToolUse Agent — realize subagent with tool_use_id correlation
	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      "sess-1",
		"tool_name":       "Agent",
		"tool_use_id":     "tu-1",
		"tool_response":   map[string]any{"agentId": "agent-sub-1"},
		"meta":            map[string]any{"timestamp": float64(1712700003000)},
	}, "")

	session, err := st.Read().GetSessionByID(ctx, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.EventCount != 4 {
		t.Fatalf("got eventCount=%d, want 4", session.EventCount)
	}
	if session.AgentCount < 2 {
		t.Fatalf("got agentCount=%d, want >=2", session.AgentCount)
	}
}

func TestIngestSessionEnd(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "sess-1",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}, "")

	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionEnd",
		"session_id":      "sess-1",
		"meta":            map[string]any{"timestamp": float64(1712700010000)},
	}, "")

	session, err := st.Read().GetSessionByID(ctx, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "stopped" {
		t.Fatalf("got status=%q, want stopped", session.Status)
	}
}

func TestIngestOpenCodeSessionIdleMarksStopped(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.idle",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700010000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "opencode-1")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Status != "stopped" {
		t.Fatalf("got status=%q, want stopped", session.Status)
	}
}

func TestIngestOpenCodeSessionDeletedMarksStopped(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.deleted",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700010000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "opencode-1")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Status != "stopped" {
		t.Fatalf("got status=%q, want stopped", session.Status)
	}
}

func TestIngestOpenCodeEventReactivatesStoppedSession(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.idle",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700010000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "tool.execute.before",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"tool":        "Read",
		"call_id":     "call-1",
		"timestamp":   float64(1712700020000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "opencode-1")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Status != "active" {
		t.Fatalf("got status=%q, want active", session.Status)
	}
}

func TestIngestProjectSlugOverride(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	result, err := IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "sess-1",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}, "my-custom-project")
	if err != nil {
		t.Fatal(err)
	}

	proj, err := st.Read().GetProjectBySlug(ctx, "my-custom-project")
	if err != nil {
		t.Fatal(err)
	}
	if proj == nil {
		t.Fatal("project not created")
	}
	if result.ProjectID != proj.ID {
		t.Fatalf("project ID mismatch: result=%d proj=%d", result.ProjectID, proj.ID)
	}
}

func TestDeriveSlugCandidates(t *testing.T) {
	cases := []struct {
		input string
		want  string // first candidate
	}{
		{"/home/user/.claude/projects/-home-user-my-app/s.jsonl", "my-app"},
		{"/home/user/.claude/projects/-Users-my-new-project/s.jsonl", "new-project"},
		{"/x/-a/s.jsonl", "a"},
	}
	for _, tc := range cases {
		candidates := deriveSlugCandidates(tc.input)
		if len(candidates) == 0 {
			t.Fatalf("no candidates for %q", tc.input)
		}
		if candidates[0] != tc.want {
			t.Fatalf("deriveSlugCandidates(%q)[0] = %q, want %q", tc.input, candidates[0], tc.want)
		}
	}
}

func TestExtractProjectDir(t *testing.T) {
	got := extractProjectDir("/home/user/.claude/projects/-home-user-app/session.jsonl")
	want := "/home/user/.claude/projects/-home-user-app"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadSessionSlug(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	os.WriteFile(path, []byte(`{"type":"init"}
{"slug":"my-session-slug","type":"meta"}
`), 0o644)

	slug := loadSessionSlug(path)
	if slug != "my-session-slug" {
		t.Fatalf("got slug=%q", slug)
	}
}
