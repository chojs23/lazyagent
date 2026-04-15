package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

const mouseScrollLines = 3

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}

	// Compute layout from pre-click state so coordinates map correctly
	// even after focus changes the pane sizes.
	sz := m.calcSizes()
	mainH := m.height - m.footerHeight()

	pane := hitTestPane(msg.X, msg.Y, sz, mainH)
	if pane < 0 {
		return m, nil
	}

	m.focus = pane
	m.syncLayout()

	switch pane {
	case focusProjects:
		row := contentRow(msg.Y, 0)
		if row >= 0 {
			idx := m.projects.scroll + row
			if idx < len(m.projects.items) {
				m.projects.cursor = idx
				if m.projects.enter() {
					m.agents.selectedAgent = ""
					m.agents.cursor = 0
					m.syncSessionPane()
					return m, m.loadDataCmd()
				}
			}
		}

	case focusAgents:
		row := contentRow(msg.Y, sz.projH+sz.sessionH)
		if row >= 0 {
			idx := m.agents.scroll + row
			if idx < len(m.agents.agents) {
				m.agents.cursor = idx
				m.agents.enter()
				agentLabel := "all"
				if id := m.agents.selectedAgentID(); id != "" {
					agentLabel = shortID(id)
					if a, _ := m.store.Read().GetAgentByID(context.Background(), id); a != nil && a.Name != "" {
						agentLabel = a.Name
					}
				}
				m.filter.setAgentLabel(agentLabel)
				return m, m.loadDataCmd()
			}
		}

	case focusEvents:
		row := contentRow(msg.Y, 0)
		if row >= 0 {
			idx := m.events.scroll + row
			if idx < len(m.events.events) {
				m.events.cursor = idx
				m.events.autoFollow = false
				m.events.clampScroll()
				m.syncDetailFromEvent()
			}
		}
	}

	return m, nil
}

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	sz := m.calcSizes()
	mainH := m.height - m.footerHeight()

	pane := hitTestPane(msg.X, msg.Y, sz, mainH)
	if pane < 0 {
		return m, nil
	}

	up := msg.Button == tea.MouseWheelUp

	switch pane {
	case focusProjects:
		for range mouseScrollLines {
			if up {
				m.projects.moveUp()
			} else {
				m.projects.moveDown()
			}
		}

	case focusSession:
		for range mouseScrollLines {
			if up {
				m.session.moveUp()
			} else {
				m.session.moveDown()
			}
		}

	case focusAgents:
		for range mouseScrollLines {
			if up {
				m.agents.moveUp()
			} else {
				m.agents.moveDown()
			}
		}

	case focusEvents:
		for range mouseScrollLines {
			if up {
				m.events.moveUp()
			} else {
				m.events.moveDown()
			}
		}
		m.syncDetailFromEvent()
		if m.events.needsOlder() {
			return m, m.loadOlderEventsCmd()
		}

	case focusDetail:
		if up {
			m.detail.viewport.ScrollUp(mouseScrollLines)
		} else {
			m.detail.viewport.ScrollDown(mouseScrollLines)
		}
	}

	return m, nil
}

// hitTestPane returns which pane contains the given terminal coordinates,
// or -1 if coordinates fall outside any pane (e.g. footer area).
func hitTestPane(x, y int, sz paneSizes, mainH int) focusPane {
	if y >= mainH {
		return -1
	}

	if x < sz.sidebarW {
		if y < sz.projH {
			return focusProjects
		}
		if y < sz.projH+sz.sessionH {
			return focusSession
		}
		return focusAgents
	}

	if y < sz.eventsH {
		return focusEvents
	}
	if y < sz.eventsH+sz.detailH {
		return focusDetail
	}
	return -1
}

// contentRow converts a terminal y coordinate to a zero-based content row
// index within a pane. Returns -1 if the coordinate falls in the border or
// title area. paneY is the y offset where the pane starts on screen.
func contentRow(y, paneY int) int {
	// row 0 = top border, row 1 = title, row 2+ = content
	row := y - paneY - 2
	if row < 0 {
		return -1
	}
	return row
}
