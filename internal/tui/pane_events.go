package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/textutil"
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
	brief := eventBrief(ev)
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
		if isBriefHighlighted(ev) {
			parts = append(parts, lipgloss.NewStyle().Foreground(colorWhite).Render(brief))
		} else {
			parts = append(parts, dimStyle.Render(brief))
		}
	}

	return "  " + strings.Join(parts, "  ")
}

// isBriefHighlighted returns true for events whose brief text should be
// rendered in white (user messages and AI text responses) instead of dim gray.
func isBriefHighlighted(ev model.Event) bool {
	switch ev.Subtype {
	case "UserPromptSubmit":
		return true
	case "Stop", "SubagentStop":
		return true
	case "PartUpdated":
		var p map[string]any
		if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
			return false
		}
		pt := jsonutil.GetString(p, "part_type")
		return pt == "text" || pt == "reasoning"
	default:
		return false
	}
}

type eventBriefContext struct {
	payload map[string]any
	input   map[string]any
}

func newEventBriefContext(ev model.Event) (eventBriefContext, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
		return eventBriefContext{}, false
	}
	input := jsonutil.MapOrEmpty(payload["tool_input"])
	if len(input) == 0 {
		input = jsonutil.MapOrEmpty(payload["args"])
	}
	return eventBriefContext{payload: payload, input: input}, true
}

func (c eventBriefContext) payloadString(key string) string {
	return jsonutil.GetString(c.payload, key)
}

func (c eventBriefContext) inputString(key string) string {
	return jsonutil.GetString(c.input, key)
}

func (c eventBriefContext) openCodeOutput() string {
	return textutil.FirstNonEmpty(c.payloadString("title"), c.payloadString("output"))
}

func (c eventBriefContext) filePath() string {
	return textutil.FirstNonEmpty(c.inputString("file_path"), c.inputString("filePath"))
}

func (c eventBriefContext) editStrings() (string, string) {
	return textutil.FirstNonEmpty(c.inputString("old_string"), c.inputString("oldString")), textutil.FirstNonEmpty(c.inputString("new_string"), c.inputString("newString"))
}

func (c eventBriefContext) patchText() string {
	patch := textutil.FirstNonEmpty(c.inputString("input"), c.inputString("patch"), c.inputString("patchText"))
	if patch == "" {
		if meta := jsonutil.MapOrEmpty(c.payload["metadata"]); len(meta) > 0 {
			patch = jsonutil.GetString(meta, "diff")
		}
	}
	return patch
}

func (c eventBriefContext) bashOutput() string {
	resp := jsonutil.MapOrEmpty(c.payload["tool_response"])
	out := jsonutil.GetString(resp, "stdout")
	if out == "" {
		out = jsonutil.GetString(resp, "stderr")
	}
	if out == "" {
		out = c.openCodeOutput()
	}
	return out
}

func (c eventBriefContext) genericToolResponse() string {
	resp := c.payloadString("tool_response")
	if resp == "" {
		respMap := jsonutil.MapOrEmpty(c.payload["tool_response"])
		resp = jsonutil.GetString(respMap, "stdout")
	}
	if resp == "" {
		resp = c.openCodeOutput()
	}
	return resp
}

func eventBrief(ev model.Event) string {
	ctx, ok := newEventBriefContext(ev)
	if !ok {
		return ""
	}

	switch ev.Subtype {
	case "UserPromptSubmit":
		return truncate(textutil.FirstLine(ctx.payloadString("prompt")), 80)

	case "PreToolUse":
		return briefForPreToolUse(ev, ctx)

	case "PostToolUse", "PostToolUseFailure":
		return briefForPostToolUse(ev, ctx)

	case "SessionStart":
		return ctx.payloadString("model")
	case "SessionEnd":
		return ctx.payloadString("reason")
	case "Stop":
		return truncate(textutil.FirstLine(ctx.payloadString("last_assistant_message")), 80)
	case "SubagentStop":
		return truncate(textutil.FirstLine(ctx.payloadString("last_assistant_message")), 80)
	case "Notification":
		return truncate(textutil.FirstNonEmpty(ctx.payloadString("message"), ctx.payloadString("permission")), 80)

	case "SessionStatus":
		st := ctx.payloadString("status_type")
		if st == "retry" {
			attempt := ctx.payloadString("retry_attempt")
			msg := ctx.payloadString("retry_message")
			if attempt != "" {
				return truncate(fmt.Sprintf("retry #%s: %s", attempt, msg), 80)
			}
			return truncate("retry: "+msg, 80)
		}
		return st
	case "SessionDiff":
		fc := ctx.payloadString("diff_file_count")
		add := ctx.payloadString("diff_additions")
		del := ctx.payloadString("diff_deletions")
		if fc != "" {
			return fmt.Sprintf("%s files (+%s -%s)", fc, add, del)
		}
		return ""
	case "PermissionReply":
		return ctx.payloadString("reply")
	case "TodoUpdate":
		return ctx.payloadString("todo_count") + " todos"
	case "CommandExecuted":
		name := ctx.payloadString("command_name")
		args := ctx.payloadString("command_args")
		if args != "" {
			return truncate(name+" "+args, 80)
		}
		return name
	case "FileEdited":
		return ctx.payloadString("file")

	case "MessageUpdated":
		role := ctx.payloadString("message_role")
		if role == "assistant" {
			cost := ctx.payloadString("cost")
			in := ctx.payloadString("tokens_input")
			out := ctx.payloadString("tokens_output")
			if in != "" || out != "" {
				s := fmt.Sprintf("token in:%s token out:%s", in, out)
				if cost != "" && cost != "0" {
					s += fmt.Sprintf(" $%s", cost)
				}
				return s
			}
			return role
		}
		return role

	case "PartUpdated":
		return briefForPartUpdated(ctx)

	default:
		return ""
	}
}

func briefForPreToolUse(ev model.Event, ctx eventBriefContext) string {
	switch ev.ToolName {
	case "Bash":
		return truncate(textutil.FirstLine(ctx.inputString("command")), 80)
	case "Read":
		return ctx.filePath()
	case "Edit":
		filePath := ctx.filePath()
		oldStr, newStr := ctx.editStrings()
		stats := editDiffStats(oldStr, newStr)
		if stats != "" {
			return truncate(filePath+" "+stats, 80)
		}
		return filePath
	case "Write":
		filePath := ctx.filePath()
		if content := ctx.inputString("content"); content != "" {
			n := strings.Count(content, "\n") + 1
			return truncate(fmt.Sprintf("%s (+%d)", filePath, n), 80)
		}
		return filePath
	case "apply_patch":
		return truncate(patchDiffStats(ctx.patchText()), 80)
	case "Grep":
		s := ctx.inputString("pattern")
		if path := ctx.inputString("path"); path != "" {
			s += " in " + path
		}
		return truncate(s, 80)
	case "Glob":
		return ctx.inputString("pattern")
	case "Agent":
		desc := ctx.inputString("description")
		if t := ctx.inputString("subagent_type"); t != "" {
			desc = "[" + t + "] " + desc
		}
		return truncate(desc, 80)
	default:
		return truncate(textutil.FirstLine(ctx.inputString("description")), 80)
	}
}

func briefForPostToolUse(ev model.Event, ctx eventBriefContext) string {
	switch ev.ToolName {
	case "Bash":
		return truncate(textutil.FirstLine(ctx.bashOutput()), 80)
	case "Read":
		return truncate(textutil.FirstNonEmpty(ctx.filePath(), ctx.openCodeOutput()), 80)
	case "Edit":
		filePath := textutil.FirstNonEmpty(ctx.filePath(), ctx.openCodeOutput())
		oldStr, newStr := ctx.editStrings()
		stats := editDiffStats(oldStr, newStr)
		if stats == "" {
			if meta := jsonutil.MapOrEmpty(ctx.payload["metadata"]); len(meta) > 0 {
				stats = patchDiffStats(jsonutil.GetString(meta, "diff"))
			}
		}
		if stats != "" {
			return truncate(filePath+" "+stats, 80)
		}
		return truncate(filePath, 80)
	case "Write":
		filePath := textutil.FirstNonEmpty(ctx.filePath(), ctx.openCodeOutput())
		if content := ctx.inputString("content"); content != "" {
			n := strings.Count(content, "\n") + 1
			return truncate(fmt.Sprintf("%s (+%d)", filePath, n), 80)
		}
		return truncate(filePath, 80)
	case "apply_patch":
		return truncate(patchDiffStats(ctx.patchText()), 80)
	default:
		return truncate(textutil.FirstLine(ctx.genericToolResponse()), 80)
	}
}

func briefForPartUpdated(ctx eventBriefContext) string {
	partType := ctx.payloadString("part_type")
	switch partType {
	case "text":
		return truncate(textutil.FirstLine(ctx.payloadString("text")), 80)
	case "reasoning":
		return truncate("reasoning: "+textutil.FirstLine(ctx.payloadString("text")), 80)
	case "tool":
		name := ctx.payloadString("tool_name")
		status := ctx.payloadString("tool_status")
		title := ctx.payloadString("tool_title")
		s := name + " [" + status + "]"
		if title != "" {
			s += " " + title
		}
		return truncate(s, 80)
	case "step-finish":
		in := ctx.payloadString("tokens_input")
		out := ctx.payloadString("tokens_output")
		return fmt.Sprintf("step done (in:%s out:%s)", in, out)
	case "step-start":
		return "step start"
	default:
		return partType
	}
}

// splitLines splits a string into lines, returning nil for empty input
// instead of []string{""} which strings.Split produces.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// editDiffStats computes diff stats from old_string/new_string using Myers diff.
func editDiffStats(oldStr, newStr string) string {
	if oldStr == "" && newStr == "" {
		return ""
	}
	oldLines := splitLines(oldStr)
	newLines := splitLines(newStr)
	script := ComputeDiff(oldLines, newLines)
	s := Stats(script)
	if s.Additions == 0 && s.Deletions == 0 {
		return ""
	}
	return fmt.Sprintf("(+%d -%d)", s.Additions, s.Deletions)
}

// patchDiffStats counts +/- lines in a unified patch or Codex apply_patch format.
// Skips diff metadata lines (---, +++, *** headers) to avoid inflating counts.
func patchDiffStats(patch string) string {
	if patch == "" {
		return ""
	}
	var adds, dels int
	for _, line := range splitLines(patch) {
		if len(line) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+++"):
			// unified diff header, skip
		case strings.HasPrefix(line, "---"):
			// unified diff header, skip
		case strings.HasPrefix(line, "***"):
			// Codex patch metadata, skip
		case strings.HasPrefix(line, "+"):
			adds++
		case strings.HasPrefix(line, "-"):
			dels++
		}
	}
	if adds == 0 && dels == 0 {
		return "patch"
	}
	return fmt.Sprintf("(+%d -%d)", adds, dels)
}
