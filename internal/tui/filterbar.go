package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

var typeFilters = []struct {
	value string
	label string
}{
	{"", "All"},
	{"user", "User"},
	{"codechange", "Code"},
	{"system", "System"},
	{"tool", "Tool"},
	{"session", "Session"},
}

type filterModel struct {
	typeIndex   int
	agentLabel  string
	searchInput textinput.Model
	searchMode  bool
	searchQuery string
}

func newFilter() filterModel {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Blur()
	return filterModel{searchInput: ti}
}

func (f *filterModel) cycleType() {
	f.typeIndex = (f.typeIndex + 1) % len(typeFilters)
}

func (f *filterModel) cycleTypeReverse() {
	f.typeIndex = (f.typeIndex - 1 + len(typeFilters)) % len(typeFilters)
}

func (f *filterModel) typeValue() string {
	return typeFilters[f.typeIndex].value
}

func (f *filterModel) typeLabel() string {
	return typeFilters[f.typeIndex].label
}

func (f *filterModel) setAgentLabel(label string) {
	f.agentLabel = label
}

func (f *filterModel) enterSearch() {
	f.searchMode = true
	f.searchInput.SetValue(f.searchQuery)
	f.searchInput.Focus()
}

func (f *filterModel) commitSearch() {
	f.searchQuery = strings.TrimSpace(f.searchInput.Value())
	f.searchMode = false
	f.searchInput.Blur()
}

func (f *filterModel) cancelSearch() {
	f.searchMode = false
	f.searchInput.Blur()
	f.searchInput.SetValue(f.searchQuery)
}

func (f *filterModel) clearSearch() {
	f.searchQuery = ""
	f.searchInput.SetValue("")
}

func (f *filterModel) view(width int) string {
	if f.searchMode {
		return f.searchInput.View()
	}

	var parts []string
	for i, tf := range typeFilters {
		label := tf.label
		if i == f.typeIndex {
			label = lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("[" + label + "]")
		} else {
			label = dimStyle.Render(" " + label + " ")
		}
		parts = append(parts, label)
	}

	filterStr := strings.Join(parts, "")

	agent := "agent:" + orDefault(f.agentLabel, "all")
	search := "search:" + orDefault(f.searchQuery, "off")

	bar := fmt.Sprintf("%s │ %s │ %s", filterStr, agent, search)

	return statusBarStyle.Width(width).Render(bar)
}
