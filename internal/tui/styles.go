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

	titleStyle          = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	subtitleStyle       = lipgloss.NewStyle().Foreground(colorGray)
	selectedStyle       = lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	cursorStyle         = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("25")).Foreground(colorWhite)
	cursorSelectedStyle = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("25")).Foreground(colorGreen)
	dimStyle            = lipgloss.NewStyle().Foreground(colorGray)
	statusBarStyle      = lipgloss.NewStyle().Foreground(colorDimWhite)

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
	case "SessionStart", "SessionEnd", "SessionDiff", "SessionUpdated":
		return colorBlue
	case "UserPromptSubmit":
		return colorMagenta
	case "Stop", "SubagentStop", "StopFailure", "SessionStatus":
		return colorOrange
	case "Notification", "PermissionReply":
		return colorDimWhite
	case "PartUpdated":
		return colorCyan
	case "MessageUpdated":
		return colorMagenta
	case "TodoUpdate":
		return colorYellow
	case "CommandExecuted":
		return colorGreen
	case "FileEdited":
		return colorBlue
	default:
		return colorWhite
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
