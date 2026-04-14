package tui

import (
	"testing"
)

func TestComputeDiff_NoChange(t *testing.T) {
	lines := []string{"a", "b", "c"}
	script := ComputeDiff(lines, lines)
	for _, dl := range script {
		if dl.Op != DiffEqual {
			t.Errorf("expected all DiffEqual, got %v for %q", dl.Op, dl.Text)
		}
	}
	s := Stats(script)
	if s.Additions != 0 || s.Deletions != 0 {
		t.Errorf("expected 0 additions/deletions, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestComputeDiff_AllNew(t *testing.T) {
	script := ComputeDiff(nil, []string{"a", "b"})
	s := Stats(script)
	if s.Additions != 2 || s.Deletions != 0 {
		t.Errorf("expected +2 -0, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestComputeDiff_AllDeleted(t *testing.T) {
	script := ComputeDiff([]string{"a", "b"}, nil)
	s := Stats(script)
	if s.Additions != 0 || s.Deletions != 2 {
		t.Errorf("expected +0 -2, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestComputeDiff_MiddleChange(t *testing.T) {
	old := []string{"a", "b", "c", "d", "e"}
	new := []string{"a", "b", "x", "d", "e"}
	script := ComputeDiff(old, new)
	s := Stats(script)
	if s.Additions != 1 || s.Deletions != 1 {
		t.Errorf("expected +1 -1, got +%d -%d", s.Additions, s.Deletions)
	}

	// verify the changed line
	for _, dl := range script {
		if dl.Op == DiffDelete && dl.Text != "c" {
			t.Errorf("expected deleted line 'c', got %q", dl.Text)
		}
		if dl.Op == DiffInsert && dl.Text != "x" {
			t.Errorf("expected inserted line 'x', got %q", dl.Text)
		}
	}
}

func TestComputeDiff_MultiLineInsert(t *testing.T) {
	old := []string{"a", "b"}
	new := []string{"a", "x", "y", "b"}
	script := ComputeDiff(old, new)
	s := Stats(script)
	if s.Additions != 2 || s.Deletions != 0 {
		t.Errorf("expected +2 -0, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestWithContext(t *testing.T) {
	old := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	new := []string{"1", "2", "3", "4", "X", "6", "7", "8", "9", "10"}
	script := ComputeDiff(old, new)

	// with 1 line of context, lines far from the change should be collapsed
	ctx := WithContext(script, 1)
	hasGap := false
	for _, dl := range ctx {
		if dl.Op == DiffEqual && dl.Text == "~~~" {
			hasGap = true
			break
		}
	}
	if !hasGap {
		t.Error("expected collapsed gap in context view")
	}
}

func TestComputeDiff_Empty(t *testing.T) {
	script := ComputeDiff(nil, nil)
	if len(script) != 0 {
		t.Errorf("expected empty script, got %d entries", len(script))
	}
}
