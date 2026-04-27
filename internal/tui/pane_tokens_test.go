package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

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
