package tui

// Line-level diff using the Myers algorithm (O(ND) shortest edit script).
// Produces unified-diff-style output with context lines.

// DiffOp classifies a line in the edit script.
type DiffOp int

const (
	DiffEqual  DiffOp = iota // line appears in both old and new
	DiffDelete               // line removed from old
	DiffInsert               // line added in new
)

// DiffLine is a single entry in the edit script.
type DiffLine struct {
	Op   DiffOp
	Text string
}

// DiffStats holds aggregate counts for a diff.
type DiffStats struct {
	Additions int
	Deletions int
}

// ComputeDiff returns the shortest edit script between old and new line slices
// using the Myers diff algorithm.
func ComputeDiff(oldLines, newLines []string) []DiffLine {
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
				// backtrack to build the edit script
				return backtrack(trace, oldLines, newLines, d, max)
			}
		}
	}

	// fallback: should not reach here
	return fallbackDiff(oldLines, newLines)
}

func backtrack(trace [][]int, oldLines, newLines []string, d, max int) []DiffLine {
	var script []DiffLine
	x := len(oldLines)
	y := len(newLines)

	for di := d; di > 0; di-- {
		// trace[di] = V snapshot taken at the START of step di = V after step di-1
		v := trace[di]
		k := x - y

		// determine which diagonal we came from
		var prevK int
		if k == -di || (k != di && v[k-1+max] < v[k+1+max]) {
			prevK = k + 1 // down move (insert)
		} else {
			prevK = k - 1 // right move (delete)
		}

		prevX := v[prevK+max]
		prevY := prevX - prevK

		// follow diagonals backward (equal lines)
		for x > prevX && y > prevY {
			x--
			y--
			script = append(script, DiffLine{DiffEqual, oldLines[x]})
		}

		// emit the move
		if prevK == k+1 {
			y--
			script = append(script, DiffLine{DiffInsert, newLines[y]})
		} else {
			x--
			script = append(script, DiffLine{DiffDelete, oldLines[x]})
		}
	}

	// remaining diagonal at d=0
	for x > 0 && y > 0 {
		x--
		y--
		script = append(script, DiffLine{DiffEqual, oldLines[x]})
	}

	// reverse since we built it backwards
	for i, j := 0, len(script)-1; i < j; i, j = i+1, j-1 {
		script[i], script[j] = script[j], script[i]
	}
	return script
}

func fallbackDiff(oldLines, newLines []string) []DiffLine {
	var result []DiffLine
	for _, l := range oldLines {
		result = append(result, DiffLine{DiffDelete, l})
	}
	for _, l := range newLines {
		result = append(result, DiffLine{DiffInsert, l})
	}
	return result
}

// Stats returns the addition/deletion counts for the diff.
func Stats(script []DiffLine) DiffStats {
	var s DiffStats
	for _, dl := range script {
		switch dl.Op {
		case DiffInsert:
			s.Additions++
		case DiffDelete:
			s.Deletions++
		}
	}
	return s
}

// WithContext filters a diff script to show only hunks with changes,
// surrounded by up to contextLines of equal lines. Collapsed regions
// are represented by a single DiffEqual line with text "".
func WithContext(script []DiffLine, contextLines int) []DiffLine {
	if len(script) == 0 {
		return nil
	}

	// mark which lines are "near" a change
	keep := make([]bool, len(script))
	for i, dl := range script {
		if dl.Op != DiffEqual {
			lo := max(0, i-contextLines)
			hi := min(len(script), i+contextLines+1)
			for j := lo; j < hi; j++ {
				keep[j] = true
			}
		}
	}

	// all lines are context (no changes) -- return as-is
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

	var result []DiffLine
	inGap := false
	for i, dl := range script {
		if keep[i] {
			inGap = false
			result = append(result, dl)
		} else if !inGap {
			inGap = true
			result = append(result, DiffLine{DiffEqual, "~~~"})
		}
	}
	return result
}
