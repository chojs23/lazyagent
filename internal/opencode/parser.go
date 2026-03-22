package opencode

import (
	"time"

	"github.com/chojs23/lazyagent/internal/model"
)

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
		ToolName:       str(raw["tool"]),
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
	case "session.idle":
		p.Type = "system"
		p.Subtype = "Stop"
	case "session.deleted":
		p.Type = "session"
		p.Subtype = "SessionEnd"
	case "session.error":
		p.Type = "system"
		p.Subtype = "StopFailure"
	case "permission.asked":
		p.Type = "system"
		p.Subtype = "Notification"
	case "session.compacted":
		p.Type = "system"
		p.Subtype = "Notification"
	default:
		p.Type = "system"
		p.Subtype = event
	}

	// extract project slug from project_dir
	if dir := str(raw["project_dir"]); dir != "" {
		p.ProjectName = dir
	}

	// child session: keep as separate session, store parent reference
	if parent := str(raw["parent_session_id"]); parent != "" {
		p.Metadata["parent_session_id"] = parent
	}

	for _, k := range []string{"cwd", "project_dir"} {
		if v, ok := raw[k]; ok {
			p.Metadata[k] = v
		}
	}

	return p
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
