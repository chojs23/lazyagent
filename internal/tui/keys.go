package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	NextPane     key.Binding
	PrevPane     key.Binding
	PaneProjects key.Binding
	PaneSession  key.Binding
	PaneAgents   key.Binding
	PaneEvents   key.Binding
	PaneDetail   key.Binding
	Search       key.Binding
	Refresh      key.Binding
	ToggleAuto   key.Binding
	CycleType    key.Binding
	CycleTypeRev key.Binding
	AgentAll     key.Binding
	Delete       key.Binding
	ClearEvt     key.Binding
	Help         key.Binding
	Quit         key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		NextPane:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		PrevPane:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("S-tab", "prev pane")),
		PaneProjects: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "projects")),
		PaneSession:  key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "session")),
		PaneAgents:   key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "agents")),
		PaneEvents:   key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "events")),
		PaneDetail:   key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "detail")),
		Search:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		ToggleAuto:   key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "auto-follow")),
		CycleType:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "type filter")),
		CycleTypeRev: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "type filter rev")),
		AgentAll:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all agents")),
		Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		ClearEvt:     key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "clear events")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextPane, k.Search, k.CycleType, k.ToggleAuto, k.Refresh, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.NextPane, k.PrevPane, k.PaneProjects, k.PaneSession, k.PaneAgents, k.PaneEvents, k.PaneDetail},
		{k.Search, k.CycleType, k.ToggleAuto, k.AgentAll, k.Refresh, k.Quit},
	}
}
