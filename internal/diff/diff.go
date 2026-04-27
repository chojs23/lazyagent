// Package diff implements line-level diffing using the Myers algorithm
// (O(ND) shortest edit script). It produces unified-diff-style output with
// optional context line trimming.
//
// The package is shared between the TUI detail pane and the web event-brief
// helpers so both produce identical "+N -M" stats and diff renderings.
package diff

// Op classifies a line in the edit script.
type Op int

const (
	OpEqual  Op = iota // line appears in both old and new
	OpDelete           // line removed from old
	OpInsert           // line added in new
)

// Line is a single entry in the edit script.
type Line struct {
	Op   Op
	Text string
}

// Stats holds aggregate counts for a diff.
type Stats struct {
	Additions int
	Deletions int
}

// Compute returns the shortest edit script between old and new line slices
// using the Myers diff algorithm.
func Compute(oldLines, newLines []string) []Line {
	n := len(oldLines)
	m := len(newLines)
	max := n + m
	if max == 0 {
		return nil
	}

	// V stores the furthest-reaching endpoint for each diagonal k.
	// We use an offset of max so that negative indices work.
	v := make([]int, 2*max+1)
	// trace records each V snapshot for backtracking.
	var trace [][]int

	for d := 0; d <= max; d++ {
		snap := make([]int, len(v))
		copy(snap, v)
		trace = append(trace, snap)

		for k := -d; k <= d; k += 2 {
			idx := k + max
			var x int
			if k == -d || (k != d && v[idx-1] < v[idx+1]) {
				x = v[idx+1] // move down (insert)
			} else {
				x = v[idx-1] + 1 // move right (delete)
			}
			y := x - k

			// follow diagonal (equal lines)
			for x < n && y < m && oldLines[x] == newLines[y] {
				x++
				y++
			}

			v[idx] = x

			if x >= n && y >= m {
				return backtrack(trace, oldLines, newLines, d, max)
			}
		}
	}

	return fallback(oldLines, newLines)
}

func backtrack(trace [][]int, oldLines, newLines []string, d, max int) []Line {
	var script []Line
	x := len(oldLines)
	y := len(newLines)

	for di := d; di > 0; di-- {
		v := trace[di]
		k := x - y

		var prevK int
		if k == -di || (k != di && v[k-1+max] < v[k+1+max]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := v[prevK+max]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			x--
			y--
			script = append(script, Line{OpEqual, oldLines[x]})
		}

		if prevK == k+1 {
			y--
			script = append(script, Line{OpInsert, newLines[y]})
		} else {
			x--
			script = append(script, Line{OpDelete, oldLines[x]})
		}
	}

	for x > 0 && y > 0 {
		x--
		y--
		script = append(script, Line{OpEqual, oldLines[x]})
	}

	for i, j := 0, len(script)-1; i < j; i, j = i+1, j-1 {
		script[i], script[j] = script[j], script[i]
	}
	return script
}

func fallback(oldLines, newLines []string) []Line {
	var result []Line
	for _, l := range oldLines {
		result = append(result, Line{OpDelete, l})
	}
	for _, l := range newLines {
		result = append(result, Line{OpInsert, l})
	}
	return result
}

// Count returns the addition/deletion counts for the diff.
func Count(script []Line) Stats {
	var s Stats
	for _, dl := range script {
		switch dl.Op {
		case OpInsert:
			s.Additions++
		case OpDelete:
			s.Deletions++
		}
	}
	return s
}

// WithContext filters a diff script to show only hunks with changes,
// surrounded by up to contextLines of equal lines. Collapsed regions are
// represented by a single OpEqual line with text "~~~".
func WithContext(script []Line, contextLines int) []Line {
	if len(script) == 0 {
		return nil
	}

	keep := make([]bool, len(script))
	for i, dl := range script {
		if dl.Op != OpEqual {
			lo := max(0, i-contextLines)
			hi := min(len(script), i+contextLines+1)
			for j := lo; j < hi; j++ {
				keep[j] = true
			}
		}
	}

	allKept := true
	for _, k := range keep {
		if !k {
			allKept = false
			break
		}
	}
	if allKept {
		return script
	}

	var result []Line
	inGap := false
	for i, dl := range script {
		if keep[i] {
			inGap = false
			result = append(result, dl)
		} else if !inGap {
			inGap = true
			result = append(result, Line{OpEqual, "~~~"})
		}
	}
	return result
}
