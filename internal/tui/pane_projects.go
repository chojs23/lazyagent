package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/textutil"
)

type projectsModel struct {
	listPaneState
	projects        []model.Project
	sessions        []model.Session
	rootSessions    map[int64][]model.Session
	sessionStatuses map[string]string
	items           []sidebarItem
	selectedSession string
	expandedProjs   map[int64]bool
	spinnerFrame    int
}

type sidebarItem struct {
	kind      string // "project" or "session"
	projectID int64
	sessionID string
	label     string
}

func newProjects() projectsModel {
	return projectsModel{
		listPaneState:   listPaneState{scrolloff: 3},
		expandedProjs:   map[int64]bool{},
		rootSessions:    map[int64][]model.Session{},
		sessionStatuses: map[string]string{},
	}
}

func (p *projectsModel) setData(projects []model.Project, sessions []model.Session) {
	p.projects = projects
	p.sessions = sessions
	p.rootSessions = buildRootSessionsByProject(sessions)
	p.sessionStatuses = buildSessionStatusIndex(sessions)
	p.rebuildItems()
}

func (p *projectsModel) rebuildItems() {
	p.items = nil
	for _, proj := range p.projects {
		p.items = append(p.items, sidebarItem{
			kind:      "project",
			projectID: proj.ID,
			label:     fmt.Sprintf("%s (%d)", orDefault(proj.Name, proj.Slug), proj.SessionCount),
		})
		if p.expandedProjs[proj.ID] {
			for _, sess := range p.rootSessions[proj.ID] {
				p.addSessionItem(proj.ID, sess, 0)
			}
		}
	}
	if p.cursor >= len(p.items) {
		p.clampCursor(len(p.items))
	}
}

func (p *projectsModel) addSessionItem(projectID int64, sess model.Session, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}
	tree := ""
	if depth > 0 {
		tree = "└─ "
	}
	p.items = append(p.items, sidebarItem{
		kind:      "session",
		projectID: projectID,
		sessionID: sess.ID,
		label:     buildProjectSessionLabel(indent, tree, sess),
	})
}

func buildProjectSessionLabel(indent, tree string, sess model.Session) string {
	rt := "C"
	switch sess.Runtime {
	case "opencode":
		rt = "O"
	case "codex":
		rt = "X"
	}
	name := shortID(sess.ID)
	if slug := strings.TrimSpace(textutil.FirstLine(sess.Slug)); slug != "" {
		name = slug
	}
	return fmt.Sprintf("%s%s[%s] %s - %s", indent, tree, rt, formatTime(sess.StartedAt), name)
}

func (p *projectsModel) moveUp() {
	p.listPaneState.moveUp()
}

func (p *projectsModel) moveDown() {
	p.listPaneState.moveDown(len(p.items))
}

func (p *projectsModel) halfPageUp(viewH int) {
	p.listPaneState.halfPageUp(viewH)
}

func (p *projectsModel) halfPageDown(viewH int) {
	p.listPaneState.halfPageDown(viewH, len(p.items))
}

func (p *projectsModel) goTop() {
	p.listPaneState.goTop()
}

func (p *projectsModel) goBottom() {
	p.listPaneState.goBottom(len(p.items))
}

func (p *projectsModel) enter() (sessionChanged bool) {
	if p.cursor >= len(p.items) {
		return false
	}
	item := p.items[p.cursor]
	switch item.kind {
	case "project":
		p.expandedProjs[item.projectID] = !p.expandedProjs[item.projectID]
		p.rebuildItems()
	case "session":
		if p.selectedSession != item.sessionID {
			p.selectedSession = item.sessionID
			return true
		}
	}
	return false
}

func (p *projectsModel) currentSessionID() string {
	return p.selectedSession
}

func (p *projectsModel) currentItem() *sidebarItem {
	if p.cursor < len(p.items) {
		return &p.items[p.cursor]
	}
	return nil
}

func (p *projectsModel) tick() {
	p.spinnerFrame = (p.spinnerFrame + 1) % len(spinnerFrames)
}

// sessionIcons returns the prefix icons for a session line.
// Layout: selection indicator + active spinner + trailing space.
//   - Selected session: green ●
//   - Active session: animated spinner
//
// When raw is true, no ANSI styling is applied so the caller's
// style (e.g. cursor background) covers the entire line.
func (p *projectsModel) sessionIcons(item sidebarItem, raw bool) string {
	sel := " "
	if item.sessionID == p.selectedSession {
		if raw {
			sel = "*"
		} else {
			sel = lipgloss.NewStyle().Foreground(colorGreen).Render("*")
		}
	}
	active := " "
	if p.sessionStatus(item.sessionID) == "active" {
		if raw {
			active = spinnerFrames[p.spinnerFrame]
		} else {
			active = lipgloss.NewStyle().Foreground(colorCyan).Render(spinnerFrames[p.spinnerFrame])
		}
	}
	return sel + " " + active + " "
}

func (p *projectsModel) sessionStatus(id string) string {
	if status, ok := p.sessionStatuses[id]; ok {
		return status
	}
	for _, sess := range p.sessions {
		if sess.ID == id {
			return sess.Status
		}
	}
	return ""
}

func buildRootSessionsByProject(sessions []model.Session) map[int64][]model.Session {
	rootSessions := make(map[int64][]model.Session)
	for _, sess := range sessions {
		if sess.ParentSessionID == "" {
			rootSessions[sess.ProjectID] = append(rootSessions[sess.ProjectID], sess)
		}
	}
	return rootSessions
}

func buildSessionStatusIndex(sessions []model.Session) map[string]string {
	statuses := make(map[string]string, len(sessions))
	for _, sess := range sessions {
		statuses[sess.ID] = sess.Status
	}
	return statuses
}

func (p *projectsModel) view(width, height int, focused bool) string {
	p.height = height

	title := titleStyle.Render("Projects")

	var lines []string
	for i, item := range p.items {
		prefix := "  "
		if i == p.cursor {
			prefix = "> "
		}
		var line string
		isSelected := item.kind == "session" && item.sessionID == p.selectedSession
		switch {
		case i == p.cursor && focused:
			style := cursorStyle
			if isSelected {
				style = cursorSelectedStyle
			}
			switch item.kind {
			case "project":
				arrow := "▶"
				if p.expandedProjs[item.projectID] {
					arrow = "▼"
				}
				line = style.Render("  " + arrow + " " + item.label)
			case "session":
				line = style.Render("    " + p.sessionIcons(item, true) + item.label)
			}
		case isSelected:
			line = selectedStyle.Render(prefix + "  " + p.sessionIcons(item, true) + item.label)
		default:
			switch item.kind {
			case "project":
				arrow := "▶"
				if p.expandedProjs[item.projectID] {
					arrow = "▼"
				}
				line = prefix + arrow + " " + item.label
			case "session":
				line = prefix + "  " + p.sessionIcons(item, false) + item.label
			}
		}
		lines = append(lines, line)
	}
	visible := p.listPaneState.visibleLines(lines, width)
	return renderPane(width, height, focused, title, visible)
}
