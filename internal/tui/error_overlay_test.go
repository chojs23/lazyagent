package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chojs23/lazyagent/internal/applog"
)

func TestReportErrorShowsOverlayWithoutStealingFocus(t *testing.T) {
	tmp := t.TempDir()
	applog.SetDefault(applog.NewForPath(filepath.Join(tmp, "lazyagent.log")))
	t.Cleanup(func() {
		applog.SetDefault(nil)
	})

	m := newModel(nil, time.Second)
	m.width = 120
	m.height = 32

	m.reportError("Refresh failed", errors.New("database is locked"))

	if !m.errorOverlay.visible {
		t.Fatal("error overlay should be visible")
	}
	if m.status != "Refresh failed: database is locked" {
		t.Fatalf("status = %q", m.status)
	}

	view := m.View().Content
	for _, want := range []string{
		"Refresh failed",
		"database is locked",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in %q", want, view)
		}
	}

	updated, _ := m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	next := updated.(Model)
	if next.focus != focusAgents {
		t.Fatalf("focus = %v, want %v", next.focus, focusAgents)
	}
	if !next.errorOverlay.visible {
		t.Fatal("error overlay should stay visible while focus moves")
	}
}

func TestErrorOverlayAutoHidesAfterTTL(t *testing.T) {
	m := newModel(nil, time.Second)
	now := time.Unix(100, 0)
	m.errorOverlay.showAt("Test error", "toast", now)

	if !m.errorOverlay.visible {
		t.Fatal("error overlay should start visible")
	}

	m.errorOverlay.update(now.Add(errorToastTTL - time.Millisecond))
	if !m.errorOverlay.visible {
		t.Fatal("error overlay should still be visible before ttl")
	}

	m.errorOverlay.update(now.Add(errorToastTTL))
	if m.errorOverlay.visible {
		t.Fatal("error overlay should auto hide after ttl")
	}
}

func TestRenderOverlayKeepsBaseOutsideModal(t *testing.T) {
	base := strings.Join([]string{
		"top line stays",
		strings.Repeat("a", 60),
		strings.Repeat("b", 60),
		strings.Repeat("c", 60),
		strings.Repeat("d", 60),
		strings.Repeat("e", 60),
		strings.Repeat("f", 60),
		"bottom line stays",
	}, "\n")

	var overlay errorOverlay
	overlay.showAt("Toast title", "Toast body", time.Unix(100, 0))
	got := renderOverlay(base, 80, 10, overlay.view(80, 10))

	if !strings.Contains(got, "top line stays") {
		t.Fatalf("overlay should keep top line: %q", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) < 7 {
		t.Fatalf("overlay output too short: %q", got)
	}
	if !strings.Contains(got, "Toast title") || !strings.Contains(got, "Toast body") {
		t.Fatalf("overlay should render near the bottom-right corner: %q", got)
	}
	if !strings.Contains(got, "bottom line stays") {
		t.Fatalf("overlay should still preserve base content outside the toast: %q", got)
	}
}
