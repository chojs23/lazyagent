package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

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

	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "sess-1",
		"transcript_path": "/home/user/.claude/projects/-home-user-my-app/s.jsonl",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	})

	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-1",
		"tool_name":       "Agent",
		"tool_use_id":     "tu-1",
		"tool_input":      map[string]any{"name": "planner", "description": "Plans things", "subagent_type": "Plan"},
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	})

	IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "Notification",
		"session_id":      "sess-1",
		"agent_id":        "agent-sub-1",
		"meta":            map[string]any{"timestamp": float64(1712700002000)},
	})

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
