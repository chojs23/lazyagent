package tokens

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

func TestFromEventsPaginatesTreeAndDedupesMessages(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	var projectID int64
	err := st.WithTx(ctx, func(q *store.Queries) error {
		var err error
		projectID, err = q.CreateProject(ctx, "token-proj", "Token", "/tmp/token", "/tmp/token.jsonl")
		if err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "parent", "", projectID, "parent", "opencode", nil, 1000, ""); err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "child", "parent", projectID, "child", "opencode", nil, 2000, ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "parent", "parent", "", "parent", "", "", ""); err != nil {
			return err
		}
		if err := q.UpsertAgent(ctx, "child", "child", "parent", "child", "", "", ""); err != nil {
			return err
		}

		events := []model.Event{
			{AgentID: "parent", SessionID: "parent", Type: "message", Subtype: "MessageUpdated", Timestamp: 1000, Payload: `{"message_role":"assistant","message_id":"msg-1","model_id":"claude-sonnet-4-6","tokens_input":100,"tokens_output":40}`},
			{AgentID: "parent", SessionID: "parent", Type: "message", Subtype: "MessageUpdated", Timestamp: 1100, Payload: `{"message_role":"assistant","message_id":"msg-1","model_id":"claude-sonnet-4-6","tokens_input":120,"tokens_output":60}`},
			{AgentID: "child", SessionID: "child", Type: "tool", Subtype: "PreToolUse", ToolName: "Bash", Timestamp: 1200, Payload: `{"tool_input":{"command":"git status && ls"}}`},
			{AgentID: "child", SessionID: "child", Type: "message", Subtype: "MessageUpdated", Timestamp: 1300, Payload: `{"message_role":"assistant","message_id":"msg-2","model_id":"claude-haiku-4-5","tokens_input":80,"tokens_output":20,"tokens_cache_read":10}`},
			{AgentID: "parent", SessionID: "parent", Type: "message", Subtype: "MessageUpdated", Timestamp: 1400, Payload: `{"message_role":"user","message_id":"msg-user","tokens_input":999}`},
		}
		for _, ev := range events {
			if _, err := q.InsertEvent(ctx, ev); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setup store: %v", err)
	}

	originalPageSize := EventPageSize
	EventPageSize = 2
	t.Cleanup(func() { EventPageSize = originalPageSize })

	summary, err := FromEvents(ctx, st.Read(), "parent")
	if err != nil {
		t.Fatalf("FromEvents: %v", err)
	}
	if summary == nil {
		t.Fatal("summary should not be nil")
	}

	if summary.APICalls != 2 {
		t.Fatalf("APICalls = %d, want 2", summary.APICalls)
	}
	if summary.Tokens.InputTokens != 200 {
		t.Fatalf("InputTokens = %d, want 200", summary.Tokens.InputTokens)
	}
	if summary.Tokens.OutputTokens != 80 {
		t.Fatalf("OutputTokens = %d, want 80", summary.Tokens.OutputTokens)
	}
	if summary.Tokens.CacheReadTokens != 10 {
		t.Fatalf("CacheReadTokens = %d, want 10", summary.Tokens.CacheReadTokens)
	}
	if summary.ToolBreakdown["Bash"] == nil || summary.ToolBreakdown["Bash"].Calls != 1 {
		t.Fatalf("Bash tool calls = %v, want 1", summary.ToolBreakdown["Bash"])
	}
	if summary.BashBreakdown["git"] == nil || summary.BashBreakdown["git"].Calls != 1 {
		t.Fatalf("git bash calls = %v, want 1", summary.BashBreakdown["git"])
	}
	if summary.BashBreakdown["ls"] == nil || summary.BashBreakdown["ls"].Calls != 1 {
		t.Fatalf("ls bash calls = %v, want 1", summary.BashBreakdown["ls"])
	}
	if summary.ModelBreakdown["claude-sonnet-4-6"] == nil || summary.ModelBreakdown["claude-sonnet-4-6"].Calls != 1 {
		t.Fatalf("sonnet model breakdown = %v, want 1 call", summary.ModelBreakdown["claude-sonnet-4-6"])
	}
	if summary.ModelBreakdown["claude-haiku-4-5"] == nil || summary.ModelBreakdown["claude-haiku-4-5"].Calls != 1 {
		t.Fatalf("haiku model breakdown = %v, want 1 call", summary.ModelBreakdown["claude-haiku-4-5"])
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
