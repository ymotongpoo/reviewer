// Package pdfimport locates text extracted from PDF highlights in Markdown.
package pdfimport

import (
	"bytes"
	"html"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ymotongpoo/reviewer/internal/anchor"
	"github.com/ymotongpoo/reviewer/internal/textutil"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

type Highlight struct {
	ID      string `json:"id"`
	Page    int    `json:"page"`
	Body    string `json:"body"`
	Quote   string `json:"quote"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
	Warning string `json:"warning,omitempty"`
}

type Candidate struct {
	Path  string           `json:"path"`
	Hash  string           `json:"hash"`
	Range anchor.TextRange `json:"range"`
}

// Index retains original byte offsets for every normalized character. Markdown
// syntax is removed by parsing, never by modifying the manuscript.
type Index struct {
	path, hash, source string
	chars              []rune
	starts, ends       []int
	lines              []int
	searchText         string
	textLines          []string
}

func NewIndex(path, hash string, source []byte) *Index {
	x := &Index{path: path, hash: hash, source: string(source), lines: []int{0}}
	for i, b := range source {
		if b == '\n' {
			x.lines = append(x.lines, i+1)
		}
	}
	parseSource := bytes.Clone(source)
	// Hide front matter without changing offsets.
	start := 0
	if bytes.HasPrefix(parseSource, []byte("\xef\xbb\xbf")) {
		start = 3
		copy(parseSource[:3], "   ")
	}
	if bytes.HasPrefix(parseSource[start:], []byte("---\n")) || bytes.HasPrefix(parseSource[start:], []byte("---\r\n")) {
		for _, offset := range x.lines[1:] {
			end := bytes.IndexByte(parseSource[offset:], '\n')
			if end < 0 {
				end = len(parseSource) - offset
			}
			if strings.TrimSpace(string(parseSource[offset:offset+end])) == "---" {
				for i := start; i < offset+end; i++ {
					if parseSource[i] != '\n' && parseSource[i] != '\r' {
						parseSource[i] = ' '
					}
				}
				break
			}
		}
	}
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(parseSource))
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Type() == ast.TypeBlock {
			x.barrier()
		}
		switch n := n.(type) {
		case *ast.Image, *ast.RawHTML, *ast.HTMLBlock:
			x.barrier()
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			x.appendSegment(parseSource, n.Segment.Start, n.Segment.Stop, n.IsRaw())
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			for i := 0; i < n.Lines().Len(); i++ {
				s := n.Lines().At(i)
				x.appendSegment(parseSource, s.Start, s.Stop, true)
			}
		case *ast.String:
			// Synthetic text has no source offsets; never join across it.
			x.barrier()
		}
		return ast.WalkContinue, nil
	})
	x.searchText = string(x.chars)
	x.textLines = anchor.TextLines(textutil.SplitLines(x.source))
	return x
}

func (x *Index) barrier() {
	x.chars = append(x.chars, 0)
	x.starts = append(x.starts, 0)
	x.ends = append(x.ends, 0)
}

func normalized(r rune) string {
	if unicode.IsSpace(r) || r == '\u00ad' || r == '\ufeff' {
		return ""
	}
	switch r {
	case 'ﬀ':
		return "ff"
	case 'ﬁ':
		return "fi"
	case 'ﬂ':
		return "fl"
	case 'ﬃ':
		return "ffi"
	case 'ﬄ':
		return "ffl"
	}
	return string(r)
}

func Normalize(s string) []rune {
	var b strings.Builder
	for _, r := range s {
		b.WriteString(normalized(r))
	}
	return []rune(b.String())
}

func (x *Index) appendSegment(src []byte, start, end int, raw bool) {
	for i := start; i < end; {
		from := i
		r, size := utf8.DecodeRune(src[i:end])
		i += size
		value := string(r)
		if !raw && r == '\\' && i < end && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(src[i])) {
			value = string(src[i])
			i++
		} else if !raw && r == '&' {
			if at := bytes.IndexByte(src[i:min(end, i+32)], ';'); at >= 0 {
				token := string(src[from : i+at+1])
				if decoded := html.UnescapeString(token); decoded != token {
					value = decoded
					i += at + 1
				}
			}
		}
		for _, c := range Normalize(value) {
			x.chars = append(x.chars, c)
			x.starts = append(x.starts, from)
			x.ends = append(x.ends, i)
		}
	}
}

type match struct {
	candidate Candidate
	score     int
}

// Find returns bounded candidates. Automatic selection requires a single
// occurrence, or uniquely matching surrounding text, across the whole scope.
// A truncated candidate list is never automatically selected.
func Find(indexes []*Index, h Highlight) (candidates []Candidate, automatic bool, truncated bool) {
	candidates = []Candidate{}
	q, before, after := Normalize(h.Quote), Normalize(h.Before), Normalize(h.After)
	if len(q) == 0 || strings.ContainsRune(h.Quote, 0) || h.Warning != "" {
		return
	}
	var matches []match
	for _, x := range indexes {
		// Search with byte offsets into the normalized UTF-8 string, then map
		// matches back to rune indices and finally the original source.
		haystack, needle := x.searchText, string(q)
		at, runeAt := 0, 0
		for at <= len(haystack)-len(needle) {
			delta := strings.Index(haystack[at:], needle)
			if delta < 0 {
				break
			}
			s := runeAt + utf8.RuneCountInString(haystack[at:at+delta])
			e := s + len(q)
			start, end := x.starts[s], x.ends[e-1]
			if end-start <= anchor.MaxRangeTextBytes {
				sl, sc := x.position(start)
				el, ec := x.position(end)
				r := anchor.TextRange{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec}
				r = anchor.WithContext(x.textLines, r)
				score := 0
				for k := 1; k <= len(before) && k <= s && before[len(before)-k] == x.chars[s-k]; k++ {
					score++
				}
				for k := 0; k < len(after) && e+k < len(x.chars) && after[k] == x.chars[e+k]; k++ {
					score++
				}
				matches = append(matches, match{Candidate{x.path, x.hash, r}, score})
				if len(matches) > 100 {
					truncated = true
					break
				}
			}
			_, size := utf8.DecodeRuneInString(haystack[at+delta:])
			at += delta + size
			runeAt = s + 1
		}
		if truncated {
			break
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	automatic = !truncated && len(q) >= 4 && (len(matches) == 1 || len(matches) > 1 && matches[0].score >= 12 && matches[0].score > matches[1].score)
	if len(matches) > 10 {
		truncated = true
	}
	if truncated {
		automatic = false
	}
	for _, m := range matches[:min(10, len(matches))] {
		candidates = append(candidates, m.candidate)
	}
	return
}

func (x *Index) position(offset int) (line, column int) {
	i := sort.Search(len(x.lines), func(i int) bool { return x.lines[i] > offset }) - 1
	column = utf8.RuneCountInString(x.source[x.lines[i]:offset])
	if i == 0 && strings.HasPrefix(x.source, "\ufeff") {
		column--
	}
	return i + 1, column
}
