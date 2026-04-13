package claude

import (
	"time"

	"github.com/chojs23/lazyagent/internal/model"
)

func ParseRawEvent(raw map[string]any) model.ParsedEvent {
	p := model.ParsedEvent{
		ProjectName:    str(raw["project_name"]),
		SessionID:      firstNonEmpty(str(raw["session_id"]), str(raw["sessionId"]), "unknown"),
		Slug:           str(raw["slug"]),
		TranscriptPath: str(raw["transcript_path"]),
		ToolUseID:      str(raw["tool_use_id"]),
		OwnerAgentID:   str(raw["agent_id"]),
		Metadata:       map[string]any{},
		Raw:            raw,
	}

	meta := asMap(raw["meta"])
	p.Timestamp = parseTimestamp(firstPresent(meta["timestamp"], raw["timestamp"]))

	if hookName := str(raw["hook_event_name"]); hookName != "" {
		parseHookEvent(&p, raw, hookName)
	} else {
		parseTranscriptEvent(&p, raw)
	}

	for _, k := range []string{"version", "gitBranch", "cwd", "entrypoint", "permissionMode", "userType", "permission_mode"} {
		if v, ok := raw[k]; ok {
			p.Metadata[k] = v
		}
	}
	if prompt := str(raw["prompt"]); prompt != "" {
		p.Metadata["prompt"] = prompt
	}

	return p
}

func parseHookEvent(p *model.ParsedEvent, raw map[string]any, hookName string) {
	toolName := str(raw["tool_name"])
	toolInput := asMap(raw["tool_input"])
	p.ToolName = toolName

	switch hookName {
	case "SessionStart":
		p.Type, p.Subtype = "session", "SessionStart"
	case "SessionEnd":
		p.Type, p.Subtype = "session", "SessionEnd"
	case "UserPromptSubmit":
		p.Type, p.Subtype = "user", "UserPromptSubmit"
	case "PreToolUse":
		p.Type, p.Subtype = "tool", "PreToolUse"
		if toolName == "Agent" {
			p.SubAgentName = str(toolInput["name"])
			p.SubAgentDescription = str(toolInput["description"])
		}
	case "PostToolUse":
		p.Type, p.Subtype = "tool", "PostToolUse"
		if toolName == "Agent" {
			toolResp := asMap(raw["tool_response"])
			p.SubAgentID = str(toolResp["agentId"])
			p.SubAgentName = str(toolInput["name"])
			p.SubAgentDescription = str(toolInput["description"])
		}
	case "PostToolUseFailure":
		p.Type, p.Subtype = "tool", "PostToolUseFailure"
	case "SubagentStop":
		p.Type, p.Subtype = "system", "SubagentStop"
		p.SubAgentID = str(raw["agent_id"])
	case "Stop":
		p.Type, p.Subtype = "system", "Stop"
	case "Notification":
		p.Type, p.Subtype = "system", "Notification"
	default:
		p.Type, p.Subtype = "system", hookName
	}
}

func parseTranscriptEvent(p *model.ParsedEvent, raw map[string]any) {
	p.Type = firstNonEmpty(str(raw["type"]), "unknown")
	p.Subtype = str(raw["subtype"])

	data := asMap(raw["data"])
	message := asMap(raw["message"])
	toolUseResult := asMap(raw["toolUseResult"])

	if p.Type == "progress" && len(data) > 0 {
		switch str(data["type"]) {
		case "hook_progress":
			p.Subtype = str(data["hookEvent"])
			if hookName := str(data["hookName"]); hookName != "" {
				p.ToolName = afterColon(hookName)
			}
		case "agent_progress":
			p.Subtype = "agent_progress"
			p.SubAgentID = str(data["agentId"])
			nested := asMap(data["message"])
			inner := asMap(nested["message"])
			if tu := findToolUse(inner["content"]); tu != nil {
				p.ToolName = str(tu["name"])
			}
		}
	}

	if p.Type == "assistant" && len(message) > 0 {
		if tu := findToolUse(message["content"]); tu != nil {
			p.ToolName = str(tu["name"])
			if p.ToolName == "Agent" {
				input := asMap(tu["input"])
				p.SubAgentName = str(input["name"])
				p.SubAgentDescription = str(input["description"])
			}
		}
	}

	if len(toolUseResult) > 0 {
		p.SubAgentID = firstNonEmpty(p.SubAgentID, str(toolUseResult["agentId"]))
	}
}

func findToolUse(content any) map[string]any {
	items, ok := content.([]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		entry := asMap(item)
		if str(entry["type"]) == "tool_use" {
			return entry
		}
	}
	return nil
}

func afterColon(s string) string {
	for i, c := range s {
		if c == ':' {
			return s[i+1:]
		}
	}
	return ""
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

func firstPresent(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
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
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed.UnixMilli()
		}
	}
	return time.Now().UnixMilli()
}
