package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func formatTime(ts int64) string {
	if ts == 0 {
		return "--:--:--"
	}
	return time.UnixMilli(ts).Format("15:04:05")
}

func formatDateTime(ts int64) string {
	if ts == 0 {
		return "-"
	}
	return time.UnixMilli(ts).Format("2006-01-02 15:04:05")
}

func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func relativeTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(ts))
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// hScrollLine applies horizontal scroll offset and truncates to width.
// ANSI escape codes are preserved correctly.
func hScrollLine(line string, offset, width int) string {
	w := ansi.StringWidth(line)
	if offset >= w {
		return ""
	}
	return ansi.Cut(line, offset, offset+width)
}

// clampHScroll limits horizontal scroll so it never scrolls past the content.
func clampHScroll(lines []string, hScroll, textWidth int) int {
	maxW := 0
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > maxW {
			maxW = w
		}
	}
	maxHScroll := max(maxW-textWidth, 0)
	return min(hScroll, maxHScroll)
}
