package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/model"
)

type sidebarModel struct {
	projects        []model.Project
	sessions        []model.Session
	agents          []model.Agent
	items           []sidebarItem
	cursor          int
	selectedSession string
	expandedProjs   map[int64]bool
	agentCursor     int
	selectedAgent   string
	focusAgents     bool
	height          int
	scroll          int
	agentScroll     int
}

type sidebarItem struct {
	kind      string // "project" or "session"
	projectID int64
	sessionID string
	label     string
}

func newSidebar() sidebarModel {
	return sidebarModel{
		expandedProjs: map[int64]bool{},
	}
}

func (s *sidebarModel) setData(projects []model.Project, sessions []model.Session, agents []model.Agent) {
	s.projects = projects
	s.sessions = sessions
	s.agents = agents
	s.rebuildItems()
}

func (s *sidebarModel) rebuildItems() {
	s.items = nil
	for _, p := range s.projects {
		s.items = append(s.items, sidebarItem{
			kind:      "project",
			projectID: p.ID,
			label:     fmt.Sprintf("%s (%d)", orDefault(p.Name, p.Slug), p.SessionCount),
		})
		if s.expandedProjs[p.ID] {
			for _, sess := range s.sessions {
				if sess.ProjectID == p.ID {
					slug := orDefault(sess.Slug, shortID(sess.ID))
					s.items = append(s.items, sidebarItem{
						kind:      "session",
						projectID: p.ID,
						sessionID: sess.ID,
						label:     fmt.Sprintf("%s  e:%d a:%d", slug, sess.EventCount, sess.AgentCount),
					})
				}
			}
		}
	}
	if s.cursor >= len(s.items) {
		s.cursor = maxInt(len(s.items)-1, 0)
	}
}

func (s *sidebarModel) moveUp() {
	if s.focusAgents {
		if s.agentCursor > 0 {
			s.agentCursor--
		}
		return
	}
	if s.cursor > 0 {
		s.cursor--
	}
}

func (s *sidebarModel) moveDown() {
	if s.focusAgents {
		if s.agentCursor < len(s.agents)-1 {
			s.agentCursor++
		}
		return
	}
	if s.cursor < len(s.items)-1 {
		s.cursor++
	}
}

func (s *sidebarModel) halfPageUp(viewH int) {
	if s.focusAgents {
		s.agentCursor = maxInt(s.agentCursor-viewH/2, 0)
	} else {
		s.cursor = maxInt(s.cursor-viewH/2, 0)
	}
}

func (s *sidebarModel) halfPageDown(viewH int) {
	if s.focusAgents {
		s.agentCursor = minInt(s.agentCursor+viewH/2, maxInt(len(s.agents)-1, 0))
	} else {
		s.cursor = minInt(s.cursor+viewH/2, maxInt(len(s.items)-1, 0))
	}
}

func (s *sidebarModel) goTop() {
	if s.focusAgents {
		s.agentCursor = 0
	} else {
		s.cursor = 0
	}
}

func (s *sidebarModel) goBottom() {
	if s.focusAgents {
		if len(s.agents) > 0 {
			s.agentCursor = len(s.agents) - 1
		}
	} else {
		if len(s.items) > 0 {
			s.cursor = len(s.items) - 1
		}
	}
}

func (s *sidebarModel) enter() (sessionChanged bool) {
	if s.focusAgents {
		if s.agentCursor < len(s.agents) {
			a := s.agents[s.agentCursor]
			if s.selectedAgent == a.ID {
				s.selectedAgent = "" // toggle off
			} else {
				s.selectedAgent = a.ID
			}
		}
		return false
	}

	if s.cursor >= len(s.items) {
		return false
	}
	item := s.items[s.cursor]
	switch item.kind {
	case "project":
		s.expandedProjs[item.projectID] = !s.expandedProjs[item.projectID]
		s.rebuildItems()
	case "session":
		if s.selectedSession != item.sessionID {
			s.selectedSession = item.sessionID
			s.selectedAgent = ""
			s.agentCursor = 0
			return true
		}
	}
	return false
}

func (s *sidebarModel) toggleAgentFocus() {
	s.focusAgents = !s.focusAgents
}

func (s *sidebarModel) currentSessionID() string {
	return s.selectedSession
}

func (s *sidebarModel) selectedAgentID() string {
	return s.selectedAgent
}

func (s *sidebarModel) currentItem() *sidebarItem {
	if s.cursor < len(s.items) {
		return &s.items[s.cursor]
	}
	return nil
}

func (s *sidebarModel) view(width, height int, focused bool) string {
	s.height = height

	// Split: top = project/session tree, bottom = agent tree
	agentHeight := minInt(len(s.agents)+2, height/3)
	treeHeight := maxInt(height-agentHeight-1, 4)

	// ── Project/Session tree ──
	var treeLines []string
	for i, item := range s.items {
		prefix := "  "
		if i == s.cursor && !s.focusAgents {
			prefix = "> "
		}

		switch item.kind {
		case "project":
			arrow := "▶"
			if s.expandedProjs[item.projectID] {
				arrow = "▼"
			}
			treeLines = append(treeLines, prefix+arrow+" "+item.label)
		case "session":
			icon := statusIcon(s.sessionStatus(item.sessionID))
			marker := "  "
			if item.sessionID == s.selectedSession {
				marker = lipgloss.NewStyle().Foreground(colorCyan).Render("● ")
			}
			treeLines = append(treeLines, prefix+"  "+icon+" "+marker+item.label)
		}
	}

	// scroll tree
	if s.cursor >= s.scroll+treeHeight {
		s.scroll = s.cursor - treeHeight + 1
	}
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	}
	visibleTree := sliceLines(treeLines, s.scroll, treeHeight)

	// ── Agent tree ──
	agentTitle := dimStyle.Render("── Agents ──")
	var agentLines []string
	for i, a := range s.agents {
		prefix := "  "
		if i == s.agentCursor && s.focusAgents {
			prefix = "> "
		}
		name := orDefault(a.Name, shortID(a.ID))
		if a.AgentType != "" {
			name += " (" + a.AgentType + ")"
		}
		tree := ""
		if a.ParentAgentID != "" {
			if i == len(s.agents)-1 || s.agents[i+1].ParentAgentID == "" {
				tree = "└─ "
			} else {
				tree = "├─ "
			}
		}
		marker := ""
		if a.ID == s.selectedAgent {
			marker = lipgloss.NewStyle().Foreground(colorCyan).Render("*")
		}
		line := prefix + tree + name + marker
		agentLines = append(agentLines, line)
	}

	if s.agentCursor >= s.agentScroll+agentHeight {
		s.agentScroll = s.agentCursor - agentHeight + 1
	}
	if s.agentCursor < s.agentScroll {
		s.agentScroll = s.agentCursor
	}
	visibleAgents := sliceLines(agentLines, s.agentScroll, agentHeight)

	content := strings.Join(visibleTree, "\n") + "\n" + agentTitle + "\n" + strings.Join(visibleAgents, "\n")

	style := paneStyle(focused).Width(width)
	return style.Render(content)
}

func (s *sidebarModel) sessionStatus(id string) string {
	for _, sess := range s.sessions {
		if sess.ID == id {
			return sess.Status
		}
	}
	return ""
}

func sliceLines(lines []string, offset, count int) []string {
	if offset >= len(lines) {
		return nil
	}
	end := minInt(offset+count, len(lines))
	return lines[offset:end]
}
