package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/model"
)

type detailModel struct {
	viewport      viewport.Model
	event         *model.Event
	eventID       int64
	agents        map[string]*model.Agent
	showJSON      bool
	expandContent bool
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
		d.showJSON = false
		d.expandContent = false
		if ev != nil {
			d.eventID = ev.ID
		} else {
			d.eventID = 0
		}
	}
	d.syncContent(sameEvent)
}

func (d *detailModel) toggleJSON() {
	d.showJSON = !d.showJSON
	d.syncContent(false)
}

func (d *detailModel) toggleExpand() {
	d.expandContent = !d.expandContent
	d.syncContent(false)
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

	labelStyle := lipgloss.NewStyle().Foreground(colorGray).Width(12)
	valStyle := lipgloss.NewStyle().Foreground(colorWhite)

	row := func(label, value string) string {
		return labelStyle.Render(label) + valStyle.Render(value)
	}

	statusStr := model.DeriveEventStatus(ev.Subtype)
	statusColor := colorWhite
	switch statusStr {
	case "running":
		statusStr = "● running"
		statusColor = colorYellow
	case "completed":
		statusStr = "✓ completed"
		statusColor = colorGreen
	case "failed":
		statusStr = "✗ failed"
		statusColor = colorRed
	case "pending":
		statusStr = "○ pending"
		statusColor = colorGray
	}

	var lines []string
	lines = append(lines,
		row("Agent", agentName),
		row("Type", ev.Type+" / "+orDefault(ev.Subtype, "-")),
		row("Tool", orDefault(ev.ToolName, "-")),
		row("Time", formatTime(ev.Timestamp)+"  "+relativeTime(ev.Timestamp)),
		labelStyle.Render("Status")+lipgloss.NewStyle().Foreground(statusColor).Render(statusStr),
	)

	header := strings.Join(lines, "\n")
	sep := dimStyle.Render(strings.Repeat("─", 50))

	// tool-specific structured content
	body := d.renderToolDetail(ev)

	content := header + "\n" + sep + "\n" + body

	if d.showJSON {
		content += "\n\n" + dimStyle.Render("── Raw JSON ──") + "\n" + ev.PayloadPretty()
	} else {
		content += "\n\n" + dimStyle.Render("[J to toggle raw JSON]")
	}

	d.viewport.SetContent(content)
	if !preserveScroll {
		d.viewport.GotoTop()
		d.viewport.SetXOffset(0)
	}
}

func (d *detailModel) renderToolDetail(ev *model.Event) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
		return ev.PayloadPretty()
	}

	// Claude stores tool parameters in "tool_input", OpenCode in "args".
	input := asMapSafe(payload["tool_input"])
	if len(input) == 0 {
		input = asMapSafe(payload["args"])
	}
	response := prettyJSON(getStr(payload, "tool_response"))

	// merge: prefer input fields, fall back to top-level payload
	get := func(key string) string {
		if v := getStr(input, key); v != "" {
			return v
		}
		return getStr(payload, key)
	}

	fieldStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	contentStyle := lipgloss.NewStyle().Foreground(colorDimWhite)

	field := func(label, value string) string {
		if value == "" {
			return ""
		}
		return fieldStyle.Render(label+":") + " " + contentStyle.Render(value)
	}

	expand := d.expandContent
	block := func(label, value string) string {
		if value == "" {
			return ""
		}
		lines := strings.Split(value, "\n")
		totalLines := len(lines)
		if !expand && totalLines > 20 {
			lines = lines[:20]
			return fieldStyle.Render(label+fmt.Sprintf(" (%d lines):", totalLines)) + "\n" +
				contentStyle.Render(strings.Join(lines, "\n")) + "\n" +
				dimStyle.Render(fmt.Sprintf("  ... (%d more lines, e to expand)", totalLines-20))
		}
		return fieldStyle.Render(label+":") + "\n" + contentStyle.Render(strings.Join(lines, "\n"))
	}

	// non-tool events: render by subtype
	switch ev.Subtype {
	case "UserPromptSubmit":
		return joinNonEmpty("\n",
			field("Permission", get("permission_mode")),
			block("Prompt", get("prompt")),
		)
	case "SessionStart":
		return joinNonEmpty("\n",
			field("Model", get("model")),
			field("Source", get("source")),
			field("CWD", get("cwd")),
		)
	case "SessionEnd":
		return joinNonEmpty("\n",
			field("Reason", get("reason")),
		)
	case "Stop":
		return joinNonEmpty("\n",
			field("Permission", get("permission_mode")),
			block("Last Message", get("last_assistant_message")),
		)
	case "SubagentStop":
		return joinNonEmpty("\n",
			field("Agent Type", get("agent_type")),
			field("Permission", get("permission_mode")),
			block("Last Message", get("last_assistant_message")),
		)
	case "Notification":
		return joinNonEmpty("\n",
			field("Type", get("notification_type")),
			field("Permission", get("permission")),
			field("Message", get("message")),
		)
	case "SessionStatus":
		return joinNonEmpty("\n",
			field("Status", get("status_type")),
			field("Retry Attempt", get("retry_attempt")),
			field("Retry Message", get("retry_message")),
		)
	case "SessionDiff":
		header := joinNonEmpty("\n",
			field("Files Changed", get("diff_file_count")),
			field("Additions", get("diff_additions")),
			field("Deletions", get("diff_deletions")),
		)
		// New OpenCode events may omit `diff_files` entirely because the ingest
		// plugin now keeps only summary counts for `session.diff`. Older stored
		// rows can still include per-file patches, so we render them when present
		// and otherwise fall back to the summary header above.
		if filesRaw, ok := payload["diff_files"]; ok {
			if files, ok := filesRaw.([]any); ok {
				var patches []string
				for _, f := range files {
					fm, _ := f.(map[string]any)
					if fm == nil {
						continue
					}
					filePath := getStr(fm, "file")
					patch := getStr(fm, "patch")
					if patch != "" {
						patches = append(patches, d.renderPatchBlock(filePath, patch, expand))
					} else if filePath != "" {
						patches = append(patches, field("File", filePath))
					}
				}
				if len(patches) > 0 {
					return header + "\n" + strings.Join(patches, "\n")
				}
			}
		}
		return header
	case "PermissionReply":
		return joinNonEmpty("\n",
			field("Reply", get("reply")),
		)
	case "TodoUpdate":
		return joinNonEmpty("\n",
			field("Count", get("todo_count")),
			block("Todos", get("todos")),
		)
	case "CommandExecuted":
		return joinNonEmpty("\n",
			field("Command", get("command_name")),
			field("Arguments", get("command_args")),
		)
	case "FileEdited":
		return joinNonEmpty("\n",
			field("File", get("file")),
		)
	case "MessageUpdated":
		return joinNonEmpty("\n",
			field("Role", get("message_role")),
			field("Model", get("model_id")),
			field("Agent", get("agent_name")),
			field("Finish", get("finish_reason")),
			field("Cost", get("cost")),
			field("Input Tokens", get("tokens_input")),
			field("Output Tokens", get("tokens_output")),
			field("Reasoning Tokens", get("tokens_reasoning")),
			field("Cache Read", get("tokens_cache_read")),
			field("Cache Write", get("tokens_cache_write")),
			field("Error", get("error_name")),
			field("Error Message", get("error_message")),
		)
	case "PartUpdated":
		partType := get("part_type")
		switch partType {
		case "text":
			return joinNonEmpty("\n",
				field("Part", "text"),
				block("Content", get("text")),
			)
		case "reasoning":
			return joinNonEmpty("\n",
				field("Part", "reasoning"),
				block("Content", get("text")),
			)
		case "tool":
			return joinNonEmpty("\n",
				field("Part", "tool"),
				field("Tool", get("tool_name")),
				field("Call ID", get("call_id")),
				field("Status", get("tool_status")),
				field("Title", get("tool_title")),
				field("Error", get("tool_error")),
			)
		case "step-finish":
			return joinNonEmpty("\n",
				field("Part", "step-finish"),
				field("Reason", get("finish_reason")),
				field("Cost", get("cost")),
				field("Input Tokens", get("tokens_input")),
				field("Output Tokens", get("tokens_output")),
				field("Reasoning Tokens", get("tokens_reasoning")),
				field("Cache Read", get("tokens_cache_read")),
				field("Cache Write", get("tokens_cache_write")),
			)
		default:
			return field("Part", partType)
		}
	}

	// tool events: render by tool name
	switch ev.ToolName {
	case "Bash":
		return joinNonEmpty("\n",
			field("Command", get("command")),
			field("Description", get("description")),
			field("CWD", get("cwd")),
			field("Timeout", get("timeout")),
			block("Output", firstNonEmpty(response, get("stdout"), get("result"), get("output"))),
			blockIfPresent("Stderr", get("stderr"), fieldStyle, contentStyle),
		)

	case "Read":
		rangeStr := ""
		if off := get("offset"); off != "" {
			rangeStr = "offset:" + off
			if lim := get("limit"); lim != "" {
				rangeStr += " limit:" + lim
			}
		}
		return joinNonEmpty("\n",
			field("File", get("file_path")),
			field("Range", rangeStr),
			block("Content", firstNonEmpty(response, get("content"))),
		)

	case "Edit":
		filePath := pick(get("file_path"), get("filePath"))
		oldStr := pick(get("old_string"), get("oldString"))
		newStr := pick(get("new_string"), get("newString"))

		// OpenCode PostToolUse: metadata contains a precomputed unified diff
		metaDiff := ""
		if meta := asMapSafe(payload["metadata"]); len(meta) > 0 {
			metaDiff = getStr(meta, "diff")
		}

		var diffBlock string
		if oldStr != "" || newStr != "" {
			diffBlock = d.renderDiffBlock("Diff", filePath, oldStr, newStr, expand)
		} else if metaDiff != "" {
			diffBlock = d.renderPatchBlock("Diff", metaDiff, expand)
		}

		return joinNonEmpty("\n",
			field("File", filePath),
			diffBlock,
		)

	case "Write":
		filePath := pick(get("file_path"), get("filePath"))
		return joinNonEmpty("\n",
			field("File", filePath),
			d.renderAdditionsBlock("Content", filePath, get("content"), expand),
		)

	case "apply_patch":
		patch := pick(get("input"), get("patch"), get("patchText"))
		// OpenCode PostToolUse: metadata contains a precomputed unified diff
		if patch == "" {
			if meta := asMapSafe(payload["metadata"]); len(meta) > 0 {
				patch = getStr(meta, "diff")
			}
		}
		return joinNonEmpty("\n",
			d.renderPatchBlock("Patch", patch, expand),
		)

	case "Grep":
		return joinNonEmpty("\n",
			field("Pattern", get("pattern")),
			field("Path", get("path")),
			field("Glob", get("glob")),
			field("Type", get("type")),
			block("Result", firstNonEmpty(response, get("result"))),
		)

	case "Glob":
		return joinNonEmpty("\n",
			field("Pattern", get("pattern")),
			field("Path", get("path")),
			block("Result", firstNonEmpty(response, get("result"))),
		)

	case "Agent":
		return joinNonEmpty("\n",
			field("Name", get("name")),
			field("Type", get("subagent_type")),
			field("Model", get("model")),
			field("Description", get("description")),
			block("Prompt", get("prompt")),
			block("Result", firstNonEmpty(response, get("result"))),
		)

	default:
		// merge input into payload for generic display
		for k, v := range input {
			if _, exists := payload[k]; !exists {
				payload[k] = v
			}
		}
		return d.renderGenericDetail(payload)
	}
}

// renderDiffBlock renders old_string and new_string as a colored unified diff
// using the Myers diff algorithm with 3 lines of context and syntax highlighting.
func (d *detailModel) renderDiffBlock(label, filePath, oldStr, newStr string, expand bool) string {
	if oldStr == "" && newStr == "" {
		return ""
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	removePrefix := lipgloss.NewStyle().Foreground(colorRed).Render("- ")
	addPrefix := lipgloss.NewStyle().Foreground(colorGreen).Render("+ ")
	sepStyle := lipgloss.NewStyle().Foreground(colorMagenta)

	lang := langFromPath(filePath)
	oldLines := splitLines(oldStr)
	newLines := splitLines(newStr)
	script := ComputeDiff(oldLines, newLines)
	stats := Stats(script)

	contextScript := WithContext(script, 3)

	var lines []string
	for _, dl := range contextScript {
		switch dl.Op {
		case DiffDelete:
			lines = append(lines, removePrefix+highlightLine(dl.Text, lang, hlBgDel))
		case DiffInsert:
			lines = append(lines, addPrefix+highlightLine(dl.Text, lang, hlBgAdd))
		case DiffEqual:
			if dl.Text == "~~~" {
				lines = append(lines, sepStyle.Render("  ~~~"))
			} else {
				lines = append(lines, dimStyle.Render("  ")+highlightLine(dl.Text, lang))
			}
		}
	}

	statsStr := fmt.Sprintf(" (+%d -%d)", stats.Additions, stats.Deletions)
	totalLines := len(lines)
	if !expand && totalLines > 30 {
		lines = lines[:30]
		return headerStyle.Render(label+statsStr+fmt.Sprintf(" (%d lines):", totalLines)) + "\n" +
			strings.Join(lines, "\n") + "\n" +
			dimStyle.Render(fmt.Sprintf("  ... (%d more lines, e to expand)", totalLines-30))
	}
	return headerStyle.Render(label+statsStr+":") + "\n" + strings.Join(lines, "\n")
}

// renderAdditionsBlock renders content as all-additions (green + prefix)
// with syntax highlighting based on file extension.
func (d *detailModel) renderAdditionsBlock(label, filePath, content string, expand bool) string {
	if content == "" {
		return ""
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	addPrefix := lipgloss.NewStyle().Foreground(colorGreen).Render("+ ")

	lang := langFromPath(filePath)
	raw := strings.Split(content, "\n")
	totalLines := len(raw)
	if !expand && totalLines > 20 {
		raw = raw[:20]
	}

	var lines []string
	for _, l := range raw {
		lines = append(lines, addPrefix+highlightLine(l, lang, hlBgAdd))
	}

	if !expand && totalLines > 20 {
		return headerStyle.Render(label+fmt.Sprintf(" (%d lines):", totalLines)) + "\n" +
			strings.Join(lines, "\n") + "\n" +
			dimStyle.Render(fmt.Sprintf("  ... (%d more lines, e to expand)", totalLines-20))
	}
	return headerStyle.Render(label+":") + "\n" + strings.Join(lines, "\n")
}

// renderPatchBlock renders a unified patch with diff coloring and syntax highlighting.
// It detects the file path from patch headers (*** Update File: path, --- a/path, etc.)
// and applies language-appropriate highlighting to changed lines.
func (d *detailModel) renderPatchBlock(label, patch string, expand bool) string {
	if patch == "" {
		return ""
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	removePrefix := lipgloss.NewStyle().Foreground(colorRed).Render("-")
	addPrefix := lipgloss.NewStyle().Foreground(colorGreen).Render("+")
	metaStyle := lipgloss.NewStyle().Foreground(colorMagenta)

	raw := strings.Split(patch, "\n")
	totalLines := len(raw)
	if !expand && totalLines > 40 {
		raw = raw[:40]
	}

	var lang string
	var lines []string
	for _, l := range raw {
		switch {
		case strings.HasPrefix(l, "*** ") && strings.Contains(l, "File:"):
			// Codex format: *** Update File: path/to/file.go
			if idx := strings.Index(l, "File:"); idx >= 0 {
				filePath := strings.TrimSpace(l[idx+5:])
				lang = langFromPath(filePath)
			}
			lines = append(lines, metaStyle.Render(l))
		case strings.HasPrefix(l, "--- "):
			// unified diff: --- a/path/to/file.go
			filePath := strings.TrimPrefix(strings.TrimPrefix(l, "--- "), "a/")
			lang = langFromPath(filePath)
			lines = append(lines, metaStyle.Render(l))
		case strings.HasPrefix(l, "+++ "):
			lines = append(lines, metaStyle.Render(l))
		case strings.HasPrefix(l, "-"):
			lines = append(lines, removePrefix+highlightLine(l[1:], lang, hlBgDel))
		case strings.HasPrefix(l, "+"):
			lines = append(lines, addPrefix+highlightLine(l[1:], lang, hlBgAdd))
		case strings.HasPrefix(l, "***"):
			lines = append(lines, metaStyle.Render(l))
		case strings.HasPrefix(l, "@@"):
			lines = append(lines, metaStyle.Render(l))
		default:
			lines = append(lines, dimStyle.Render(l))
		}
	}

	if !expand && totalLines > 40 {
		return headerStyle.Render(label+fmt.Sprintf(" (%d lines):", totalLines)) + "\n" +
			strings.Join(lines, "\n") + "\n" +
			dimStyle.Render(fmt.Sprintf("  ... (%d more lines, e to expand)", totalLines-40))
	}
	return headerStyle.Render(label+":") + "\n" + strings.Join(lines, "\n")
}

func asMapSafe(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func (d *detailModel) renderGenericDetail(payload map[string]any) string {
	fieldStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	contentStyle := lipgloss.NewStyle().Foreground(colorDimWhite)

	var parts []string
	shown := map[string]bool{}

	// show message/content/result first if present
	for _, key := range []string{"message", "content", "result", "text", "prompt", "error"} {
		if v := getStr(payload, key); v != "" {
			label := strings.ToUpper(key[:1]) + key[1:]
			if len(v) > 500 {
				v = v[:500] + "..."
			}
			parts = append(parts, fieldStyle.Render(label+":")+"\n"+contentStyle.Render(v))
			shown[key] = true
		}
	}

	// show remaining simple fields (sorted)
	keys := make([]string, 0, len(payload))
	for k := range payload {
		if !shown[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := payload[k].(string); ok && s != "" && len(s) < 200 {
			parts = append(parts, fieldStyle.Render(k+":")+contentStyle.Render(" "+s))
		}
	}

	if len(parts) == 0 {
		return dimStyle.Render("(no structured data)")
	}
	return strings.Join(parts, "\n")
}

func (d *detailModel) view(width, height int, focused bool) string {
	d.viewport.SetWidth(max(width-4, 10))
	d.viewport.SetHeight(max(height-3, 4))

	title := titleStyle.Render("Detail")
	return renderPane(width, height, focused, title, strings.Split(d.viewport.View(), "\n"))
}

// helpers

func getStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%v", val)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func joinNonEmpty(sep string, parts ...string) string {
	var filtered []string
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(filtered, sep)
}

func blockIfPresent(label, value string, fieldStyle, contentStyle lipgloss.Style) string {
	if value == "" {
		return ""
	}
	return fieldStyle.Render(label+":") + "\n" + contentStyle.Render(value)
}

func prettyJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || (s[0] != '{' && s[0] != '[') {
		return s
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}
