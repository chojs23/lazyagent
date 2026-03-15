package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/model"
)

type detailFocus int

const (
	detailFocusInfo detailFocus = iota
	detailFocusJSON
)

type detailModel struct {
	infoVP     viewport.Model
	jsonVP     viewport.Model
	event      *model.Event
	eventID    int64
	thread     []model.Event
	showThread bool
	agents     map[string]*model.Agent
	focus      detailFocus
	jsonExpand bool
}

func newDetail() detailModel {
	return detailModel{
		infoVP: viewport.New(viewport.WithWidth(0), viewport.WithHeight(0)),
		jsonVP: viewport.New(viewport.WithWidth(0), viewport.WithHeight(0)),
		agents: map[string]*model.Agent{},
	}
}

func (d *detailModel) setEvent(ev *model.Event, agents []model.Agent) {
	sameEvent := ev != nil && ev.ID == d.eventID
	d.event = ev
	d.agents = map[string]*model.Agent{}
	for i := range agents {
		d.agents[agents[i].ID] = &agents[i]
	}
	if !sameEvent {
		d.thread = nil
		d.showThread = false
		d.focus = detailFocusInfo
		d.jsonExpand = false
		if ev != nil {
			d.eventID = ev.ID
		} else {
			d.eventID = 0
		}
	}
	d.syncContent(sameEvent)
}

func (d *detailModel) setThread(thread []model.Event) {
	d.thread = thread
	d.showThread = true
	d.syncContent(false)
}

func (d *detailModel) toggleThread() bool {
	if d.showThread {
		d.showThread = false
		d.syncContent(false)
		return false
	}
	return true
}

func (d *detailModel) expandJSON() {
	d.jsonExpand = true
	d.focus = detailFocusJSON
}

func (d *detailModel) collapseJSON() {
	d.jsonExpand = false
	d.focus = detailFocusInfo
}

func (d *detailModel) syncContent(preserveScroll bool) {
	d.syncInfo(preserveScroll)
	d.syncJSON(preserveScroll)
}

// ── Info panel content ──

func (d *detailModel) syncInfo(preserveScroll bool) {
	if d.event == nil {
		d.infoVP.SetContent(dimStyle.Render("  No event selected"))
		return
	}

	ev := d.event
	lbl := lipgloss.NewStyle().Foreground(colorGray).Width(10)
	val := lipgloss.NewStyle().Foreground(colorWhite)
	accent := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)

	agentName := shortID(ev.AgentID)
	if a, ok := d.agents[ev.AgentID]; ok && a.Name != "" {
		agentName = a.Name
		if a.AgentType != "" {
			agentName += dimStyle.Render(" ("+a.AgentType+")")
		}
	}

	statusStr, statusColor := eventStatusDisplay(ev.Subtype)
	statusStyled := lipgloss.NewStyle().Foreground(statusColor).Render(statusStr)

	subtypeStyled := lipgloss.NewStyle().Foreground(subtypeColor(ev.Subtype)).Render(orDefault(ev.Subtype, "-"))

	var lines []string
	lines = append(lines, "")
	lines = append(lines, accent.Render("  "+orDefault(ev.Subtype, ev.Type)+" ─ "+orDefault(ev.ToolName, "")))
	lines = append(lines, "")
	lines = append(lines, "  "+lbl.Render("Session")+val.Render(shortID(ev.SessionID)))
	lines = append(lines, "  "+lbl.Render("Agent")+agentName)
	lines = append(lines, "  "+lbl.Render("Type")+val.Render(ev.Type)+" / "+subtypeStyled)
	if ev.ToolName != "" {
		lines = append(lines, "  "+lbl.Render("Tool")+lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render(ev.ToolName))
	}
	lines = append(lines, "  "+lbl.Render("Time")+val.Render(formatTime(ev.Timestamp))+"  "+dimStyle.Render(relativeTime(ev.Timestamp)))
	lines = append(lines, "  "+lbl.Render("Status")+statusStyled)

	// extract key fields from payload for a summary
	summary := extractSummary(ev)
	if summary != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+dimStyle.Render("── Summary ──"))
		for _, sl := range strings.Split(summary, "\n") {
			lines = append(lines, "  "+val.Render(sl))
		}
	}

	if d.showThread && len(d.thread) > 0 {
		lines = append(lines, "")
		lines = append(lines, "  "+dimStyle.Render("── Thread ("+fmt.Sprintf("%d", len(d.thread))+") ──"))
		for _, te := range d.thread {
			marker := "   "
			if te.ID == ev.ID {
				marker = " " + lipgloss.NewStyle().Foreground(colorCyan).Render("▸") + " "
			}
			tColor := subtypeColor(te.Subtype)
			lines = append(lines, marker+dimStyle.Render(formatTime(te.Timestamp))+"  "+lipgloss.NewStyle().Foreground(tColor).Render(model.EventSummary(te)))
		}
	}

	d.infoVP.SetContent(strings.Join(lines, "\n"))
	if !preserveScroll {
		d.infoVP.GotoTop()
	}
}

// ── JSON panel content ──

func (d *detailModel) syncJSON(preserveScroll bool) {
	if d.event == nil {
		d.jsonVP.SetContent(dimStyle.Render("  {}"))
		return
	}
	d.jsonVP.SetContent(d.event.PayloadPretty())
	if !preserveScroll {
		d.jsonVP.GotoTop()
	}
}

// ── View ──

func (d *detailModel) view(width, height int, focused bool) string {
	innerH := maxInt(height-3, 4)

	var infoW, jsonW int
	if d.jsonExpand {
		infoW = maxInt(width*30/100, 16)
		jsonW = width - infoW - 3
	} else {
		infoW = maxInt(width*65/100, 20)
		jsonW = width - infoW - 3
	}

	d.infoVP.SetWidth(maxInt(infoW-2, 8))
	d.infoVP.SetHeight(maxInt(innerH-2, 2))
	d.jsonVP.SetWidth(maxInt(jsonW-2, 8))
	d.jsonVP.SetHeight(maxInt(innerH-2, 2))

	// info box
	infoBorder := lipgloss.NormalBorder()
	infoStyle := lipgloss.NewStyle().
		Border(infoBorder).
		BorderForeground(colorGray).
		Width(infoW).
		Height(innerH)
	if focused && d.focus == detailFocusInfo {
		infoStyle = infoStyle.BorderForeground(colorCyan)
	}
	infoBox := infoStyle.Render(d.infoVP.View())

	// json box
	jsonBorderColor := colorGray
	if focused && d.focus == detailFocusJSON {
		jsonBorderColor = colorCyan
	}
	jsonTitle := " JSON "
	if d.jsonExpand {
		jsonTitle = " JSON [h: close] "
	} else {
		jsonTitle = " JSON [l: expand] "
	}
	jsonStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(jsonBorderColor).
		Width(jsonW).
		Height(innerH)
	jsonHeader := lipgloss.NewStyle().Foreground(jsonBorderColor).Render(jsonTitle)
	jsonContent := jsonHeader + "\n" + d.jsonVP.View()
	jsonBox := jsonStyle.Render(jsonContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, infoBox, jsonBox)
	return row
}

// ── Helpers ──

func eventStatusDisplay(subtype string) (string, color.Color) {
	switch subtype {
	case "PreToolUse":
		return "● running", colorYellow
	case "PostToolUse":
		return "✓ completed", colorGreen
	case "PostToolUseFailure":
		return "✗ failed", colorRed
	case "SessionStart":
		return "▸ started", colorBlue
	case "SessionEnd":
		return "■ ended", colorBlue
	case "Stop", "SubagentStop":
		return "■ stopped", colorOrange
	default:
		return "○ pending", colorGray
	}
}

func extractSummary(ev *model.Event) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
		return ""
	}

	var parts []string

	// tool_input summary
	if input, ok := payload["tool_input"].(map[string]any); ok {
		switch ev.ToolName {
		case "Bash":
			if cmd, ok := input["command"].(string); ok {
				parts = append(parts, truncate(cmd, 120))
			}
		case "Read":
			if fp, ok := input["file_path"].(string); ok {
				parts = append(parts, fp)
			}
		case "Write", "Edit":
			if fp, ok := input["file_path"].(string); ok {
				parts = append(parts, fp)
			}
		case "Grep":
			if pat, ok := input["pattern"].(string); ok {
				parts = append(parts, "pattern: "+pat)
			}
		case "Glob":
			if pat, ok := input["pattern"].(string); ok {
				parts = append(parts, "pattern: "+pat)
			}
		case "Agent":
			if desc, ok := input["description"].(string); ok {
				parts = append(parts, truncate(desc, 120))
			}
		}
	}

	// notification message
	if ev.Subtype == "Notification" {
		if msg, ok := payload["message"].(string); ok {
			parts = append(parts, truncate(msg, 120))
		}
	}

	// user prompt
	if ev.Subtype == "UserPromptSubmit" {
		if msg, ok := payload["message"].(string); ok {
			parts = append(parts, truncate(msg, 200))
		}
	}

	return strings.Join(parts, "\n")
}
