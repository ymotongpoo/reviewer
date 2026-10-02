package anchor

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// MaxRangeTextBytes is the largest selected text a range comment may hold.
const MaxRangeTextBytes = 16 << 10

// RangeContextRunes is the number of characters kept before and after a
// range to tell identical selections apart.
const RangeContextRunes = 40

// ErrInvalidRange is returned for ranges outside the text or on lines that
// are not valid UTF-8.
var ErrInvalidRange = errors.New("invalid text range")

// TextRange is the half-open character range [start, end) of a comment.
// Lines are 1-based. Columns are 0-based Unicode code points of the text
// without a byte order mark; the CR of a CRLF line break is not counted.
type TextRange struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
	// Text is the selected text, with lines joined by "\n".
	Text string `json:"text,omitempty"`
	// Before and After are up to RangeContextRunes characters around it.
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// SamePosition reports whether r and o cover the same characters.
func (r TextRange) SamePosition(o TextRange) bool {
	return r.StartLine == o.StartLine && r.StartColumn == o.StartColumn && r.EndLine == o.EndLine && r.EndColumn == o.EndColumn
}

// TextLines returns the lines columns refer to: lines as split by
// textutil.SplitLines, without the byte order mark of the first one.
func TextLines(lines []string) []string {
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "\uFEFF") {
		return lines
	}
	out := cloneLines(lines)
	out[0] = strings.TrimPrefix(out[0], "\uFEFF")
	return out
}

// NormalizeRange checks r against lines (see TextLines) and returns it with
// an end at column 0 of a later line moved to the end of the previous line.
// Text and context are not filled in.
func NormalizeRange(lines []string, r TextRange) (TextRange, error) {
	out := TextRange{StartLine: r.StartLine, StartColumn: r.StartColumn, EndLine: r.EndLine, EndColumn: r.EndColumn}
	if !validRange(lines, out) {
		return TextRange{}, ErrInvalidRange
	}
	if out.EndColumn == 0 && out.EndLine > out.StartLine {
		out.EndLine--
		out.EndColumn = utf8.RuneCountInString(lines[out.EndLine-1])
	}
	return out, nil
}

// validRange reports whether r lies within lines, on valid UTF-8, with its
// start not after its end.
func validRange(lines []string, r TextRange) bool {
	if r.StartLine < 1 || r.EndLine < r.StartLine || r.EndLine > len(lines) {
		return false
	}
	for l := r.StartLine; l <= r.EndLine; l++ {
		if !utf8.ValidString(lines[l-1]) {
			return false
		}
	}
	if r.StartColumn < 0 || r.StartColumn > utf8.RuneCountInString(lines[r.StartLine-1]) ||
		r.EndColumn < 0 || r.EndColumn > utf8.RuneCountInString(lines[r.EndLine-1]) {
		return false
	}
	return r.StartLine < r.EndLine || r.StartColumn <= r.EndColumn
}

// RangeText returns the text of a normalized range.
func RangeText(lines []string, r TextRange) string {
	if r.StartLine == r.EndLine {
		l := lines[r.StartLine-1]
		return l[byteOffset(l, r.StartColumn):byteOffset(l, r.EndColumn)]
	}
	var b strings.Builder
	first := lines[r.StartLine-1]
	b.WriteString(first[byteOffset(first, r.StartColumn):])
	for l := r.StartLine + 1; l < r.EndLine; l++ {
		b.WriteByte('\n')
		b.WriteString(lines[l-1])
	}
	last := lines[r.EndLine-1]
	b.WriteByte('\n')
	b.WriteString(last[:byteOffset(last, r.EndColumn)])
	return b.String()
}

// OnlyLineBreaks reports whether s has no characters other than "\n".
func OnlyLineBreaks(s string) bool {
	return strings.Trim(s, "\n") == ""
}

// WithContext returns r with its text and the characters around it.
func WithContext(lines []string, r TextRange) TextRange {
	t := newFlat(lines)
	s, e := t.offset(r.StartLine, r.StartColumn), t.offset(r.EndLine, r.EndColumn)
	r.Text = t.s[s:e]
	r.Before, r.After = t.context(s, e)
	return r
}

// ResolveRange locates the text of orig in lines (see TextLines). prev is the
// last known position, with the context found there. It is kept when the
// text is still there; otherwise the text is searched for, and when it occurs
// more than once the occurrence whose surroundings best match the context
// wins. ok is false when the text is missing or the match is ambiguous.
func ResolveRange(orig, prev TextRange, lines []string) (TextRange, bool) {
	text := orig.Text
	if text == "" {
		return TextRange{}, false
	}
	t := newFlat(lines)
	at := func(s int) TextRange {
		e := s + len(text)
		sl, sc := t.position(s)
		el, ec := t.position(e)
		r := TextRange{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec}
		r.Before, r.After = t.context(s, e)
		return r
	}

	if validRange(lines, prev) {
		if s := t.offset(prev.StartLine, prev.StartColumn); t.s[s:t.offset(prev.EndLine, prev.EndColumn)] == text {
			return at(s), true
		}
	}

	var candidates []int
	for i := 0; i <= len(t.s)-len(text); {
		j := strings.Index(t.s[i:], text)
		if j < 0 {
			break
		}
		// Columns cannot address text on lines that are not valid UTF-8.
		if validRange(lines, at(i+j)) {
			candidates = append(candidates, i+j)
		}
		_, size := utf8.DecodeRuneInString(t.s[i+j:])
		i += j + size
	}
	switch len(candidates) {
	case 0:
		return TextRange{}, false
	case 1:
		return at(candidates[0]), true
	}
	before, after := prev.Before, prev.After
	if before == "" && after == "" {
		before, after = orig.Before, orig.After
	}
	best, bestScore, tie := -1, -1, false
	for _, c := range candidates {
		b, a := t.context(c, c+len(text))
		score := commonSuffixRunes(b, before) + commonPrefixRunes(a, after)
		switch {
		case score > bestScore:
			best, bestScore, tie = c, score, false
		case score == bestScore:
			tie = true
		}
	}
	if tie || bestScore <= 0 {
		return TextRange{}, false
	}
	return at(best), true
}

// flat is the lines joined by "\n" with the byte offset of each line.
type flat struct {
	s     string
	lines []string
	start []int
}

func newFlat(lines []string) flat {
	t := flat{s: strings.Join(lines, "\n"), lines: lines, start: make([]int, len(lines))}
	n := 0
	for i, l := range lines {
		t.start[i] = n
		n += len(l) + 1
	}
	return t
}

func (t flat) offset(line, col int) int {
	return t.start[line-1] + byteOffset(t.lines[line-1], col)
}

func (t flat) position(off int) (line, col int) {
	i := len(t.start) - 1
	for i > 0 && t.start[i] > off {
		i--
	}
	return i + 1, utf8.RuneCountInString(t.s[t.start[i]:off])
}

func (t flat) context(s, e int) (before, after string) {
	b := s
	for n := 0; n < RangeContextRunes && b > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(t.s[:b])
		b -= size
	}
	a := e
	for n := 0; n < RangeContextRunes && a < len(t.s); n++ {
		_, size := utf8.DecodeRuneInString(t.s[a:])
		a += size
	}
	return t.s[b:s], t.s[e:a]
}

// byteOffset is the byte offset of the col-th code point of s.
func byteOffset(s string, col int) int {
	off := 0
	for n := 0; n < col && off < len(s); n++ {
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
	}
	return off
}

func commonSuffixRunes(a, b string) int {
	n := 0
	for a != "" && b != "" {
		ra, sa := utf8.DecodeLastRuneInString(a)
		rb, sb := utf8.DecodeLastRuneInString(b)
		if ra != rb {
			break
		}
		a, b = a[:len(a)-sa], b[:len(b)-sb]
		n++
	}
	return n
}

func commonPrefixRunes(a, b string) int {
	n := 0
	for a != "" && b != "" {
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if ra != rb {
			break
		}
		a, b = a[sa:], b[sb:]
		n++
	}
	return n
}
