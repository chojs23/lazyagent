package diff

import "testing"

func TestCompute_NoChange(t *testing.T) {
	lines := []string{"a", "b", "c"}
	script := Compute(lines, lines)
	for _, dl := range script {
		if dl.Op != OpEqual {
			t.Errorf("expected all OpEqual, got %v for %q", dl.Op, dl.Text)
		}
	}
	s := Count(script)
	if s.Additions != 0 || s.Deletions != 0 {
		t.Errorf("expected 0 additions/deletions, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestCompute_AllNew(t *testing.T) {
	script := Compute(nil, []string{"a", "b"})
	s := Count(script)
	if s.Additions != 2 || s.Deletions != 0 {
		t.Errorf("expected +2 -0, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestCompute_AllDeleted(t *testing.T) {
	script := Compute([]string{"a", "b"}, nil)
	s := Count(script)
	if s.Additions != 0 || s.Deletions != 2 {
		t.Errorf("expected +0 -2, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestCompute_MiddleChange(t *testing.T) {
	old := []string{"a", "b", "c", "d", "e"}
	new := []string{"a", "b", "x", "d", "e"}
	script := Compute(old, new)
	s := Count(script)
	if s.Additions != 1 || s.Deletions != 1 {
		t.Errorf("expected +1 -1, got +%d -%d", s.Additions, s.Deletions)
	}

	for _, dl := range script {
		if dl.Op == OpDelete && dl.Text != "c" {
			t.Errorf("expected deleted line 'c', got %q", dl.Text)
		}
		if dl.Op == OpInsert && dl.Text != "x" {
			t.Errorf("expected inserted line 'x', got %q", dl.Text)
		}
	}
}

func TestCompute_MultiLineInsert(t *testing.T) {
	old := []string{"a", "b"}
	new := []string{"a", "x", "y", "b"}
	script := Compute(old, new)
	s := Count(script)
	if s.Additions != 2 || s.Deletions != 0 {
		t.Errorf("expected +2 -0, got +%d -%d", s.Additions, s.Deletions)
	}
}

func TestWithContext(t *testing.T) {
	old := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	new := []string{"1", "2", "3", "4", "X", "6", "7", "8", "9", "10"}
	script := Compute(old, new)

	ctx := WithContext(script, 1)
	hasGap := false
	for _, dl := range ctx {
		if dl.Op == OpEqual && dl.Text == "~~~" {
			hasGap = true
			break
		}
	}
	if !hasGap {
		t.Error("expected collapsed gap in context view")
	}
}

func TestCompute_Empty(t *testing.T) {
	script := Compute(nil, nil)
	if len(script) != 0 {
		t.Errorf("expected empty script, got %d entries", len(script))
	}
}
