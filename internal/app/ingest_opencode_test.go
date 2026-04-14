package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

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

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.status",
		"session_id":  "opencode-1",
		"status_type": "idle",
		"timestamp":   float64(1712700010000),
	})

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

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.status", "session_id": "parent-1",
		"status_type": "idle", "timestamp": float64(1712700002000),
	})

	session, _ := st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child still running)", session.Status)
	}

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

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.idle",
		"session_id":  "parent-1",
		"project_dir": "/home/user/my-app",
		"timestamp":   float64(1712700002000),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child still running)", session.Status)
	}

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

	child, err := st.Read().GetSessionByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "stopped" {
		t.Fatalf("child status=%q, want stopped", child.Status)
	}

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

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "parent-1",
		"timestamp": float64(1712700002000),
	})

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.idle", "session_id": "child-a",
		"parent_session_id": "parent-1", "timestamp": float64(1712700003000),
	})

	session, _ := st.Read().GetSessionByID(ctx, "parent-1")
	if session.Status != "active" {
		t.Fatalf("parent status=%q, want active (child-b still running)", session.Status)
	}

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

	_, err = IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":      "session.idle",
		"session_id": "opencode-1",
		"timestamp":  float64(1712700010000),
	})
	if err != nil {
		t.Fatal(err)
	}

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

	IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "parent-1",
		"project_dir": "/home/user/my-app", "title": "main",
		"timestamp": float64(1712700000000),
	})

	_, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event": "session.created", "session_id": "child-1",
		"parent_session_id": "parent-1", "project_dir": "/home/user/my-app",
		"title": "New session - placeholder", "timestamp": float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

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

func TestUpsertSessionParentIDUpdatedOnConflict(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

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

	session, err := st.Read().GetSessionByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.ParentSessionID != "parent-1" {
		t.Fatalf("parent_session_id=%q, want parent-1 (should not be wiped)", session.ParentSessionID)
	}
}
