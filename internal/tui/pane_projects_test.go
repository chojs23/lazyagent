package tui

import (
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func TestBuildProjectSessionLabelUsesTimeAndShortID(t *testing.T) {
	sess := model.Session{
		ID:         "session-abcdef1234567890",
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
	if !strings.Contains(label, shortID(sess.ID)) {
		t.Fatalf("label missing short id: %q", label)
	}
	if strings.Contains(label, "e:") || strings.Contains(label, "a:") {
		t.Fatalf("label should not contain counters: %q", label)
	}
}
