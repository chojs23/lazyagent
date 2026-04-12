package tui

import (
	"fmt"

	"github.com/chojs23/lazyagent/internal/model"
)

type sessionInfoModel struct {
	session *model.Session
	project *model.Project
	scroll  int
	hScroll int
	height  int
}

func newSessionInfo() sessionInfoModel {
	return sessionInfoModel{}
}

func (s *sessionInfoModel) setSession(session *model.Session, project *model.Project) {
	if sameSessionSummary(s.session, session) && sameProjectSummary(s.project, project) {
		s.session = session
		s.project = project
		return
	}

	s.session = session
	s.project = project
	s.scroll = 0
	s.hScroll = 0
}

func (s *sessionInfoModel) moveUp() {
	s.scroll = max(s.scroll-1, 0)
}

func (s *sessionInfoModel) moveDown() {
	lines := s.lines()
	contentHeight := max(s.height-3, 1)
	maxScroll := max(len(lines)-contentHeight, 0)
	s.scroll = min(s.scroll+1, maxScroll)
}

func (s *sessionInfoModel) halfPageUp(viewH int) {
	contentHeight := max(viewH-3, 1)
	s.scroll = max(s.scroll-contentHeight/2, 0)
}

func (s *sessionInfoModel) halfPageDown(viewH int) {
	lines := s.lines()
	contentHeight := max(viewH-3, 1)
	maxScroll := max(len(lines)-contentHeight, 0)
	s.scroll = min(s.scroll+contentHeight/2, maxScroll)
}

func (s *sessionInfoModel) goTop() {
	s.scroll = 0
}

func (s *sessionInfoModel) goBottom() {
	lines := s.lines()
	contentHeight := max(s.height-3, 1)
	s.scroll = max(len(lines)-contentHeight, 0)
}

func (s *sessionInfoModel) lines() []string {
	if s.session == nil {
		return []string{dimStyle.Render("No session selected")}
	}

	session := s.session
	projectPath := "-"
	if s.project != nil && s.project.Directory != "" {
		projectPath = s.project.Directory
	}

	return []string{
		renderSessionField("AI Tool", runtimeLabel(session.Runtime)),
		renderSessionField("Project", orDefault(session.ProjectName, session.ProjectSlug)),
		renderSessionField("Project Path", projectPath),
		renderSessionField("Session", session.ID),
		renderSessionField("Started", formatDateTime(session.StartedAt)),
		renderSessionField("Last Event", formatDateTime(session.LastActivity)),
		renderSessionField("Events", fmt.Sprintf("%d", session.EventCount)),
		renderSessionField("Agents", fmt.Sprintf("%d", session.AgentCount)),
	}
}

func (s *sessionInfoModel) view(width, height int, focused bool) string {
	s.height = height

	title := titleStyle.Render("Session")
	lines := s.lines()
	contentHeight := max(height-3, 1)
	textWidth := max(width-4, 1)

	s.hScroll = clampHScroll(lines, s.hScroll, textWidth)
	for i, line := range lines {
		lines[i] = hScrollLine(line, s.hScroll, textWidth)
	}

	maxScroll := max(len(lines)-contentHeight, 0)
	s.scroll = min(s.scroll, maxScroll)
	visible := sliceLines(lines, s.scroll, contentHeight)

	return renderPane(width, height, focused, title, visible)
}

func renderSessionField(label, value string) string {
	return subtitleStyle.Render(label+":") + " " + value
}

func sameSessionSummary(current, next *model.Session) bool {
	switch {
	case current == nil && next == nil:
		return true
	case current == nil || next == nil:
		return false
	default:
		return current.ID == next.ID &&
			current.ProjectID == next.ProjectID &&
			current.ProjectName == next.ProjectName &&
			current.ProjectSlug == next.ProjectSlug &&
			current.Runtime == next.Runtime &&
			current.StartedAt == next.StartedAt &&
			current.LastActivity == next.LastActivity &&
			current.EventCount == next.EventCount &&
			current.AgentCount == next.AgentCount
	}
}

func sameProjectSummary(current, next *model.Project) bool {
	switch {
	case current == nil && next == nil:
		return true
	case current == nil || next == nil:
		return false
	default:
		return current.ID == next.ID &&
			current.Name == next.Name &&
			current.Slug == next.Slug &&
			current.Directory == next.Directory
	}
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
