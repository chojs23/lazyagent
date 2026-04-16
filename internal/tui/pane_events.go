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
	contentHeight := max(e.height-3, 1)
	scrolloff := min(3, (contentHeight-1)/2)

	// cursor too close to bottom edge → scroll down
	if e.cursor > e.scroll+contentHeight-1-scrolloff {
		e.scroll = e.cursor - contentHeight + 1 + scrolloff
	}
	// cursor too close to top edge → scroll up
	if e.cursor < e.scroll+scrolloff {
		e.scroll = e.cursor - scrolloff
	}

	e.scroll = max(e.scroll, 0)
	maxScroll := max(len(e.events)-contentHeight, 0)
	e.scroll = min(e.scroll, maxScroll)
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

	// scroll is computed by clampScroll() in Update()-context methods.
	// Just read it here.
	var lines []string
	end := min(e.scroll+contentHeight, len(e.events))
	totalDigits := len(fmt.Sprintf("%d", e.loadedOffset+len(e.events)))
	for i := e.scroll; i < end; i++ {
		ev := e.events[i]
		absIndex := e.loadedOffset + i
		line := e.renderEventLine(ev, absIndex, i == e.cursor, focused, agentMap, width-4, totalDigits)
		lines = append(lines, line)
	}

	textWidth := max(width-4, 1)
	e.hScroll = clampHScroll(lines, e.hScroll, textWidth)
	for i, l := range lines {
		lines[i] = hScrollLine(l, e.hScroll, textWidth)
	}

	return renderPane(width, height, focused, headerLine, lines)
}

func (e *eventsModel) renderEventLine(ev model.Event, index int, atCursor bool, focused bool, agentMap map[string]agentInfo, maxW int, totalDigits int) string {
	numStr := fmt.Sprintf("%*d", totalDigits, index+1)
	subtype := truncate(orDefault(ev.Subtype, ev.Type), 20)

	agentLabel := ""
	if info, ok := agentMap[ev.AgentID]; ok {
		agentLabel = info.name
	}

	brief := eventBrief(ev)

	// order: num | agent | subtype | tool | brief
	if atCursor {
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
		style := selectedStyle
		if focused {
			style = cursorStyle
		}
		return style.Render("  " + strings.Join(parts, "  "))
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
		pt := jsonutil.GetString(p, "part_type")
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
	input := jsonutil.MapOrEmpty(p["tool_input"])
	if len(input) == 0 {
		input = jsonutil.MapOrEmpty(p["args"])
	}

	switch ev.Subtype {
	case "UserPromptSubmit":
		return truncate(textutil.FirstLine(jsonutil.GetString(p, "prompt")), 80)

	case "PreToolUse":
		switch ev.ToolName {
		case "Bash":
			return truncate(textutil.FirstLine(jsonutil.GetString(input, "command")), 80)
		case "Read":
			return textutil.FirstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"))
		case "Edit":
			filePath := textutil.FirstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"))
			oldStr := textutil.FirstNonEmpty(jsonutil.GetString(input, "old_string"), jsonutil.GetString(input, "oldString"))
			newStr := textutil.FirstNonEmpty(jsonutil.GetString(input, "new_string"), jsonutil.GetString(input, "newString"))
			stats := editDiffStats(oldStr, newStr)
			if stats != "" {
				return truncate(filePath+" "+stats, 80)
			}
			return filePath
		case "Write":
			filePath := textutil.FirstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"))
			if content := jsonutil.GetString(input, "content"); content != "" {
				n := strings.Count(content, "\n") + 1
				return truncate(fmt.Sprintf("%s (+%d)", filePath, n), 80)
			}
			return filePath
		case "apply_patch":
			patch := textutil.FirstNonEmpty(jsonutil.GetString(input, "input"), jsonutil.GetString(input, "patch"), jsonutil.GetString(input, "patchText"))
			if patch == "" {
				if meta := jsonutil.MapOrEmpty(p["metadata"]); len(meta) > 0 {
					patch = jsonutil.GetString(meta, "diff")
				}
			}
			stats := patchDiffStats(patch)
			return truncate(stats, 80)
		case "Grep":
			s := jsonutil.GetString(input, "pattern")
			if path := jsonutil.GetString(input, "path"); path != "" {
				s += " in " + path
			}
			return truncate(s, 80)
		case "Glob":
			return jsonutil.GetString(input, "pattern")
		case "Agent":
			desc := jsonutil.GetString(input, "description")
			if t := jsonutil.GetString(input, "subagent_type"); t != "" {
				desc = "[" + t + "] " + desc
			}
			return truncate(desc, 80)
		default:
			return truncate(textutil.FirstLine(jsonutil.GetString(input, "description")), 80)
		}

	case "PostToolUse", "PostToolUseFailure":
		// OpenCode puts tool output in top-level "output" or "title" fields.
		ocOutput := textutil.FirstNonEmpty(jsonutil.GetString(p, "title"), jsonutil.GetString(p, "output"))

		switch ev.ToolName {
		case "Bash":
			resp := jsonutil.MapOrEmpty(p["tool_response"])
			out := jsonutil.GetString(resp, "stdout")
			if out == "" {
				out = jsonutil.GetString(resp, "stderr")
			}
			if out == "" {
				out = ocOutput
			}
			return truncate(textutil.FirstLine(out), 80)
		case "Read":
			return truncate(textutil.FirstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"), ocOutput), 80)
		case "Edit":
			filePath := textutil.FirstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"), ocOutput)
			oldStr := textutil.FirstNonEmpty(jsonutil.GetString(input, "old_string"), jsonutil.GetString(input, "oldString"))
			newStr := textutil.FirstNonEmpty(jsonutil.GetString(input, "new_string"), jsonutil.GetString(input, "newString"))
			stats := editDiffStats(oldStr, newStr)
			// fallback: try metadata.diff from OpenCode PostToolUse
			if stats == "" {
				if meta := jsonutil.MapOrEmpty(p["metadata"]); len(meta) > 0 {
					stats = patchDiffStats(jsonutil.GetString(meta, "diff"))
				}
			}
			if stats != "" {
				return truncate(filePath+" "+stats, 80)
			}
			return truncate(filePath, 80)
		case "Write":
			filePath := textutil.FirstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"), ocOutput)
			if content := jsonutil.GetString(input, "content"); content != "" {
				n := strings.Count(content, "\n") + 1
				return truncate(fmt.Sprintf("%s (+%d)", filePath, n), 80)
			}
			return truncate(filePath, 80)
		case "apply_patch":
			patch := textutil.FirstNonEmpty(jsonutil.GetString(input, "input"), jsonutil.GetString(input, "patch"), jsonutil.GetString(input, "patchText"))
			if patch == "" {
				if meta := jsonutil.MapOrEmpty(p["metadata"]); len(meta) > 0 {
					patch = jsonutil.GetString(meta, "diff")
				}
			}
			stats := patchDiffStats(patch)
			return truncate(stats, 80)
		default:
			resp := jsonutil.GetString(p, "tool_response")
			if resp == "" {
				respMap := jsonutil.MapOrEmpty(p["tool_response"])
				resp = jsonutil.GetString(respMap, "stdout")
			}
			if resp == "" {
				resp = ocOutput
			}
			return truncate(textutil.FirstLine(resp), 80)
		}

	case "SessionStart":
		return jsonutil.GetString(p, "model")
	case "SessionEnd":
		return jsonutil.GetString(p, "reason")
	case "Stop":
		return truncate(textutil.FirstLine(jsonutil.GetString(p, "last_assistant_message")), 80)
	case "SubagentStop":
		return truncate(textutil.FirstLine(jsonutil.GetString(p, "last_assistant_message")), 80)
	case "Notification":
		return truncate(textutil.FirstNonEmpty(jsonutil.GetString(p, "message"), jsonutil.GetString(p, "permission")), 80)

	case "SessionStatus":
		st := jsonutil.GetString(p, "status_type")
		if st == "retry" {
			attempt := jsonutil.GetString(p, "retry_attempt")
			msg := jsonutil.GetString(p, "retry_message")
			if attempt != "" {
				return truncate(fmt.Sprintf("retry #%s: %s", attempt, msg), 80)
			}
			return truncate("retry: "+msg, 80)
		}
		return st
	case "SessionDiff":
		fc := jsonutil.GetString(p, "diff_file_count")
		add := jsonutil.GetString(p, "diff_additions")
		del := jsonutil.GetString(p, "diff_deletions")
		if fc != "" {
			return fmt.Sprintf("%s files (+%s -%s)", fc, add, del)
		}
		return ""
	case "PermissionReply":
		return jsonutil.GetString(p, "reply")
	case "TodoUpdate":
		return jsonutil.GetString(p, "todo_count") + " todos"
	case "CommandExecuted":
		name := jsonutil.GetString(p, "command_name")
		args := jsonutil.GetString(p, "command_args")
		if args != "" {
			return truncate(name+" "+args, 80)
		}
		return name
	case "FileEdited":
		return jsonutil.GetString(p, "file")

	case "MessageUpdated":
		role := jsonutil.GetString(p, "message_role")
		if role == "assistant" {
			cost := jsonutil.GetString(p, "cost")
			in := jsonutil.GetString(p, "tokens_input")
			out := jsonutil.GetString(p, "tokens_output")
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
		partType := jsonutil.GetString(p, "part_type")
		switch partType {
		case "text":
			return truncate(textutil.FirstLine(jsonutil.GetString(p, "text")), 80)
		case "reasoning":
			return truncate("reasoning: "+textutil.FirstLine(jsonutil.GetString(p, "text")), 80)
		case "tool":
			name := jsonutil.GetString(p, "tool_name")
			status := jsonutil.GetString(p, "tool_status")
			title := jsonutil.GetString(p, "tool_title")
			s := name + " [" + status + "]"
			if title != "" {
				s += " " + title
			}
			return truncate(s, 80)
		case "step-finish":
			in := jsonutil.GetString(p, "tokens_input")
			out := jsonutil.GetString(p, "tokens_output")
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
	for _, line := range strings.Split(patch, "\n") {
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
