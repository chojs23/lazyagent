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

func TestParseRawEvent_SessionStatus(t *testing.T) {
	raw := map[string]any{
		"event":       "session.status",
		"session_id":  "sess-1",
		"status_type": "idle",
		"project_dir": "/home/user/project",
	}
	p := ParseRawEvent(raw)

	if p.Type != "system" {
		t.Fatalf("Type = %q, want system", p.Type)
	}
	if p.Subtype != "SessionStatus" {
		t.Fatalf("Subtype = %q, want SessionStatus", p.Subtype)
	}
	if p.Metadata["status_type"] != "idle" {
		t.Fatalf("status_type = %v, want idle", p.Metadata["status_type"])
	}
}

func TestParseRawEvent_SessionStatusRetry(t *testing.T) {
	raw := map[string]any{
		"event":         "session.status",
		"session_id":    "sess-1",
		"status_type":   "retry",
		"retry_attempt": float64(2),
		"retry_message": "rate limited",
		"retry_next":    float64(1712700030000),
		"project_dir":   "/home/user/project",
	}
	p := ParseRawEvent(raw)

	if p.Metadata["status_type"] != "retry" {
		t.Fatalf("status_type = %v, want retry", p.Metadata["status_type"])
	}
	if p.Metadata["retry_attempt"] != float64(2) {
		t.Fatalf("retry_attempt = %v, want 2", p.Metadata["retry_attempt"])
	}
	if p.Metadata["retry_message"] != "rate limited" {
		t.Fatalf("retry_message = %v, want 'rate limited'", p.Metadata["retry_message"])
	}
}

func TestParseRawEvent_SessionDiff(t *testing.T) {
	raw := map[string]any{
		"event":           "session.diff",
		"session_id":      "sess-1",
		"diff_file_count": float64(3),
		"diff_additions":  float64(42),
		"diff_deletions":  float64(10),
		"project_dir":     "/home/user/project",
	}
	p := ParseRawEvent(raw)

	if p.Type != "session" {
		t.Fatalf("Type = %q, want session", p.Type)
	}
	if p.Subtype != "SessionDiff" {
		t.Fatalf("Subtype = %q, want SessionDiff", p.Subtype)
	}
	if p.Metadata["diff_file_count"] != float64(3) {
		t.Fatalf("diff_file_count = %v, want 3", p.Metadata["diff_file_count"])
	}
}

func TestParseRawEvent_NewEventTypes(t *testing.T) {
	cases := []struct {
		event   string
		wantTyp string
		wantSub string
	}{
		{"permission.replied", "system", "PermissionReply"},
		{"todo.updated", "system", "TodoUpdate"},
		{"command.executed", "system", "CommandExecuted"},
		{"file.edited", "system", "FileEdited"},
	}
	for _, tc := range cases {
		p := ParseRawEvent(map[string]any{
			"event":      tc.event,
			"session_id": "sess-1",
		})
		if p.Type != tc.wantTyp {
			t.Errorf("%s: Type = %q, want %q", tc.event, p.Type, tc.wantTyp)
		}
		if p.Subtype != tc.wantSub {
			t.Errorf("%s: Subtype = %q, want %q", tc.event, p.Subtype, tc.wantSub)
		}
	}
}

func TestParseRawEvent_NormalizeToolName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"bash", "Bash"},
		{"read", "Read"},
		{"edit", "Edit"},
		{"write", "Write"},
		{"grep", "Grep"},
		{"glob", "Glob"},
		{"agent", "Agent"},
		{"apply_patch", "apply_patch"},
	}
	for _, tc := range cases {
		p := ParseRawEvent(map[string]any{
			"event":      "tool.execute.before",
			"session_id": "sess-1",
			"tool":       tc.input,
		})
		if p.ToolName != tc.want {
			t.Errorf("normalizeToolName(%q) = %q, want %q", tc.input, p.ToolName, tc.want)
		}
	}
}
