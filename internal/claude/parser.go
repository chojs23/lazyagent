package claude

import (
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/textutil"
)

func ParseRawEvent(raw map[string]any) model.ParsedEvent {
	p := model.ParsedEvent{
		ProjectName:    jsonutil.String(raw["project_name"]),
		SessionID:      textutil.FirstNonEmpty(jsonutil.String(raw["session_id"]), jsonutil.String(raw["sessionId"]), "unknown"),
		Slug:           jsonutil.String(raw["slug"]),
		TranscriptPath: jsonutil.String(raw["transcript_path"]),
		ToolUseID:      jsonutil.String(raw["tool_use_id"]),
		OwnerAgentID:   jsonutil.String(raw["agent_id"]),
		Metadata:       map[string]any{},
		Raw:            raw,
	}

	meta := jsonutil.Map(raw["meta"])
	p.Timestamp = jsonutil.TimestampMillis(jsonutil.FirstPresent(meta["timestamp"], raw["timestamp"]))

	if hookName := jsonutil.String(raw["hook_event_name"]); hookName != "" {
		parseHookEvent(&p, raw, hookName)
	} else {
		parseTranscriptEvent(&p, raw)
	}

	for _, k := range []string{"version", "gitBranch", "cwd", "entrypoint", "permissionMode", "userType", "permission_mode"} {
		if v, ok := raw[k]; ok {
			p.Metadata[k] = v
		}
	}
	if prompt := jsonutil.String(raw["prompt"]); prompt != "" {
		p.Metadata["prompt"] = prompt
	}

	return p
}

func parseHookEvent(p *model.ParsedEvent, raw map[string]any, hookName string) {
	toolName := jsonutil.String(raw["tool_name"])
	toolInput := jsonutil.Map(raw["tool_input"])
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
			p.SubAgentName = jsonutil.String(toolInput["name"])
			p.SubAgentDescription = jsonutil.String(toolInput["description"])
		}
	case "PostToolUse":
		p.Type, p.Subtype = "tool", "PostToolUse"
		if toolName == "Agent" {
			toolResp := jsonutil.Map(raw["tool_response"])
			p.SubAgentID = jsonutil.String(toolResp["agentId"])
			p.SubAgentName = jsonutil.String(toolInput["name"])
			p.SubAgentDescription = jsonutil.String(toolInput["description"])
		}
	case "PostToolUseFailure":
		p.Type, p.Subtype = "tool", "PostToolUseFailure"
	case "SubagentStop":
		p.Type, p.Subtype = "system", "SubagentStop"
		p.SubAgentID = jsonutil.String(raw["agent_id"])
	case "Stop":
		p.Type, p.Subtype = "system", "Stop"
	case "Notification":
		p.Type, p.Subtype = "system", "Notification"
	default:
		p.Type, p.Subtype = "system", hookName
	}
}

func parseTranscriptEvent(p *model.ParsedEvent, raw map[string]any) {
	p.Type = textutil.FirstNonEmpty(jsonutil.String(raw["type"]), "unknown")
	p.Subtype = jsonutil.String(raw["subtype"])

	data := jsonutil.Map(raw["data"])
	message := jsonutil.Map(raw["message"])
	toolUseResult := jsonutil.Map(raw["toolUseResult"])

	if p.Type == "progress" && len(data) > 0 {
		switch jsonutil.String(data["type"]) {
		case "hook_progress":
			p.Subtype = jsonutil.String(data["hookEvent"])
			if hookName := jsonutil.String(data["hookName"]); hookName != "" {
				p.ToolName = afterColon(hookName)
			}
		case "agent_progress":
			p.Subtype = "agent_progress"
			p.SubAgentID = jsonutil.String(data["agentId"])
			nested := jsonutil.Map(data["message"])
			inner := jsonutil.Map(nested["message"])
			if tu := findToolUse(inner["content"]); tu != nil {
				p.ToolName = jsonutil.String(tu["name"])
			}
		}
	}

	if p.Type == "assistant" && len(message) > 0 {
		if tu := findToolUse(message["content"]); tu != nil {
			p.ToolName = jsonutil.String(tu["name"])
			if p.ToolName == "Agent" {
				input := jsonutil.Map(tu["input"])
				p.SubAgentName = jsonutil.String(input["name"])
				p.SubAgentDescription = jsonutil.String(input["description"])
			}
		}
	}

	if len(toolUseResult) > 0 {
		p.SubAgentID = textutil.FirstNonEmpty(p.SubAgentID, jsonutil.String(toolUseResult["agentId"]))
	}
}

func findToolUse(content any) map[string]any {
	items, ok := content.([]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		entry := jsonutil.Map(item)
		if jsonutil.String(entry["type"]) == "tool_use" {
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
