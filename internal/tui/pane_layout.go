package tui

import "strings"

func renderPane(width, height int, focused bool, title string, body []string) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	innerHeight := max(height-2, 0)
	if innerHeight == 0 {
		return paneStyle(focused).Width(width).MaxHeight(height).Render("")
	}

	lines := []string{title}
	bodyHeight := max(innerHeight-1, 0)
	if len(body) > bodyHeight {
		body = body[:bodyHeight]
	}
	lines = append(lines, body...)
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}

	return paneStyle(focused).Width(width).MaxHeight(height).Render(strings.Join(lines, "\n"))
}
