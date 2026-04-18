package tui

type listPaneState struct {
	cursor  int
	scroll  int
	hScroll int
	height  int
}

func (s *listPaneState) clampCursor(total int) {
	if total <= 0 {
		s.cursor = 0
		s.scroll = 0
		return
	}
	if s.cursor >= total {
		s.cursor = total - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *listPaneState) moveUp() {
	if s.cursor > 0 {
		s.cursor--
	}
}

func (s *listPaneState) moveDown(total int) {
	if s.cursor < total-1 {
		s.cursor++
	}
}

func (s *listPaneState) halfPageUp(viewH int) {
	s.cursor = max(s.cursor-viewH/2, 0)
}

func (s *listPaneState) halfPageDown(viewH, total int) {
	s.cursor = min(s.cursor+viewH/2, max(total-1, 0))
}

func (s *listPaneState) goTop() {
	s.cursor = 0
}

func (s *listPaneState) goBottom(total int) {
	if total > 0 {
		s.cursor = total - 1
	}
}

func (s *listPaneState) syncListScroll(total int) {
	contentHeight := max(s.height-3, 1)
	if s.cursor >= s.scroll+contentHeight {
		s.scroll = s.cursor - contentHeight + 1
	}
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	}
	maxScroll := max(total-contentHeight, 0)
	s.scroll = min(s.scroll, maxScroll)
	if s.scroll < 0 {
		s.scroll = 0
	}
}

func (s *listPaneState) visibleLines(lines []string, width int) []string {
	textWidth := max(width-4, 1)
	s.hScroll = clampHScroll(lines, s.hScroll, textWidth)
	for i, line := range lines {
		lines[i] = hScrollLine(line, s.hScroll, textWidth)
	}
	s.syncListScroll(len(lines))
	return sliceLines(lines, s.scroll, max(s.height-3, 1))
}
