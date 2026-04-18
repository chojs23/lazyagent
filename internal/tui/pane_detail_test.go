package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func detailContent(d detailModel) string {
	return d.view(100, 24, false)
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func TestRenderToolDetail_SessionDiffSummaryOnly(t *testing.T) {
	detail := newDetail()
	ev := &model.Event{
		Subtype: "SessionDiff",
		Payload: `{"diff_file_count":3,"diff_additions":42,"diff_deletions":10}`,
	}

	got := detail.renderToolDetail(ev)

	for _, want := range []string{"Files Changed:", "3", "Additions:", "42", "Deletions:", "10"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderToolDetail() missing %q in %q", want, got)
		}
	}
}

func TestDetailToggleJSONPreservesSameEventAndResetsOnNewEvent(t *testing.T) {
	detail := newDetail()
	ev1 := &model.Event{
		ID:      1,
		AgentID: "agent-1",
		Type:    "user",
		Subtype: "UserPromptSubmit",
		Payload: `{"prompt":"first prompt"}`,
	}
	ev2 := &model.Event{
		ID:      2,
		AgentID: "agent-1",
		Type:    "user",
		Subtype: "UserPromptSubmit",
		Payload: `{"prompt":"second prompt"}`,
	}

	detail.setEvent(ev1, nil)
	if got := detailContent(detail); !strings.Contains(got, "[J to toggle raw JSON]") {
		t.Fatalf("initial detail view missing raw toggle hint: %q", got)
	}

	detail.toggleJSON()
	if got := detailContent(detail); !strings.Contains(got, "── Raw JSON ──") || !strings.Contains(got, `"first prompt"`) {
		t.Fatalf("detail view missing raw JSON after toggle: %q", got)
	}

	detail.setEvent(ev1, nil)
	if got := detailContent(detail); !strings.Contains(got, "── Raw JSON ──") {
		t.Fatalf("same event should preserve raw JSON toggle: %q", got)
	}

	detail.setEvent(ev2, nil)
	if got := detailContent(detail); strings.Contains(got, "── Raw JSON ──") || !strings.Contains(got, "[J to toggle raw JSON]") {
		t.Fatalf("new event should reset raw JSON toggle: %q", got)
	}
}

func TestDetailToggleExpandShowsFullWriteContent(t *testing.T) {
	detail := newDetail()
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "line " + string(rune('A'+i))
	}
	ev := &model.Event{
		ID:       1,
		AgentID:  "agent-1",
		Type:     "tool",
		Subtype:  "PostToolUse",
		ToolName: "Write",
		Payload:  `{"file_path":"notes.txt","content":"` + strings.Join(lines, `\n`) + `"}`,
	}

	detail.setEvent(ev, nil)
	got := stripANSI(detail.renderToolDetail(ev))
	if !strings.Contains(got, "e to expand") || strings.Contains(got, "line Y") {
		t.Fatalf("collapsed write detail should truncate content: %q", got)
	}

	detail.toggleExpand()
	got = stripANSI(detail.renderToolDetail(ev))
	if strings.Contains(got, "e to expand") || !strings.Contains(got, "line Y") {
		t.Fatalf("expanded write detail should show full content: %q", got)
	}
}

func TestRenderToolDetail_EditUsesMetadataDiffFallback(t *testing.T) {
	detail := newDetail()
	ev := &model.Event{
		Subtype:  "PostToolUse",
		ToolName: "Edit",
		Payload:  `{"file_path":"main.go","metadata":{"diff":"--- a/main.go\n+++ b/main.go\n-func old() {}\n+func new() {}"}}`,
	}

	got := stripANSI(detail.renderToolDetail(ev))

	for _, want := range []string{"File:", "main.go", "Diff:", "func old() {}", "func new() {}"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderToolDetail() missing %q in %q", want, got)
		}
	}
}

func TestRenderToolDetail_ApplyPatchUsesMetadataDiffFallback(t *testing.T) {
	detail := newDetail()
	ev := &model.Event{
		Subtype:  "PostToolUse",
		ToolName: "apply_patch",
		Payload:  `{"metadata":{"diff":"*** Update File: main.go\n@@\n-func old() {}\n+func new() {}"}}`,
	}

	got := stripANSI(detail.renderToolDetail(ev))

	for _, want := range []string{"Patch:", "Update File: main.go", "func old() {}", "func new() {}"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderToolDetail() missing %q in %q", want, got)
		}
	}
}

func TestRenderToolDetail_PartUpdatedToolShowsToolFields(t *testing.T) {
	detail := newDetail()
	ev := &model.Event{
		Subtype: "PartUpdated",
		Payload: `{"part_type":"tool","tool_name":"Read","call_id":"call-1","tool_status":"running","tool_title":"Inspect file","tool_error":""}`,
	}

	got := stripANSI(detail.renderToolDetail(ev))

	for _, want := range []string{"Part:", "tool", "Tool:", "Read", "Call ID:", "call-1", "Status:", "running", "Title:", "Inspect file"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderToolDetail() missing %q in %q", want, got)
		}
	}
}
