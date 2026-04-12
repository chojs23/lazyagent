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

	view := pane.view(64, 12, true)

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

func TestSessionInfoViewScrollsVertically(t *testing.T) {
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

	initial := pane.view(48, 5, true)
	if strings.Contains(initial, "Agents:") {
		t.Fatalf("initial view unexpectedly shows bottom field: %q", initial)
	}

	pane.goBottom()
	scrolled := pane.view(48, 5, true)
	for _, want := range []string{"Events:", "Agents:"} {
		if !strings.Contains(scrolled, want) {
			t.Fatalf("scrolled view missing %q in %q", want, scrolled)
		}
	}
}

func TestSessionInfoViewScrollsHorizontally(t *testing.T) {
	pane := newSessionInfo()
	pane.setSession(&model.Session{
		ID:          "session-abcdef123456",
		ProjectName: "my-app",
		ProjectID:   7,
		Runtime:     "codex",
	}, &model.Project{
		ID:        7,
		Directory: "/very/long/path/with/final-segment-visible",
	})

	initial := pane.view(32, 8, true)
	if strings.Contains(initial, "final-segment-visible") {
		t.Fatalf("initial view unexpectedly shows far-right path text: %q", initial)
	}

	pane.hScroll = 28
	scrolled := pane.view(32, 8, true)
	if !strings.Contains(scrolled, "final-segment-visible") {
		t.Fatalf("scrolled view missing far-right path text in %q", scrolled)
	}
}
