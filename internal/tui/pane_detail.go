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
	viewport     viewport.Model
	event        *model.Event
	eventID      int64
	agents       map[string]*model.Agent
	showJSON     bool
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

	// extract tool_input and tool_response from hook events
	input := asMapSafe(payload["tool_input"])
	response := prettyJSON(getStr(payload, "tool_response"))

	// merge: prefer tool_input fields, fall back to top-level
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
			field("Message", get("message")),
		)
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
		return joinNonEmpty("\n",
			field("File", get("file_path")),
			block("Old", get("old_string")),
			block("New", get("new_string")),
			block("Result", response),
		)

	case "Write":
		return joinNonEmpty("\n",
			field("File", get("file_path")),
			block("Content", get("content")),
			block("Result", response),
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
	content := title + "\n" + d.viewport.View()
	return paneStyle(focused).Width(width).Height(height).Render(content)
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
