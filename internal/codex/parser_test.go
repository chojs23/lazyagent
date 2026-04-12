package codex

import (
	"testing"
)

func TestParseSessionStart(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "cdx-sess-1",
		"cwd":             "/home/user/project",
		"model":           "o3",
		"permission_mode": "default",
		"transcript_path": "/tmp/codex/transcript.jsonl",
		"source":          "startup",
	}
	p := ParseRawEvent(raw)
	if p.Type != "session" || p.Subtype != "SessionStart" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.SessionID != "cdx-sess-1" {
		t.Fatalf("got sessionID=%q", p.SessionID)
	}
	if p.TranscriptPath != "/tmp/codex/transcript.jsonl" {
		t.Fatalf("got transcriptPath=%q", p.TranscriptPath)
	}
	if p.Metadata["model"] != "o3" {
		t.Fatalf("got model=%v", p.Metadata["model"])
	}
	if p.Metadata["source"] != "startup" {
		t.Fatalf("got source=%v", p.Metadata["source"])
	}
	if p.Metadata["cwd"] != "/home/user/project" {
		t.Fatalf("got cwd=%v", p.Metadata["cwd"])
	}
}

func TestParsePreToolUse(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "cdx-sess-1",
		"cwd":             "/home/user/project",
		"model":           "o3",
		"turn_id":         "turn-1",
		"tool_name":       "Bash",
		"tool_use_id":     "tu-1",
		"tool_input":      map[string]any{"command": "ls -la"},
	}
	p := ParseRawEvent(raw)
	if p.Type != "tool" || p.Subtype != "PreToolUse" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.ToolName != "Bash" {
		t.Fatalf("got toolName=%q", p.ToolName)
	}
	if p.ToolUseID != "tu-1" {
		t.Fatalf("got toolUseID=%q", p.ToolUseID)
	}
	if p.Metadata["turn_id"] != "turn-1" {
		t.Fatalf("got turn_id=%v", p.Metadata["turn_id"])
	}
	if p.Metadata["command"] != "ls -la" {
		t.Fatalf("got command=%v", p.Metadata["command"])
	}
}

func TestParsePostToolUse(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      "cdx-sess-1",
		"cwd":             "/home/user/project",
		"model":           "o3",
		"turn_id":         "turn-1",
		"tool_name":       "Bash",
		"tool_use_id":     "tu-1",
		"tool_input":      map[string]any{"command": "ls -la"},
		"tool_response":   "file1.txt\nfile2.txt",
	}
	p := ParseRawEvent(raw)
	if p.Type != "tool" || p.Subtype != "PostToolUse" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.ToolName != "Bash" {
		t.Fatalf("got toolName=%q", p.ToolName)
	}
	if p.Metadata["command"] != "ls -la" {
		t.Fatalf("got command=%v", p.Metadata["command"])
	}
}

func TestParseUserPromptSubmit(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "cdx-sess-1",
		"cwd":             "/home/user/project",
		"model":           "o3",
		"turn_id":         "turn-2",
		"prompt":          "fix the build error",
	}
	p := ParseRawEvent(raw)
	if p.Type != "user" || p.Subtype != "UserPromptSubmit" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.Metadata["prompt"] != "fix the build error" {
		t.Fatalf("got prompt=%v", p.Metadata["prompt"])
	}
	if p.Metadata["turn_id"] != "turn-2" {
		t.Fatalf("got turn_id=%v", p.Metadata["turn_id"])
	}
}

func TestParseStop(t *testing.T) {
	raw := map[string]any{
		"hook_event_name":        "Stop",
		"session_id":             "cdx-sess-1",
		"cwd":                    "/home/user/project",
		"model":                  "o3",
		"turn_id":                "turn-2",
		"last_assistant_message": "Done. The build error is fixed.",
	}
	p := ParseRawEvent(raw)
	if p.Type != "system" || p.Subtype != "Stop" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
	if p.Metadata["last_assistant_message"] != "Done. The build error is fixed." {
		t.Fatalf("got last_assistant_message=%v", p.Metadata["last_assistant_message"])
	}
}

func TestParseMissingSessionID(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "SessionStart",
		"cwd":             "/home/user/project",
	}
	p := ParseRawEvent(raw)
	if p.SessionID != "unknown" {
		t.Fatalf("expected unknown sessionID, got %q", p.SessionID)
	}
}

func TestParseUnknownEvent(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "FutureEvent",
		"session_id":      "cdx-sess-1",
	}
	p := ParseRawEvent(raw)
	if p.Type != "system" || p.Subtype != "FutureEvent" {
		t.Fatalf("got type=%q subtype=%q", p.Type, p.Subtype)
	}
}

func TestParseTimestampFloat(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "cdx-sess-1",
		"timestamp":       float64(1712700000000),
	}
	p := ParseRawEvent(raw)
	if p.Timestamp != 1712700000000 {
		t.Fatalf("got timestamp=%d", p.Timestamp)
	}
}

func TestParseNoToolInputCommand(t *testing.T) {
	raw := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "cdx-sess-1",
		"tool_name":       "Bash",
		"tool_use_id":     "tu-1",
	}
	p := ParseRawEvent(raw)
	if _, ok := p.Metadata["command"]; ok {
		t.Fatalf("expected no command in metadata, got %v", p.Metadata["command"])
	}
}
