package tui

import (
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
		e.cursor = maxInt(len(events)-1, 0)
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
	e.cursor = maxInt(e.cursor-viewH/2, 0)
	e.autoFollow = false
}

func (e *eventsModel) halfPageDown(viewH int) {
	e.cursor = minInt(e.cursor+viewH/2, maxInt(len(e.events)-1, 0))
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

func (e *eventsModel) selectedEventID() int64 {
	if ev := e.selectedEvent(); ev != nil {
		return ev.ID
	}
	return 0
}

func (e *eventsModel) view(width, height int, focused bool, agentMap map[string]int) string {
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

	contentHeight := maxInt(height-3, 1)

	// ensure cursor is visible with 3-item lookahead below
	scrollPad := 3
	if e.cursor+scrollPad >= e.scroll+contentHeight {
		e.scroll = e.cursor + scrollPad - contentHeight + 1
	}
	if e.cursor < e.scroll+scrollPad && e.scroll > 0 {
		e.scroll = maxInt(e.cursor-scrollPad, 0)
	}
	if e.cursor < e.scroll {
		e.scroll = e.cursor
	}
	// clamp scroll
	maxScroll := maxInt(len(e.events)-contentHeight, 0)
	e.scroll = minInt(e.scroll, maxScroll)

	var lines []string
	end := minInt(e.scroll+contentHeight, len(e.events))
	totalDigits := len(fmt.Sprintf("%d", len(e.events)))
	for i := e.scroll; i < end; i++ {
		ev := e.events[i]
		line := e.renderEventLine(ev, i, i == e.cursor, agentMap, width-4, totalDigits)
		lines = append(lines, line)
	}

	content := headerLine + "\n" + strings.Join(lines, "\n")
	return paneStyle(focused).Width(width).Render(content)
}

func (e *eventsModel) renderEventLine(ev model.Event, index int, selected bool, agentMap map[string]int, maxW int, totalDigits int) string {
	// line number (1-based)
	numStr := fmt.Sprintf("%*d", totalDigits, index+1)

	ts := formatTime(ev.Timestamp)
	subtype := orDefault(ev.Subtype, ev.Type)
	stColor := subtypeColor(ev.Subtype)
	subtypeStr := lipgloss.NewStyle().Foreground(stColor).Render(truncate(subtype, 20))

	var parts []string
	parts = append(parts, dimStyle.Render(numStr))
	parts = append(parts, dimStyle.Render(ts))
	parts = append(parts, subtypeStr)

	if ev.ToolName != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorBlue).Render(ev.ToolName))
	}

	if idx, ok := agentMap[ev.AgentID]; ok && len(agentMap) > 1 {
		c := agentColor(idx)
		parts = append(parts, lipgloss.NewStyle().Foreground(c).Render(shortID(ev.AgentID)))
	}

	line := strings.Join(parts, "  ")
	if selected {
		line = lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("> ") + line
	} else {
		line = "  " + line
	}
	return line
}
