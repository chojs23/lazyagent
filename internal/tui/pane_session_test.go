package tui

import (
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func TestSessionInfoViewShowsSelectedSessionSummary(t *testing.T) {
	pane := newSessionInfo()
	pane.setSession(&model.Session{
		ID:           "session-abcdef123456",
		ProjectName:  "my-app",
		ProjectID:    7,
		Runtime:      "codex",
		StartedAt:    1712700000000,
		LastActivity: 1712700015000,
		EventCount:   987,
		AgentCount:   654,
	}, &model.Project{
		ID:        7,
		Directory: "/home/conyneo/projects/lazyagent2",
	})

	view := pane.view(48, 12, true)

	for _, want := range []string{
		"Session",
		"Codex",
		"my-app",
		"/home/conyneo/projects/lazyagent2",
		"session-abcdef123456",
		"987",
		"654",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in %q", want, view)
		}
	}
}

func TestSessionInfoViewShowsEmptyState(t *testing.T) {
	pane := newSessionInfo()
	view := pane.view(40, 8, false)

	if !strings.Contains(view, "No session selected") {
		t.Fatalf("view = %q", view)
	}
}
