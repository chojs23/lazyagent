package tui

import (
	"strings"

	"github.com/chojs23/lazyagent/internal/model"
)

type agentsModel struct {
	agents        []model.Agent
	cursor        int
	scroll        int
	selectedAgent string
	height        int
}

func newAgents() agentsModel {
	return agentsModel{}
}

func (a *agentsModel) setAgents(agents []model.Agent) {
	a.agents = agents
	if a.cursor >= len(a.agents) {
		a.cursor = maxInt(len(a.agents)-1, 0)
	}
}

func (a *agentsModel) moveUp() {
	if a.cursor > 0 {
		a.cursor--
	}
}

func (a *agentsModel) moveDown() {
	if a.cursor < len(a.agents)-1 {
		a.cursor++
	}
}

func (a *agentsModel) halfPageUp(viewH int) {
	a.cursor = maxInt(a.cursor-viewH/2, 0)
}

func (a *agentsModel) halfPageDown(viewH int) {
	a.cursor = minInt(a.cursor+viewH/2, maxInt(len(a.agents)-1, 0))
}

func (a *agentsModel) goTop() {
	a.cursor = 0
}

func (a *agentsModel) goBottom() {
	if len(a.agents) > 0 {
		a.cursor = len(a.agents) - 1
	}
}

func (a *agentsModel) enter() {
	if a.cursor < len(a.agents) {
		ag := a.agents[a.cursor]
		if a.selectedAgent == ag.ID {
			a.selectedAgent = "" // toggle off
		} else {
			a.selectedAgent = ag.ID
		}
	}
}

func (a *agentsModel) selectedAgentID() string {
	return a.selectedAgent
}

func (a *agentsModel) view(width, height int, focused bool) string {
	a.height = height

	title := titleStyle.Render("Agents")

	contentHeight := maxInt(height-3, 1)

	var lines []string
	for i, ag := range a.agents {
		prefix := "  "
		if i == a.cursor {
			prefix = "> "
		}
		name := orDefault(ag.Name, shortID(ag.ID))
		if ag.AgentType != "" {
			name += " (" + ag.AgentType + ")"
		}
		tree := ""
		if ag.ParentAgentID != "" {
			if i == len(a.agents)-1 || a.agents[i+1].ParentAgentID == "" {
				tree = "└─ "
			} else {
				tree = "├─ "
			}
		}
		var line string
		switch {
		case i == a.cursor:
			style := cursorStyle
			if ag.ID == a.selectedAgent {
				style = cursorSelectedStyle
			}
			line = style.Render("  " + tree + name)
		case ag.ID == a.selectedAgent:
			line = selectedStyle.Render(prefix + tree + name)
		default:
			line = prefix + tree + name
		}
		lines = append(lines, line)
	}

	if a.cursor >= a.scroll+contentHeight {
		a.scroll = a.cursor - contentHeight + 1
	}
	if a.cursor < a.scroll {
		a.scroll = a.cursor
	}
	maxScroll := maxInt(len(lines)-contentHeight, 0)
	a.scroll = minInt(a.scroll, maxScroll)

	visible := sliceLines(lines, a.scroll, contentHeight)
	content := title + "\n" + strings.Join(visible, "\n")
	return paneStyle(focused).Width(width).Render(content)
}

func sliceLines(lines []string, offset, count int) []string {
	if offset >= len(lines) {
		return nil
	}
	end := minInt(offset+count, len(lines))
	return lines[offset:end]
}
