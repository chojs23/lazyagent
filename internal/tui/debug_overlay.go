package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
)

const debugLogCapacity = 100

type debugEntry struct {
	time    time.Time
	message string
}

type debugOverlay struct {
	visible bool
	entries []debugEntry
	scroll  int
}

func (d *debugOverlay) toggle() {
	d.visible = !d.visible
	if d.visible {
		// Reset scroll to bottom on open.
		d.scroll = 0
	}
}

func (d *debugOverlay) add(format string, args ...any) {
	entry := debugEntry{
		time:    time.Now(),
		message: fmt.Sprintf(format, args...),
	}
	d.entries = append(d.entries, entry)
	if len(d.entries) > debugLogCapacity {
		d.entries = d.entries[len(d.entries)-debugLogCapacity:]
	}
}

func (d *debugOverlay) clear() {
	d.entries = d.entries[:0]
	d.scroll = 0
}

func (d *debugOverlay) scrollUp(n int) {
	d.scroll = min(d.scroll+n, max(len(d.entries)-1, 0))
}

func (d *debugOverlay) scrollDown(n int) {
	d.scroll = max(d.scroll-n, 0)
}

func (d debugOverlay) view(width, height int) string {
	if !d.visible {
		return ""
	}

	bodyWidth := min(max(width-8, 40), 120)
	viewHeight := min(max(height-8, 6), 30)

	title := lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("Debug Log")
	hint := dimStyle.Render("j/k scroll  c clear  ` close")
	header := title + "  " + hint

	if len(d.entries) == 0 {
		content := strings.Join([]string{header, "", dimStyle.Render("(empty)")}, "\n")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCyan).
			Background(lipgloss.Color("235")).
			Padding(1, 2).
			Width(bodyWidth).
			Render(content)
	}

	// Build visible lines from the bottom (newest first), offset by scroll.
	end := len(d.entries) - d.scroll
	start := max(end-viewHeight, 0)
	if end <= 0 {
		end = 1
		start = 0
	}

	var lines []string
	for i := start; i < end && i < len(d.entries); i++ {
		e := d.entries[i]
		ts := dimStyle.Render(e.time.Format("15:04:05.000"))
		msg := lipgloss.NewStyle().
			Width(bodyWidth - 16).
			Render(e.message)
		lines = append(lines, ts+" "+msg)
	}

	scrollInfo := ""
	if d.scroll > 0 {
		scrollInfo = dimStyle.Render(fmt.Sprintf(" (+%d newer)", d.scroll))
	}

	content := strings.Join(append([]string{header + scrollInfo, ""}, lines...), "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(bodyWidth).
		Render(content)
}

// Global debug logger for use outside the TUI model.
var (
	globalDebugMu      sync.Mutex
	globalDebugOverlay *debugOverlay
)

func setGlobalDebug(d *debugOverlay) {
	globalDebugMu.Lock()
	defer globalDebugMu.Unlock()
	globalDebugOverlay = d
}

// DebugLog writes a debug message to the in-app debug overlay.
func DebugLog(format string, args ...any) {
	globalDebugMu.Lock()
	defer globalDebugMu.Unlock()
	if globalDebugOverlay != nil {
		globalDebugOverlay.add(format, args...)
	}
}
