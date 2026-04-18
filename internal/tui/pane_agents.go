package tui

import "github.com/chojs23/lazyagent/internal/model"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type agentsModel struct {
	listPaneState
	agents        []model.Agent
	selectedAgent string
	spinnerFrame  int
}

func newAgents() agentsModel {
	return agentsModel{}
}

func (a *agentsModel) setAgents(agents []model.Agent) {
	a.agents = agents
	if a.cursor >= len(a.agents) {
		a.clampCursor(len(a.agents))
	}
}

func (a *agentsModel) moveUp() {
	a.listPaneState.moveUp()
}

func (a *agentsModel) moveDown() {
	a.listPaneState.moveDown(len(a.agents))
}

func (a *agentsModel) halfPageUp(viewH int) {
	a.listPaneState.halfPageUp(viewH)
}

func (a *agentsModel) halfPageDown(viewH int) {
	a.listPaneState.halfPageDown(viewH, len(a.agents))
}

func (a *agentsModel) goTop() {
	a.listPaneState.goTop()
}

func (a *agentsModel) goBottom() {
	a.listPaneState.goBottom(len(a.agents))
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
	visible := a.listPaneState.visibleLines(lines, width)
	return renderPane(width, height, focused, title, visible)
}

func sliceLines(lines []string, offset, count int) []string {
	if offset >= len(lines) {
		return nil
	}
	end := min(offset+count, len(lines))
	return lines[offset:end]
}
