package codex

import (
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/textutil"
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
		SessionID:      textutil.FirstNonEmpty(jsonutil.String(raw["session_id"]), "unknown"),
		TranscriptPath: jsonutil.String(raw["transcript_path"]),
		ToolName:       jsonutil.String(raw["tool_name"]),
		ToolUseID:      jsonutil.String(raw["tool_use_id"]),
		Metadata:       map[string]any{},
		Raw:            raw,
	}

	p.Timestamp = jsonutil.TimestampMillis(raw["timestamp"])

	hookName := jsonutil.String(raw["hook_event_name"])
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
		if prompt := jsonutil.String(raw["prompt"]); prompt != "" {
			p.Metadata["prompt"] = prompt
		}
	}

	// Store last assistant message for Stop events.
	if hookName == "Stop" {
		if msg := jsonutil.String(raw["last_assistant_message"]); msg != "" {
			p.Metadata["last_assistant_message"] = msg
		}
	}

	// Store tool_input command for Bash tool events.
	if hookName == "PreToolUse" || hookName == "PostToolUse" {
		if input := jsonutil.Map(raw["tool_input"]); input != nil {
			if cmd := jsonutil.String(input["command"]); cmd != "" {
				p.Metadata["command"] = cmd
			}
		}
	}

	return p
}
