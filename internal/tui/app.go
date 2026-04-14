package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/applog"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

type focusPane int

const (
	focusProjects focusPane = iota
	focusSession
	focusAgents
	focusEvents
	focusDetail
	paneCount = 5
)

type dataMsg struct {
	projects []model.Project
	sessions []model.Session
	agents   []model.Agent
	events   []model.Event
	rawCount int
	offset   int // offset of the first loaded event within the (possibly filtered) result set
	err      error
}

type moreEventsMsg struct {
	events []model.Event
	offset int
	err    error
}

type tickMsg time.Time
type spinnerTickMsg time.Time

type Model struct {
	store           *store.Store
	refreshInterval time.Duration
	keys            keyMap
	help            help.Model

	projects projectsModel
	session  sessionInfoModel
	agents   agentsModel
	events   eventsModel
	detail   detailModel
	filter   filterModel

	focus     focusPane
	status    string
	width     int
	height    int
	lastError error
	lastKey   string

	errorOverlay errorOverlay
	debug        *debugOverlay

	allProjects []model.Project
	allSessions []model.Session
}

func Run(st *store.Store, refreshInterval time.Duration) error {
	p := tea.NewProgram(newModel(st, refreshInterval))
	_, err := p.Run()
	return err
}

func newModel(st *store.Store, refreshInterval time.Duration) Model {
	m := Model{
		store:           st,
		refreshInterval: refreshInterval,
		keys:            defaultKeyMap(),
		help:            help.New(),
		projects:        newProjects(),
		session:         newSessionInfo(),
		agents:          newAgents(),
		events:          newEvents(),
		detail:          newDetail(),
		filter:          newFilter(),
		focus:           focusProjects,
		status:          "Loading...",
		debug:           &debugOverlay{},
	}
	setGlobalDebug(m.debug)
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadDataCmd(), tickCmd(m.refreshInterval), spinnerTickCmd())
}

func (m *Model) syncLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	sz := m.calcSizes()
	m.projects.height = sz.projH
	m.syncSessionPane()
	m.agents.height = sz.agentH
	m.events.height = sz.eventsH
	m.detail.viewport.SetWidth(max(sz.rightW-4, 10))
	m.detail.viewport.SetHeight(max(sz.detailH-3, 4))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle non-key messages first so they are processed regardless of
	// whether the search input is active.
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncLayout()
		return m, nil

	case dataMsg:
		if msg.err != nil {
			m.reportError("Refresh failed", msg.err)
			return m, nil
		}
		m.lastError = nil
		m.applyData(msg)
		return m, nil

	case moreEventsMsg:
		if msg.err != nil {
			m.reportError("Load older events failed", msg.err)
			return m, nil
		}
		if len(msg.events) > 0 {
			m.events.prependEvents(msg.events, msg.offset)
			m.syncDetailFromEvent()
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.loadDataCmd(), tickCmd(m.refreshInterval))

	case spinnerTickMsg:
		m.errorOverlay.update(time.Time(msg))
		m.agents.tick()
		m.projects.tick()
		return m, spinnerTickCmd()
	}

	// While the search input is focused, route remaining messages
	// (primarily key events) to the search handler.
	if m.filter.searchMode {
		return m.updateSearch(msg)
	}

	// When the debug overlay is open, capture keys for its navigation
	// but allow the toggle key to pass through to handleKey.
	if m.debug.isVisible() {
		if msg, ok := msg.(tea.KeyMsg); ok {
			switch msg.String() {
			case "j", "down":
				m.debug.scrollDown(1)
				return m, nil
			case "k", "up":
				m.debug.scrollUp(1)
				return m, nil
			case "G":
				m.debug.scrollToNewest()
				return m, nil
			case "g":
				if m.lastKey == "g" {
					m.debug.scrollToOldest()
					m.lastKey = ""
					return m, nil
				}
				m.lastKey = "g"
				return m, nil
			case "c":
				m.debug.clear()
				return m, nil
			case "`", "esc":
				m.debug.toggle()
				return m, nil
			}
			return m, nil
		}
	}

	switch msg := msg.(type) {
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
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.PrevPane):
		m.focus = (m.focus + paneCount - 1) % paneCount
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.PaneProjects):
		m.focus = focusProjects
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.PaneSession):
		m.focus = focusSession
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.PaneAgents):
		m.focus = focusAgents
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.PaneEvents):
		m.focus = focusEvents
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.PaneDetail):
		m.focus = focusDetail
		m.syncLayout()
		return m, nil
	case msg.Key().Code == tea.KeyEscape && m.filter.searchQuery != "":
		m.filter.clearSearch()
		m.status = "Search: off"
		return m, m.loadDataCmd()
	case key.Matches(msg, m.keys.Search):
		m.filter.enterSearch()
		m.status = "Type search query, enter to apply, esc to cancel"
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.CycleType):
		m.filter.cycleType()
		m.status = "Filter: " + m.filter.typeLabel()
		return m, m.loadDataCmd()
	case key.Matches(msg, m.keys.CycleTypeRev):
		m.filter.cycleTypeReverse()
		m.status = "Filter: " + m.filter.typeLabel()
		return m, m.loadDataCmd()
	case key.Matches(msg, m.keys.ToggleAuto):
		m.events.toggleAutoFollow()
		m.status = "Auto-follow: " + onOff(m.events.autoFollow)
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		m.status = "Refreshing..."
		return m, m.loadDataCmd()
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.AgentAll):
		if m.focus == focusAgents {
			m.agents.selectedAgent = ""
			m.filter.setAgentLabel("all")
			m.status = "Agent filter: all"
			return m, m.loadDataCmd()
		}
	case key.Matches(msg, m.keys.DebugLog):
		m.debug.toggle()
		return m, nil
	case key.Matches(msg, m.keys.Delete):
		return m.handleDelete()
	case key.Matches(msg, m.keys.ClearEvt):
		return m.handleClearEvents()
	}

	// pane-specific navigation
	switch m.focus {
	case focusProjects:
		return m.updateProjects(msg)
	case focusSession:
		return m.updateSession(msg)
	case focusAgents:
		return m.updateAgents(msg)
	case focusEvents:
		return m.updateEvents(msg)
	case focusDetail:
		k := msg.String()
		switch k {
		case "esc":
			m.focus = focusEvents
			m.syncLayout()
			m.lastKey = k
			return m, nil
		case "J":
			m.detail.toggleJSON()
			m.lastKey = k
			return m, nil
		case "e":
			m.detail.toggleExpand()
			m.lastKey = k
			return m, nil
		case "G":
			m.detail.viewport.GotoBottom()
			m.lastKey = k
			return m, nil
		case "g":
			if m.lastKey == "g" {
				m.detail.viewport.GotoTop()
				m.lastKey = ""
				return m, nil
			}
			m.lastKey = "g"
			return m, nil
		}
		m.lastKey = k
		var cmd tea.Cmd
		m.detail.viewport, cmd = m.detail.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateProjects(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "j", "down":
		m.projects.moveDown()
	case "k", "up":
		m.projects.moveUp()
	case "ctrl+d":
		m.projects.halfPageDown(m.projects.height)
	case "ctrl+u":
		m.projects.halfPageUp(m.projects.height)
	case "G":
		m.projects.goBottom()
	case "g":
		if m.lastKey == "g" {
			m.projects.goTop()
			m.lastKey = ""
			return m, nil
		}
		m.lastKey = "g"
		return m, nil
	case "l", "right":
		m.projects.hScroll += 4
	case "h", "left":
		m.projects.hScroll = max(m.projects.hScroll-4, 0)
	case "enter", "space":
		if m.projects.enter() {
			m.agents.selectedAgent = ""
			m.agents.cursor = 0
			m.syncSessionPane()
			m.lastKey = k
			return m, m.loadDataCmd()
		}
	}
	m.lastKey = k
	return m, nil
}

func (m Model) updateSession(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "j", "down":
		m.session.moveDown()
	case "k", "up":
		m.session.moveUp()
	case "ctrl+d":
		m.session.halfPageDown(m.session.height)
	case "ctrl+u":
		m.session.halfPageUp(m.session.height)
	case "G":
		m.session.goBottom()
	case "g":
		if m.lastKey == "g" {
			m.session.goTop()
			m.lastKey = ""
			return m, nil
		}
		m.lastKey = "g"
		return m, nil
	case "l", "right":
		m.session.hScroll += 4
	case "h", "left":
		m.session.hScroll = max(m.session.hScroll-4, 0)
	}
	m.lastKey = k
	return m, nil
}

func (m Model) updateAgents(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "j", "down":
		m.agents.moveDown()
	case "k", "up":
		m.agents.moveUp()
	case "ctrl+d":
		m.agents.halfPageDown(m.agents.height)
	case "ctrl+u":
		m.agents.halfPageUp(m.agents.height)
	case "G":
		m.agents.goBottom()
	case "g":
		if m.lastKey == "g" {
			m.agents.goTop()
			m.lastKey = ""
			return m, nil
		}
		m.lastKey = "g"
		return m, nil
	case "l", "right":
		m.agents.hScroll += 4
	case "h", "left":
		m.agents.hScroll = max(m.agents.hScroll-4, 0)
	case "enter", "space":
		m.agents.enter()
		agentLabel := "all"
		if id := m.agents.selectedAgentID(); id != "" {
			agentLabel = shortID(id)
			if a, _ := m.store.Read().GetAgentByID(context.Background(), id); a != nil && a.Name != "" {
				agentLabel = a.Name
			}
		}
		m.filter.setAgentLabel(agentLabel)
		m.lastKey = k
		return m, m.loadDataCmd()
	}
	m.lastKey = k
	return m, nil
}

func (m Model) updateEvents(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "j", "down":
		m.events.moveDown()
		m.syncDetailFromEvent()
	case "k", "up":
		m.events.moveUp()
		m.syncDetailFromEvent()
	case "ctrl+d":
		m.events.halfPageDown(m.events.height)
		m.syncDetailFromEvent()
	case "ctrl+u":
		m.events.halfPageUp(m.events.height)
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
		m.lastKey = "g"
		return m, nil
	case "l", "right":
		m.events.hScroll += 4
	case "h", "left":
		m.events.hScroll = max(m.events.hScroll-4, 0)
	case "enter":
		m.focus = focusDetail
		m.syncLayout()
		m.lastKey = k
		return m, nil
	}
	m.lastKey = k
	if m.events.needsOlder() {
		return m, m.loadOlderEventsCmd()
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Key().Code {
		case tea.KeyEnter:
			m.filter.commitSearch()
			m.status = "Search: " + orDefault(m.filter.searchQuery, "off")
			m.syncLayout()
			return m, m.loadDataCmd()
		case tea.KeyEscape:
			m.filter.cancelSearch()
			m.status = "Search cancelled"
			m.syncLayout()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.filter.searchInput, cmd = m.filter.searchInput.Update(msg)
	return m, cmd
}

func (m *Model) syncDetailFromEvent() {
	ev := m.events.selectedEvent()
	m.detail.setEvent(ev, m.agents.agents)
}

func (m *Model) applyData(d dataMsg) {
	m.allProjects = d.projects
	m.allSessions = d.sessions
	m.projects.setData(d.projects, d.sessions)
	m.agents.setAgents(d.agents)

	// auto-expand project of the first session if nothing selected
	if m.projects.selectedSession == "" && len(d.sessions) > 0 {
		m.projects.selectedSession = d.sessions[0].ID
		m.projects.expandedProjs[d.sessions[0].ProjectID] = true
		m.projects.rebuildItems()
	}

	m.syncSessionPane()
	m.events.setEvents(d.events, d.rawCount, d.offset)
	m.syncDetailFromEvent()

	agentLabel := "all"
	if id := m.agents.selectedAgentID(); id != "" {
		agentLabel = shortID(id)
	}
	m.filter.setAgentLabel(agentLabel)

	m.status = fmt.Sprintf("P:%d S:%d E:%d/%d A:%d",
		len(d.projects), len(d.sessions), len(d.events), d.rawCount, len(d.agents))
}

func (m *Model) reportError(context string, err error) {
	if err == nil {
		return
	}
	applog.Error(context, err)
	m.debug.add("%s: %s", context, err.Error())
	m.lastError = err
	m.status = context + ": " + err.Error()
	m.errorOverlay.show(context, err.Error())
}

func (m *Model) syncSessionPane() {
	session := m.selectedSessionSummary()
	project := m.selectedProjectSummary(session)
	m.session.setSession(session, project)
}

func (m Model) selectedSessionSummary() *model.Session {
	sessionID := m.projects.currentSessionID()
	if sessionID == "" {
		return nil
	}
	for i := range m.allSessions {
		if m.allSessions[i].ID == sessionID {
			return &m.allSessions[i]
		}
	}
	return nil
}

func (m Model) selectedProjectSummary(session *model.Session) *model.Project {
	if session == nil {
		return nil
	}
	for i := range m.allProjects {
		if m.allProjects[i].ID == session.ProjectID {
			return &m.allProjects[i]
		}
	}
	return nil
}

func (m Model) handleDelete() (tea.Model, tea.Cmd) {
	if m.focus != focusProjects {
		return m, nil
	}
	item := m.projects.currentItem()
	if item == nil {
		return m, nil
	}
	ctx := context.Background()
	switch item.kind {
	case "session":
		if err := m.store.WithTx(ctx, func(q *store.Queries) error {
			return q.DeleteSession(ctx, item.sessionID)
		}); err != nil {
			m.reportError("Delete failed", err)
			return m, nil
		}
		m.projects.selectedSession = ""
		m.syncSessionPane()
		m.status = "Session deleted"
	case "project":
		if err := m.store.WithTx(ctx, func(q *store.Queries) error {
			return q.DeleteProject(ctx, item.projectID)
		}); err != nil {
			m.reportError("Delete failed", err)
			return m, nil
		}
		m.projects.selectedSession = ""
		m.syncSessionPane()
		m.status = "Project deleted"
	}
	return m, m.loadDataCmd()
}

func (m Model) handleClearEvents() (tea.Model, tea.Cmd) {
	sid := m.projects.currentSessionID()
	if sid == "" {
		return m, nil
	}
	ctx := context.Background()
	if err := m.store.WithTx(ctx, func(q *store.Queries) error {
		return q.ClearSessionEvents(ctx, sid)
	}); err != nil {
		m.reportError("Clear failed", err)
		return m, nil
	}
	m.status = "Events cleared"
	return m, m.loadDataCmd()
}

type paneSizes struct {
	sidebarW int
	rightW   int
	projH    int
	sessionH int
	agentH   int
	eventsH  int
	detailH  int
}

func (m Model) footerViews() (string, string) {
	filterBar := m.filter.view(m.width)
	statusContent := m.status
	if helpLine := m.help.View(m.keys); helpLine != "" {
		statusContent += " " + helpLine
	}
	statusLine := statusBarStyle.Width(m.width).Render(statusContent)
	return filterBar, statusLine
}

func (m Model) footerHeight() int {
	if m.width <= 0 {
		return 0
	}
	filterBar, statusLine := m.footerViews()
	return lipgloss.Height(filterBar) + lipgloss.Height(statusLine)
}

func (m Model) calcSizes() paneSizes {
	sidebarW := max(m.width/4, 24)
	rightW := m.width - sidebarW
	mainH := max(m.height-m.footerHeight(), 3)
	leftH := mainH
	rightH := mainH

	// left: projects vs session vs agents
	var projH, sessionH, agentH int
	switch m.focus {
	case focusProjects:
		projH, sessionH, agentH = splitThreeHeights(leftH, [3]int{5, 2, 3}, [3]int{6, 4, 4})
	case focusSession:
		projH, sessionH, agentH = splitThreeHeights(leftH, [3]int{3, 4, 3}, [3]int{6, 4, 4})
	case focusAgents:
		projH, sessionH, agentH = splitThreeHeights(leftH, [3]int{3, 2, 5}, [3]int{6, 4, 4})
	default:
		projH, sessionH, agentH = splitThreeHeights(leftH, [3]int{4, 2, 3}, [3]int{6, 4, 4})
	}

	// right: events vs detail — 7:3 ratio based on focus
	var eventsH, detailH int
	switch m.focus {
	case focusEvents:
		eventsH = max(rightH*70/100, 6)
		detailH = max(rightH-eventsH, 4)
	case focusDetail:
		detailH = max(rightH*70/100, 6)
		eventsH = max(rightH-detailH, 4)
	default:
		eventsH = max(rightH*55/100, 6)
		detailH = max(rightH-eventsH, 4)
	}

	return paneSizes{sidebarW, rightW, projH, sessionH, agentH, eventsH, detailH}
}

func splitThreeHeights(total int, weights [3]int, mins [3]int) (int, int, int) {
	base := mins[0] + mins[1] + mins[2]
	if total <= base {
		first := max(total*weights[0]/(weights[0]+weights[1]+weights[2]), 1)
		second := max((total-first)*weights[1]/(weights[1]+weights[2]), 1)
		third := max(total-first-second, 1)
		return first, second, third
	}

	remaining := total - base
	weightSum := weights[0] + weights[1] + weights[2]
	first := mins[0] + remaining*weights[0]/weightSum
	second := mins[1] + remaining*weights[1]/weightSum
	third := total - first - second
	return first, second, third
}

func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		return v
	}

	sz := m.calcSizes()

	projView := m.projects.view(sz.sidebarW, sz.projH, m.focus == focusProjects)
	sessionView := m.session.view(sz.sidebarW, sz.sessionH, m.focus == focusSession)
	agentView := m.agents.view(sz.sidebarW, sz.agentH, m.focus == focusAgents)

	agentMap := buildAgentMap(m.agents.agents)
	eventsView := m.events.view(sz.rightW, sz.eventsH, m.focus == focusEvents, agentMap)
	detailView := m.detail.view(sz.rightW, sz.detailH, m.focus == focusDetail)

	left := lipgloss.JoinVertical(lipgloss.Left, projView, sessionView, agentView)
	right := lipgloss.JoinVertical(lipgloss.Left, eventsView, detailView)
	main := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	filterBar, statusLine := m.footerViews()

	full := lipgloss.JoinVertical(lipgloss.Left, main, filterBar, statusLine)
	if m.debug.isVisible() {
		full = renderOverlay(full, m.width, m.height, m.debug.view(m.width, m.height))
	}
	if m.errorOverlay.visible {
		full = renderOverlay(full, m.width, m.height, m.errorOverlay.view(m.width, m.height))
	}
	if lipgloss.Height(full) > m.height {
		full = lipgloss.NewStyle().MaxHeight(m.height).Render(full)
	}

	v := tea.NewView(full)
	v.AltScreen = true
	return v
}

func (m Model) loadDataCmd() tea.Cmd {
	st := m.store
	sessionID := m.projects.currentSessionID()
	agentID := m.agents.selectedAgentID()
	typeFilter := m.filter.typeValue()
	search := m.filter.searchQuery
	// Preserve the number of already-loaded events on refresh so pagination
	// progress is not lost when the periodic tick reloads data.
	loadedCount := len(m.events.events)

	return func() tea.Msg {
		ctx := context.Background()

		// Auto-stop sessions that have been idle for over 5 minutes
		// with no active child sessions. Handles ungraceful shutdowns.
		if _, err := st.ReapStaleSessions(ctx, 5*60*1000); err != nil {
			return dataMsg{err: err}
		}

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
		eventsOffset := 0

		if sessionID != "" {
			agents, err = q.ListAgentsForSessionTree(ctx, sessionID)
			if err != nil {
				return dataMsg{err: err}
			}

			rawCount, err = q.CountEventsForSessionTree(ctx, sessionID)
			if err != nil {
				return dataMsg{err: err}
			}

			// When filters are active the SQL OFFSET must be relative to the
			// filtered result set, not the total event count. Use a filtered
			// count so pagination and needsOlder() work correctly.
			hasFilter := agentID != "" || typeFilter != "" || search != ""
			filteredCount := rawCount
			if hasFilter {
				cf := model.EventFilter{Type: typeFilter, Search: search}
				if agentID != "" {
					cf.AgentIDs = []string{agentID}
				}
				filteredCount, err = q.CountFilteredEventsForSessionTree(ctx, sessionID, cf)
				if err != nil {
					return dataMsg{err: err}
				}
			}

			// Load from the end so the user sees the latest events first.
			// On refresh, preserve the number of already-loaded events.
			pageLimit := max(eventsPageSize, loadedCount)
			eventsOffset = max(0, filteredCount-pageLimit)
			filter := model.EventFilter{
				Type:   typeFilter,
				Search: search,
				Limit:  pageLimit,
				Offset: eventsOffset,
			}
			if agentID != "" {
				filter.AgentIDs = []string{agentID}
			}
			events, err = q.ListEventsForSessionTree(ctx, sessionID, filter)
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
			offset:   eventsOffset,
		}
	}
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) loadOlderEventsCmd() tea.Cmd {
	st := m.store
	sessionID := m.projects.currentSessionID()
	agentID := m.agents.selectedAgentID()
	typeFilter := m.filter.typeValue()
	search := m.filter.searchQuery
	currentOffset := m.events.loadedOffset

	return func() tea.Msg {
		if sessionID == "" || currentOffset <= 0 {
			return moreEventsMsg{}
		}
		ctx := context.Background()
		q := st.Read()

		newOffset := max(0, currentOffset-eventsPageSize)
		limit := currentOffset - newOffset

		filter := model.EventFilter{
			Type:   typeFilter,
			Search: search,
			Limit:  limit,
			Offset: newOffset,
		}
		if agentID != "" {
			filter.AgentIDs = []string{agentID}
		}
		events, err := q.ListEventsForSessionTree(ctx, sessionID, filter)
		if err != nil {
			return moreEventsMsg{err: err}
		}
		return moreEventsMsg{events: events, offset: newOffset}
	}
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

type agentInfo struct {
	index int
	name  string
}

func buildAgentMap(agents []model.Agent) map[string]agentInfo {
	m := make(map[string]agentInfo, len(agents))
	for i, a := range agents {
		name := shortID(a.ID)
		if a.Name != "" {
			name = a.Name
		}
		m[a.ID] = agentInfo{index: i, name: name}
	}
	return m
}
