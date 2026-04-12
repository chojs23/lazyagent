package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

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
	width        int
}

func newEvents() eventsModel {
	return eventsModel{autoFollow: true}
}

func (e *eventsModel) setEvents(events []model.Event, rawCount int, offset int) {
	e.events = events
	e.rawCount = rawCount
	e.loadedOffset = offset
	if e.autoFollow && len(events) > 0 {
		e.cursor = len(events) - 1
	}
	if e.cursor >= len(events) {
		e.cursor = max(len(events)-1, 0)
	}
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
	totalDigits := len(fmt.Sprintf("%d", e.loadedOffset+len(e.events)))
	for i := e.scroll; i < end; i++ {
		ev := e.events[i]
		absIndex := e.loadedOffset + i
		line := e.renderEventLine(ev, absIndex, i == e.cursor && focused, agentMap, width-4, totalDigits)
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
		pt := getStr(p, "part_type")
		return pt == "text" || pt == "reasoning"
	default:
		return false
	}
}

func eventBrief(ev model.Event) string {
	var p map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
		return ""
	}

	// Claude stores tool parameters in "tool_input", OpenCode in "args".
	// Merge both so the same extraction logic works for either runtime.
	input := asMapSafe(p["tool_input"])
	if len(input) == 0 {
		input = asMapSafe(p["args"])
	}

	switch ev.Subtype {
	case "UserPromptSubmit":
		return truncate(firstLine(getStr(p, "prompt")), 80)

	case "PreToolUse":
		switch ev.ToolName {
		case "Bash":
			return truncate(firstLine(getStr(input, "command")), 80)
		case "Read":
			return pick(getStr(input, "file_path"), getStr(input, "filePath"))
		case "Edit", "Write":
			return pick(getStr(input, "file_path"), getStr(input, "filePath"))
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
		// OpenCode puts tool output in top-level "output" or "title" fields.
		ocOutput := pick(getStr(p, "title"), getStr(p, "output"))

		switch ev.ToolName {
		case "Bash":
			resp := asMapSafe(p["tool_response"])
			out := getStr(resp, "stdout")
			if out == "" {
				out = getStr(resp, "stderr")
			}
			if out == "" {
				out = ocOutput
			}
			return truncate(firstLine(out), 80)
		case "Read":
			return truncate(pick(getStr(input, "file_path"), getStr(input, "filePath"), ocOutput), 80)
		case "Edit", "Write":
			return truncate(pick(getStr(input, "file_path"), getStr(input, "filePath"), ocOutput), 80)
		default:
			resp := getStr(p, "tool_response")
			if resp == "" {
				respMap := asMapSafe(p["tool_response"])
				resp = getStr(respMap, "stdout")
			}
			if resp == "" {
				resp = ocOutput
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
		return truncate(firstLine(getStr(p, "last_assistant_message")), 80)
	case "Notification":
		return truncate(pick(getStr(p, "message"), getStr(p, "permission")), 80)

	case "SessionStatus":
		st := getStr(p, "status_type")
		if st == "retry" {
			attempt := getStr(p, "retry_attempt")
			msg := getStr(p, "retry_message")
			if attempt != "" {
				return truncate(fmt.Sprintf("retry #%s: %s", attempt, msg), 80)
			}
			return truncate("retry: "+msg, 80)
		}
		return st
	case "SessionDiff":
		fc := getStr(p, "diff_file_count")
		add := getStr(p, "diff_additions")
		del := getStr(p, "diff_deletions")
		if fc != "" {
			return fmt.Sprintf("%s files (+%s -%s)", fc, add, del)
		}
		return ""
	case "PermissionReply":
		return getStr(p, "reply")
	case "TodoUpdate":
		return getStr(p, "todo_count") + " todos"
	case "CommandExecuted":
		name := getStr(p, "command_name")
		args := getStr(p, "command_args")
		if args != "" {
			return truncate(name+" "+args, 80)
		}
		return name
	case "FileEdited":
		return getStr(p, "file")

	case "MessageUpdated":
		role := getStr(p, "message_role")
		if role == "assistant" {
			cost := getStr(p, "cost")
			in := getStr(p, "tokens_input")
			out := getStr(p, "tokens_output")
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
		partType := getStr(p, "part_type")
		switch partType {
		case "text":
			return truncate(firstLine(getStr(p, "text")), 80)
		case "reasoning":
			return truncate("reasoning: "+firstLine(getStr(p, "text")), 80)
		case "tool":
			name := getStr(p, "tool_name")
			status := getStr(p, "tool_status")
			title := getStr(p, "tool_title")
			s := name + " [" + status + "]"
			if title != "" {
				s += " " + title
			}
			return truncate(s, 80)
		case "step-finish":
			in := getStr(p, "tokens_input")
			out := getStr(p, "tokens_output")
			return fmt.Sprintf("step done (in:%s out:%s)", in, out)
		case "step-start":
			return "step start"
		default:
			return partType
		}

	default:
		return ""
	}
}

// pick returns the first non-empty string.
func pick(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
