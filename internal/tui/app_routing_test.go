package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

func testKey(text string) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Text: text})
}

func testRoutingTUIStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tui-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSetFocusUpdatesLayout(t *testing.T) {
	m := newModel(nil, time.Second)
	m.width = 100
	m.height = 30

	m.setFocus(focusDetail)

	if m.focus != focusDetail {
		t.Fatalf("focus = %v, want %v", m.focus, focusDetail)
	}
	if m.detail.viewport.Width() == 0 || m.detail.viewport.Height() == 0 {
		t.Fatalf("detail viewport not sized after focus change: %dx%d", m.detail.viewport.Width(), m.detail.viewport.Height())
	}
}

func TestActivateProjectSelectionResetsAgentsAndSyncsSession(t *testing.T) {
	m := newModel(nil, time.Second)
	m.allProjects = []model.Project{{ID: 7, Name: "proj", Directory: "/tmp/proj"}}
	m.allSessions = []model.Session{{ID: "sess-1", ProjectID: 7, ProjectName: "proj", Runtime: "claude"}}
	m.projects.selectedSession = "sess-1"
	m.agents.selectedAgent = "agent-1"
	m.agents.cursor = 3

	cmd := m.activateProjectSelection()

	if cmd == nil {
		t.Fatal("activateProjectSelection should return reload command")
	}
	if m.agents.selectedAgent != "" {
		t.Fatalf("selectedAgent = %q, want empty", m.agents.selectedAgent)
	}
	if m.agents.cursor != 0 {
		t.Fatalf("agent cursor = %d, want 0", m.agents.cursor)
	}
	if m.session.session == nil || m.session.session.ID != "sess-1" {
		t.Fatalf("session pane did not sync selected session: %#v", m.session.session)
	}
}

func TestSelectedAgentLabelPrefersStoredName(t *testing.T) {
	st := testRoutingTUIStore(t)
	m := newModel(st, time.Second)
	m.agents.selectedAgent = "agent-1"

	ctx := t.Context()
	if err := st.WithTx(ctx, func(q *store.Queries) error {
		projectID, err := q.CreateProject(ctx, "proj", "proj", "/tmp/proj", "")
		if err != nil {
			return err
		}
		if err := q.UpsertSession(ctx, "sess-1", "", projectID, "", "claude", nil, 1, ""); err != nil {
			return err
		}
		return q.UpsertAgent(ctx, "agent-1", "sess-1", "", "Planner", "", "", "")
	}); err != nil {
		t.Fatal(err)
	}

	if got := m.selectedAgentLabel(); got != "Planner" {
		t.Fatalf("selectedAgentLabel = %q, want Planner", got)
	}
}

func TestApplyAgentSelectionUpdatesFilterLabel(t *testing.T) {
	m := newModel(nil, time.Second)
	m.agents.selectedAgent = "agent-abcdef123456"

	cmd := m.applyAgentSelection()

	if cmd == nil {
		t.Fatal("applyAgentSelection should return reload command")
	}
	if got := m.filter.agentLabel; got != shortID("agent-abcdef123456") {
		t.Fatalf("agent label = %q, want %q", got, shortID("agent-abcdef123456"))
	}
}

func TestSelectEventAtSyncsDetailAndDisablesAutoFollow(t *testing.T) {
	m := newModel(nil, time.Second)
	m.agents.setAgents([]model.Agent{{ID: "agent-1", Name: "main"}})
	m.events.setEvents([]model.Event{{ID: 10, AgentID: "agent-1"}, {ID: 20, AgentID: "agent-1"}}, 2, 0)
	m.events.autoFollow = true

	m.selectEventAt(0)

	if m.events.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.events.cursor)
	}
	if m.events.autoFollow {
		t.Fatal("autoFollow should be disabled after selecting an explicit event")
	}
	if m.detail.event == nil || m.detail.event.ID != 10 {
		t.Fatalf("detail event = %#v, want ID 10", m.detail.event)
	}
}

func TestSyncEventSelectionAndMaybeLoadOlderReturnsCommandAtThreshold(t *testing.T) {
	m := newModel(nil, time.Second)
	m.agents.setAgents([]model.Agent{{ID: "agent-1"}})
	m.events.events = makeEvents(eventsPageSize)
	m.events.loadedOffset = 10
	m.events.cursor = 0

	cmd := m.syncEventSelectionAndMaybeLoadOlder()

	if cmd == nil {
		t.Fatal("expected load older command when cursor is near top and older events exist")
	}
	if m.detail.event == nil || m.detail.event.ID != 0 {
		t.Fatalf("detail event = %#v, want selected top event", m.detail.event)
	}
}

func TestUpdateProjectsDoubleGoesTop(t *testing.T) {
	m := newModel(nil, time.Second)
	m.projects.setData(
		[]model.Project{{ID: 1, Name: "proj", SessionCount: 2}},
		[]model.Session{
			{ID: "session-1", ProjectID: 1, Runtime: "claude", StartedAt: 1},
			{ID: "session-2", ProjectID: 1, Runtime: "claude", StartedAt: 2},
		},
	)
	m.projects.enter()
	m.projects.cursor = 1

	updated, cmd := m.updateProjects(testKey("g"))
	if cmd != nil {
		t.Fatal("first g should not return command")
	}
	m = updated.(Model)
	if m.lastKey != "g" {
		t.Fatalf("lastKey = %q, want g", m.lastKey)
	}

	updated, cmd = m.updateProjects(testKey("g"))
	if cmd != nil {
		t.Fatal("second g in projects should not return command")
	}
	m = updated.(Model)
	if m.projects.cursor != 0 {
		t.Fatalf("projects cursor = %d, want 0", m.projects.cursor)
	}
	if m.lastKey != "" {
		t.Fatalf("lastKey after gg = %q, want empty", m.lastKey)
	}
}

func TestUpdateProjectsHorizontalScrollClamps(t *testing.T) {
	m := newModel(nil, time.Second)
	m.projects.hScroll = 1

	updated, _ := m.updateProjects(testKey("h"))
	m = updated.(Model)
	if m.projects.hScroll != 0 {
		t.Fatalf("projects hScroll after h = %d, want 0", m.projects.hScroll)
	}

	updated, _ = m.updateProjects(testKey("l"))
	m = updated.(Model)
	if m.projects.hScroll != 4 {
		t.Fatalf("projects hScroll after l = %d, want 4", m.projects.hScroll)
	}
}

func TestUpdateEventsDoubleGRequestsOlderAndClearsLastKey(t *testing.T) {
	m := newModel(nil, time.Second)
	m.agents.setAgents([]model.Agent{{ID: "agent-1"}})
	m.events.events = makeEvents(eventsPageSize)
	m.events.loadedOffset = 10
	m.events.cursor = 5

	updated, cmd := m.updateEvents(testKey("g"))
	if cmd != nil {
		t.Fatal("first g should not return command")
	}
	m = updated.(Model)
	if m.lastKey != "g" {
		t.Fatalf("lastKey = %q, want g", m.lastKey)
	}

	updated, cmd = m.updateEvents(testKey("g"))
	if cmd == nil {
		t.Fatal("second g in events should return sync/load command")
	}
	m = updated.(Model)
	if m.events.cursor != 0 {
		t.Fatalf("events cursor = %d, want 0", m.events.cursor)
	}
	if m.lastKey != "" {
		t.Fatalf("lastKey after gg = %q, want empty", m.lastKey)
	}
}

func TestUpdateEventsHorizontalScrollPreservesSyncPath(t *testing.T) {
	m := newModel(nil, time.Second)
	m.agents.setAgents([]model.Agent{{ID: "agent-1"}})
	m.events.events = makeEvents(eventsPageSize)
	m.events.loadedOffset = 10
	m.events.cursor = 0
	m.events.hScroll = 1

	updated, cmd := m.updateEvents(testKey("h"))
	if cmd == nil {
		t.Fatal("events h should still return sync/load command")
	}
	m = updated.(Model)
	if m.events.hScroll != 0 {
		t.Fatalf("events hScroll after h = %d, want 0", m.events.hScroll)
	}

	updated, cmd = m.updateEvents(testKey("l"))
	if cmd == nil {
		t.Fatal("events l should still return sync/load command")
	}
	m = updated.(Model)
	if m.events.hScroll != 4 {
		t.Fatalf("events hScroll after l = %d, want 4", m.events.hScroll)
	}
}

func TestUpdateEventsEnterStillFocusesDetail(t *testing.T) {
	m := newModel(nil, time.Second)
	m.focus = focusEvents

	updated, cmd := m.updateEvents(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd != nil {
		t.Fatal("events enter should not return command")
	}
	m = updated.(Model)
	if m.focus != focusDetail {
		t.Fatalf("focus = %v, want %v", m.focus, focusDetail)
	}
}
