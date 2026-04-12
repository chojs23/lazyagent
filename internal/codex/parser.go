package codex

import (
	"time"

	"github.com/chojs23/lazyagent/internal/model"
)

// ParseRawEvent converts a Codex hook stdin payload into a normalized ParsedEvent.
//
// Codex hooks deliver one JSON object per invocation with these common fields:
//
//	hook_event_name, session_id, cwd, model, permission_mode, transcript_path
//
// Event-specific fields vary by hook_event_name:
//
//	SessionStart:      source
//	PreToolUse:        turn_id, tool_name, tool_use_id, tool_input
//	PostToolUse:       turn_id, tool_name, tool_use_id, tool_input, tool_response
//	UserPromptSubmit:  turn_id, prompt
//	Stop:              turn_id, last_assistant_message
func ParseRawEvent(raw map[string]any) model.ParsedEvent {
	p := model.ParsedEvent{
		SessionID:      firstNonEmpty(str(raw["session_id"]), "unknown"),
		TranscriptPath: str(raw["transcript_path"]),
		ToolName:       str(raw["tool_name"]),
		ToolUseID:      str(raw["tool_use_id"]),
		Metadata:       map[string]any{},
		Raw:            raw,
	}

	if ts := raw["timestamp"]; ts != nil {
		p.Timestamp = parseTimestamp(ts)
	} else {
		p.Timestamp = time.Now().UnixMilli()
	}

	hookName := str(raw["hook_event_name"])
	switch hookName {
	case "SessionStart":
		p.Type, p.Subtype = "session", "SessionStart"
	case "PreToolUse":
		p.Type, p.Subtype = "tool", "PreToolUse"
	case "PostToolUse":
		p.Type, p.Subtype = "tool", "PostToolUse"
	case "UserPromptSubmit":
		p.Type, p.Subtype = "user", "UserPromptSubmit"
	case "Stop":
		p.Type, p.Subtype = "system", "Stop"
	default:
		p.Type, p.Subtype = "system", hookName
	}

	// Propagate Codex-specific fields into metadata so TUI and ingest
	// can access them without re-parsing the raw payload.
	for _, k := range []string{
		"cwd", "model", "permission_mode", "turn_id", "source",
	} {
		if v, ok := raw[k]; ok {
			p.Metadata[k] = v
		}
	}

	// Store prompt text for UserPromptSubmit events.
	if hookName == "UserPromptSubmit" {
		if prompt := str(raw["prompt"]); prompt != "" {
			p.Metadata["prompt"] = prompt
		}
	}

	// Store last assistant message for Stop events.
	if hookName == "Stop" {
		if msg := str(raw["last_assistant_message"]); msg != "" {
			p.Metadata["last_assistant_message"] = msg
		}
	}

	// Store tool_input command for Bash tool events.
	if hookName == "PreToolUse" || hookName == "PostToolUse" {
		if input := asMap(raw["tool_input"]); input != nil {
			if cmd := str(input["command"]); cmd != "" {
				p.Metadata["command"] = cmd
			}
		}
	}

	return p
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
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
	case int:
		return int64(t)
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed.UnixMilli()
		}
	}
	return time.Now().UnixMilli()
}
