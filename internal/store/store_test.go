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
