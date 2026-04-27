package eventview

import (
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func TestBrief_UserPromptSubmitUsesFirstLine(t *testing.T) {
	ev := model.Event{
		Subtype: "UserPromptSubmit",
		Payload: `{"prompt":"first line\nsecond line"}`,
	}
	if got := Brief(ev); got != "first line" {
		t.Fatalf("Brief() = %q, want %q", got, "first line")
	}
}

func TestBrief_PreToolUseEditUsesDiffStats(t *testing.T) {
	ev := model.Event{
		Subtype:  "PreToolUse",
		ToolName: "Edit",
		Payload:  `{"tool_input":{"file_path":"main.go","old_string":"before","new_string":"after"}}`,
	}
	if got := Brief(ev); got != "main.go (+1 -1)" {
		t.Fatalf("Brief() = %q, want %q", got, "main.go (+1 -1)")
	}
}

func TestBrief_PreToolUseApplyPatchSkipsPatchHeaders(t *testing.T) {
	ev := model.Event{
		Subtype:  "PreToolUse",
		ToolName: "apply_patch",
		Payload:  `{"tool_input":{"patchText":"*** Update File: main.go\n--- a/main.go\n+++ b/main.go\n-old\n+new"}}`,
	}
	if got := Brief(ev); got != "(+1 -1)" {
		t.Fatalf("Brief() = %q, want %q", got, "(+1 -1)")
	}
}

func TestBrief_PostToolUseEditUsesMetadataDiffFallback(t *testing.T) {
	ev := model.Event{
		Subtype:  "PostToolUse",
		ToolName: "Edit",
		Payload:  `{"args":{"file_path":"main.go"},"metadata":{"diff":"--- a/main.go\n+++ b/main.go\n-old\n+new"}}`,
	}
	if got := Brief(ev); got != "main.go (+1 -1)" {
		t.Fatalf("Brief() = %q, want %q", got, "main.go (+1 -1)")
	}
}

func TestBrief_PostToolUseBashPrefersStdout(t *testing.T) {
	ev := model.Event{
		Subtype:  "PostToolUse",
		ToolName: "Bash",
		Payload:  `{"tool_response":{"stdout":"ok line\nnext","stderr":"bad line"},"output":"fallback"}`,
	}
	if got := Brief(ev); got != "ok line" {
		t.Fatalf("Brief() = %q, want %q", got, "ok line")
	}
}

func TestBrief_SessionDiffSummary(t *testing.T) {
	ev := model.Event{
		Subtype: "SessionDiff",
		Payload: `{"diff_file_count":"3","diff_additions":"42","diff_deletions":"10"}`,
	}
	if got := Brief(ev); got != "3 files (+42 -10)" {
		t.Fatalf("Brief() = %q, want %q", got, "3 files (+42 -10)")
	}
}

func TestBrief_SessionStatusRetry(t *testing.T) {
	ev := model.Event{
		Subtype: "SessionStatus",
		Payload: `{"status_type":"retry","retry_attempt":"2","retry_message":"temporary failure"}`,
	}
	if got := Brief(ev); got != "retry #2: temporary failure" {
		t.Fatalf("Brief() = %q, want %q", got, "retry #2: temporary failure")
	}
}

func TestBrief_PartUpdatedBranches(t *testing.T) {
	tests := []struct {
		name string
		ev   model.Event
		want string
	}{
		{
			name: "reasoning",
			ev:   model.Event{Subtype: "PartUpdated", Payload: `{"part_type":"reasoning","text":"thinking out loud\nnext"}`},
			want: "reasoning: thinking out loud",
		},
		{
			name: "tool",
			ev:   model.Event{Subtype: "PartUpdated", Payload: `{"part_type":"tool","tool_name":"Read","tool_status":"running","tool_title":"Inspect file"}`},
			want: "Read [running] Inspect file",
		},
		{
			name: "step finish",
			ev:   model.Event{Subtype: "PartUpdated", Payload: `{"part_type":"step-finish","tokens_input":"12","tokens_output":"34"}`},
			want: "step done (in:12 out:34)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Brief(tt.ev); got != tt.want {
				t.Fatalf("Brief() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsHighlighted(t *testing.T) {
	tests := []struct {
		name string
		ev   model.Event
		want bool
	}{
		{name: "user prompt", ev: model.Event{Subtype: "UserPromptSubmit"}, want: true},
		{name: "stop", ev: model.Event{Subtype: "Stop"}, want: true},
		{name: "part updated text", ev: model.Event{Subtype: "PartUpdated", Payload: `{"part_type":"text","text":"hello"}`}, want: true},
		{name: "part updated tool", ev: model.Event{Subtype: "PartUpdated", Payload: `{"part_type":"tool","tool_name":"Read"}`}, want: false},
		{name: "notification", ev: model.Event{Subtype: "Notification", Payload: `{"message":"notice"}`}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsHighlighted(tt.ev); got != tt.want {
				t.Fatalf("IsHighlighted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEditDiffStats(t *testing.T) {
	if got := EditDiffStats("same", "same"); got != "" {
		t.Fatalf("EditDiffStats() = %q, want empty", got)
	}
	if got := EditDiffStats("before", "after"); got != "(+1 -1)" {
		t.Fatalf("EditDiffStats() = %q, want %q", got, "(+1 -1)")
	}
}

func TestPatchDiffStats(t *testing.T) {
	patch := "*** Update File: main.go\n--- a/main.go\n+++ b/main.go\n-old\n+new"
	if got := PatchDiffStats(patch); got != "(+1 -1)" {
		t.Fatalf("PatchDiffStats() = %q, want %q", got, "(+1 -1)")
	}
	if got := PatchDiffStats("*** Update File: main.go"); got != "patch" {
		t.Fatalf("PatchDiffStats() = %q, want %q", got, "patch")
	}
}
