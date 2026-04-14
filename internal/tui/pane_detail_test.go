package tui

import (
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

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
