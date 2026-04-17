package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const errorToastTTL = 5 * time.Second

type errorOverlay struct {
	visible   bool
	title     string
	message   string
	expiresAt time.Time
}

func (o *errorOverlay) show(title, message string) {
	o.showAt(title, message, time.Now())
}

func (o *errorOverlay) showAt(title, message string, now time.Time) {
	o.visible = true
	o.title = title
	o.message = message
	o.expiresAt = now.Add(errorToastTTL)
}

func (o *errorOverlay) update(now time.Time) {
	if !o.visible {
		return
	}
	if !o.expiresAt.IsZero() && !now.Before(o.expiresAt) {
		o.visible = false
	}
}

func (o errorOverlay) view(width int) string {
	if !o.visible {
		return ""
	}

	bodyWidth := min(max(width-20, 32), 88)
	body := lipgloss.NewStyle().
		Width(bodyWidth).
		Foreground(colorWhite).
		Render(o.message)
	title := lipgloss.NewStyle().Bold(true).Foreground(colorRed).Render(o.title)

	content := strings.Join([]string{title, "", body}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorRed).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(bodyWidth).
		Render(content)
}

func renderOverlay(base string, width, height int, overlay string) string {
	return renderOverlayAt(base, width, height, overlay, func(overlayWidth, overlayHeight int) (int, int) {
		return max(width-overlayWidth-2, 0), max(height-overlayHeight-2, 0)
	})
}

func renderOverlayCentered(base string, width, height int, overlay string) string {
	return renderOverlayAt(base, width, height, overlay, func(overlayWidth, overlayHeight int) (int, int) {
		return max((width-overlayWidth)/2, 0), max((height-overlayHeight)/2, 0)
	})
}

func renderOverlayAt(base string, width, height int, overlay string, position func(overlayWidth, overlayHeight int) (int, int)) string {
	if overlay == "" {
		return base
	}

	lines := strings.Split(base, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}

	overlayLines := strings.Split(overlay, "\n")
	overlayWidth := lipgloss.Width(overlay)
	overlayHeight := lipgloss.Height(overlay)
	x, y := position(overlayWidth, overlayHeight)

	for i, overlayLine := range overlayLines {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		baseLine := lines[row]
		left := ansi.Cut(baseLine, 0, x)
		right := ""
		if x+overlayWidth < width {
			right = ansi.Cut(baseLine, x+overlayWidth, width)
		}
		lines[row] = left + padVisibleRight(overlayLine, overlayWidth) + right
	}

	return strings.Join(lines, "\n")
}

func padVisibleRight(s string, width int) string {
	short := width - ansi.StringWidth(s)
	if short <= 0 {
		return s
	}
	return s + strings.Repeat(" ", short)
}
