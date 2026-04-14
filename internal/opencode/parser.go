package opencode

import (
	"regexp"
	"strings"
	"time"

	"github.com/chojs23/lazyagent/internal/model"
)

var subagentRe = regexp.MustCompile(`\(@([\w-]+)(?:\s+subagent)?\)`)

// ParseRawEvent converts an OpenCode plugin event payload into a normalized ParsedEvent.
// Expected payload fields from the TypeScript plugin:
//
//	{
//	  "event": "tool.execute.before" | "tool.execute.after" | "session.created" | "session.idle" | ...,
//	  "session_id": "...",
//	  "tool": "...",
//	  "call_id": "...",
//	  "args": { ... },
//	  "output": "...",
//	  "title": "...",
//	  "project_dir": "...",
//	  "agent_id": "...",
//	  "parent_session_id": "..."
//	}
func ParseRawEvent(raw map[string]any) model.ParsedEvent {
	p := model.ParsedEvent{
		SessionID:      firstNonEmpty(str(raw["session_id"]), "unknown"),
		TranscriptPath: str(raw["project_dir"]),
		ToolName:       normalizeToolName(str(raw["tool"])),
		ToolUseID:      str(raw["call_id"]),
		OwnerAgentID:   firstNonEmpty(str(raw["agent_id"]), str(raw["session_id"])),
		Metadata:       map[string]any{},
		Raw:            raw,
	}

	if ts := raw["timestamp"]; ts != nil {
		p.Timestamp = parseTimestamp(ts)
	} else {
		p.Timestamp = time.Now().UnixMilli()
	}

	event := str(raw["event"])
	switch event {
	case "tool.execute.before":
		p.Type = "tool"
		p.Subtype = "PreToolUse"
	case "tool.execute.after":
		p.Type = "tool"
		p.Subtype = "PostToolUse"
	case "session.created":
		p.Type = "session"
		p.Subtype = "SessionStart"
	case "session.updated":
		p.Type = "session"
		p.Subtype = "SessionUpdated"
	case "session.idle":
		p.Type = "system"
		p.Subtype = "Stop"
	case "session.deleted":
		p.Type = "session"
		p.Subtype = "SessionEnd"
	case "session.error":
		p.Type = "system"
		p.Subtype = "StopFailure"
	case "session.status":
		p.Type = "system"
		p.Subtype = "SessionStatus"
	case "session.diff":
		p.Type = "session"
		p.Subtype = "SessionDiff"
	case "session.compacted":
		p.Type = "system"
		p.Subtype = "Notification"
	case "permission.asked":
		p.Type = "system"
		p.Subtype = "Notification"
	case "permission.replied":
		p.Type = "system"
		p.Subtype = "PermissionReply"
	case "todo.updated":
		p.Type = "system"
		p.Subtype = "TodoUpdate"
	case "command.executed":
		p.Type = "system"
		p.Subtype = "CommandExecuted"
	case "file.edited":
		p.Type = "system"
		p.Subtype = "FileEdited"
	case "message.updated":
		p.Type = "message"
		p.Subtype = "MessageUpdated"
	case "message.removed":
		p.Type = "message"
		p.Subtype = "MessageRemoved"
	case "message.part.updated":
		p.Type = "message"
		p.Subtype = "PartUpdated"
	case "message.part.removed":
		p.Type = "message"
		p.Subtype = "PartRemoved"
	default:
		p.Type = "system"
		p.Subtype = event
	}

	// extract project slug from project_dir
	if dir := str(raw["project_dir"]); dir != "" {
		p.ProjectName = dir
	}

	// child session: extract subagent info from title
	if parent := str(raw["parent_session_id"]); parent != "" {
		p.Metadata["parent_session_id"] = parent
		p.SubAgentID = p.SessionID
		if title := str(raw["title"]); title != "" {
			if m := subagentRe.FindStringSubmatch(title); m != nil {
				p.SubAgentName = m[1]
				p.SubAgentDescription = strings.TrimSpace(title[:strings.Index(title, m[0])])
			} else {
				p.SubAgentName = title
			}
		}
	}

	for _, k := range []string{"cwd", "project_dir"} {
		if v, ok := raw[k]; ok {
			p.Metadata[k] = v
		}
	}

	// Propagate event-specific fields into metadata so downstream consumers
	// (ingest, TUI) can access them without re-parsing the raw payload.
	switch event {
	case "session.status":
		for _, k := range []string{"status_type", "retry_attempt", "retry_message", "retry_next"} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "session.diff":
		for _, k := range []string{"diff_file_count", "diff_additions", "diff_deletions", "diff_files"} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "session.error":
		for _, k := range []string{"error_type", "error_message"} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "permission.asked":
		for _, k := range []string{"permission", "patterns"} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "permission.replied":
		if v, ok := raw["reply"]; ok {
			p.Metadata["reply"] = v
		}
	case "todo.updated":
		for _, k := range []string{"todo_count", "todos"} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "command.executed":
		for _, k := range []string{"command_name", "command_args"} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "file.edited":
		if v, ok := raw["file"]; ok {
			p.Metadata["file"] = v
		}
	case "message.updated":
		for _, k := range []string{
			"message_role", "message_id", "model_id", "agent_name",
			"cost", "finish_reason",
			"tokens_input", "tokens_output", "tokens_reasoning",
			"tokens_cache_read", "tokens_cache_write",
			"error_name", "error_message",
		} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "message.removed":
		for _, k := range []string{"message_role", "message_id", "message_data"} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	case "message.part.updated", "message.part.removed":
		for _, k := range []string{
			"part_type", "part_id", "message_id",
			"text", "tool_name", "call_id", "tool_status", "tool_title", "tool_error",
			"finish_reason", "cost",
			"tokens_input", "tokens_output", "tokens_reasoning",
			"tokens_cache_read", "tokens_cache_write",
			"part_data",
		} {
			if v, ok := raw[k]; ok {
				p.Metadata[k] = v
			}
		}
	}

	return p
}

// normalizeToolName maps OpenCode's lowercase tool names to the PascalCase
// convention used by Claude, so the TUI can use a single set of switch cases.
func normalizeToolName(name string) string {
	switch name {
	case "bash":
		return "Bash"
	case "read":
		return "Read"
	case "edit":
		return "Edit"
	case "write":
		return "Write"
	case "grep":
		return "Grep"
	case "glob":
		return "Glob"
	case "agent":
		return "Agent"
	default:
		return name
	}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseTimestamp(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed.UnixMilli()
		}
	}
	return time.Now().UnixMilli()
}
