package tui

import (
	"fmt"
	"time"
)

func formatTime(ts int64) string {
	return time.UnixMilli(ts).Format("15:04:05")
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

