// Package eventview produces the short, human-readable summaries ("briefs")
// shown in the events list of both the TUI and the web UI.
//
// The TUI renders briefs with lipgloss colors while the web UI emits plain
// strings; keeping the brief logic in this leaf package (no UI deps) means
// both surfaces always agree on what each event "looks like" at a glance.
package eventview

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chojs23/lazyagent/internal/diff"
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/textutil"
)

const briefMaxLen = 80

// Brief returns a one-line summary of the given event. Returns "" when the
// event has no useful summary (e.g. unknown subtypes or empty payloads).
func Brief(ev model.Event) string {
	ctx, ok := newBriefContext(ev)
	if !ok {
		return ""
	}

	switch ev.Subtype {
	case "UserPromptSubmit":
		return truncate(textutil.FirstLine(ctx.payloadString("prompt")), briefMaxLen)

	case "PreToolUse":
		return briefForPreToolUse(ev, ctx)

	case "PostToolUse", "PostToolUseFailure":
		return briefForPostToolUse(ev, ctx)

	case "SessionStart":
		return ctx.payloadString("model")
	case "SessionEnd":
		return ctx.payloadString("reason")
	case "Stop":
		return truncate(textutil.FirstLine(ctx.payloadString("last_assistant_message")), briefMaxLen)
	case "SubagentStop":
		return truncate(textutil.FirstLine(ctx.payloadString("last_assistant_message")), briefMaxLen)
	case "Notification":
		return truncate(textutil.FirstNonEmpty(ctx.payloadString("message"), ctx.payloadString("permission")), briefMaxLen)

	case "SessionStatus":
		st := ctx.payloadString("status_type")
		if st == "retry" {
			attempt := ctx.payloadString("retry_attempt")
			msg := ctx.payloadString("retry_message")
			if attempt != "" {
				return truncate(fmt.Sprintf("retry #%s: %s", attempt, msg), briefMaxLen)
			}
			return truncate("retry: "+msg, briefMaxLen)
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
			return truncate(name+" "+args, briefMaxLen)
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

// IsHighlighted reports whether the brief text for an event should be
// rendered with strong emphasis (e.g. user prompts, AI responses) rather
// than dimmed metadata.
func IsHighlighted(ev model.Event) bool {
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

type briefContext struct {
	payload map[string]any
	input   map[string]any
}

func newBriefContext(ev model.Event) (briefContext, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
		return briefContext{}, false
	}
	input := jsonutil.MapOrEmpty(payload["tool_input"])
	if len(input) == 0 {
		input = jsonutil.MapOrEmpty(payload["args"])
	}
	return briefContext{payload: payload, input: input}, true
}

func (c briefContext) payloadString(key string) string {
	return jsonutil.GetString(c.payload, key)
}

func (c briefContext) inputString(key string) string {
	return jsonutil.GetString(c.input, key)
}

func (c briefContext) openCodeOutput() string {
	return textutil.FirstNonEmpty(c.payloadString("title"), c.payloadString("output"))
}

func (c briefContext) filePath() string {
	return textutil.FirstNonEmpty(c.inputString("file_path"), c.inputString("filePath"))
}

func (c briefContext) editStrings() (string, string) {
	return textutil.FirstNonEmpty(c.inputString("old_string"), c.inputString("oldString")),
		textutil.FirstNonEmpty(c.inputString("new_string"), c.inputString("newString"))
}

func (c briefContext) patchText() string {
	patch := textutil.FirstNonEmpty(c.inputString("input"), c.inputString("patch"), c.inputString("patchText"))
	if patch == "" {
		if meta := jsonutil.MapOrEmpty(c.payload["metadata"]); len(meta) > 0 {
			patch = jsonutil.GetString(meta, "diff")
		}
	}
	return patch
}

func (c briefContext) bashOutput() string {
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

func (c briefContext) genericToolResponse() string {
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

func briefForPreToolUse(ev model.Event, ctx briefContext) string {
	switch ev.ToolName {
	case "Bash":
		return truncate(textutil.FirstLine(ctx.inputString("command")), briefMaxLen)
	case "Read":
		return ctx.filePath()
	case "Edit":
		filePath := ctx.filePath()
		oldStr, newStr := ctx.editStrings()
		stats := EditDiffStats(oldStr, newStr)
		if stats != "" {
			return truncate(filePath+" "+stats, briefMaxLen)
		}
		return filePath
	case "Write":
		filePath := ctx.filePath()
		if content := ctx.inputString("content"); content != "" {
			n := strings.Count(content, "\n") + 1
			return truncate(fmt.Sprintf("%s (+%d)", filePath, n), briefMaxLen)
		}
		return filePath
	case "apply_patch":
		return truncate(PatchDiffStats(ctx.patchText()), briefMaxLen)
	case "Grep":
		s := ctx.inputString("pattern")
		if path := ctx.inputString("path"); path != "" {
			s += " in " + path
		}
		return truncate(s, briefMaxLen)
	case "Glob":
		return ctx.inputString("pattern")
	case "Agent":
		desc := ctx.inputString("description")
		if t := ctx.inputString("subagent_type"); t != "" {
			desc = "[" + t + "] " + desc
		}
		return truncate(desc, briefMaxLen)
	default:
		return truncate(textutil.FirstLine(ctx.inputString("description")), briefMaxLen)
	}
}

func briefForPostToolUse(ev model.Event, ctx briefContext) string {
	switch ev.ToolName {
	case "Bash":
		return truncate(textutil.FirstLine(ctx.bashOutput()), briefMaxLen)
	case "Read":
		return truncate(textutil.FirstNonEmpty(ctx.filePath(), ctx.openCodeOutput()), briefMaxLen)
	case "Edit":
		filePath := textutil.FirstNonEmpty(ctx.filePath(), ctx.openCodeOutput())
		oldStr, newStr := ctx.editStrings()
		stats := EditDiffStats(oldStr, newStr)
		if stats == "" {
			if meta := jsonutil.MapOrEmpty(ctx.payload["metadata"]); len(meta) > 0 {
				stats = PatchDiffStats(jsonutil.GetString(meta, "diff"))
			}
		}
		if stats != "" {
			return truncate(filePath+" "+stats, briefMaxLen)
		}
		return truncate(filePath, briefMaxLen)
	case "Write":
		filePath := textutil.FirstNonEmpty(ctx.filePath(), ctx.openCodeOutput())
		if content := ctx.inputString("content"); content != "" {
			n := strings.Count(content, "\n") + 1
			return truncate(fmt.Sprintf("%s (+%d)", filePath, n), briefMaxLen)
		}
		return truncate(filePath, briefMaxLen)
	case "apply_patch":
		return truncate(PatchDiffStats(ctx.patchText()), briefMaxLen)
	default:
		return truncate(textutil.FirstLine(ctx.genericToolResponse()), briefMaxLen)
	}
}

func briefForPartUpdated(ctx briefContext) string {
	partType := ctx.payloadString("part_type")
	switch partType {
	case "text":
		return truncate(textutil.FirstLine(ctx.payloadString("text")), briefMaxLen)
	case "reasoning":
		return truncate("reasoning: "+textutil.FirstLine(ctx.payloadString("text")), briefMaxLen)
	case "tool":
		name := ctx.payloadString("tool_name")
		status := ctx.payloadString("tool_status")
		title := ctx.payloadString("tool_title")
		s := name + " [" + status + "]"
		if title != "" {
			s += " " + title
		}
		return truncate(s, briefMaxLen)
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

// EditDiffStats computes "(+N -M)" stats from old/new string pairs using
// Myers diff. Returns "" when there are no changes.
func EditDiffStats(oldStr, newStr string) string {
	if oldStr == "" && newStr == "" {
		return ""
	}
	oldLines := splitLines(oldStr)
	newLines := splitLines(newStr)
	script := diff.Compute(oldLines, newLines)
	s := diff.Count(script)
	if s.Additions == 0 && s.Deletions == 0 {
		return ""
	}
	return fmt.Sprintf("(+%d -%d)", s.Additions, s.Deletions)
}

// PatchDiffStats counts +/- lines in a unified patch or Codex apply_patch
// format. Skips diff metadata lines (---, +++, *** headers) so counts are
// not inflated by header rows.
func PatchDiffStats(patch string) string {
	if patch == "" {
		return ""
	}
	var adds, dels int
	for _, line := range splitLines(patch) {
		if len(line) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+++"),
			strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "***"):
			// header lines, skip
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

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "…"
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
