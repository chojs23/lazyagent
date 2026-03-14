package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"

	"github.com/chojs23/lazyagent/internal/model"
)

type detailModel struct {
	viewport viewport.Model
	event    *model.Event
	thread   []model.Event
	showThread bool
	agents   map[string]*model.Agent
}

func newDetail() detailModel {
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	return detailModel{viewport: vp, agents: map[string]*model.Agent{}}
}

func (d *detailModel) setEvent(ev *model.Event, agents []model.Agent) {
	d.event = ev
	d.thread = nil
	d.showThread = false
	d.agents = map[string]*model.Agent{}
	for i := range agents {
		d.agents[agents[i].ID] = &agents[i]
	}
	d.syncContent()
}

func (d *detailModel) setThread(thread []model.Event) {
	d.thread = thread
	d.showThread = true
	d.syncContent()
}

func (d *detailModel) toggleThread() bool {
	if d.showThread {
		d.showThread = false
		d.syncContent()
		return false
	}
	// caller should fetch thread and call setThread
	return true
}

func (d *detailModel) syncContent() {
	if d.event == nil {
		d.viewport.SetContent("No event selected")
		return
	}

	ev := d.event
	agentName := shortID(ev.AgentID)
	if a, ok := d.agents[ev.AgentID]; ok && a.Name != "" {
		agentName = a.Name
		if a.AgentType != "" {
			agentName += " (" + a.AgentType + ")"
		}
	}

	header := strings.Join([]string{
		fmt.Sprintf("Session:  %s", shortID(ev.SessionID)),
		fmt.Sprintf("Agent:    %s", agentName),
		fmt.Sprintf("Type:     %s / %s", ev.Type, orDefault(ev.Subtype, "-")),
		fmt.Sprintf("Tool:     %s", orDefault(ev.ToolName, "-")),
		fmt.Sprintf("Time:     %s  %s", formatTime(ev.Timestamp), relativeTime(ev.Timestamp)),
		fmt.Sprintf("Status:   %s", model.DeriveEventStatus(ev.Subtype)),
	}, "\n")

	sep := dimStyle.Render(strings.Repeat("─", 40))
	payload := ev.PayloadPretty()

	content := header + "\n" + sep + "\n" + payload

	if d.showThread && len(d.thread) > 0 {
		content += "\n\n" + dimStyle.Render("── Thread ──") + "\n"
		for _, te := range d.thread {
			marker := "  "
			if te.ID == ev.ID {
				marker = "> "
			}
			content += fmt.Sprintf("%s%s  %s\n", marker, formatTime(te.Timestamp), model.EventSummary(te))
		}
	}

	d.viewport.SetContent(content)
	d.viewport.GotoTop()
}

func (d *detailModel) view(width, height int, focused bool) string {
	d.viewport.SetWidth(maxInt(width-4, 10))
	d.viewport.SetHeight(maxInt(height-3, 4))

	title := titleStyle.Render("Detail")
	if d.showThread {
		title += dimStyle.Render(" [thread]")
	}
	content := title + "\n" + d.viewport.View()
	return paneStyle(focused).Width(width).Render(content)
}
