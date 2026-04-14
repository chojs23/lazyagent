package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/applog"
	"github.com/chojs23/lazyagent/internal/model"
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

	result, err := IngestClaudeEvent(ctx, st, payload)
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
	})

	// 2. PreToolUse Agent — stash name
	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-1",
		"tool_name":       "Agent",
		"tool_use_id":     "tu-1",
		"tool_input":      map[string]any{"name": "planner", "description": "Plans things", "subagent_type": "Plan"},
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	})

	// 3. Subagent event arrives (agent_id = subagent)
	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "Notification",
		"session_id":      "sess-1",
		"agent_id":        "agent-sub-1",
		"meta":            map[string]any{"timestamp": float64(1712700002000)},
	})

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
	})

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
	})

	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionEnd",
		"session_id":      "sess-1",
		"meta":            map[string]any{"timestamp": float64(1712700010000)},
	})

	session, err := st.Read().GetSessionByID(ctx, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "stopped" {
		t.Fatalf("got status=%q, want stopped", session.Status)
	}
}

func TestIngestClaudeDisplaySlugUsesFirstMeaningfulPrompt(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	transcriptPath := filepath.Join(t.TempDir(), "claude-session.jsonl")

	_, err := IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "claude-1",
		"slug":            "elegant-giggling-flame",
		"transcript_path": transcriptPath,
		"cwd":             "/home/user/project",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "claude-1",
		"slug":            "elegant-giggling-flame",
		"transcript_path": transcriptPath,
		"cwd":             "/home/user/project",
		"prompt":          "<command-name>/clear</command-name>\n<command-message>clear</command-message>",
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "claude-1",
		"slug":            "elegant-giggling-flame",
		"transcript_path": transcriptPath,
		"cwd":             "/home/user/project",
		"prompt":          "investigate the pagination bug\nwith full context",
		"meta":            map[string]any{"timestamp": float64(1712700002000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "claude-1",
		"slug":            "elegant-giggling-flame",
		"transcript_path": transcriptPath,
		"cwd":             "/home/user/project",
		"prompt":          "later follow-up prompt",
		"meta":            map[string]any{"timestamp": float64(1712700003000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "claude-1")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Slug != "investigate the pagination bug" {
		t.Fatalf("display slug=%q, want first meaningful prompt", session.Slug)
	}

	events, err := st.Read().ListEventsForSession(ctx, "claude-1", model.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
}

func TestIngestClaudeMeaningfulPromptReplacesLegacyRandomSlug(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var projectID int64
	err := st.WithTx(ctx, func(q *store.Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "proj", "Project", "/home/user/project", "")
		if err != nil {
			return err
		}
		return q.UpsertSession(ctx, "claude-legacy", "", projectID, "quirky-doodling-zephyr", "claude", nil, 1712700000000, "")
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "claude-legacy",
		"slug":            "quirky-doodling-zephyr",
		"cwd":             "/home/user/project",
		"prompt":          "real bug report title",
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "claude-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Slug != "real bug report title" {
		t.Fatalf("display slug=%q, want replaced display slug", session.Slug)
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
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.idle",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700010000),
	})
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

func TestIngestOpenCodeSessionStatusIdleMarksStopped(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// session.status with type "idle" should stop the session
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.status",
		"session_id":  "opencode-1",
		"status_type": "idle",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700010000),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "opencode-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "stopped" {
		t.Fatalf("got status=%q, want stopped", session.Status)
	}
}

func TestIngestOpenCodeSessionStatusBusyReactivates(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})

	// Stop via session.status idle
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.status",
		"session_id":  "opencode-1",
		"status_type": "idle",
		"timestamp":   float64(1712700010000),
	})

	// session.status with type "busy" should reactivate
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.status",
		"session_id":  "opencode-1",
		"status_type": "busy",
		"timestamp":   float64(1712700020000),
	})

	session, _ := st.Read().GetSessionByID(ctx, "opencode-1")
	if session.Status != "active" {
		t.Fatalf("got status=%q, want active (busy should reactivate)", session.Status)
	}
}

func TestIngestOpenCodeSessionStatusIdleDeferredWithChildren(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Parent + child
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "parent-1",
		"project_dir": "/home/user/my-app", "title": "main",
		"timestamp": float64(1712700000000),
	})
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-1",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "(@worker subagent)", "timestamp": float64(1712700001000),
	})

	// Parent gets session.status idle while child is active
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.status", "session_id": "parent-1",
		"status_type": "idle", "timestamp": float64(1712700002000),
	})

	session, _ := st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child still running)", session.Status)
	}

	// Child gets session.status idle -> parent should cascade to stopped
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.status", "session_id": "child-1",
		"parent_session_id": "parent-1",
		"status_type":       "idle", "timestamp": float64(1712700003000),
	})

	child, _ := st.Read().GetSessionByID(ctx, "child-1")
	if child.Status != "stopped" {
		t.Fatalf("child status=%q, want stopped", child.Status)
	}

	session, _ = st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "stopped" {
		t.Fatalf("parent status=%q, want stopped (all children done)", session.Status)
	}
}

func TestIngestOpenCodeSessionStatusRetryNotStopped(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})

	// session.status with type "retry" should NOT stop the session
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":         "session.status",
		"session_id":    "opencode-1",
		"status_type":   "retry",
		"retry_attempt": float64(1),
		"retry_message": "rate limited",
		"timestamp":     float64(1712700010000),
	})

	session, _ := st.Read().GetSessionByID(ctx, "opencode-1")
	if session.Status != "active" {
		t.Fatalf("got status=%q, want active (retry should not stop)", session.Status)
	}
}

func TestIngestOpenCodeIdleDeferredWhileChildrenActive(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create parent session
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "parent-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create child session
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":             "session.created",
		"session_id":        "child-1",
		"parent_session_id": "parent-1",
		"project_dir":       "/home/user/my-app",
		"title":             "Map modules (@mapper subagent)",
		"timestamp":         float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Parent goes idle while child is still running
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.idle",
		"session_id":  "parent-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700002000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Parent must stay active because child is still running
	session, err := st.Read().GetSessionByID(ctx, "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child still running)", session.Status)
	}

	// 4. Child goes idle
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":             "session.idle",
		"session_id":        "child-1",
		"parent_session_id": "parent-1",
		"project_dir":       "/home/user/my-app",
		"timestamp":         float64(1712700003000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Child should be stopped
	child, err := st.Read().GetSessionByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "stopped" {
		t.Fatalf("child status=%q, want stopped", child.Status)
	}

	// Parent should now be stopped via cascade
	session, err = st.Read().GetSessionByID(ctx, "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "stopped" {
		t.Fatalf("parent status=%q, want stopped (all children done, parent already idle)", session.Status)
	}
}

func TestIngestOpenCodeIdleDeferredMultipleChildren(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Parent + two children
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "parent-1",
		"project_dir": "/home/user/my-app", "title": "main",
		"timestamp": float64(1712700000000),
	})
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-a",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "(@agent-a subagent)", "timestamp": float64(1712700001000),
	})
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-b",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "(@agent-b subagent)", "timestamp": float64(1712700001500),
	})

	// Parent idles
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "parent-1",
		"timestamp": float64(1712700002000),
	})

	// First child stops — parent should stay active (child-b still running)
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "child-a",
		"parent_session_id": "parent-1", "timestamp": float64(1712700003000),
	})

	session, _ := st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child-b still running)", session.Status)
	}

	// Second child stops — parent should now cascade to stopped
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "child-b",
		"parent_session_id": "parent-1", "timestamp": float64(1712700004000),
	})

	session, _ = st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "stopped" {
		t.Fatalf("parent status=%q, want stopped (all children done)", session.Status)
	}
}

func TestIngestOpenCodeStoppedNotReactivatedByPassiveEvents(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"title":       "Greeting",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Stop the session
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":      "session.idle",
		"session_id": "opencode-1",
		"timestamp":  float64(1712700010000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Passive follow-up events that OpenCode emits after session.idle
	// should NOT reactivate the session.
	for _, evt := range []map[string]any{
		{"event": "session.updated", "session_id": "opencode-1", "title": "Greeting", "timestamp": float64(1712700010046)},
		{"event": "session.diff", "session_id": "opencode-1", "timestamp": float64(1712700010054)},
		{"event": "session.status", "session_id": "opencode-1", "timestamp": float64(1712700010060)},
	} {
		if _, err := IngestOpenCodeEvent(ctx, st, evt); err != nil {
			t.Fatal(err)
		}
	}

	session, err := st.Read().GetSessionByID(ctx, "opencode-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "stopped" {
		t.Fatalf("got status=%q, want stopped (passive events should not reactivate)", session.Status)
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
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.deleted",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700010000),
	})
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
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.idle",
		"session_id":  "opencode-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700010000),
	})
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
	})
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

func TestIngestOpenCodeRootAgentNameStaysMain(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create root session with placeholder title -> agent name = "main"
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "root-1",
		"project_dir": "/home/user/my-app",
		"title":       "New session - 2026-04-12T05:17:16.808Z",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "opencode" {
		t.Fatalf("initial name=%q, want opencode", agent.Name)
	}

	// 2. session.updated should NOT overwrite root agent name
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":      "session.updated",
		"session_id": "root-1",
		"title":      "Exhaustive bug hunt across codebase",
		"timestamp":  float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err = st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "opencode" {
		t.Fatalf("after session.updated: got name=%q, want opencode (root agent name should not change)", agent.Name)
	}
}

func TestIngestOpenCodeChildAgentNameUpdatedBySessionUpdated(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create parent
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "parent-1",
		"project_dir": "/home/user/my-app", "title": "main",
		"timestamp": float64(1712700000000),
	})

	// 2. Create child with placeholder title
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-1",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "New session - placeholder", "timestamp": float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. session.updated with real title should update child agent name
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.updated", "session_id": "child-1",
		"parent_session_id": "parent-1",
		"title":             "Map affected modules", "timestamp": float64(1712700002000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := st.Read().GetAgentByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Map affected modules" {
		t.Fatalf("after session.updated: got name=%q, want 'Map affected modules'", agent.Name)
	}
}

func TestIngestOpenCodeAgentNameNotOverwrittenByToolTitle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create root session with title — agent name should be "main"
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "root-1",
		"project_dir": "/home/user/my-app",
		"title":       "Greeting",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "opencode" {
		t.Fatalf("after session.created: got name=%q, want opencode", agent.Name)
	}

	// 2. PostToolUse with tool output title should NOT overwrite agent name
	for _, evt := range []map[string]any{
		{"event": "tool.execute.after", "session_id": "root-1", "tool": "Task", "call_id": "c1", "title": "0 todos", "timestamp": float64(1712700001000)},
		{"event": "tool.execute.after", "session_id": "root-1", "tool": "Read", "call_id": "c2", "title": "plugins/opencode/src/index.ts", "timestamp": float64(1712700002000)},
		{"event": "session.status", "session_id": "root-1", "timestamp": float64(1712700003000)},
		{"event": "tool.execute.after", "session_id": "root-1", "tool": "Agent", "call_id": "c3", "title": "unspecified-low - Map app architecture", "timestamp": float64(1712700004000)},
	} {
		if _, err := IngestOpenCodeEvent(ctx, st, evt); err != nil {
			t.Fatal(err)
		}
	}

	agent, err = st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "opencode" {
		t.Fatalf("after tool events: got name=%q, want opencode", agent.Name)
	}
}

func TestIngestOpenCodeAgentNameFromMessageUpdated(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create root session — agent name defaults to "main"
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "root-1",
		"project_dir": "/home/user/my-app",
		"title":       "New session - 2026-04-12",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "opencode" {
		t.Fatalf("initial name=%q, want opencode", agent.Name)
	}

	// 2. message.updated with agent_name should update root agent name
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":        "message.updated",
		"session_id":   "root-1",
		"message_role": "assistant",
		"message_id":   "msg-1",
		"agent_name":   "User main agent",
		"timestamp":    float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err = st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "User main agent" {
		t.Fatalf("after message.updated: got name=%q, want 'User main agent'", agent.Name)
	}

	// 3. user message.updated (no agent_name) should NOT overwrite
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":        "message.updated",
		"session_id":   "root-1",
		"message_role": "user",
		"message_id":   "msg-2",
		"timestamp":    float64(1712700002000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err = st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "User main agent" {
		t.Fatalf("after user message: got name=%q, want 'User main agent'", agent.Name)
	}
}

func TestIngestOpenCodeUserTextPartNormalizesToUserPromptSubmit(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "root-1",
		"project_dir": "/home/user/my-app",
		"title":       "New session - 2026-04-12",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":        "message.updated",
		"session_id":   "root-1",
		"message_role": "user",
		"message_id":   "msg-user-1",
		"timestamp":    float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":      "message.part.updated",
		"session_id": "root-1",
		"part_type":  "text",
		"part_id":    "part-1",
		"message_id": "msg-user-1",
		"text":       "please inspect the parser",
		"timestamp":  float64(1712700002000),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := st.Read().ListEventsForSession(ctx, "root-1", model.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	last := events[len(events)-1]
	if last.Type != "user" {
		t.Fatalf("last type=%q, want user", last.Type)
	}
	if last.Subtype != "UserPromptSubmit" {
		t.Fatalf("last subtype=%q, want UserPromptSubmit", last.Subtype)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(last.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["prompt"] != "please inspect the parser" {
		t.Fatalf("prompt=%v, want prompt text", payload["prompt"])
	}
	if payload["message_id"] != "msg-user-1" {
		t.Fatalf("message_id=%v, want msg-user-1", payload["message_id"])
	}
	if payload["message_role"] != "user" {
		t.Fatalf("message_role=%v, want user", payload["message_role"])
	}
	if payload["event"] != "message.part.updated" {
		t.Fatalf("event=%v, want original message.part.updated", payload["event"])
	}
	if payload["text"] != "please inspect the parser" {
		t.Fatalf("text=%v, want original text preserved", payload["text"])
	}
	if payload["part_type"] != "text" {
		t.Fatalf("part_type=%v, want text", payload["part_type"])
	}
	if events[1].Subtype != "MessageUpdated" {
		t.Fatalf("message event subtype=%q, want MessageUpdated", events[1].Subtype)
	}
	if events[2].Timestamp <= events[1].Timestamp {
		t.Fatalf("prompt timestamp=%d, want > %d", events[2].Timestamp, events[1].Timestamp)
	}
	thread, err := st.Read().GetEventThread(ctx, last.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread) != 1 {
		t.Fatalf("thread len=%d, want 1", len(thread))
	}
	if thread[0].Subtype != "UserPromptSubmit" {
		t.Fatalf("thread[0] subtype=%q, want UserPromptSubmit", thread[0].Subtype)
	}
}

func TestIngestOpenCodeOnlyFirstUserTextPartBecomesUserPromptSubmit(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "root-1",
		"project_dir": "/home/user/my-app",
		"title":       "New session - 2026-04-12",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":        "message.updated",
		"session_id":   "root-1",
		"message_role": "user",
		"message_id":   "msg-user-2",
		"timestamp":    float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	for i, text := range []string{"plan the fix", "Called the Read tool with the following input"} {
		_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
			"event":      "message.part.updated",
			"session_id": "root-1",
			"part_type":  "text",
			"part_id":    "part-dup-" + string(rune('1'+i)),
			"message_id": "msg-user-2",
			"text":       text,
			"timestamp":  float64(1712700002000 + (i * 1000)),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	events, err := st.Read().ListEventsForSession(ctx, "root-1", model.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	if events[2].Subtype != "UserPromptSubmit" {
		t.Fatalf("first text subtype=%q, want UserPromptSubmit", events[2].Subtype)
	}
	if events[3].Subtype != "PartUpdated" {
		t.Fatalf("second text subtype=%q, want PartUpdated", events[3].Subtype)
	}

	var secondPayload map[string]any
	if err := json.Unmarshal([]byte(events[3].Payload), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if secondPayload["prompt"] != nil {
		t.Fatalf("second payload prompt=%v, want nil", secondPayload["prompt"])
	}
	if secondPayload["text"] != "Called the Read tool with the following input" {
		t.Fatalf("second payload text=%v, want original text", secondPayload["text"])
	}
}

func TestIngestOpenCodeChildSessionAgentNamePreserved(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create parent session
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "parent-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create child session with subagent name in title
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":             "session.created",
		"session_id":        "child-1",
		"parent_session_id": "parent-1",
		"project_dir":       "/home/user/my-app",
		"title":             "Map affected modules (@subagent1 subagent)",
		"timestamp":         float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := st.Read().GetAgentByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "subagent1" {
		t.Fatalf("after session.created: got agent name=%q, want subagent1", agent.Name)
	}

	// 3. Subsequent events without parent_session_id or title (session.status, tool events)
	for _, evt := range []map[string]any{
		{"event": "session.status", "session_id": "child-1", "timestamp": float64(1712700002000)},
		{"event": "tool.execute.before", "session_id": "child-1", "tool": "Read", "call_id": "c1", "timestamp": float64(1712700003000)},
		{"event": "tool.execute.after", "session_id": "child-1", "tool": "Read", "call_id": "c1", "timestamp": float64(1712700004000)},
	} {
		if _, err := IngestOpenCodeEvent(ctx, st, evt); err != nil {
			t.Fatal(err)
		}
	}

	agent, err = st.Read().GetAgentByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "subagent1" {
		t.Fatalf("after follow-up events: got agent name=%q, want subagent1", agent.Name)
	}
}

func TestIngestCrossRuntimeProjectUnification(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Claude creates project first (transcript path encodes the full path)
	claudeResult, err := IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "claude-sess-1",
		"transcript_path": "/home/user/.claude/projects/-home-user-projects-lazyagent2/session.jsonl",
		"cwd":             "/home/user/projects/lazyagent2",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. OpenCode creates session for the same directory
	ocResult, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-sess-1",
		"project_dir": "/home/user/projects/lazyagent2",
		"title":       "main",
		"timestamp":   float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Both sessions must belong to the same project
	if claudeResult.ProjectID != ocResult.ProjectID {
		t.Fatalf("project IDs differ: claude=%d opencode=%d", claudeResult.ProjectID, ocResult.ProjectID)
	}
}

func TestIngestCrossRuntimeProjectUnificationOpenCodeFirst(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. OpenCode creates project first
	ocResult, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-sess-1",
		"project_dir": "/home/user/projects/myapp",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Claude creates session for the same directory
	claudeResult, err := IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "claude-sess-1",
		"transcript_path": "/home/user/.claude/projects/-home-user-projects-myapp/session.jsonl",
		"cwd":             "/home/user/projects/myapp",
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if ocResult.ProjectID != claudeResult.ProjectID {
		t.Fatalf("project IDs differ: opencode=%d claude=%d", ocResult.ProjectID, claudeResult.ProjectID)
	}
}

func TestIngestCodexFirstUserPromptSetsSessionSlug(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-1",
		"transcript_path": "/tmp/codex-1.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "codex-1",
		"transcript_path": "/tmp/codex-1.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"turn_id":         "turn-1",
		"prompt":          "fix broken filtered paging\nwith a regression test",
		"timestamp":       float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "codex-1")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Slug != "fix broken filtered paging" {
		t.Fatalf("slug=%q, want first prompt line", session.Slug)
	}
	if session.Runtime != "codex" {
		t.Fatalf("runtime=%q, want codex", session.Runtime)
	}

	events, err := st.Read().ListEventsForSession(ctx, "codex-1", model.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[1].Subtype != "UserPromptSubmit" {
		t.Fatalf("subtype=%q, want UserPromptSubmit", events[1].Subtype)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(events[1].Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["prompt"] != "fix broken filtered paging\nwith a regression test" {
		t.Fatalf("prompt=%v, want original prompt preserved", payload["prompt"])
	}
}

func TestIngestCodexLaterPromptDoesNotOverwriteExistingSlug(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-2",
		"transcript_path": "/tmp/codex-2.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "codex-2",
		"transcript_path": "/tmp/codex-2.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"turn_id":         "turn-1",
		"prompt":          "first codex prompt",
		"timestamp":       float64(1712700000500),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "codex-2",
		"transcript_path": "/tmp/codex-2.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"turn_id":         "turn-2",
		"prompt":          "second codex prompt",
		"timestamp":       float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "codex-2")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Slug != "first codex prompt" {
		t.Fatalf("slug=%q, want first codex prompt", session.Slug)
	}
}

func TestIngestCodexReplaysPatchEventsFromTranscript(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	transcriptPath := filepath.Join(t.TempDir(), "codex.jsonl")

	line := `{"type":"event_msg","timestamp":"2026-04-14T12:00:00Z","payload":{"type":"patch_apply_end","call_id":"patch-1","success":true,"stdout":"ok","changes":{"main.go":{"type":"update","unified_diff":"@@ -1 +1 @@\n-old\n+new\n"}}}}`
	if err := os.WriteFile(transcriptPath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-patch-1",
		"transcript_path": transcriptPath,
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := st.Read().ListEventsForSession(ctx, "codex-patch-1", model.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	patch := events[1]
	if patch.ToolName != "apply_patch" {
		t.Fatalf("tool_name=%q, want apply_patch", patch.ToolName)
	}
	if patch.ToolUseID != "patch-1" {
		t.Fatalf("tool_use_id=%q, want patch-1", patch.ToolUseID)
	}
	if patch.Subtype != "PostToolUse" {
		t.Fatalf("subtype=%q, want PostToolUse", patch.Subtype)
	}
	if !strings.Contains(patch.Payload, "\"diff\":") {
		t.Fatalf("payload missing diff metadata: %q", patch.Payload)
	}
	if !strings.Contains(patch.Payload, "--- main.go") {
		t.Fatalf("payload missing unified diff: %q", patch.Payload)
	}
}

func TestIngestCodexLogsPatchReadErrors(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	logPath := filepath.Join(t.TempDir(), "lazyagent.log")
	applog.SetDefault(applog.NewForPath(logPath))
	t.Cleanup(func() {
		applog.SetDefault(nil)
	})

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-log-1",
		"transcript_path": filepath.Join(t.TempDir(), "missing.jsonl"),
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(data)
	if !strings.Contains(logText, "Read Codex patch events failed") {
		t.Fatalf("log missing context: %q", logText)
	}
	if !strings.Contains(logText, "missing.jsonl") {
		t.Fatalf("log missing transcript path: %q", logText)
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

func TestExtractProjectDirKeepsExistingDottedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "foo.bar")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := extractProjectDir(dir); got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}

func TestCreateProjectWithUniqueSlugFailsWhenSuffixesAreExhausted(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	err := st.WithTx(ctx, func(q *store.Queries) error {
		for suffix := 2; suffix <= maxProjectSlugSuffix; suffix++ {
			slug := fmt.Sprintf("my-app-%d", suffix)
			if _, err := q.CreateProject(ctx, slug, slug, fmt.Sprintf("/taken/%d", suffix), ""); err != nil {
				return err
			}
		}
		_, err := createProjectWithUniqueSlug(ctx, q, "my-app", "/home/user/my-app", "")
		return err
	})
	if err == nil {
		t.Fatal("expected createProjectWithUniqueSlug to fail after slug suffix exhaustion")
	}
	if want := fmt.Sprintf("resolve project slug: exhausted suffixes for %q up to %d", "my-app", maxProjectSlugSuffix); err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestUpsertSessionParentIDUpdatedOnConflict(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Child session arrives first without parent info.
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "child-1",
		"project_dir": "/home/user/my-app",
		"title":       "worker",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.ParentSessionID != "" {
		t.Fatalf("expected empty parent, got %q", session.ParentSessionID)
	}

	// 2. Later event for the same session arrives WITH parent_session_id.
	//    Create parent first so the FK constraint is satisfied.
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "parent-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700001000),
	})

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":             "session.updated",
		"session_id":        "child-1",
		"parent_session_id": "parent-1",
		"project_dir":       "/home/user/my-app",
		"title":             "worker",
		"timestamp":         float64(1712700002000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// parent_session_id must now be set.
	session, err = st.Read().GetSessionByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.ParentSessionID != "parent-1" {
		t.Fatalf("parent_session_id=%q, want parent-1", session.ParentSessionID)
	}
}

func TestUpsertSessionParentIDPreservedWhenNewEventOmitsIt(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create parent, then child with parent_session_id.
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "parent-1",
		"project_dir": "/home/user/my-app",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":             "session.created",
		"session_id":        "child-1",
		"parent_session_id": "parent-1",
		"project_dir":       "/home/user/my-app",
		"title":             "worker",
		"timestamp":         float64(1712700001000),
	})

	// 2. A follow-up event for the child WITHOUT parent_session_id.
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.updated",
		"session_id":  "child-1",
		"project_dir": "/home/user/my-app",
		"title":       "worker updated",
		"timestamp":   float64(1712700002000),
	})
	if err != nil {
		t.Fatal(err)
	}

	// parent_session_id must still be set (not wiped to NULL).
	session, err := st.Read().GetSessionByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.ParentSessionID != "parent-1" {
		t.Fatalf("parent_session_id=%q, want parent-1 (should not be wiped)", session.ParentSessionID)
	}
}
