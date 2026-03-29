package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/model"
)

type eventsModel struct {
	events     []model.Event
	rawCount   int
	cursor     int
	scroll     int
	hScroll    int
	autoFollow bool
	height     int
	width      int
}

func newEvents() eventsModel {
	return eventsModel{autoFollow: true}
}

func (e *eventsModel) setEvents(events []model.Event, rawCount int) {
	e.events = events
	e.rawCount = rawCount
	if e.autoFollow && len(events) > 0 {
		e.cursor = len(events) - 1
	}
	if e.cursor >= len(events) {
		e.cursor = max(len(events)-1, 0)
	}
}

func (e *eventsModel) moveUp() {
	if e.cursor > 0 {
		e.cursor--
		e.autoFollow = false
	}
}

func (e *eventsModel) moveDown() {
	if e.cursor < len(e.events)-1 {
		e.cursor++
	}
}

func (e *eventsModel) halfPageUp(viewH int) {
	e.cursor = max(e.cursor-viewH/2, 0)
	e.autoFollow = false
}

func (e *eventsModel) halfPageDown(viewH int) {
	e.cursor = min(e.cursor+viewH/2, max(len(e.events)-1, 0))
}

func (e *eventsModel) goTop() {
	e.cursor = 0
	e.autoFollow = false
}

func (e *eventsModel) goBottom() {
	if len(e.events) > 0 {
		e.cursor = len(e.events) - 1
	}
}

func (e *eventsModel) toggleAutoFollow() {
	e.autoFollow = !e.autoFollow
	if e.autoFollow && len(e.events) > 0 {
		e.cursor = len(e.events) - 1
	}
}

func (e *eventsModel) selectedEvent() *model.Event {
	if e.cursor >= 0 && e.cursor < len(e.events) {
		ev := e.events[e.cursor]
		return &ev
	}
	return nil
}

func (e *eventsModel) view(width, height int, focused bool, agentMap map[string]agentInfo) string {
	e.height = height
	e.width = width

	// header
	header := fmt.Sprintf("Events: %d", len(e.events))
	if e.rawCount > 0 && e.rawCount != len(e.events) {
		header = fmt.Sprintf("Events: %d / %d raw", len(e.events), e.rawCount)
	}
	headerLine := titleStyle.Render(header)
	if e.autoFollow {
		headerLine += dimStyle.Render(" [auto]")
	}

	contentHeight := max(height-3, 1)

	// ensure cursor is visible with 3-item lookahead below
	scrollPad := 3
	if e.cursor+scrollPad >= e.scroll+contentHeight {
		e.scroll = e.cursor + scrollPad - contentHeight + 1
	}
	if e.cursor < e.scroll+scrollPad && e.scroll > 0 {
		e.scroll = max(e.cursor-scrollPad, 0)
	}
	if e.cursor < e.scroll {
		e.scroll = e.cursor
	}
	// clamp scroll
	maxScroll := max(len(e.events)-contentHeight, 0)
	e.scroll = min(e.scroll, maxScroll)

	var lines []string
	end := min(e.scroll+contentHeight, len(e.events))
	totalDigits := len(fmt.Sprintf("%d", len(e.events)))
	for i := e.scroll; i < end; i++ {
		ev := e.events[i]
		line := e.renderEventLine(ev, i, i == e.cursor && focused, agentMap, width-4, totalDigits)
		lines = append(lines, line)
	}

	textWidth := max(width-4, 1)
	e.hScroll = clampHScroll(lines, e.hScroll, textWidth)
	for i, l := range lines {
		lines[i] = hScrollLine(l, e.hScroll, textWidth)
	}

	content := headerLine + "\n" + strings.Join(lines, "\n")
	return paneStyle(focused).Width(width).Height(height).Render(content)
}

func (e *eventsModel) renderEventLine(ev model.Event, index int, selected bool, agentMap map[string]agentInfo, maxW int, totalDigits int) string {
	numStr := fmt.Sprintf("%*d", totalDigits, index+1)
	subtype := truncate(orDefault(ev.Subtype, ev.Type), 20)

	agentLabel := ""
	if info, ok := agentMap[ev.AgentID]; ok {
		agentLabel = info.name
	}

	brief := eventBrief(ev)

	// order: num | agent | subtype | tool | brief
	if selected {
		var parts []string
		parts = append(parts, numStr)
		if agentLabel != "" {
			parts = append(parts, agentLabel)
		}
		parts = append(parts, subtype)
		if ev.ToolName != "" {
			parts = append(parts, ev.ToolName)
		}
		if brief != "" {
			parts = append(parts, brief)
		}
		return cursorStyle.Render("  " + strings.Join(parts, "  "))
	}

	stColor := subtypeColor(ev.Subtype)
	subtypeStr := lipgloss.NewStyle().Foreground(stColor).Render(subtype)

	var parts []string
	parts = append(parts, dimStyle.Render(numStr))
	if agentLabel != "" {
		info := agentMap[ev.AgentID]
		c := agentColor(info.index)
		parts = append(parts, lipgloss.NewStyle().Foreground(c).Render(agentLabel))
	}
	parts = append(parts, subtypeStr)
	if ev.ToolName != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorBlue).Render(ev.ToolName))
	}
	if brief != "" {
		parts = append(parts, dimStyle.Render(brief))
	}

	return "  " + strings.Join(parts, "  ")
}

func eventBrief(ev model.Event) string {
	var p map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
		return ""
	}
	input := asMapSafe(p["tool_input"])

	switch ev.Subtype {
	case "UserPromptSubmit":
		return truncate(firstLine(getStr(p, "prompt")), 80)

	case "PreToolUse":
		switch ev.ToolName {
		case "Bash":
			return truncate(firstLine(getStr(input, "command")), 80)
		case "Read":
			return getStr(input, "file_path")
		case "Edit", "Write":
			return getStr(input, "file_path")
		case "Grep":
			s := getStr(input, "pattern")
			if path := getStr(input, "path"); path != "" {
				s += " in " + path
			}
			return truncate(s, 80)
		case "Glob":
			return getStr(input, "pattern")
		case "Agent":
			desc := getStr(input, "description")
			if t := getStr(input, "subagent_type"); t != "" {
				desc = "[" + t + "] " + desc
			}
			return truncate(desc, 80)
		default:
			return truncate(firstLine(getStr(input, "description")), 80)
		}

	case "PostToolUse", "PostToolUseFailure":
		switch ev.ToolName {
		case "Bash":
			resp := asMapSafe(p["tool_response"])
			out := getStr(resp, "stdout")
			if out == "" {
				out = getStr(resp, "stderr")
			}
			return truncate(firstLine(out), 80)
		case "Read":
			return truncate(getStr(input, "file_path"), 80)
		case "Edit", "Write":
			return truncate(getStr(input, "file_path"), 80)
		default:
			resp := getStr(p, "tool_response")
			if resp == "" {
				respMap := asMapSafe(p["tool_response"])
				resp = getStr(respMap, "stdout")
			}
			return truncate(firstLine(resp), 80)
		}

	case "SessionStart":
		return getStr(p, "model")
	case "SessionEnd":
		return getStr(p, "reason")
	case "Stop":
		return truncate(firstLine(getStr(p, "last_assistant_message")), 80)
	case "SubagentStop":
		return truncate(getStr(p, "agent_type"), 80)
	case "Notification":
		return truncate(getStr(p, "message"), 80)
	default:
		return ""
	}
}
