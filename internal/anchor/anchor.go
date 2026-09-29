// Package anchor tracks the position of a line-range comment across edits of
// the underlying file.
package anchor

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/ymotongpoo/reviewer/internal/textutil"
)

// ContextLines is the number of lines captured before and after a range.
const ContextLines = 3

// DefaultThreshold is the minimum similarity accepted for a fuzzy match.
const DefaultThreshold = 0.7

// distancePenalty lowers fuzzy scores of windows far from the expected line.
const distancePenalty = 0.005

// Anchor captures the text of a line range and its surroundings.
type Anchor struct {
	Lines  []string `json:"lines"`
	Before []string `json:"before"`
	After  []string `json:"after"`
	Hash   string   `json:"hash"`
}

// State describes how a range was located in the new content.
type State string

const (
	Exact    State = "exact"    // the same text at the same lines
	Moved    State = "moved"    // the same text found elsewhere
	Fuzzy    State = "fuzzy"    // similar text found near the expected place
	Outdated State = "outdated" // not found
)

// New builds an anchor for the 1-based inclusive range [start, end] of lines.
func New(lines []string, start, end int) Anchor {
	s, e := start-1, end
	a := Anchor{
		Lines:  cloneLines(lines[s:e]),
		Before: cloneLines(lines[max(0, s-ContextLines):s]),
		After:  cloneLines(lines[e:min(len(lines), e+ContextLines)]),
	}
	a.Hash = Hash(a.Lines)
	return a
}

// Hash returns a short content hash for lines.
func Hash(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
}

func cloneLines(l []string) []string {
	out := make([]string, len(l))
	copy(out, l)
	return out
}

// Result is the outcome of resolving an anchor against new content.
type Result struct {
	Start int   `json:"start"`
	End   int   `json:"end"`
	State State `json:"state"`
	// Score is the similarity for fuzzy matches.
	Score float64 `json:"score,omitempty"`
}

// Resolve locates anchor a, previously at [oldStart, oldEnd] (1-based) in
// oldLines, within newLines. oldLines may be nil when the previous content is
// unknown. When the anchor cannot be located the old range is returned with
// state Outdated.
func Resolve(a Anchor, oldLines []string, oldStart, oldEnd int, newLines []string, threshold float64) Result {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	n := len(a.Lines)
	outdated := Result{Start: oldStart, End: oldEnd, State: Outdated}
	if n == 0 {
		return outdated
	}

	// 1. Same text at the same place.
	if oldStart >= 1 && oldStart-1+n <= len(newLines) && equalLines(newLines[oldStart-1:oldStart-1+n], a.Lines) {
		return Result{Start: oldStart, End: oldStart + n - 1, State: Exact}
	}

	// Where do we expect the range to be now?
	expected := oldStart - 1
	var lm *textutil.LineMap
	if oldLines != nil && oldStart >= 1 && oldEnd <= len(oldLines) {
		m := textutil.BuildLineMap(textutil.LineDiff(oldLines, newLines), len(oldLines), len(newLines))
		lm = &m
		if len(newLines) > 0 {
			expected = m.Near[oldStart-1]
		}
	}

	// 2. Same text elsewhere, preferring matching context and proximity.
	candidates := findExact(newLines, a.Lines)
	if len(candidates) > 0 {
		best, bestScore := -1, -1
		for _, c := range candidates {
			s := contextScore(newLines, c, n, a)
			if s > bestScore || (s == bestScore && abs(c-expected) < abs(best-expected)) {
				best, bestScore = c, s
			}
		}
		if len(candidates) == 1 || bestScore > 0 || lm != nil {
			return Result{Start: best + 1, End: best + n, State: Moved}
		}
	}

	// 3. Fuzzy match around the expected location.
	if len(newLines) == 0 {
		return outdated
	}
	target := strings.Join(a.Lines, "\n")
	type window struct{ s, e int }
	var windows []window
	if lm != nil {
		ms := lm.Near[oldStart-1]
		me := lm.Near[oldEnd-1]
		if me < ms {
			me = ms
		}
		windows = append(windows, window{ms, me})
	}
	lo := max(0, expected-20)
	hi := min(len(newLines)-1, expected+20)
	for s := lo; s <= hi; s++ {
		for _, size := range []int{n - 1, n, n + 1} {
			if size < 1 {
				continue
			}
			e := s + size - 1
			if e < len(newLines) {
				windows = append(windows, window{s, e})
			}
		}
	}
	best := Result{State: Outdated}
	for _, w := range windows {
		if w.s < 0 || w.e >= len(newLines) {
			continue
		}
		score := textutil.Similarity(target, strings.Join(newLines[w.s:w.e+1], "\n"))
		score -= distancePenalty * float64(abs(w.s-expected))
		if score > best.Score || (score == best.Score && best.State != Outdated && abs(w.s-expected) < abs(best.Start-1-expected)) {
			best = Result{Start: w.s + 1, End: w.e + 1, State: Fuzzy, Score: score}
		}
	}
	if best.State == Fuzzy && best.Score >= threshold {
		return best
	}
	return outdated
}

func findExact(hay, needle []string) []int {
	var out []int
	n := len(needle)
	for i := 0; i+n <= len(hay); i++ {
		if equalLines(hay[i:i+n], needle) {
			out = append(out, i)
		}
	}
	return out
}

func contextScore(lines []string, start, n int, a Anchor) int {
	score := 0
	for k := 1; k <= len(a.Before); k++ {
		i := start - k
		if i < 0 {
			break
		}
		if strings.TrimSpace(lines[i]) == strings.TrimSpace(a.Before[len(a.Before)-k]) {
			score++
		}
	}
	for k := 0; k < len(a.After); k++ {
		i := start + n + k
		if i >= len(lines) {
			break
		}
		if strings.TrimSpace(lines[i]) == strings.TrimSpace(a.After[k]) {
			score++
		}
	}
	return score
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
