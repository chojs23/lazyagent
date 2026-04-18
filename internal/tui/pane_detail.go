package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/textutil"
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

type detailRenderContext struct {
	payload      map[string]any
	input        map[string]any
	response     string
	expand       bool
	fieldStyle   lipgloss.Style
	contentStyle lipgloss.Style
}

func (c detailRenderContext) get(key string) string {
	if v := jsonutil.GetString(c.input, key); v != "" {
		return v
	}
	return jsonutil.GetString(c.payload, key)
}

func (c detailRenderContext) field(label, value string) string {
	if value == "" {
		return ""
	}
	return c.fieldStyle.Render(label+":") + " " + c.contentStyle.Render(value)
}

func (c detailRenderContext) block(label, value string) string {
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	totalLines := len(lines)
	if !c.expand && totalLines > 20 {
		lines = lines[:20]
		return c.fieldStyle.Render(label+fmt.Sprintf(" (%d lines):", totalLines)) + "\n" +
			c.contentStyle.Render(strings.Join(lines, "\n")) + "\n" +
			dimStyle.Render(fmt.Sprintf("  ... (%d more lines, e to expand)", totalLines-20))
	}
	return c.fieldStyle.Render(label+":") + "\n" + c.contentStyle.Render(strings.Join(lines, "\n"))
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
	d.syncContent(true)
}

func (d *detailModel) toggleExpand() {
	d.expandContent = !d.expandContent
	d.syncContent(true)
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
	input := jsonutil.MapOrEmpty(payload["tool_input"])
	if len(input) == 0 {
		input = jsonutil.MapOrEmpty(payload["args"])
	}
	ctx := detailRenderContext{
		payload:      payload,
		input:        input,
		response:     prettyJSON(jsonutil.GetString(payload, "tool_response")),
		expand:       d.expandContent,
		fieldStyle:   lipgloss.NewStyle().Foreground(colorCyan).Bold(true),
		contentStyle: lipgloss.NewStyle().Foreground(colorDimWhite),
	}

	if body, handled := d.renderSubtypeDetail(ev, ctx); handled {
		return body
	}
	if body, handled := d.renderKnownToolDetail(ev, ctx); handled {
		return body
	}

	for k, v := range ctx.input {
		if _, exists := ctx.payload[k]; !exists {
			ctx.payload[k] = v
		}
	}
	return d.renderGenericDetail(ctx.payload)
}

func (d *detailModel) renderSubtypeDetail(ev *model.Event, ctx detailRenderContext) (string, bool) {
	switch ev.Subtype {
	case "UserPromptSubmit":
		return joinNonEmpty("\n",
			ctx.field("Permission", ctx.get("permission_mode")),
			ctx.block("Prompt", ctx.get("prompt")),
		), true
	case "SessionStart":
		return joinNonEmpty("\n",
			ctx.field("Model", ctx.get("model")),
			ctx.field("Source", ctx.get("source")),
			ctx.field("CWD", ctx.get("cwd")),
		), true
	case "SessionEnd":
		return joinNonEmpty("\n", ctx.field("Reason", ctx.get("reason"))), true
	case "Stop":
		return joinNonEmpty("\n",
			ctx.field("Permission", ctx.get("permission_mode")),
			ctx.block("Last Message", ctx.get("last_assistant_message")),
		), true
	case "SubagentStop":
		return joinNonEmpty("\n",
			ctx.field("Agent Type", ctx.get("agent_type")),
			ctx.field("Permission", ctx.get("permission_mode")),
			ctx.block("Last Message", ctx.get("last_assistant_message")),
		), true
	case "Notification":
		return joinNonEmpty("\n",
			ctx.field("Type", ctx.get("notification_type")),
			ctx.field("Permission", ctx.get("permission")),
			ctx.field("Message", ctx.get("message")),
		), true
	case "SessionStatus":
		return joinNonEmpty("\n",
			ctx.field("Status", ctx.get("status_type")),
			ctx.field("Retry Attempt", ctx.get("retry_attempt")),
			ctx.field("Retry Message", ctx.get("retry_message")),
		), true
	case "SessionDiff":
		return d.renderSessionDiffDetail(ctx), true
	case "PermissionReply":
		return joinNonEmpty("\n", ctx.field("Reply", ctx.get("reply"))), true
	case "TodoUpdate":
		return joinNonEmpty("\n",
			ctx.field("Count", ctx.get("todo_count")),
			ctx.block("Todos", ctx.get("todos")),
		), true
	case "CommandExecuted":
		return joinNonEmpty("\n",
			ctx.field("Command", ctx.get("command_name")),
			ctx.field("Arguments", ctx.get("command_args")),
		), true
	case "FileEdited":
		return joinNonEmpty("\n", ctx.field("File", ctx.get("file"))), true
	case "MessageUpdated":
		return joinNonEmpty("\n",
			ctx.field("Role", ctx.get("message_role")),
			ctx.field("Model", ctx.get("model_id")),
			ctx.field("Agent", ctx.get("agent_name")),
			ctx.field("Finish", ctx.get("finish_reason")),
			ctx.field("Cost", ctx.get("cost")),
			ctx.field("Input Tokens", ctx.get("tokens_input")),
			ctx.field("Output Tokens", ctx.get("tokens_output")),
			ctx.field("Reasoning Tokens", ctx.get("tokens_reasoning")),
			ctx.field("Cache Read", ctx.get("tokens_cache_read")),
			ctx.field("Cache Write", ctx.get("tokens_cache_write")),
			ctx.field("Error", ctx.get("error_name")),
			ctx.field("Error Message", ctx.get("error_message")),
		), true
	case "PartUpdated":
		return d.renderPartUpdatedDetail(ctx), true
	default:
		return "", false
	}
}

func (d *detailModel) renderSessionDiffDetail(ctx detailRenderContext) string {
	header := joinNonEmpty("\n",
		ctx.field("Files Changed", ctx.get("diff_file_count")),
		ctx.field("Additions", ctx.get("diff_additions")),
		ctx.field("Deletions", ctx.get("diff_deletions")),
	)
	if filesRaw, ok := ctx.payload["diff_files"]; ok {
		if files, ok := filesRaw.([]any); ok {
			var patches []string
			for _, f := range files {
				fm, _ := f.(map[string]any)
				if fm == nil {
					continue
				}
				filePath := jsonutil.GetString(fm, "file")
				patch := jsonutil.GetString(fm, "patch")
				if patch != "" {
					patches = append(patches, d.renderPatchBlock(filePath, patch, ctx.expand))
				} else if filePath != "" {
					patches = append(patches, ctx.field("File", filePath))
				}
			}
			if len(patches) > 0 {
				return header + "\n" + strings.Join(patches, "\n")
			}
		}
	}
	return header
}

func (d *detailModel) renderPartUpdatedDetail(ctx detailRenderContext) string {
	partType := ctx.get("part_type")
	switch partType {
	case "text":
		return joinNonEmpty("\n",
			ctx.field("Part", "text"),
			ctx.block("Content", ctx.get("text")),
		)
	case "reasoning":
		return joinNonEmpty("\n",
			ctx.field("Part", "reasoning"),
			ctx.block("Content", ctx.get("text")),
		)
	case "tool":
		return joinNonEmpty("\n",
			ctx.field("Part", "tool"),
			ctx.field("Tool", ctx.get("tool_name")),
			ctx.field("Call ID", ctx.get("call_id")),
			ctx.field("Status", ctx.get("tool_status")),
			ctx.field("Title", ctx.get("tool_title")),
			ctx.field("Error", ctx.get("tool_error")),
		)
	case "step-finish":
		return joinNonEmpty("\n",
			ctx.field("Part", "step-finish"),
			ctx.field("Reason", ctx.get("finish_reason")),
			ctx.field("Cost", ctx.get("cost")),
			ctx.field("Input Tokens", ctx.get("tokens_input")),
			ctx.field("Output Tokens", ctx.get("tokens_output")),
			ctx.field("Reasoning Tokens", ctx.get("tokens_reasoning")),
			ctx.field("Cache Read", ctx.get("tokens_cache_read")),
			ctx.field("Cache Write", ctx.get("tokens_cache_write")),
		)
	default:
		return ctx.field("Part", partType)
	}
}

func (d *detailModel) renderKnownToolDetail(ev *model.Event, ctx detailRenderContext) (string, bool) {
	switch ev.ToolName {
	case "Bash":
		return joinNonEmpty("\n",
			ctx.field("Command", ctx.get("command")),
			ctx.field("Description", ctx.get("description")),
			ctx.field("CWD", ctx.get("cwd")),
			ctx.field("Timeout", ctx.get("timeout")),
			ctx.block("Output", textutil.FirstNonEmpty(ctx.response, ctx.get("stdout"), ctx.get("result"), ctx.get("output"))),
			blockIfPresent("Stderr", ctx.get("stderr"), ctx.fieldStyle, ctx.contentStyle),
		), true
	case "Read":
		rangeStr := ""
		if off := ctx.get("offset"); off != "" {
			rangeStr = "offset:" + off
			if lim := ctx.get("limit"); lim != "" {
				rangeStr += " limit:" + lim
			}
		}
		return joinNonEmpty("\n",
			ctx.field("File", ctx.get("file_path")),
			ctx.field("Range", rangeStr),
			ctx.block("Content", textutil.FirstNonEmpty(ctx.response, ctx.get("content"))),
		), true
	case "Edit":
		return d.renderEditToolDetail(ctx), true
	case "Write":
		filePath := textutil.FirstNonEmpty(ctx.get("file_path"), ctx.get("filePath"))
		return joinNonEmpty("\n",
			ctx.field("File", filePath),
			d.renderAdditionsBlock("Content", filePath, ctx.get("content"), ctx.expand),
		), true
	case "apply_patch":
		return d.renderApplyPatchDetail(ctx), true
	case "Grep":
		return joinNonEmpty("\n",
			ctx.field("Pattern", ctx.get("pattern")),
			ctx.field("Path", ctx.get("path")),
			ctx.field("Glob", ctx.get("glob")),
			ctx.field("Type", ctx.get("type")),
			ctx.block("Result", textutil.FirstNonEmpty(ctx.response, ctx.get("result"))),
		), true
	case "Glob":
		return joinNonEmpty("\n",
			ctx.field("Pattern", ctx.get("pattern")),
			ctx.field("Path", ctx.get("path")),
			ctx.block("Result", textutil.FirstNonEmpty(ctx.response, ctx.get("result"))),
		), true
	case "Agent":
		return joinNonEmpty("\n",
			ctx.field("Name", ctx.get("name")),
			ctx.field("Type", ctx.get("subagent_type")),
			ctx.field("Model", ctx.get("model")),
			ctx.field("Description", ctx.get("description")),
			ctx.block("Prompt", ctx.get("prompt")),
			ctx.block("Result", textutil.FirstNonEmpty(ctx.response, ctx.get("result"))),
		), true
	default:
		return "", false
	}
}

func (d *detailModel) renderEditToolDetail(ctx detailRenderContext) string {
	filePath := textutil.FirstNonEmpty(ctx.get("file_path"), ctx.get("filePath"))
	oldStr := textutil.FirstNonEmpty(ctx.get("old_string"), ctx.get("oldString"))
	newStr := textutil.FirstNonEmpty(ctx.get("new_string"), ctx.get("newString"))
	metaDiff := ""
	if meta := jsonutil.MapOrEmpty(ctx.payload["metadata"]); len(meta) > 0 {
		metaDiff = jsonutil.GetString(meta, "diff")
	}

	var diffBlock string
	if oldStr != "" || newStr != "" {
		diffBlock = d.renderDiffBlock("Diff", filePath, oldStr, newStr, ctx.expand)
	} else if metaDiff != "" {
		diffBlock = d.renderPatchBlock("Diff", metaDiff, ctx.expand)
	}

	return joinNonEmpty("\n",
		ctx.field("File", filePath),
		diffBlock,
	)
}

func (d *detailModel) renderApplyPatchDetail(ctx detailRenderContext) string {
	patch := textutil.FirstNonEmpty(ctx.get("input"), ctx.get("patch"), ctx.get("patchText"))
	if patch == "" {
		if meta := jsonutil.MapOrEmpty(ctx.payload["metadata"]); len(meta) > 0 {
			patch = jsonutil.GetString(meta, "diff")
		}
	}
	return joinNonEmpty("\n", d.renderPatchBlock("Patch", patch, ctx.expand))
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

func (d *detailModel) renderGenericDetail(payload map[string]any) string {
	fieldStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	contentStyle := lipgloss.NewStyle().Foreground(colorDimWhite)

	var parts []string
	shown := map[string]bool{}

	// show message/content/result first if present
	for _, key := range []string{"message", "content", "result", "text", "prompt", "error"} {
		if v := jsonutil.GetString(payload, key); v != "" {
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
