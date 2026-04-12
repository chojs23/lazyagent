package tui

import "github.com/chojs23/lazyagent/internal/model"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type agentsModel struct {
	agents        []model.Agent
	cursor        int
	scroll        int
	hScroll       int
	selectedAgent string
	height        int
	spinnerFrame  int
}

func newAgents() agentsModel {
	return agentsModel{}
}

func (a *agentsModel) setAgents(agents []model.Agent) {
	a.agents = agents
	if a.cursor >= len(a.agents) {
		a.cursor = max(len(a.agents)-1, 0)
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
	a.cursor = max(a.cursor-viewH/2, 0)
}

func (a *agentsModel) halfPageDown(viewH int) {
	a.cursor = min(a.cursor+viewH/2, max(len(a.agents)-1, 0))
}

func (a *agentsModel) goTop() {
	a.cursor = 0
}

func (a *agentsModel) goBottom() {
	if len(a.agents) > 0 {
		a.cursor = len(a.agents) - 1
	}
}

func (a *agentsModel) tick() {
	a.spinnerFrame = (a.spinnerFrame + 1) % len(spinnerFrames)
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

	title := titleStyle.Render("Agents/Sessions")

	contentHeight := max(height-3, 1)
	textWidth := max(width-4, 1)

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
		// Show animated spinner for active subagents
		if ag.ParentAgentID != "" && ag.Status == "active" {
			frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
			name = frame + " " + name
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
		case i == a.cursor && focused:
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

	a.hScroll = clampHScroll(lines, a.hScroll, textWidth)
	for i, l := range lines {
		lines[i] = hScrollLine(l, a.hScroll, textWidth)
	}

	if a.cursor >= a.scroll+contentHeight {
		a.scroll = a.cursor - contentHeight + 1
	}
	if a.cursor < a.scroll {
		a.scroll = a.cursor
	}
	maxScroll := max(len(lines)-contentHeight, 0)
	a.scroll = min(a.scroll, maxScroll)

	visible := sliceLines(lines, a.scroll, contentHeight)
	return renderPane(width, height, focused, title, visible)
}

func sliceLines(lines []string, offset, count int) []string {
	if offset >= len(lines) {
		return nil
	}
	end := min(offset+count, len(lines))
	return lines[offset:end]
}
