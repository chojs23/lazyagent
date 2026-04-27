package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/eventview"
	"github.com/chojs23/lazyagent/internal/model"
)

const eventsPageSize = 3000

type eventsModel struct {
	events       []model.Event
	rawCount     int
	loadedOffset int // offset of the first loaded event in the full list
	cursor       int
	scroll       int
	hScroll      int
	autoFollow   bool
	height       int
}

func newEvents() eventsModel {
	return eventsModel{autoFollow: true}
}

func (e *eventsModel) setEvents(events []model.Event, rawCount int, offset int) {
	prevOffset := e.loadedOffset

	e.events = events
	e.rawCount = rawCount
	e.loadedOffset = offset

	if e.autoFollow && len(events) > 0 {
		e.cursor = len(events) - 1
	} else {
		// Compensate cursor/scroll for the offset shift so the user
		// stays on the same event when new events arrive at the tail.
		delta := offset - prevOffset
		e.cursor -= delta
		e.scroll -= delta
	}

	e.cursor = max(e.cursor, 0)
	if e.cursor >= len(events) {
		e.cursor = max(len(events)-1, 0)
	}
	e.clampScroll()
}

func (e *eventsModel) prependEvents(events []model.Event, newOffset int) {
	added := len(events)
	e.events = append(events, e.events...)
	e.loadedOffset = newOffset
	// Shift cursor so it stays on the same event
	e.cursor += added
	e.scroll += added
}

// needsOlder returns true when the cursor is near the top of loaded events
// and there are older events available to load.
func (e *eventsModel) needsOlder() bool {
	return e.loadedOffset > 0 && e.cursor < eventsPageSize/2
}

// clampScroll enforces vim-like scrolloff=3: the cursor stays at least 3
// lines from the top and bottom edges of the viewport.  At the very start
// or end of the list the margin is naturally relaxed by the final clamp.
//
// This method must be called from Update()-context methods (cursor moves,
// setEvents, layout changes) so the result persists.  bubbletea's View()
// runs on a value copy, so scroll mutations there are silently lost.
func (e *eventsModel) clampScroll() {
	e.scroll = clampListScroll(e.cursor, e.scroll, e.height, len(e.events), 3)
}

func (e *eventsModel) moveUp() {
	if e.cursor > 0 {
		e.cursor--
		e.autoFollow = false
	}
	e.clampScroll()
}

func (e *eventsModel) moveDown() {
	if e.cursor < len(e.events)-1 {
		e.cursor++
	}
	e.clampScroll()
}

func (e *eventsModel) halfPageUp(viewH int) {
	e.cursor = max(e.cursor-viewH/2, 0)
	e.autoFollow = false
	e.clampScroll()
}

func (e *eventsModel) halfPageDown(viewH int) {
	e.cursor = min(e.cursor+viewH/2, max(len(e.events)-1, 0))
	e.clampScroll()
}

func (e *eventsModel) goTop() {
	e.cursor = 0
	e.autoFollow = false
	e.clampScroll()
}

func (e *eventsModel) centerCursor() {
	contentHeight := max(e.height-3, 1)
	e.scroll = e.cursor - contentHeight/2
	e.scroll = max(e.scroll, 0)
	maxScroll := max(len(e.events)-contentHeight, 0)
	e.scroll = min(e.scroll, maxScroll)
}

func (e *eventsModel) goBottom() {
	if len(e.events) > 0 {
		e.cursor = len(e.events) - 1
	}
	e.clampScroll()
}

func (e *eventsModel) toggleAutoFollow() {
	e.autoFollow = !e.autoFollow
	if e.autoFollow && len(e.events) > 0 {
		e.cursor = len(e.events) - 1
	}
	e.clampScroll()
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
	return renderPane(width, height, focused, e.headerLine(), e.visibleLines(focused, agentMap, width))
}

func (e *eventsModel) headerLine() string {
	header := fmt.Sprintf("Events: %d", len(e.events))
	if e.rawCount > 0 && e.rawCount != len(e.events) {
		header = fmt.Sprintf("Events: %d / %d raw", len(e.events), e.rawCount)
	}
	headerLine := titleStyle.Render(header)
	if e.autoFollow {
		headerLine += dimStyle.Render(" [auto]")
	}
	return headerLine
}

func (e *eventsModel) visibleLines(focused bool, agentMap map[string]agentInfo, width int) []string {
	contentHeight := max(e.height-3, 1)
	end := min(e.scroll+contentHeight, len(e.events))
	totalDigits := len(fmt.Sprintf("%d", e.loadedOffset+len(e.events)))

	var lines []string
	for i := e.scroll; i < end; i++ {
		ev := e.events[i]
		absIndex := e.loadedOffset + i
		line := e.renderEventLine(ev, absIndex, i == e.cursor, focused, agentMap, totalDigits)
		lines = append(lines, line)
	}
	return e.applyHorizontalScroll(lines, width)
}

func (e *eventsModel) applyHorizontalScroll(lines []string, width int) []string {
	textWidth := max(width-4, 1)
	e.hScroll = clampHScroll(lines, e.hScroll, textWidth)
	for i, l := range lines {
		lines[i] = hScrollLine(l, e.hScroll, textWidth)
	}
	return lines
}

func (e *eventsModel) renderEventLine(ev model.Event, index int, atCursor bool, focused bool, agentMap map[string]agentInfo, totalDigits int) string {
	numStr := fmt.Sprintf("%*d", totalDigits, index+1)
	subtype := truncate(orDefault(ev.Subtype, ev.Type), 20)
	agentLabel, agentInfo := eventAgentLabel(ev, agentMap)
	brief := eventview.Brief(ev)
	if atCursor {
		return renderSelectedEventLine(ev, focused, numStr, agentLabel, subtype, brief)
	}
	return renderPlainEventLine(ev, numStr, subtype, agentLabel, agentInfo, brief)
}

func eventAgentLabel(ev model.Event, agentMap map[string]agentInfo) (string, agentInfo) {
	info, ok := agentMap[ev.AgentID]
	if !ok {
		return "", agentInfo{}
	}
	return info.name, info
}

func eventLineParts(numStr, agentLabel, subtype, toolName, brief string) []string {
	parts := []string{numStr}
	if agentLabel != "" {
		parts = append(parts, agentLabel)
	}
	parts = append(parts, subtype)
	if toolName != "" {
		parts = append(parts, toolName)
	}
	if brief != "" {
		parts = append(parts, brief)
	}
	return parts
}

func renderSelectedEventLine(ev model.Event, focused bool, numStr, agentLabel, subtype, brief string) string {
	parts := eventLineParts(numStr, agentLabel, subtype, ev.ToolName, brief)
	style := selectedStyle
	if focused {
		style = cursorStyle
	}
	return style.Render("  " + strings.Join(parts, "  "))
}

func renderPlainEventLine(ev model.Event, numStr, subtype, agentLabel string, info agentInfo, brief string) string {

	stColor := subtypeColor(ev.Subtype)
	subtypeStr := lipgloss.NewStyle().Foreground(stColor).Render(subtype)

	var parts []string
	parts = append(parts, dimStyle.Render(numStr))
	if agentLabel != "" {
		c := agentColor(info.index)
		parts = append(parts, lipgloss.NewStyle().Foreground(c).Render(agentLabel))
	}
	parts = append(parts, subtypeStr)
	if ev.ToolName != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorBlue).Render(ev.ToolName))
	}
	if brief != "" {
		if eventview.IsHighlighted(ev) {
			parts = append(parts, lipgloss.NewStyle().Foreground(colorWhite).Render(brief))
		} else {
			parts = append(parts, dimStyle.Render(brief))
		}
	}

	return "  " + strings.Join(parts, "  ")
}
