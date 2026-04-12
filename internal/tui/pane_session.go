package tui

import (
	"fmt"
	"strings"

	"github.com/chojs23/lazyagent/internal/model"
)

type sessionInfoModel struct {
	session *model.Session
	project *model.Project
}

func newSessionInfo() sessionInfoModel {
	return sessionInfoModel{}
}

func (s *sessionInfoModel) setSession(session *model.Session, project *model.Project) {
	s.session = session
	s.project = project
}

func (s sessionInfoModel) view(width, height int, focused bool) string {
	title := titleStyle.Render("Session")

	if s.session == nil {
		content := title + "\n" + dimStyle.Render("No session selected")
		return paneStyle(focused).Width(width).Height(height).Render(content)
	}

	session := s.session
	projectPath := "-"
	if s.project != nil && s.project.Directory != "" {
		projectPath = s.project.Directory
	}
	lines := []string{
		renderSessionField("AI Tool", runtimeLabel(session.Runtime)),
		renderSessionField("Project", orDefault(session.ProjectName, session.ProjectSlug)),
		renderSessionField("Project Path", projectPath),
		renderSessionField("Session", session.ID),
		renderSessionField("Started", formatDateTime(session.StartedAt)),
		renderSessionField("Last Event", formatDateTime(session.LastActivity)),
		renderSessionField("Events", fmt.Sprintf("%d", session.EventCount)),
		renderSessionField("Agents", fmt.Sprintf("%d", session.AgentCount)),
	}

	content := title + "\n" + strings.Join(lines, "\n")
	return paneStyle(focused).Width(width).Height(height).Render(content)
}

func renderSessionField(label, value string) string {
	return subtitleStyle.Render(label+":") + " " + value
}

func runtimeLabel(runtime string) string {
	switch runtime {
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "claude":
		return "Claude"
	default:
		return orDefault(runtime, "-")
	}
}
