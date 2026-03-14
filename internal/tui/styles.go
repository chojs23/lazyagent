package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

var (
	colorCyan     = lipgloss.Color("86")
	colorGray     = lipgloss.Color("240")
	colorGreen    = lipgloss.Color("46")
	colorYellow   = lipgloss.Color("226")
	colorRed      = lipgloss.Color("196")
	colorBlue     = lipgloss.Color("33")
	colorMagenta  = lipgloss.Color("213")
	colorOrange   = lipgloss.Color("208")
	colorWhite    = lipgloss.Color("255")
	colorDimWhite = lipgloss.Color("250")

	activePane   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorCyan).Padding(0, 1)
	inactivePane = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorGray).Padding(0, 1)

	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	subtitleStyle  = lipgloss.NewStyle().Foreground(colorGray)
	selectedStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	dimStyle       = lipgloss.NewStyle().Foreground(colorGray)
	statusBarStyle = lipgloss.NewStyle().Foreground(colorDimWhite)

	agentColors = []color.Color{
		colorCyan, colorMagenta, colorYellow, colorGreen, colorBlue, colorOrange,
	}
)

func subtypeColor(subtype string) color.Color {
	switch subtype {
	case "PreToolUse":
		return colorYellow
	case "PostToolUse":
		return colorGreen
	case "PostToolUseFailure":
		return colorRed
	case "SessionStart", "SessionEnd":
		return colorBlue
	case "UserPromptSubmit":
		return colorMagenta
	case "Stop", "SubagentStop", "StopFailure":
		return colorOrange
	case "Notification":
		return colorDimWhite
	default:
		return colorWhite
	}
}

func statusIcon(status string) string {
	switch status {
	case "active":
		return lipgloss.NewStyle().Foreground(colorGreen).Render("●")
	default:
		return lipgloss.NewStyle().Foreground(colorGray).Render("○")
	}
}

func agentColor(index int) color.Color {
	if index < 0 {
		index = 0
	}
	return agentColors[index%len(agentColors)]
}

func paneStyle(focused bool) lipgloss.Style {
	if focused {
		return activePane
	}
	return inactivePane
}
