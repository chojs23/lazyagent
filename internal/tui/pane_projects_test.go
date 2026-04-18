package tui

import (
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func TestBuildProjectSessionLabelUsesSlugWhenAvailable(t *testing.T) {
	sess := model.Session{
		ID:         "session-abcdef1234567890",
		Slug:       "fix broken filtered paging\nextra detail should stay hidden",
		Runtime:    "codex",
		StartedAt:  1712700000000,
		EventCount: 99,
		AgentCount: 7,
	}

	label := buildProjectSessionLabel("  ", "└─ ", sess)

	if !strings.Contains(label, formatTime(sess.StartedAt)) {
		t.Fatalf("label missing start time: %q", label)
	}
	if !strings.Contains(label, "[X]") {
		t.Fatalf("label missing runtime marker: %q", label)
	}
	if !strings.Contains(label, " - ") {
		t.Fatalf("label missing time-id separator: %q", label)
	}
	if !strings.Contains(label, "fix broken filtered paging") {
		t.Fatalf("label missing slug: %q", label)
	}
	if strings.Contains(label, "extra detail should stay hidden") {
		t.Fatalf("label should use only first slug line: %q", label)
	}
	if strings.Contains(label, shortID(sess.ID)) {
		t.Fatalf("label should prefer slug over short id: %q", label)
	}
	if strings.Contains(label, "e:") || strings.Contains(label, "a:") {
		t.Fatalf("label should not contain counters: %q", label)
	}
}

func TestBuildProjectSessionLabelFallsBackToShortIDWithoutSlug(t *testing.T) {
	sess := model.Session{
		ID:        "session-abcdef1234567890",
		Runtime:   "codex",
		StartedAt: 1712700000000,
	}

	label := buildProjectSessionLabel("  ", "└─ ", sess)

	if !strings.Contains(label, shortID(sess.ID)) {
		t.Fatalf("label missing short id fallback: %q", label)
	}
}

func TestProjectsEnterTogglesExpansionAndShowsOnlyRootSessions(t *testing.T) {
	pane := newProjects()
	pane.setData(
		[]model.Project{{ID: 1, Name: "proj", SessionCount: 2}},
		[]model.Session{
			{ID: "root-session", ProjectID: 1, ParentSessionID: "", Runtime: "claude", StartedAt: 1712700000000},
			{ID: "child-session", ProjectID: 1, ParentSessionID: "root-session", Runtime: "claude", StartedAt: 1712700100000},
		},
	)

	if len(pane.items) != 1 {
		t.Fatalf("collapsed items len = %d, want 1", len(pane.items))
	}

	if changed := pane.enter(); changed {
		t.Fatal("project enter should not report session change")
	}
	if !pane.expandedProjs[1] {
		t.Fatal("project should be marked expanded")
	}
	if len(pane.items) != 2 {
		t.Fatalf("expanded items len = %d, want 2", len(pane.items))
	}
	if pane.items[1].kind != "session" || pane.items[1].sessionID != "root-session" {
		t.Fatalf("expanded session item = %#v, want root session", pane.items[1])
	}

	if changed := pane.enter(); changed {
		t.Fatal("collapsing project should not report session change")
	}
	if pane.expandedProjs[1] {
		t.Fatal("project should be collapsed again")
	}
	if len(pane.items) != 1 {
		t.Fatalf("collapsed items len after second enter = %d, want 1", len(pane.items))
	}
}

func TestProjectsEnterSelectsSessionOnce(t *testing.T) {
	pane := newProjects()
	pane.expandedProjs[1] = true
	pane.setData(
		[]model.Project{{ID: 1, Name: "proj", SessionCount: 1}},
		[]model.Session{{ID: "root-session", ProjectID: 1, Runtime: "claude", StartedAt: 1712700000000}},
	)
	pane.cursor = 1

	if changed := pane.enter(); !changed {
		t.Fatal("selecting a new session should report session change")
	}
	if pane.selectedSession != "root-session" {
		t.Fatalf("selectedSession = %q, want root-session", pane.selectedSession)
	}

	if changed := pane.enter(); changed {
		t.Fatal("re-selecting same session should not report session change")
	}
}

func TestProjectsCurrentItemAndCurrentSessionID(t *testing.T) {
	pane := newProjects()
	pane.expandedProjs[1] = true
	pane.setData(
		[]model.Project{{ID: 1, Name: "proj", SessionCount: 1}},
		[]model.Session{{ID: "root-session", ProjectID: 1, Runtime: "claude", StartedAt: 1712700000000}},
	)
	pane.cursor = 1
	pane.selectedSession = "root-session"

	item := pane.currentItem()
	if item == nil {
		t.Fatal("currentItem() returned nil")
	}
	if item.kind != "session" || item.sessionID != "root-session" {
		t.Fatalf("currentItem() = %#v, want selected session item", item)
	}
	if got := pane.currentSessionID(); got != "root-session" {
		t.Fatalf("currentSessionID() = %q, want root-session", got)
	}
}

func TestProjectsSessionIconsShowSelectionAndSpinnerInRawMode(t *testing.T) {
	pane := newProjects()
	pane.selectedSession = "session-1"
	pane.spinnerFrame = 0
	pane.sessions = []model.Session{{ID: "session-1", Status: "active"}}

	icons := pane.sessionIcons(sidebarItem{kind: "session", sessionID: "session-1"}, true)

	if icons != "* ⠋ " {
		t.Fatalf("sessionIcons(raw) = %q, want %q", icons, "* ⠋ ")
	}
}

func TestProjectsViewShowsExpandedProjectArrowAndSessionLine(t *testing.T) {
	pane := newProjects()
	pane.expandedProjs[1] = true
	pane.selectedSession = "root-session"
	pane.spinnerFrame = 0
	pane.setData(
		[]model.Project{{ID: 1, Name: "proj", SessionCount: 1}},
		[]model.Session{{ID: "root-session", ProjectID: 1, Runtime: "claude", StartedAt: 1712700000000, Status: "active"}},
	)

	view := pane.view(80, 10, false)

	for _, want := range []string{
		"Projects",
		"▼ proj (1)",
		formatTime(1712700000000),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in %q", want, view)
		}
	}
}
