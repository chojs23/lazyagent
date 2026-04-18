package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chojs23/lazyagent/internal/model"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestClearSessionEventsClearsEntireTree(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Create a project.
	var projectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "test-proj", "Test", "/tmp/test", "/tmp/transcript")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a parent session and a child session.
	err = st.WithTx(ctx, func(q *Queries) error {
		if err := q.UpsertSession(ctx, "parent", "", projectID, "parent-slug", "claude", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "child", "parent", projectID, "child-slug", "claude", nil, 2000, ""); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert agents first (events have a FK on agent_id), then events.
	err = st.WithTx(ctx, func(q *Queries) error {
		if err := q.UpsertAgent(ctx, "agent-p", "parent", "", "ParentAgent", "", "main", ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "agent-c", "child", "", "ChildAgent", "", "sub", ""); err != nil {
			return err
		}
		if _, err := q.InsertEvent(ctx, model.Event{
			AgentID: "agent-p", SessionID: "parent", Type: "message",
			Timestamp: 1000, Payload: `{"text":"hello from parent"}`,
		}); err != nil {
			return err
		}
		if _, err := q.InsertEvent(ctx, model.Event{
			AgentID: "agent-c", SessionID: "child", Type: "message",
			Timestamp: 2000, Payload: `{"text":"hello from child"}`,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify both sessions have data via tree queries.
	eventCount, err := st.Read().CountEventsForSessionTree(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("expected 2 events in tree before clear, got %d", eventCount)
	}

	agents, err := st.Read().ListAgentsForSessionTree(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents in tree before clear, got %d", len(agents))
	}

	// Clear events on the parent session — should clear the whole tree.
	err = st.WithTx(ctx, func(q *Queries) error {
		return q.ClearSessionEvents(ctx, "parent")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify tree is fully cleared.
	eventCount, err = st.Read().CountEventsForSessionTree(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("expected 0 events after clear, got %d", eventCount)
	}

	agents, err = st.Read().ListAgentsForSessionTree(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents after clear, got %d", len(agents))
	}

	// Verify child session counters are also reset.
	child, err := st.Read().GetSessionByID(ctx, "child")
	if err != nil {
		t.Fatal(err)
	}
	if child.EventCount != 0 {
		t.Fatalf("child event_count should be 0 after clear, got %d", child.EventCount)
	}
	if child.AgentCount != 0 {
		t.Fatalf("child agent_count should be 0 after clear, got %d", child.AgentCount)
	}
}

func TestFilteredSessionTreeEventQueriesStayAligned(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var projectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "filter-proj", "Filter", "/tmp/filter", "/tmp/filter-transcript")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(q *Queries) error {
		if err := q.UpsertSession(ctx, "parent", "", projectID, "parent", "claude", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "child", "parent", projectID, "child", "claude", nil, 2000, ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "agent-parent", "parent", "", "Parent", "", "main", ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "agent-child", "child", "", "Child", "", "sub", ""); err != nil {
			return err
		}
		if _, err := q.InsertEvent(ctx, model.Event{
			AgentID:   "agent-parent",
			SessionID: "parent",
			Type:      "tool",
			Subtype:   "Read",
			Timestamp: 1000,
			Payload:   `{"text":"match root"}`,
		}); err != nil {
			return err
		}
		if _, err := q.InsertEvent(ctx, model.Event{
			AgentID:   "agent-child",
			SessionID: "child",
			Type:      "tool",
			Subtype:   "Read",
			Timestamp: 2000,
			Payload:   `{"text":"match child"}`,
		}); err != nil {
			return err
		}
		if _, err := q.InsertEvent(ctx, model.Event{
			AgentID:   "agent-child",
			SessionID: "child",
			Type:      "message",
			Subtype:   "AssistantMessage",
			Timestamp: 3000,
			Payload:   `{"text":"do not match"}`,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	filter := model.EventFilter{
		AgentIDs: []string{"agent-child"},
		Type:     "tool",
		Subtype:  "Read",
		Search:   "match",
	}

	count, err := st.Read().CountFilteredEventsForSessionTree(ctx, "parent", filter)
	if err != nil {
		t.Fatal(err)
	}

	events, err := st.Read().ListEventsForSessionTree(ctx, "parent", filter)
	if err != nil {
		t.Fatal(err)
	}

	if count != len(events) {
		t.Fatalf("filtered tree count drifted from list query: count=%d list=%d", count, len(events))
	}
	if count != 1 {
		t.Fatalf("expected exactly one matching tree event, got %d", count)
	}
	if events[0].AgentID != "agent-child" {
		t.Fatalf("expected child agent match, got %q", events[0].AgentID)
	}
}

func TestFilteredSessionTreeEventQueriesStayAlignedForMessageResponses(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var projectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "ai-filter-proj", "AI Filter", "/tmp/ai-filter", "/tmp/ai-filter-transcript")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(q *Queries) error {
		if err := q.UpsertSession(ctx, "parent", "", projectID, "parent", "claude", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "child", "parent", projectID, "child", "opencode", nil, 2000, ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "agent-parent", "parent", "", "Parent", "", "main", ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "agent-child", "child", "", "Child", "", "sub", ""); err != nil {
			return err
		}

		events := []model.Event{
			{
				AgentID:   "agent-parent",
				SessionID: "parent",
				Type:      "system",
				Subtype:   "Stop",
				Timestamp: 1000,
				Payload:   `{"last_assistant_message":"root answer"}`,
			},
			{
				AgentID:   "agent-parent",
				SessionID: "parent",
				Type:      "system",
				Subtype:   "SubagentStop",
				Timestamp: 1100,
				Payload:   `{"last_assistant_message":"subagent answer"}`,
			},
			{
				AgentID:   "agent-parent",
				SessionID: "parent",
				Type:      "system",
				Subtype:   "Stop",
				Timestamp: 1150,
				Payload:   `{"reason":"user_cancelled"}`,
			},
			{
				AgentID:   "agent-child",
				SessionID: "child",
				Type:      "message",
				Subtype:   "PartUpdated",
				Timestamp: 1200,
				Payload:   `{"part_type": "text", "text":"streamed assistant text"}`,
			},
			{
				AgentID:   "agent-child",
				SessionID: "child",
				Type:      "message",
				Subtype:   "PartUpdated",
				Timestamp: 1300,
				Payload:   `{"part_type":"reasoning","text":"assistant reasoning"}`,
			},
			{
				AgentID:   "agent-child",
				SessionID: "child",
				Type:      "message",
				Subtype:   "PartUpdated",
				Timestamp: 1400,
				Payload:   `{"part_type":"tool","tool_name":"Read"}`,
			},
			{
				AgentID:   "agent-child",
				SessionID: "child",
				Type:      "user",
				Subtype:   "UserPromptSubmit",
				Timestamp: 1500,
				Payload:   `{"prompt":"user prompt"}`,
			},
		}

		for _, event := range events {
			if _, err := q.InsertEvent(ctx, event); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	filter := model.EventFilter{Type: "message"}

	count, err := st.Read().CountFilteredEventsForSessionTree(ctx, "parent", filter)
	if err != nil {
		t.Fatal(err)
	}

	events, err := st.Read().ListEventsForSessionTree(ctx, "parent", filter)
	if err != nil {
		t.Fatal(err)
	}

	if count != len(events) {
		t.Fatalf("filtered message tree count drifted from list query: count=%d list=%d", count, len(events))
	}
	if count != 4 {
		t.Fatalf("expected 4 message response events, got %d", count)
	}

	wantSubtypes := []string{"Stop", "SubagentStop", "PartUpdated", "PartUpdated"}
	for i, want := range wantSubtypes {
		if events[i].Subtype != want {
			t.Fatalf("events[%d] subtype=%q, want %q", i, events[i].Subtype, want)
		}
	}
}

func TestListAgentsForSessionTreeReparentsChildSessionAgents(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var projectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "agent-tree-proj", "Agent Tree", "/tmp/agent-tree", "")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(q *Queries) error {
		if err := q.UpsertSession(ctx, "parent", "", projectID, "parent", "claude", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "child", "parent", projectID, "child", "claude", nil, 2000, ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "parent", "parent", "", "Parent Root", "", "main", ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "child", "child", "", "Child Root", "", "sub", ""); err != nil {
			return err
		}
		return q.UpsertAgent(ctx, "child-worker", "child", "child", "Child Worker", "", "worker", "")
	})
	if err != nil {
		t.Fatal(err)
	}

	agents, err := st.Read().ListAgentsForSessionTree(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	got := make(map[string]model.Agent, len(agents))
	for _, agent := range agents {
		got[agent.ID] = agent
	}

	if got["parent"].ParentAgentID != "" {
		t.Fatalf("parent root ParentAgentID = %q, want empty", got["parent"].ParentAgentID)
	}
	if got["child"].ParentAgentID != "parent" {
		t.Fatalf("child root ParentAgentID = %q, want parent", got["child"].ParentAgentID)
	}
	if got["child-worker"].ParentAgentID != "parent" {
		t.Fatalf("child worker ParentAgentID = %q, want parent", got["child-worker"].ParentAgentID)
	}
}

func TestFilteredSessionTreeEventQueriesExcludeStopsWithoutAssistantMessage(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var projectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "message-stop-proj", "Message Stop", "/tmp/message-stop", "/tmp/message-stop-transcript")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(q *Queries) error {
		if err := q.UpsertSession(ctx, "parent", "", projectID, "parent", "claude", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "agent-parent", "parent", "", "Parent", "", "main", ""); err != nil {
			return err
		}

		events := []model.Event{
			{
				AgentID:   "agent-parent",
				SessionID: "parent",
				Type:      "system",
				Subtype:   "Stop",
				Timestamp: 1000,
				Payload:   `{"last_assistant_message":"real assistant answer"}`,
			},
			{
				AgentID:   "agent-parent",
				SessionID: "parent",
				Type:      "system",
				Subtype:   "Stop",
				Timestamp: 1100,
				Payload:   `{"reason":"user_cancelled"}`,
			},
			{
				AgentID:   "agent-parent",
				SessionID: "parent",
				Type:      "system",
				Subtype:   "SubagentStop",
				Timestamp: 1200,
				Payload:   `{"last_assistant_message":""}`,
			},
		}

		for _, event := range events {
			if _, err := q.InsertEvent(ctx, event); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	filter := model.EventFilter{Type: "message"}

	count, err := st.Read().CountFilteredEventsForSessionTree(ctx, "parent", filter)
	if err != nil {
		t.Fatal(err)
	}

	events, err := st.Read().ListEventsForSessionTree(ctx, "parent", filter)
	if err != nil {
		t.Fatal(err)
	}

	if count != len(events) {
		t.Fatalf("filtered message tree count drifted from list query: count=%d list=%d", count, len(events))
	}
	if count != 1 {
		t.Fatalf("expected only one stop event with assistant text, got %d", count)
	}
	if events[0].Subtype != "Stop" {
		t.Fatalf("event subtype=%q, want Stop", events[0].Subtype)
	}
}

func TestListRecentSessionsStaysInStartedAtOrder(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var projectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "recent-proj", "Recent", "/tmp/recent", "/tmp/recent-transcript")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(q *Queries) error {
		if err := q.UpsertSession(ctx, "older", "", projectID, "older", "claude", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "newer", "", projectID, "newer", "claude", nil, 2000, ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "agent-older", "older", "", "Older", "", "main", ""); err != nil {
			return err
		}
		if _, err := q.InsertEvent(ctx, model.Event{
			AgentID:   "agent-older",
			SessionID: "older",
			Type:      "message",
			Timestamp: 5000,
			Payload:   `{"text":"late activity"}`,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := st.Read().ListRecentSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "newer" || sessions[1].ID != "older" {
		t.Fatalf("expected started_at order [newer older], got [%s %s]", sessions[0].ID, sessions[1].ID)
	}

	projectSessions, err := st.Read().ListSessionsForProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(projectSessions) != 2 {
		t.Fatalf("expected 2 project sessions, got %d", len(projectSessions))
	}
	if projectSessions[0].ID != "newer" || projectSessions[1].ID != "older" {
		t.Fatalf("expected project session order [newer older], got [%s %s]", projectSessions[0].ID, projectSessions[1].ID)
	}
}

func TestListProjectsCountsOnlyRootSessions(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var rootProjectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		rootProjectID, err = q.CreateProject(ctx, "root-only-count", "Root Only", "/tmp/root-only", "/tmp/root-only-transcript")
		if err != nil {
			return err
		}
		if _, err := q.CreateProject(ctx, "empty-proj", "Empty", "/tmp/empty", "/tmp/empty-transcript"); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "root-1", "", rootProjectID, "root-1", "claude", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "child-1", "root-1", rootProjectID, "child-1", "claude", nil, 2000, ""); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	projects, err := st.Read().ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	counts := map[string]int64{}
	for _, project := range projects {
		counts[project.Slug] = project.SessionCount
	}
	if counts["root-only-count"] != 1 {
		t.Fatalf("root-only-count session count = %d, want 1", counts["root-only-count"])
	}
	if counts["empty-proj"] != 0 {
		t.Fatalf("empty-proj session count = %d, want 0", counts["empty-proj"])
	}
}

func TestStoreReapStaleSessionsUsesTransactionalPath(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	var projectID int64
	err := st.WithTx(ctx, func(q *Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "reap-proj", "Reap", "/tmp/reap", "/tmp/reap-transcript")
		if err != nil {
			return err
		}

		if err := q.UpsertSession(ctx, "stale-root", "", projectID, "stale-root", "claude", nil, 1, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "parent", "", projectID, "parent", "claude", nil, 1, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "child", "parent", projectID, "child", "claude", nil, now, ""); err != nil {
			return err
		}

		if err := q.UpsertAgent(ctx, "stale-root", "stale-root", "", "Stale Root", "", "main", ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "parent", "parent", "", "Parent", "", "main", ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "child", "child", "", "Child", "", "main", ""); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	reaped, err := st.ReapStaleSessions(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if reaped != 1 {
		t.Fatalf("reaped = %d, want 1", reaped)
	}

	staleRoot, err := st.Read().GetSessionByID(ctx, "stale-root")
	if err != nil {
		t.Fatal(err)
	}
	if staleRoot.Status != "stopped" {
		t.Fatalf("stale-root status = %q, want stopped", staleRoot.Status)
	}
	if staleRoot.StoppedAt == 0 {
		t.Fatal("stale-root should record stopped_at")
	}

	staleRootAgent, err := st.Read().GetAgentByID(ctx, "stale-root")
	if err != nil {
		t.Fatal(err)
	}
	if staleRootAgent.Status != "stopped" {
		t.Fatalf("stale-root agent status = %q, want stopped", staleRootAgent.Status)
	}

	parent, err := st.Read().GetSessionByID(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "active" {
		t.Fatalf("parent status = %q, want active", parent.Status)
	}

	child, err := st.Read().GetSessionByID(ctx, "child")
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "active" {
		t.Fatalf("child status = %q, want active", child.Status)
	}
}
