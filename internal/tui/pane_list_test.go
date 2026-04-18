package tui

import "testing"

func TestListPaneStateClampCursorEmpty(t *testing.T) {
	state := listPaneState{cursor: 3, scroll: 2}
	state.clampCursor(0)

	if state.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", state.cursor)
	}
	if state.scroll != 0 {
		t.Fatalf("scroll = %d, want 0", state.scroll)
	}
}

func TestListPaneStateMoveAndPage(t *testing.T) {
	state := listPaneState{}
	state.moveDown(10)
	state.moveDown(10)
	state.moveDown(10)
	state.moveUp()
	if state.cursor != 2 {
		t.Fatalf("cursor after move = %d, want 2", state.cursor)
	}

	state.halfPageDown(6, 10)
	if state.cursor != 5 {
		t.Fatalf("cursor after halfPageDown = %d, want 5", state.cursor)
	}

	state.halfPageUp(6)
	if state.cursor != 2 {
		t.Fatalf("cursor after halfPageUp = %d, want 2", state.cursor)
	}

	state.goBottom(10)
	if state.cursor != 9 {
		t.Fatalf("cursor after goBottom = %d, want 9", state.cursor)
	}

	state.goTop()
	if state.cursor != 0 {
		t.Fatalf("cursor after goTop = %d, want 0", state.cursor)
	}
}

func TestListPaneStateVisibleLinesKeepsCursorVisible(t *testing.T) {
	state := listPaneState{cursor: 7, height: 8}
	lines := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

	visible := state.visibleLines(lines, 20)

	if state.scroll != 3 {
		t.Fatalf("scroll = %d, want 3", state.scroll)
	}
	if len(visible) != 5 {
		t.Fatalf("visible lines len = %d, want 5", len(visible))
	}
	if visible[0] != "3" || visible[len(visible)-1] != "7" {
		t.Fatalf("visible lines = %#v, want [3 4 5 6 7]", visible)
	}
}
