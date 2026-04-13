package tui

import (
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func TestBuildProjectSessionLabelUsesSlugWhenAvailable(t *testing.T) {
	sess := model.Session{
		ID:         "session-abcdef1234567890",
		Slug:       "fix broken filtered paging\nextra detail should stay hidden",
		Runtime:    "codex",
		StartedAt:  1712700000000,
		EventCount: 99,
		AgentCount: 7,
	}

	label := buildProjectSessionLabel("  ", "└─ ", sess)

	if !strings.Contains(label, formatTime(sess.StartedAt)) {
		t.Fatalf("label missing start time: %q", label)
	}
	if !strings.Contains(label, "[X]") {
		t.Fatalf("label missing runtime marker: %q", label)
	}
	if !strings.Contains(label, " - ") {
		t.Fatalf("label missing time-id separator: %q", label)
	}
	if !strings.Contains(label, "fix broken filtered paging") {
		t.Fatalf("label missing slug: %q", label)
	}
	if strings.Contains(label, "extra detail should stay hidden") {
		t.Fatalf("label should use only first slug line: %q", label)
	}
	if strings.Contains(label, shortID(sess.ID)) {
		t.Fatalf("label should prefer slug over short id: %q", label)
	}
	if strings.Contains(label, "e:") || strings.Contains(label, "a:") {
		t.Fatalf("label should not contain counters: %q", label)
	}
}

func TestBuildProjectSessionLabelFallsBackToShortIDWithoutSlug(t *testing.T) {
	sess := model.Session{
		ID:        "session-abcdef1234567890",
		Runtime:   "codex",
		StartedAt: 1712700000000,
	}

	label := buildProjectSessionLabel("  ", "└─ ", sess)

	if !strings.Contains(label, shortID(sess.ID)) {
		t.Fatalf("label missing short id fallback: %q", label)
	}
}
