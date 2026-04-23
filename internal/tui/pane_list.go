package tui

type listPaneState struct {
	cursor    int
	scroll    int
	hScroll   int
	height    int
	scrolloff int
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
	s.scroll = clampListScroll(s.cursor, s.scroll, s.height, total, s.scrolloff)
}

func clampListScroll(cursor, scroll, height, total, desiredScrolloff int) int {
	contentHeight := max(height-3, 1)
	scrolloff := min(max(desiredScrolloff, 0), (contentHeight-1)/2)

	if cursor > scroll+contentHeight-1-scrolloff {
		scroll = cursor - contentHeight + 1 + scrolloff
	}
	if cursor < scroll+scrolloff {
		scroll = cursor - scrolloff
	}

	scroll = max(scroll, 0)
	maxScroll := max(total-contentHeight, 0)
	return min(scroll, maxScroll)
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

func sliceLines(lines []string, offset, count int) []string {
	if offset >= len(lines) {
		return nil
	}
	end := min(offset+count, len(lines))
	return lines[offset:end]
}
