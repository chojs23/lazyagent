package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

type focusPane int

const (
	focusSidebar focusPane = iota
	focusEvents
	focusDetail
	paneCount = 3
)

type dataMsg struct {
	projects []model.Project
	sessions []model.Session
	agents   []model.Agent
	events   []model.Event
	rawCount int
	err      error
}

type threadMsg struct {
	events []model.Event
	err    error
}

type tickMsg time.Time

type Model struct {
	store           *store.Store
	refreshInterval time.Duration
	keys            keyMap
	help            help.Model

	sidebar sidebarModel
	events  eventsModel
	detail  detailModel
	filter  filterModel

	focus     focusPane
	status    string
	width     int
	height    int
	lastError error
	lastKey   string

	allSessions []model.Session
}

func Run(st *store.Store, refreshInterval time.Duration) error {
	p := tea.NewProgram(newModel(st, refreshInterval))
	_, err := p.Run()
	return err
}

func newModel(st *store.Store, refreshInterval time.Duration) Model {
	return Model{
		store:           st,
		refreshInterval: refreshInterval,
		keys:            defaultKeyMap(),
		help:            help.New(),
		sidebar:         newSidebar(),
		events:          newEvents(),
		detail:          newDetail(),
		filter:          newFilter(),
		focus:           focusSidebar,
		status:          "Loading...",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadDataCmd(), tickCmd(m.refreshInterval))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.filter.searchMode {
		return m.updateSearch(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case dataMsg:
		if msg.err != nil {
			m.lastError = msg.err
			m.status = msg.err.Error()
			return m, nil
		}
		m.lastError = nil
		m.applyData(msg)
		return m, nil

	case threadMsg:
		if msg.err != nil {
			m.status = "Thread error: " + msg.err.Error()
			return m, nil
		}
		m.detail.setThread(msg.events)
		m.status = fmt.Sprintf("Thread: %d events", len(msg.events))
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.loadDataCmd(), tickCmd(m.refreshInterval))

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// forward to detail viewport
	if m.focus == focusDetail {
		var cmd tea.Cmd
		m.detail.viewport, cmd = m.detail.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.NextPane):
		m.focus = (m.focus + 1) % paneCount
		return m, nil
	case key.Matches(msg, m.keys.PrevPane):
		m.focus = (m.focus + paneCount - 1) % paneCount
		return m, nil
	case key.Matches(msg, m.keys.Sidebar):
		m.focus = focusSidebar
		return m, nil
	case key.Matches(msg, m.keys.Events):
		m.focus = focusEvents
		return m, nil
	case key.Matches(msg, m.keys.Detail):
		m.focus = focusDetail
		return m, nil
	case key.Matches(msg, m.keys.Search):
		m.filter.enterSearch()
		m.status = "Type search query, enter to apply, esc to cancel"
		return m, nil
	case key.Matches(msg, m.keys.CycleType):
		m.filter.cycleType()
		m.status = "Filter: " + m.filter.typeLabel()
		return m, m.loadDataCmd()
	case key.Matches(msg, m.keys.ToggleAuto):
		m.events.toggleAutoFollow()
		m.status = "Auto-follow: " + onOff(m.events.autoFollow)
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		m.status = "Refreshing..."
		return m, m.loadDataCmd()
	case key.Matches(msg, m.keys.AgentAll):
		if m.focus == focusSidebar {
			m.sidebar.selectedAgent = ""
			m.filter.setAgentLabel("all")
			m.status = "Agent filter: all"
			return m, m.loadDataCmd()
		}
	case key.Matches(msg, m.keys.Delete):
		return m.handleDelete()
	case key.Matches(msg, m.keys.ClearEvt):
		return m.handleClearEvents()
	}

	// pane-specific navigation
	switch m.focus {
	case focusSidebar:
		return m.updateSidebar(msg)
	case focusEvents:
		return m.updateEvents(msg)
	case focusDetail:
		if kmsg, ok := msg.(tea.KeyMsg); ok {
			k := kmsg.String()
			defer func() { m.lastKey = k }()
			switch k {
			case "ctrl+d":
				m.detail.viewport.HalfPageDown()
				return m, nil
			case "ctrl+u":
				m.detail.viewport.HalfPageUp()
				return m, nil
			case "G":
				m.detail.viewport.GotoBottom()
				return m, nil
			case "g":
				if m.lastKey == "g" {
					m.detail.viewport.GotoTop()
					m.lastKey = ""
					return m, nil
				}
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.detail.viewport, cmd = m.detail.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	defer func() { m.lastKey = k }()
	switch k {
	case "j", "down":
		m.sidebar.moveDown()
	case "k", "up":
		m.sidebar.moveUp()
	case "ctrl+d":
		m.sidebar.halfPageDown(m.sidebar.height / 2)
	case "ctrl+u":
		m.sidebar.halfPageUp(m.sidebar.height / 2)
	case "G":
		m.sidebar.goBottom()
	case "g":
		if m.lastKey == "g" {
			m.sidebar.goTop()
			m.lastKey = ""
			return m, nil
		}
	case "enter":
		if m.sidebar.focusAgents {
			m.sidebar.enter()
			agentLabel := "all"
			if id := m.sidebar.selectedAgentID(); id != "" {
				agentLabel = shortID(id)
				if a, _ := m.store.Read().GetAgentByID(context.Background(), id); a != nil && a.Name != "" {
					agentLabel = a.Name
				}
			}
			m.filter.setAgentLabel(agentLabel)
			return m, m.loadDataCmd()
		}
		if m.sidebar.enter() {
			return m, m.loadDataCmd()
		}
	case "tab":
		m.sidebar.toggleAgentFocus()
	}
	return m, nil
}

func (m Model) updateEvents(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	defer func() { m.lastKey = k }()
	switch k {
	case "j", "down":
		m.events.moveDown()
		m.syncDetailFromEvent()
	case "k", "up":
		m.events.moveUp()
		m.syncDetailFromEvent()
	case "ctrl+d":
		m.events.halfPageDown(m.events.height / 2)
		m.syncDetailFromEvent()
	case "ctrl+u":
		m.events.halfPageUp(m.events.height / 2)
		m.syncDetailFromEvent()
	case "G":
		m.events.goBottom()
		m.syncDetailFromEvent()
	case "g":
		if m.lastKey == "g" {
			m.events.goTop()
			m.syncDetailFromEvent()
			m.lastKey = ""
			return m, nil
		}
	case "enter":
		if needsFetch := m.detail.toggleThread(); needsFetch {
			return m, m.loadThreadCmd()
		}
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			m.filter.commitSearch()
			m.status = "Search: " + orDefault(m.filter.searchQuery, "off")
			return m, m.loadDataCmd()
		case "esc":
			m.filter.cancelSearch()
			m.status = "Search cancelled"
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.filter.searchInput, cmd = m.filter.searchInput.Update(msg)
	return m, cmd
}

func (m *Model) syncDetailFromEvent() {
	ev := m.events.selectedEvent()
	m.detail.setEvent(ev, m.sidebar.agents)
}

func (m *Model) applyData(d dataMsg) {
	m.allSessions = d.sessions
	m.sidebar.setData(d.projects, d.sessions, d.agents)

	// auto-expand first project and select first session if nothing selected
	if m.sidebar.selectedSession == "" && len(d.sessions) > 0 {
		m.sidebar.selectedSession = d.sessions[0].ID
		if len(d.projects) > 0 {
			m.sidebar.expandedProjs[d.projects[0].ID] = true
			m.sidebar.rebuildItems()
		}
	}

	m.events.setEvents(d.events, d.rawCount)
	m.syncDetailFromEvent()

	agentLabel := "all"
	if id := m.sidebar.selectedAgentID(); id != "" {
		agentLabel = shortID(id)
	}
	m.filter.setAgentLabel(agentLabel)

	m.status = fmt.Sprintf("P:%d S:%d E:%d/%d A:%d",
		len(d.projects), len(d.sessions), len(d.events), d.rawCount, len(d.agents))
}

func (m Model) handleDelete() (tea.Model, tea.Cmd) {
	if m.focus != focusSidebar || m.sidebar.focusAgents {
		return m, nil
	}
	item := m.sidebar.currentItem()
	if item == nil {
		return m, nil
	}
	ctx := context.Background()
	switch item.kind {
	case "session":
		m.store.WithTx(ctx, func(q *store.Queries) error {
			return q.DeleteSession(ctx, item.sessionID)
		})
		m.sidebar.selectedSession = ""
		m.status = "Session deleted"
	case "project":
		m.store.WithTx(ctx, func(q *store.Queries) error {
			return q.DeleteProject(ctx, item.projectID)
		})
		m.sidebar.selectedSession = ""
		m.status = "Project deleted"
	}
	return m, m.loadDataCmd()
}

func (m Model) handleClearEvents() (tea.Model, tea.Cmd) {
	sid := m.sidebar.currentSessionID()
	if sid == "" {
		return m, nil
	}
	ctx := context.Background()
	m.store.WithTx(ctx, func(q *store.Queries) error {
		return q.ClearSessionEvents(ctx, sid)
	})
	m.status = "Events cleared"
	return m, m.loadDataCmd()
}

func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		return v
	}

	sidebarW := maxInt(m.width/4, 24)
	rightW := m.width - sidebarW
	eventsH := maxInt((m.height-3)*55/100, 6)
	detailH := maxInt(m.height-3-eventsH, 6)
	sidebarH := m.height - 3

	sideView := m.sidebar.view(sidebarW, sidebarH, m.focus == focusSidebar)

	agentMap := buildAgentMap(m.sidebar.agents)
	eventsView := m.events.view(rightW, eventsH, m.focus == focusEvents, agentMap)
	detailView := m.detail.view(rightW, detailH, m.focus == focusDetail)

	right := lipgloss.JoinVertical(lipgloss.Left, eventsView, detailView)
	main := lipgloss.JoinHorizontal(lipgloss.Top, sideView, right)

	filterBar := m.filter.view(m.width)
	helpLine := m.help.View(m.keys)
	statusLine := statusBarStyle.Render(m.status)

	full := lipgloss.JoinVertical(lipgloss.Left, main, filterBar, statusLine+" "+helpLine)

	v := tea.NewView(full)
	v.AltScreen = true
	return v
}

func (m Model) loadDataCmd() tea.Cmd {
	st := m.store
	sessionID := m.sidebar.currentSessionID()
	agentID := m.sidebar.selectedAgentID()
	typeFilter := m.filter.typeValue()
	search := m.filter.searchQuery

	return func() tea.Msg {
		ctx := context.Background()
		q := st.Read()

		projects, err := q.ListProjects(ctx)
		if err != nil {
			return dataMsg{err: err}
		}

		sessions, err := q.ListRecentSessions(ctx, 200)
		if err != nil {
			return dataMsg{err: err}
		}

		var agents []model.Agent
		var events []model.Event
		rawCount := 0

		if sessionID != "" {
			agents, err = q.ListAgentsForSession(ctx, sessionID)
			if err != nil {
				return dataMsg{err: err}
			}

			// get raw count (unfiltered)
			allEvents, err := q.ListEventsForSession(ctx, sessionID, model.EventFilter{Limit: 10000})
			if err != nil {
				return dataMsg{err: err}
			}
			rawCount = len(allEvents)

			filter := model.EventFilter{
				Type:   typeFilter,
				Search: search,
				Limit:  5000,
			}
			if agentID != "" {
				filter.AgentIDs = []string{agentID}
			}
			events, err = q.ListEventsForSession(ctx, sessionID, filter)
			if err != nil {
				return dataMsg{err: err}
			}
		}

		return dataMsg{
			projects: projects,
			sessions: sessions,
			agents:   agents,
			events:   events,
			rawCount: rawCount,
		}
	}
}

func (m Model) loadThreadCmd() tea.Cmd {
	st := m.store
	eventID := m.events.selectedEventID()
	return func() tea.Msg {
		if eventID == 0 {
			return threadMsg{}
		}
		events, err := st.Read().GetEventThread(context.Background(), eventID)
		return threadMsg{events: events, err: err}
	}
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func buildAgentMap(agents []model.Agent) map[string]int {
	m := make(map[string]int, len(agents))
	for i, a := range agents {
		m[a.ID] = i
	}
	return m
}
