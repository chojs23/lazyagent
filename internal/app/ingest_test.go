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

func TestIngestOpenCodeSessionStatusIdleMarksStopped(t *testing.T) {
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

	// session.status with type "idle" should stop the session
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.status",
		"session_id":  "opencode-1",
		"status_type": "idle",
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
	}, "")

	// Stop via session.status idle
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.status",
		"session_id":  "opencode-1",
		"status_type": "idle",
		"timestamp":   float64(1712700010000),
	}, "")

	// session.status with type "busy" should reactivate
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.status",
		"session_id":  "opencode-1",
		"status_type": "busy",
		"timestamp":   float64(1712700020000),
	}, "")

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
	}, "")
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-1",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "(@worker subagent)", "timestamp": float64(1712700001000),
	}, "")

	// Parent gets session.status idle while child is active
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.status", "session_id": "parent-1",
		"status_type": "idle", "timestamp": float64(1712700002000),
	}, "")

	session, _ := st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child still running)", session.Status)
	}

	// Child gets session.status idle -> parent should cascade to stopped
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.status", "session_id": "child-1",
		"parent_session_id": "parent-1",
		"status_type": "idle", "timestamp": float64(1712700003000),
	}, "")

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
	}, "")

	// session.status with type "retry" should NOT stop the session
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":         "session.status",
		"session_id":    "opencode-1",
		"status_type":   "retry",
		"retry_attempt": float64(1),
		"retry_message": "rate limited",
		"timestamp":     float64(1712700010000),
	}, "")

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
	}, "")
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
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// 3. Parent goes idle while child is still running
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.idle",
		"session_id":  "parent-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700002000),
	}, "")
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
	}, "")
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
	}, "")
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-a",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "(@agent-a subagent)", "timestamp": float64(1712700001000),
	}, "")
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-b",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "(@agent-b subagent)", "timestamp": float64(1712700001500),
	}, "")

	// Parent idles
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "parent-1",
		"timestamp": float64(1712700002000),
	}, "")

	// First child stops — parent should stay active (child-b still running)
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "child-a",
		"parent_session_id": "parent-1", "timestamp": float64(1712700003000),
	}, "")

	session, _ := st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child-b still running)", session.Status)
	}

	// Second child stops — parent should now cascade to stopped
	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "child-b",
		"parent_session_id": "parent-1", "timestamp": float64(1712700004000),
	}, "")

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
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// Stop the session
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":      "session.idle",
		"session_id": "opencode-1",
		"timestamp":  float64(1712700010000),
	}, "")
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
		if _, err := IngestOpenCodeEvent(ctx, st, evt, ""); err != nil {
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

func TestIngestOpenCodeAgentNameUpdatedBySessionUpdated(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create session with placeholder title
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "root-1",
		"project_dir": "/home/user/my-app",
		"title":       "New session - 2026-04-12T05:17:16.808Z",
		"timestamp":   float64(1712700000000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	agent, err := st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "New session - 2026-04-12T05:17:16.808Z" {
		t.Fatalf("initial name=%q", agent.Name)
	}

	// 2. session.updated with the real title should overwrite the placeholder
	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":      "session.updated",
		"session_id": "root-1",
		"title":      "Exhaustive bug hunt across codebase",
		"timestamp":  float64(1712700001000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	agent, err = st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Exhaustive bug hunt across codebase" {
		t.Fatalf("after session.updated: got name=%q, want real title", agent.Name)
	}
}

func TestIngestOpenCodeAgentNameNotOverwrittenByToolTitle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 1. Create root session with title "Greeting"
	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "root-1",
		"project_dir": "/home/user/my-app",
		"title":       "Greeting",
		"timestamp":   float64(1712700000000),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	agent, err := st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Greeting" {
		t.Fatalf("after session.created: got name=%q, want Greeting", agent.Name)
	}

	// 2. PostToolUse with tool output title should NOT overwrite agent name
	for _, evt := range []map[string]any{
		{"event": "tool.execute.after", "session_id": "root-1", "tool": "Task", "call_id": "c1", "title": "0 todos", "timestamp": float64(1712700001000)},
		{"event": "tool.execute.after", "session_id": "root-1", "tool": "Read", "call_id": "c2", "title": "plugins/opencode/src/index.ts", "timestamp": float64(1712700002000)},
		{"event": "session.status", "session_id": "root-1", "timestamp": float64(1712700003000)},
		{"event": "tool.execute.after", "session_id": "root-1", "tool": "Agent", "call_id": "c3", "title": "unspecified-low - Map app architecture", "timestamp": float64(1712700004000)},
	} {
		if _, err := IngestOpenCodeEvent(ctx, st, evt, ""); err != nil {
			t.Fatal(err)
		}
	}

	agent, err = st.Read().GetAgentByID(ctx, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Greeting" {
		t.Fatalf("after tool events: got name=%q, want Greeting", agent.Name)
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
	}, "")
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
	}, "")
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
		if _, err := IngestOpenCodeEvent(ctx, st, evt, ""); err != nil {
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
	}, "")
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
	}, "")
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
	}, "")
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
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	if ocResult.ProjectID != claudeResult.ProjectID {
		t.Fatalf("project IDs differ: opencode=%d claude=%d", ocResult.ProjectID, claudeResult.ProjectID)
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
