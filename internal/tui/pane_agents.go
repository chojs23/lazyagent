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
	return agentsModel{listPaneState: listPaneState{scrolloff: 3}}
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
	ag := a.currentAgent()
	if ag == nil {
		return
	}
	if a.selectedAgent == ag.ID {
		a.selectedAgent = "" // toggle off
	} else {
		a.selectedAgent = ag.ID
	}
}

func (a *agentsModel) currentAgent() *model.Agent {
	if a.cursor < 0 || a.cursor >= len(a.agents) {
		return nil
	}
	return &a.agents[a.cursor]
}

func (a *agentsModel) selectedAgentID() string {
	return a.selectedAgent
}

func (a *agentsModel) agentTreePrefix(index int) string {
	if a.agents[index].ParentAgentID == "" {
		return ""
	}
	if index == len(a.agents)-1 || a.agents[index+1].ParentAgentID == "" {
		return "└─ "
	}
	return "├─ "
}

func (a *agentsModel) agentDisplayName(agent model.Agent) string {
	name := orDefault(agent.Name, shortID(agent.ID))
	if agent.AgentType != "" {
		name += " (" + agent.AgentType + ")"
	}
	if agent.ParentAgentID != "" && agent.Status == "active" {
		frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
		name = frame + " " + name
	}
	return name
}

func (a *agentsModel) renderAgentLine(index int, focused bool) string {
	agent := a.agents[index]
	tree := a.agentTreePrefix(index)
	name := a.agentDisplayName(agent)
	prefix := "  "
	if index == a.cursor {
		prefix = "> "
	}

	switch {
	case index == a.cursor && focused:
		style := cursorStyle
		if agent.ID == a.selectedAgent {
			style = cursorSelectedStyle
		}
		return style.Render("  " + tree + name)
	case agent.ID == a.selectedAgent:
		return selectedStyle.Render(prefix + tree + name)
	default:
		return prefix + tree + name
	}
}

func (a *agentsModel) view(width, height int, focused bool) string {
	a.height = height

	title := titleStyle.Render("Agents/Sessions")

	var lines []string
	for i := range a.agents {
		lines = append(lines, a.renderAgentLine(i, focused))
	}
	visible := a.listPaneState.visibleLines(lines, width)
	return renderPane(width, height, focused, title, visible)
}
