package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

func TestReadEventTokenSummaryPaginatesTreeAndDedupesMessages(t *testing.T) {
	st := testTUIStore(t)
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

	originalPageSize := tokenEventPageSize
	tokenEventPageSize = 2
	t.Cleanup(func() {
		tokenEventPageSize = originalPageSize
	})

	summary, err := readEventTokenSummary(ctx, st.Read(), "parent")
	if err != nil {
		t.Fatalf("readEventTokenSummary: %v", err)
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

func TestRenderOverviewSectionUsesModelCallsLabel(t *testing.T) {
	lines := renderOverviewSection(&claude.SessionTokenSummary{
		APICalls: 3,
		Tokens: claude.TokenUsage{
			InputTokens:         100,
			CacheReadTokens:     50,
			CacheCreationTokens: 25,
		},
	})
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "Model Calls") {
		t.Fatalf("overview should include Model Calls label: %q", joined)
	}
	if !strings.Contains(joined, "Direct Input") {
		t.Fatalf("overview should include Direct Input label: %q", joined)
	}
	if !strings.Contains(joined, "Total Input") {
		t.Fatalf("overview should include Total Input label: %q", joined)
	}
	if strings.Contains(joined, "API Calls") {
		t.Fatalf("overview should not include old API Calls label: %q", joined)
	}
	if strings.Contains(joined, "  Input:") {
		t.Fatalf("overview should not include ambiguous Input label: %q", joined)
	}
}

func TestRenderModelSectionUsesTotalInLabel(t *testing.T) {
	lines := renderModelSection(&claude.SessionTokenSummary{
		ModelBreakdown: map[string]*claude.ModelStats{
			"gpt-5.4": {
				Calls: 1,
				Tokens: claude.TokenUsage{
					InputTokens:         100,
					CacheReadTokens:     50,
					CacheCreationTokens: 25,
					OutputTokens:        10,
				},
			},
		},
	})
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "total in:") {
		t.Fatalf("model section should include total in label: %q", joined)
	}
}

func TestRenderTokenColumnsIncludesAuditSections(t *testing.T) {
	lines := renderTokenColumns(&model.Session{
		ID:          "sess-1",
		Slug:        "token-audit",
		ProjectName: "lazyagent",
		Runtime:     "codex",
	}, &claude.SessionTokenSummary{
		APICalls: 4,
		Tokens: claude.TokenUsage{
			InputTokens:         200,
			OutputTokens:        80,
			CacheReadTokens:     50,
			CacheCreationTokens: 10,
		},
		ModelBreakdown: map[string]*claude.ModelStats{
			"gpt-5.4": {
				Calls: 3,
				Tokens: claude.TokenUsage{
					InputTokens:         180,
					OutputTokens:        70,
					CacheReadTokens:     40,
					CacheCreationTokens: 10,
				},
				CostUSD: 1.25,
			},
		},
		ToolBreakdown: map[string]*claude.ToolStats{
			"Bash": {Calls: 2},
		},
		BashBreakdown: map[string]*claude.ToolStats{
			"git": {Calls: 2},
		},
	}, 100)

	joined := strings.Join(lines, "\n")
	for _, want := range []string{"Session", "Signals", "Model Ledger", "Execution Mix", "Top Model", "Commands", "Tools"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("audit layout missing %q in %q", want, joined)
		}
	}
}

func TestTokenOverlayClampsToSmallTerminal(t *testing.T) {
	overlay := tokensOverlay{
		visible: true,
		session: &model.Session{ID: "sess-1", Slug: "tiny", Runtime: "codex", ProjectName: "lazyagent"},
		summary: &claude.SessionTokenSummary{
			APICalls: 4,
			Tokens: claude.TokenUsage{
				InputTokens:         200,
				OutputTokens:        80,
				CacheReadTokens:     50,
				CacheCreationTokens: 10,
			},
			ModelBreakdown: map[string]*claude.ModelStats{
				"gpt-5.4": {Calls: 3, Tokens: claude.TokenUsage{InputTokens: 180, OutputTokens: 70, CacheReadTokens: 40, CacheCreationTokens: 10}, CostUSD: 1.25},
			},
			ToolBreakdown: map[string]*claude.ToolStats{"Bash": {Calls: 2}},
			BashBreakdown: map[string]*claude.ToolStats{"git": {Calls: 2}},
		},
	}

	view := overlay.viewFullScreen(48, 12)
	lines := strings.Split(view, "\n")
	if len(lines) > 12 {
		t.Fatalf("overlay height = %d, want <= 12", len(lines))
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 48 {
			t.Fatalf("overlay line width = %d, want <= 48 in %q", lipgloss.Width(line), line)
		}
	}
}

func testTUIStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
