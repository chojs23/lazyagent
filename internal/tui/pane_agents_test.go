package tui

import (
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func TestAgentsEnterTogglesSelectionAtCursor(t *testing.T) {
	pane := newAgents()
	pane.setAgents([]model.Agent{{ID: "agent-1"}, {ID: "agent-2"}})
	pane.cursor = 1

	pane.enter()
	if pane.selectedAgentID() != "agent-2" {
		t.Fatalf("selectedAgentID() = %q, want agent-2", pane.selectedAgentID())
	}

	pane.enter()
	if pane.selectedAgentID() != "" {
		t.Fatalf("selectedAgentID() after toggle-off = %q, want empty", pane.selectedAgentID())
	}
}

func TestAgentsSetAgentsClampsCursorToAvailableRange(t *testing.T) {
	pane := newAgents()
	pane.cursor = 5

	pane.setAgents([]model.Agent{{ID: "agent-1"}, {ID: "agent-2"}})

	if pane.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", pane.cursor)
	}
}

func TestAgentsViewShowsTreeMarkersSpinnerAndAgentType(t *testing.T) {
	pane := newAgents()
	pane.spinnerFrame = 0
	pane.setAgents([]model.Agent{
		{ID: "root-agent", Name: "Root"},
		{ID: "child-1", ParentAgentID: "root-agent", Name: "Planner", AgentType: "review", Status: "active"},
		{ID: "child-2", ParentAgentID: "root-agent", Name: "Writer", Status: "stopped"},
	})

	view := pane.view(80, 10, false)

	for _, want := range []string{
		"Agents/Sessions",
		"├─ ⠋ Planner (review)",
		"└─ Writer",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in %q", want, view)
		}
	}
}

func TestAgentsViewFallsBackToShortIDWhenNameMissing(t *testing.T) {
	pane := newAgents()
	pane.setAgents([]model.Agent{{ID: "agent-abcdef1234567890"}})

	view := pane.view(60, 8, false)

	if !strings.Contains(view, shortID("agent-abcdef1234567890")) {
		t.Fatalf("view missing short id fallback in %q", view)
	}
}
