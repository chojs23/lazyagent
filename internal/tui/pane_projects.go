package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/chojs23/lazyagent/internal/model"
)

type projectsModel struct {
	projects        []model.Project
	sessions        []model.Session
	items           []sidebarItem
	cursor          int
	scroll          int
	selectedSession string
	expandedProjs   map[int64]bool
	height          int
}

type sidebarItem struct {
	kind      string // "project" or "session"
	projectID int64
	sessionID string
	label     string
}

func newProjects() projectsModel {
	return projectsModel{
		expandedProjs: map[int64]bool{},
	}
}

func (p *projectsModel) setData(projects []model.Project, sessions []model.Session) {
	p.projects = projects
	p.sessions = sessions
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
			// collect root sessions (no parent) for this project
			for _, sess := range p.sessions {
				if sess.ProjectID == proj.ID && sess.ParentSessionID == "" {
					p.addSessionItem(proj.ID, sess, 0)
				}
			}
		}
	}
	if p.cursor >= len(p.items) {
		p.cursor = max(len(p.items)-1, 0)
	}
}

func (p *projectsModel) addSessionItem(projectID int64, sess model.Session, depth int) {
	// Slug doesn't look decent
	slug := orDefault(sess.Slug, shortID(sess.ID))
	_ = slug
	rt := "C"
	if sess.Runtime == "opencode" {
		rt = "O"
	}
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
		label:     fmt.Sprintf("%s%s[%s] %s  e:%d a:%d", indent, tree, rt, sess.ID, sess.EventCount, sess.AgentCount),
	})
	// add child sessions
	for _, child := range p.sessions {
		if child.ParentSessionID == sess.ID {
			p.addSessionItem(projectID, child, depth+1)
		}
	}
}

func (p *projectsModel) moveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *projectsModel) moveDown() {
	if p.cursor < len(p.items)-1 {
		p.cursor++
	}
}

func (p *projectsModel) halfPageUp(viewH int) {
	p.cursor = max(p.cursor-viewH/2, 0)
}

func (p *projectsModel) halfPageDown(viewH int) {
	p.cursor = min(p.cursor+viewH/2, max(len(p.items)-1, 0))
}

func (p *projectsModel) goTop() {
	p.cursor = 0
}

func (p *projectsModel) goBottom() {
	if len(p.items) > 0 {
		p.cursor = len(p.items) - 1
	}
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

func (p *projectsModel) sessionStatus(id string) string {
	for _, sess := range p.sessions {
		if sess.ID == id {
			return sess.Status
		}
	}
	return ""
}

func (p *projectsModel) view(width, height int, focused bool) string {
	p.height = height

	title := titleStyle.Render("Projects")

	contentHeight := max(height-3, 1)
	textWidth := max(width-4, 1) // border(2) + padding(2)

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
				icon := rawStatusIcon(p.sessionStatus(item.sessionID))
				line = style.Render("    " + icon + " " + item.label)
			}
		case isSelected:
			icon := rawStatusIcon(p.sessionStatus(item.sessionID))
			line = selectedStyle.Render(prefix + "  " + icon + " " + item.label)
		default:
			switch item.kind {
			case "project":
				arrow := "▶"
				if p.expandedProjs[item.projectID] {
					arrow = "▼"
				}
				line = prefix + arrow + " " + item.label
			case "session":
				icon := statusIcon(p.sessionStatus(item.sessionID))
				line = prefix + "  " + icon + " " + item.label
			}
		}
		lines = append(lines, ansi.Truncate(line, textWidth, ""))
	}

	if p.cursor >= p.scroll+contentHeight {
		p.scroll = p.cursor - contentHeight + 1
	}
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	maxScroll := max(len(lines)-contentHeight, 0)
	p.scroll = min(p.scroll, maxScroll)

	visible := sliceLines(lines, p.scroll, contentHeight)
	content := title + "\n" + strings.Join(visible, "\n")
	return paneStyle(focused).Width(width).Height(height).Render(content)
}
