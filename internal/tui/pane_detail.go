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
	eventID  int64
	agents   map[string]*model.Agent
}

func newDetail() detailModel {
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	km := vp.KeyMap
	km.HalfPageUp.SetKeys("ctrl+u")
	km.HalfPageDown.SetKeys("ctrl+d")
	vp.KeyMap = km
	return detailModel{viewport: vp, agents: map[string]*model.Agent{}}
}

func (d *detailModel) setEvent(ev *model.Event, agents []model.Agent) {
	sameEvent := ev != nil && ev.ID == d.eventID
	d.event = ev
	d.agents = map[string]*model.Agent{}
	for i := range agents {
		d.agents[agents[i].ID] = &agents[i]
	}
	if !sameEvent {
		if ev != nil {
			d.eventID = ev.ID
		} else {
			d.eventID = 0
		}
	}
	d.syncContent(sameEvent)
}

func (d *detailModel) syncContent(preserveScroll bool) {
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

	d.viewport.SetContent(content)
	if !preserveScroll {
		d.viewport.GotoTop()
	}
}

func (d *detailModel) view(width, height int, focused bool) string {
	d.viewport.SetWidth(maxInt(width-4, 10))
	d.viewport.SetHeight(maxInt(height-3, 4))

	title := titleStyle.Render("Detail")
	content := title + "\n" + d.viewport.View()
	return paneStyle(focused).Width(width).Render(content)
}
