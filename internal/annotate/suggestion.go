package annotate

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ymotongpoo/reviewer/internal/textutil"
)

// Edit replaces one exact string inside an annotation's quote. Agents use it
// to fix part of a line without restating the rest of the line.
type Edit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

const (
	// partialRatio: a suggestion shorter than this share of the quote, when
	// the quote has several sentences or lines, is likely only a fragment.
	partialRatio = 0.6
	// repairThreshold is the minimum similarity between a fragment and the
	// span of the quote it is taken to replace.
	repairThreshold = 0.5
)

// applyEdits applies edits in order to text. Each Find must occur exactly
// once in the text at the time it is applied.
func applyEdits(text string, edits []Edit) (string, error) {
	for i, e := range edits {
		if e.Find == "" {
			return "", fmt.Errorf("edits[%d]: find が空です", i)
		}
		switch n := strings.Count(text, e.Find); {
		case n == 0:
			return "", fmt.Errorf("edits[%d]: find %q が quote の中にありません", i, e.Find)
		case n > 1:
			return "", fmt.Errorf("edits[%d]: find %q が quote の中に %d 箇所あります（一意に決まる長さにしてください）", i, e.Find, n)
		}
		text = strings.Replace(text, e.Find, e.Replace, 1)
	}
	return text, nil
}

// segments splits text into sentences and lines. Joining the result gives
// text back: terminators, closing brackets, following spaces and newlines
// stay with the segment they end.
func segments(text string) []string {
	var out []string
	var cur strings.Builder
	runes := []rune(text)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		cur.WriteRune(r)
		end := false
		switch {
		case r == '\n':
			end = true
		case strings.ContainsRune("。．！？!?", r):
			end = true
		case r == '.':
			// An English sentence ends at ". " or at the end of the text.
			end = i+1 == len(runes) || unicode.IsSpace(runes[i+1])
		}
		if !end {
			continue
		}
		// Keep closing brackets and quotes, then trailing spaces, with this
		// sentence.
		for i+1 < len(runes) && strings.ContainsRune("」』）)]\"'”’", runes[i+1]) {
			i++
			cur.WriteRune(runes[i])
		}
		for r != '\n' && i+1 < len(runes) && runes[i+1] != '\n' && unicode.IsSpace(runes[i+1]) {
			i++
			cur.WriteRune(runes[i])
		}
		flush()
	}
	flush()
	return out
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// looksPartial reports whether suggestion seems to cover only part of quote.
func looksPartial(quote, suggestion string) bool {
	if strings.TrimSpace(suggestion) == "" {
		return false
	}
	return float64(runeLen(strings.TrimSpace(suggestion))) < partialRatio*float64(runeLen(strings.TrimSpace(quote))) &&
		len(segments(quote)) >= 2
}

// splitSpace returns the leading space, the core and the trailing space.
func splitSpace(s string) (lead, core, trail string) {
	core = strings.TrimLeftFunc(s, unicode.IsSpace)
	lead = s[:len(s)-len(core)]
	trimmed := strings.TrimRightFunc(core, unicode.IsSpace)
	trail = core[len(trimmed):]
	return lead, trimmed, trail
}

// repairPartial treats suggestion as a replacement for the contiguous run of
// sentences in quote that it resembles most, and returns quote with only
// that run replaced.
func repairPartial(quote, suggestion string) (string, bool) {
	segs := segments(quote)
	if len(segs) < 2 {
		return "", false
	}
	target := strings.TrimSpace(suggestion)
	bestScore, bestI, bestJ := -1.0, -1, -1
	for i := range segs {
		for j := i; j < len(segs); j++ {
			if i == 0 && j == len(segs)-1 {
				break // the whole quote is not a partial replacement
			}
			_, core, _ := splitSpace(strings.Join(segs[i:j+1], ""))
			score := textutil.Similarity(core, target)
			if score > bestScore || (score == bestScore && j-i < bestJ-bestI) {
				bestScore, bestI, bestJ = score, i, j
			}
		}
	}
	if bestI < 0 || bestScore < repairThreshold {
		return "", false
	}
	lead, _, trail := splitSpace(strings.Join(segs[bestI:bestJ+1], ""))
	return strings.Join(segs[:bestI], "") + lead + target + trail + strings.Join(segs[bestJ+1:], ""), true
}

// normalizeSuggestion turns the agent's replacement proposal into a
// suggestion that replaces all quoted lines, so that adopting it never
// drops text the agent did not mean to change. It returns a note for the
// reviewer when the proposal was changed, and a warning for the import log.
func normalizeSuggestion(a *Annotation) (note, warning string) {
	quote := strings.Join(a.Quote, "\n")
	if len(a.Edits) > 0 {
		if a.Suggestion != "" {
			warning = "suggestion と edits の両方があるため edits を使いました"
		}
		out, err := applyEdits(quote, a.Edits)
		if err != nil {
			a.Suggestion = ""
			a.Body = appendEditsToBody(a.Body, a.Edits)
			return "AIの修正箇所（edits）を行に当てはめられなかったため、置換案にしていません。本文の修正案を確認してください。", joinWarn(warning, err.Error())
		}
		a.Suggestion = out
		return "", warning
	}
	if a.Suggestion == "" || !looksPartial(quote, a.Suggestion) {
		return "", ""
	}
	partial := strings.TrimSpace(a.Suggestion)
	if out, ok := repairPartial(quote, partial); ok {
		a.Suggestion = out
		return fmt.Sprintf("AIの置換案は行の一部（「%s」）だけでした。ほかの文が消えないよう、行全体の置換案に補完しています。", ellipsis(partial, 60)),
			"suggestion が行の一部だけだったため、行全体の置換案に補完しました"
	}
	a.Suggestion = ""
	a.Body = strings.TrimSpace(a.Body) + "\n\n修正案（該当部分のみ）\n\n> " + strings.ReplaceAll(partial, "\n", "\n> ")
	return "AIの置換案は行の一部だけで、どこを置き換えるものか判断できなかったため、置換案にしていません。本文の修正案を確認してください。",
		"suggestion が行の一部だけで補完できなかったため、本文に移しました"
}

func appendEditsToBody(body string, edits []Edit) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\n修正案（該当部分のみ）\n")
	for _, e := range edits {
		fmt.Fprintf(&b, "\n- 「%s」→「%s」", e.Find, e.Replace)
	}
	return b.String()
}

func joinWarn(a, b string) string {
	if a == "" {
		return b
	}
	return a + "。" + b
}

func ellipsis(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
