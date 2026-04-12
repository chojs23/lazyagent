package store

import (
	"context"
	"path/filepath"
	"testing"

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
