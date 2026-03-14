package claude

import (
	"testing"
)

func TestParseHookSessionStart(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "sess-1",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}
	p := ParseRawEvent(raw)
	if p.Type != "session" || p.Subtype != "SessionStart" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.SessionID != "sess-1" {
		t.Fatalf("got sessionID=%q", p.SessionID)
	}
	if p.Timestamp != 1712700000000 {
		t.Fatalf("got timestamp=%d", p.Timestamp)
	}
}

func TestParseHookPreToolUseAgent(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-1",
		"tool_name":       "Agent",
		"tool_use_id":     "tu-1",
		"tool_input":      map[string]any{"name": "planner", "description": "Strategic planning"},
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	}
	p := ParseRawEvent(raw)
	if p.Type != "tool" || p.Subtype != "PreToolUse" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.ToolName != "Agent" {
		t.Fatalf("got toolName=%q", p.ToolName)
	}
	if p.SubAgentName != "planner" {
		t.Fatalf("got subAgentName=%q", p.SubAgentName)
	}
	if p.SubAgentDescription != "Strategic planning" {
		t.Fatalf("got subAgentDescription=%q", p.SubAgentDescription)
	}
}

func TestParseHookPostToolUseAgent(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      "sess-1",
		"tool_name":       "Agent",
		"tool_use_id":     "tu-1",
		"tool_input":      map[string]any{"name": "planner"},
		"tool_response":   map[string]any{"agentId": "agent-abc"},
		"meta":            map[string]any{"timestamp": float64(1712700002000)},
	}
	p := ParseRawEvent(raw)
	if p.SubAgentID != "agent-abc" {
		t.Fatalf("got subAgentID=%q", p.SubAgentID)
	}
}

func TestParseHookNonAgentTool(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-1",
		"tool_name":       "Bash",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}
	p := ParseRawEvent(raw)
	if p.ToolName != "Bash" {
		t.Fatalf("got toolName=%q", p.ToolName)
	}
	if p.SubAgentName != "" {
		t.Fatalf("got subAgentName=%q for non-Agent tool", p.SubAgentName)
	}
}

func TestParseHookSubagentStop(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "SubagentStop",
		"session_id":      "sess-1",
		"agent_id":        "agent-xyz",
		"meta":            map[string]any{"timestamp": float64(1712700003000)},
	}
	p := ParseRawEvent(raw)
	if p.Type != "system" || p.Subtype != "SubagentStop" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.SubAgentID != "agent-xyz" {
		t.Fatalf("got subAgentID=%q", p.SubAgentID)
	}
}

func TestParseSessionIDFallback(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "Stop",
		"sessionId":       "sess-fallback",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}
	p := ParseRawEvent(raw)
	if p.SessionID != "sess-fallback" {
		t.Fatalf("got sessionID=%q", p.SessionID)
	}
}

func TestParseMetadataExtraction(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "sess-1",
		"cwd":             "/home/user/project",
		"version":         "1.0.0",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	}
	p := ParseRawEvent(raw)
	if p.Metadata["cwd"] != "/home/user/project" {
		t.Fatalf("got cwd=%v", p.Metadata["cwd"])
	}
	if p.Metadata["version"] != "1.0.0" {
		t.Fatalf("got version=%v", p.Metadata["version"])
	}
}
