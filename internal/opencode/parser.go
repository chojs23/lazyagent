package opencode

import (
	"regexp"
	"strings"

	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/textutil"
)

var subagentRe = regexp.MustCompile(`\(@([\w-]+)(?:\s+subagent)?\)`)

type eventSpec struct {
	typ          string
	subtype      string
	metadataKeys []string
}

var eventSpecs = map[string]eventSpec{
	"tool.execute.before": {typ: "tool", subtype: "PreToolUse"},
	"tool.execute.after":  {typ: "tool", subtype: "PostToolUse"},
	"session.created":     {typ: "session", subtype: "SessionStart"},
	"session.updated":     {typ: "session", subtype: "SessionUpdated"},
	"session.idle":        {typ: "system", subtype: "Stop"},
	"session.deleted":     {typ: "session", subtype: "SessionEnd"},
	"session.error": {
		typ:          "system",
		subtype:      "StopFailure",
		metadataKeys: []string{"error_type", "error_message"},
	},
	"session.status": {
		typ:          "system",
		subtype:      "SessionStatus",
		metadataKeys: []string{"status_type", "retry_attempt", "retry_message", "retry_next"},
	},
	"session.diff": {
		typ:          "session",
		subtype:      "SessionDiff",
		metadataKeys: []string{"diff_file_count", "diff_additions", "diff_deletions", "diff_files"},
	},
	"session.compacted": {typ: "system", subtype: "Notification"},
	"permission.asked": {
		typ:          "system",
		subtype:      "Notification",
		metadataKeys: []string{"permission", "patterns"},
	},
	"permission.replied": {
		typ:          "system",
		subtype:      "PermissionReply",
		metadataKeys: []string{"reply"},
	},
	"todo.updated": {
		typ:          "system",
		subtype:      "TodoUpdate",
		metadataKeys: []string{"todo_count", "todos"},
	},
	"command.executed": {
		typ:          "system",
		subtype:      "CommandExecuted",
		metadataKeys: []string{"command_name", "command_args"},
	},
	"file.edited": {
		typ:          "system",
		subtype:      "FileEdited",
		metadataKeys: []string{"file"},
	},
	"message.updated": {
		typ:     "message",
		subtype: "MessageUpdated",
		metadataKeys: []string{
			"message_role", "message_id", "model_id", "agent_name",
			"cost", "finish_reason",
			"tokens_input", "tokens_output", "tokens_reasoning",
			"tokens_cache_read", "tokens_cache_write",
			"error_name", "error_message",
		},
	},
	"message.removed": {
		typ:          "message",
		subtype:      "MessageRemoved",
		metadataKeys: []string{"message_role", "message_id", "message_data"},
	},
	"message.part.updated": {
		typ:     "message",
		subtype: "PartUpdated",
		metadataKeys: []string{
			"part_type", "part_id", "message_id",
			"text", "tool_name", "call_id", "tool_status", "tool_title", "tool_error",
			"finish_reason", "cost",
			"tokens_input", "tokens_output", "tokens_reasoning",
			"tokens_cache_read", "tokens_cache_write",
			"part_data",
		},
	},
	"message.part.removed": {
		typ:     "message",
		subtype: "PartRemoved",
		metadataKeys: []string{
			"part_type", "part_id", "message_id",
			"text", "tool_name", "call_id", "tool_status", "tool_title", "tool_error",
			"finish_reason", "cost",
			"tokens_input", "tokens_output", "tokens_reasoning",
			"tokens_cache_read", "tokens_cache_write",
			"part_data",
		},
	},
}

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
		SessionID:      textutil.FirstNonEmpty(jsonutil.String(raw["session_id"]), "unknown"),
		TranscriptPath: jsonutil.String(raw["project_dir"]),
		ToolName:       normalizeToolName(jsonutil.String(raw["tool"])),
		ToolUseID:      jsonutil.String(raw["call_id"]),
		OwnerAgentID:   textutil.FirstNonEmpty(jsonutil.String(raw["agent_id"]), jsonutil.String(raw["session_id"])),
		Metadata:       map[string]any{},
		Raw:            raw,
	}

	p.Timestamp = jsonutil.TimestampMillis(raw["timestamp"])

	event := jsonutil.String(raw["event"])
	spec := eventSpecFor(event)
	p.Type = spec.typ
	p.Subtype = spec.subtype

	// extract project slug from project_dir
	if dir := jsonutil.String(raw["project_dir"]); dir != "" {
		p.ProjectName = dir
	}

	// child session: extract subagent info from title
	if parent := jsonutil.String(raw["parent_session_id"]); parent != "" {
		p.Metadata["parent_session_id"] = parent
		p.SubAgentID = p.SessionID
		if title := jsonutil.String(raw["title"]); title != "" {
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
	copyMetadataFields(p.Metadata, raw, spec.metadataKeys)

	return p
}

func eventSpecFor(event string) eventSpec {
	if spec, ok := eventSpecs[event]; ok {
		return spec
	}
	return eventSpec{typ: "system", subtype: event}
}

func copyMetadataFields(dst, raw map[string]any, keys []string) {
	for _, k := range keys {
		if v, ok := raw[k]; ok {
			dst[k] = v
		}
	}
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
