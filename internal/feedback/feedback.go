// Package feedback renders review rounds for agents and parses their
// responses.
package feedback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"
)

// Version is the feedback.json schema version.
const Version = 1

// ThreadEntry is a past message in a comment thread.
type ThreadEntry struct {
	Author string `json:"author"`
	Round  int    `json:"round"`
	Status string `json:"status,omitempty"`
	Body   string `json:"body"`
}

// Item is a comment as presented to the agent.
type Item struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	Path  string `json:"path,omitempty"`
	Label string `json:"label"`
	Body  string `json:"body"`
	// StartLine and EndLine refer to the submitted snapshot.
	StartLine int `json:"startLine,omitempty"`
	EndLine   int `json:"endLine,omitempty"`
	// Located is false when the commented text could not be found anymore;
	// the line numbers are then those of the original snapshot.
	Located bool `json:"located"`
	// Range is set for a comment on part of the lines.
	Range       *Range        `json:"range,omitempty"`
	Quote       []string      `json:"quote,omitempty"`
	Suggestions []string      `json:"suggestions,omitempty"`
	Round       int           `json:"round"`
	Status      string        `json:"status"`
	CarriedOver bool          `json:"carriedOver"`
	Thread      []ThreadEntry `json:"thread,omitempty"`
	// NewReplies are the human replies added in this round.
	NewReplies []string `json:"newReplies,omitempty"`
}

// Range is the characters a comment refers to, as the half-open range
// [start, end). Columns are 0-based Unicode code points, not counting a byte
// order mark or the CR of a CRLF line break.
type Range struct {
	StartLine   int    `json:"startLine"`
	StartColumn int    `json:"startColumn"`
	EndLine     int    `json:"endLine"`
	EndColumn   int    `json:"endColumn"`
	Text        string `json:"text"`
}

// Doc is a whole round of feedback.
type Doc struct {
	Version      int       `json:"version"`
	Round        int       `json:"round"`
	Project      string    `json:"project"`
	SubmittedAt  time.Time `json:"submittedAt"`
	ResponsePath string    `json:"responsePath"`
	Items        []Item    `json:"comments"`
}

var suggestionRe = regexp.MustCompile("(?s)(?:^|\\n)```suggestion[^\\n]*\\n(.*?)\\n?```")

// Suggestions extracts the contents of ```suggestion blocks from body.
func Suggestions(body string) []string {
	var out []string
	for _, m := range suggestionRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// IDs returns the IDs of all items.
func (d *Doc) IDs() []string {
	out := make([]string, len(d.Items))
	for i, it := range d.Items {
		out[i] = it.ID
	}
	return out
}

// JSON renders feedback.json.
func (d *Doc) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

const maxQuoteLines = 15

// Markdown renders feedback.md.
func (d *Doc) Markdown() []byte {
	var b bytes.Buffer
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("# Review round %d\n\n", d.Round)
	w("- 対象: `%s`\n", d.Project)
	w("- 提出: %s\n", d.SubmittedAt.Format(time.RFC3339))
	w("- 返答先: `%s`（形式は末尾の「返答の書き方」を参照）\n\n", d.ResponsePath)
	w("行番号はこのラウンドを提出した時点のファイルのものです。引用（`>` の行）を手がかりに該当箇所を探してください。\n")
	if slices.ContainsFunc(d.Items, func(it Item) bool { return it.Range != nil }) {
		w("`L3:5-4:2` のように列を含む位置は、行の一部へのコメントです（列は1から数えた文字位置で、終端の文字を含みます）。対象の文字列は「対象の文字列」に示します。\n")
	}
	w("言語名が `suggestion` のコードブロックは、指定した行範囲をブロックの内容で置き換える提案です。\n\n")

	var project, carried []Item
	files := map[string][]Item{}
	for _, it := range d.Items {
		switch {
		case it.CarriedOver:
			carried = append(carried, it)
		case it.Scope == "project":
			project = append(project, it)
		default:
			files[it.Path] = append(files[it.Path], it)
		}
	}

	if len(project) > 0 {
		w("## 全体コメント\n\n")
		for _, it := range project {
			w("### [%s][%s]\n\n", it.ID, it.Label)
			writeBody(&b, it.Body)
		}
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		items := files[p]
		sort.SliceStable(items, func(i, j int) bool { return lineKey(items[i]) < lineKey(items[j]) })
		w("## %s\n\n", p)
		for _, it := range items {
			w("### [%s][%s] %s\n\n", it.ID, it.Label, location(it))
			writeQuote(&b, it.Quote)
			writeRangeText(&b, it.Range)
			writeBody(&b, it.Body)
		}
	}

	if len(carried) > 0 {
		w("## 前ラウンドから持ち越した未解決コメント\n\n")
		w("以前のラウンドで指摘し、まだ解決していないコメントです。これまでのやりとりを踏まえて対応してください。\n\n")
		for _, it := range carried {
			target := "全体"
			if it.Scope != "project" {
				target = it.Path + " " + location(it)
			}
			w("### [%s][%s] %s（ラウンド%dの指摘・現在の状態: %s）\n\n", it.ID, it.Label, target, it.Round, it.Status)
			writeQuote(&b, it.Quote)
			writeRangeText(&b, it.Range)
			writeBody(&b, it.Body)
			for _, t := range it.Thread {
				who := "レビュアー"
				if t.Author == "agent" {
					who = "エージェント"
				}
				status := ""
				if t.Status != "" {
					status = "（" + t.Status + "）"
				}
				w("- ラウンド%d %s%s: %s\n", t.Round, who, status, oneLine(t.Body))
			}
			for _, r := range it.NewReplies {
				w("- **今回のレビュアーの返信**: %s\n", oneLine(r))
			}
			w("\n")
		}
	}

	w("## 返答の書き方\n\n")
	w("修正が終わったら、次の形式の JSON を `%s` に書いてください。上のすべてのコメントIDについて1件ずつ返答してください。このファイル（feedback.md）は編集しないでください。\n\n", d.ResponsePath)
	w("- `status`: `addressed`（対応した）/ `wontfix`（対応しない。理由を `message` に書く）/ `question`（確認したいことがある。質問を `message` に書く）\n")
	w("- `message`: 何をどう直したか、または対応しない理由や質問\n")
	w("- `summary`: 変更全体の概要（任意）\n\n")
	example := struct {
		Round     int             `json:"round"`
		Responses []ResponseEntry `json:"responses"`
		Summary   string          `json:"summary"`
	}{Round: d.Round, Summary: "変更全体の概要"}
	for _, id := range d.IDs() {
		example.Responses = append(example.Responses, ResponseEntry{ID: id, Status: "addressed", Message: "…"})
	}
	ex, _ := json.MarshalIndent(example, "", "  ")
	w("```json\n%s\n```\n", ex)
	return b.Bytes()
}

func lineKey(it Item) int {
	if it.Scope == "file" {
		return -1
	}
	return it.StartLine
}

func location(it Item) string {
	switch {
	case it.Scope == "file":
		return "ファイル全体"
	case it.Scope == "project":
		return ""
	case !it.Located:
		return fmt.Sprintf("L%s（元の位置。該当箇所が見つかりません）", lineRange(it))
	default:
		return "L" + lineRange(it)
	}
}

func lineRange(it Item) string {
	if r := it.Range; r != nil {
		// 1-based columns of the first and the last character.
		return fmt.Sprintf("%d:%d-%d:%d", r.StartLine, r.StartColumn+1, r.EndLine, r.EndColumn)
	}
	if it.StartLine == it.EndLine {
		return fmt.Sprint(it.StartLine)
	}
	return fmt.Sprintf("%d-%d", it.StartLine, it.EndLine)
}

func writeQuote(b *bytes.Buffer, lines []string) {
	if len(lines) == 0 {
		return
	}
	shown := lines
	omitted := 0
	if len(lines) > maxQuoteLines {
		shown = append(append([]string{}, lines[:10]...), lines[len(lines)-3:]...)
		omitted = len(lines) - 13
	}
	for i, l := range shown {
		if omitted > 0 && i == 10 {
			fmt.Fprintf(b, "> …（%d行省略）\n", omitted)
		}
		if l == "" {
			b.WriteString(">\n")
		} else {
			b.WriteString("> " + l + "\n")
		}
	}
	b.WriteString("\n")
}

func writeRangeText(b *bytes.Buffer, r *Range) {
	if r == nil {
		return
	}
	b.WriteString("対象の文字列:\n\n")
	writeQuote(b, strings.Split(r.Text, "\n"))
}

func writeBody(b *bytes.Buffer, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "（本文なし）"
	}
	b.WriteString(body + "\n\n")
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	return strings.ReplaceAll(s, "\n", " / ")
}

// PromptData is the input of the instruction template.
type PromptData struct {
	Round            int
	FeedbackPath     string
	FeedbackJSONPath string
	ResponsePath     string
	Project          string
}

// Prompt renders the instruction for the agent.
func Prompt(tmpl string, d PromptData) (string, error) {
	t, err := template.New("prompt").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}
