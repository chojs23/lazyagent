package opencode

import "testing"

func TestParseRawEvent_ChildSessionSubagentName(t *testing.T) {
	raw := map[string]any{
		"event":             "session.created",
		"session_id":        "child-123",
		"parent_session_id": "parent-456",
		"title":             "Find active session source (@subagent-name subagent)",
		"project_dir":       "/home/user/project",
	}
	p := ParseRawEvent(raw)

	if p.SubAgentID != "child-123" {
		t.Fatalf("SubAgentID = %q, want %q", p.SubAgentID, "child-123")
	}
	if p.SubAgentName != "subagent-name" {
		t.Fatalf("SubAgentName = %q, want %q", p.SubAgentName, "subagent-name")
	}
	if p.SubAgentDescription != "Find active session source" {
		t.Fatalf("SubAgentDescription = %q, want %q", p.SubAgentDescription, "Find active session source")
	}
}

func TestParseRawEvent_ChildSessionNoSubagentPattern(t *testing.T) {
	raw := map[string]any{
		"event":             "session.created",
		"session_id":        "child-789",
		"parent_session_id": "parent-456",
		"title":             "Some plain title",
		"project_dir":       "/home/user/project",
	}
	p := ParseRawEvent(raw)

	if p.SubAgentID != "child-789" {
		t.Fatalf("SubAgentID = %q, want %q", p.SubAgentID, "child-789")
	}
	if p.SubAgentName != "Some plain title" {
		t.Fatalf("SubAgentName = %q, want %q", p.SubAgentName, "Some plain title")
	}
}

func TestParseRawEvent_RootSessionNoSubagent(t *testing.T) {
	raw := map[string]any{
		"event":       "session.created",
		"session_id":  "root-001",
		"title":       "Main session",
		"project_dir": "/home/user/project",
	}
	p := ParseRawEvent(raw)

	if p.SubAgentID != "" {
		t.Fatalf("SubAgentID = %q, want empty", p.SubAgentID)
	}
	if p.SubAgentName != "" {
		t.Fatalf("SubAgentName = %q, want empty", p.SubAgentName)
	}
}
