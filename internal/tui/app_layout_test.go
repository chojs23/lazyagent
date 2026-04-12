package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestViewFitsTerminalHeightWhenFooterWraps(t *testing.T) {
	m := newModel(nil, time.Second)
	m.width = 44
	m.height = 18
	m.status = strings.Repeat("refresh failed while loading sessions ", 3)
	m.help.ShowAll = true

	view := m.View().Content
	if got := lipgloss.Height(view); got > m.height {
		t.Fatalf("view height = %d, want <= %d\n%s", got, m.height, view)
	}
}
