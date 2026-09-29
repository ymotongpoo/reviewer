// Package textutil provides line-oriented text helpers: splitting, line diffs
// and similarity scores used for re-anchoring comments and rendering diffs.
package textutil

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// SplitLines splits content into lines without their terminators. A trailing
// newline does not produce an empty final line, and "\r\n" is treated as a
// single terminator.
func SplitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	content = strings.TrimSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// OpKind is the kind of a diff operation.
type OpKind string

const (
	OpEqual  OpKind = "eq"
	OpDelete OpKind = "del"
	OpInsert OpKind = "ins"
)

// Op is a run of lines sharing the same diff operation.
type Op struct {
	Kind  OpKind   `json:"kind"`
	Lines []string `json:"lines"`
}

// LineDiff computes a line-level diff turning a into b.
func LineDiff(a, b []string) []Op {
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = 0
	ta := joinWithNewlines(a)
	tb := joinWithNewlines(b)
	ra, rb, lineArray := dmp.DiffLinesToRunes(ta, tb)
	diffs := dmp.DiffMainRunes(ra, rb, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	var ops []Op
	for _, d := range diffs {
		var kind OpKind
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			kind = OpEqual
		case diffmatchpatch.DiffDelete:
			kind = OpDelete
		case diffmatchpatch.DiffInsert:
			kind = OpInsert
		}
		lines := SplitLines(d.Text)
		if len(lines) == 0 {
			continue
		}
		if n := len(ops); n > 0 && ops[n-1].Kind == kind {
			ops[n-1].Lines = append(ops[n-1].Lines, lines...)
			continue
		}
		ops = append(ops, Op{Kind: kind, Lines: lines})
	}
	return normalizeOrder(ops)
}

// normalizeOrder makes sure a delete run always precedes an adjacent insert
// run so that callers can treat "del followed by ins" as a replacement.
func normalizeOrder(ops []Op) []Op {
	for i := 0; i+1 < len(ops); i++ {
		if ops[i].Kind == OpInsert && ops[i+1].Kind == OpDelete {
			ops[i], ops[i+1] = ops[i+1], ops[i]
		}
	}
	return ops
}

func joinWithNewlines(lines []string) string {
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// LineMap maps 0-based line indexes of the old text onto the new text.
type LineMap struct {
	// Exact[i] is the new index of old line i when the line is unchanged,
	// or -1 otherwise.
	Exact []int
	// Near[i] is the best-effort new index of old line i: the exact index for
	// unchanged lines, the corresponding line of a replacement block for
	// changed lines, or the position the line used to occupy for deletions.
	Near   []int
	NewLen int
}

// BuildLineMap derives a LineMap from diff ops.
func BuildLineMap(ops []Op, oldLen, newLen int) LineMap {
	m := LineMap{Exact: make([]int, oldLen), Near: make([]int, oldLen), NewLen: newLen}
	oi, ni := 0, 0
	for k := 0; k < len(ops); k++ {
		op := ops[k]
		switch op.Kind {
		case OpEqual:
			for t := range op.Lines {
				m.Exact[oi+t] = ni + t
				m.Near[oi+t] = ni + t
			}
			oi += len(op.Lines)
			ni += len(op.Lines)
		case OpDelete:
			ins := 0
			if k+1 < len(ops) && ops[k+1].Kind == OpInsert {
				ins = len(ops[k+1].Lines)
			}
			for t := range op.Lines {
				m.Exact[oi+t] = -1
				if ins > 0 {
					m.Near[oi+t] = ni + min(t, ins-1)
				} else {
					m.Near[oi+t] = clamp(ni, 0, max(newLen-1, 0))
				}
			}
			oi += len(op.Lines)
		case OpInsert:
			ni += len(op.Lines)
		}
	}
	return m
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Similarity returns a score in [0, 1] describing how similar a and b are,
// computed as 2*common/(len(a)+len(b)) over runes.
func Similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	la, lb := len([]rune(a)), len([]rune(b))
	if la+lb == 0 {
		return 1
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(a, b, false)
	common := 0
	for _, d := range diffs {
		if d.Type == diffmatchpatch.DiffEqual {
			common += len([]rune(d.Text))
		}
	}
	return 2 * float64(common) / float64(la+lb)
}
