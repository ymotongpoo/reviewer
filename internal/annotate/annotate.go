// Package annotate renders annotation requests and parses the agent's output.
package annotate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// InstructionData is the input for instructions.md.
type InstructionData struct {
	RequestID  string
	Prompt     string
	Files      []string
	OutputPath string
}

// Instructions renders the file an agent reads for an annotation request.
func Instructions(d InstructionData) []byte {
	files := append([]string(nil), d.Files...)
	sort.Strings(files)
	var b strings.Builder
	fmt.Fprintf(&b, "# AI review request %s\n\n", d.RequestID)
	b.WriteString("## 確認内容\n\n")
	b.WriteString(strings.TrimSpace(d.Prompt))
	b.WriteString("\n\n## 対象ファイル\n\n")
	for _, path := range files {
		fmt.Fprintf(&b, "- `%s`\n", path)
	}
	b.WriteString("\n## 作業規則\n\n")
	b.WriteString("- 対象ファイルを読み、指摘だけを作成してください。対象ファイルや reviewer の管理ファイルは変更しないでください。\n")
	b.WriteString("- 行番号は対象ファイルの現在の内容を基準にした、1から始まる番号です。\n")
	b.WriteString("- `quote` には指摘対象の行を改変せず、行ごとの文字列として必ず入れてください。行番号より `quote` を優先して位置を確認します。\n")
	b.WriteString("- 事実や技術仕様に関する指摘には、根拠となる URL を `evidence` に必ず入れてください。可能なら根拠箇所の引用も添えてください。\n")
	b.WriteString("- 同じ問題を重複して報告しないでください。判断できない項目は無理に指摘せず、必要な場合は `confidence` を `low` にしてください。\n\n")
	b.WriteString(suggestionRules)
	b.WriteString("## 値の定義\n\n")
	b.WriteString("`severity` は次のいずれかです。\n\n")
	b.WriteString("- `critical`: 内容が成り立たない重大な事実誤り、動作しない手順、または重大な危険がある\n")
	b.WriteString("- `major`: 公開前に修正すべき誤りがある\n")
	b.WriteString("- `minor`: 読者の理解や正確さに影響する軽微な誤りがある\n")
	b.WriteString("- `info`: 誤りとは断定できないが、著者による確認が必要である\n\n")
	b.WriteString("`confidence` は `high`、`medium`、`low` のいずれかです。`label` は `must`、`suggestion`、`question`、`nit` のいずれかを提案できます。\n\n")
	b.WriteString("## 出力形式\n\n")
	fmt.Fprintf(&b, "確認後、次の形式の JSON を `%s` に書いてください。コードフェンスは付けないでください。\n\n", d.OutputPath)
	fmt.Fprintf(&b, "```json\n{\n  \"request\": %q,\n  \"annotations\": [\n    {\n      \"path\": \"docs/ch1.md\",\n      \"startLine\": 12,\n      \"endLine\": 14,\n      \"quote\": [\"指摘対象の原文\"],\n      \"severity\": \"major\",\n      \"confidence\": \"high\",\n      \"label\": \"must\",\n      \"body\": \"何が誤っているかと影響を説明します\",\n      \"evidence\": [\n        {\"url\": \"https://example.com/source\", \"quote\": \"根拠となる記述\", \"note\": \"確認方法\"}\n      ],\n      \"edits\": [{\"find\": \"quote の中の直す前の文字列\", \"replace\": \"直した後の文字列\"}]\n    }\n  ],\n  \"summary\": \"全体の所見（任意）\"\n}\n```\n", d.RequestID)
	return []byte(b.String())
}

// suggestionRules explains how replacement proposals are applied. Adopting
// a proposal replaces every quoted line, so a proposal that restates only
// the changed sentence deletes the rest of the line. "~" stands for a
// backtick, which a raw string cannot contain.
var suggestionRules = strings.ReplaceAll(`## 置換案の書き方

置換案は、採用すると ~quote~ の行全体（~startLine~ から ~endLine~ まで）を置き換えます。置換案に書かれていない文や語は削除されます。次のどちらかで書いてください。

- ~edits~（行の一部だけを直すとき。こちらを優先してください）: ~find~ に ~quote~ の中の直す前の文字列を一字一句そのまま、~replace~ に直した後の文字列を書きます。reviewer が ~quote~ に当てはめて行全体の置換案を作ります。複数書けます。~find~ は ~quote~ の中で1箇所に決まる長さにしてください。
- ~suggestion~（行全体を書き直すとき）: ~quote~ の全行を置き換えた後の全文を書きます。直さない文や語も省略せずに含め、行数は原則として ~quote~ と同じにします。

~edits~ と ~suggestion~ は同時に使わないでください。置換案が無い指摘ではどちらも省略します。

例として、1行に3つの文がある次の行の、2文目だけを直す場合を示します。

~~~text
quote: ["OpenTelemetry はベンダー中立です。トレースは3種類のシグナルの1つです。詳しくは次の節で説明します。"]
~~~

- 誤り: ~"suggestion": "トレースは3種類のシグナルのうちの1つです。"~（1文目と3文目が消えます）
- 正しい: ~"edits": [{"find": "3種類のシグナルの1つ", "replace": "3種類のシグナルのうちの1つ"}]~
- 正しい: ~"suggestion": "OpenTelemetry はベンダー中立です。トレースは3種類のシグナルのうちの1つです。詳しくは次の節で説明します。"~

`, "~", "`")

// Snapshot is one target file at request time.
type Snapshot struct {
	Blob  string
	Lines []string
}

// Evidence supports an annotation.
type Evidence struct {
	URL   string `json:"url,omitempty"`
	Quote string `json:"quote,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Annotation is one entry in annotations.json. StartLine and EndLine are
// relocated to the quote when possible; ReportedStart and ReportedEnd retain
// the line numbers written by the agent.
type Annotation struct {
	Path          string     `json:"path"`
	StartLine     int        `json:"startLine"`
	EndLine       int        `json:"endLine"`
	ReportedStart int        `json:"-"`
	ReportedEnd   int        `json:"-"`
	Quote         []string   `json:"quote"`
	Severity      string     `json:"severity"`
	Confidence    string     `json:"confidence"`
	Label         string     `json:"label,omitempty"`
	Body          string     `json:"body"`
	Evidence      []Evidence `json:"evidence"`
	Suggestion    string     `json:"suggestion,omitempty"`
	Edits         []Edit     `json:"edits,omitempty"`
	Located       bool       `json:"-"`
	// SuggestionNote tells the reviewer how the proposal was adjusted.
	SuggestionNote string `json:"-"`
}

// Response is the validated content of annotations.json.
type Response struct {
	Request     string       `json:"request"`
	Annotations []Annotation `json:"annotations"`
	Summary     string       `json:"summary,omitempty"`
}

var severities = map[string]bool{"critical": true, "major": true, "minor": true, "info": true}
var confidences = map[string]bool{"high": true, "medium": true, "low": true}

// ParseResponse parses annotations.json. Invalid and duplicate entries are
// dropped as warnings. BOMs and JSON code fences are tolerated.
func ParseResponse(b []byte, request string, snapshots map[string]Snapshot) (*Response, []string, error) {
	text := strings.TrimSpace(string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	if strings.HasPrefix(text, "```") {
		if i := strings.Index(text, "\n"); i >= 0 {
			text = text[i+1:]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	var r Response
	if err := json.NewDecoder(strings.NewReader(text)).Decode(&r); err != nil {
		return nil, nil, fmt.Errorf("annotations.json を解析できません: %w", err)
	}
	var warnings []string
	if r.Request != "" && r.Request != request {
		warnings = append(warnings, fmt.Sprintf("request が %s になっています（%s のファイルとして扱います）", r.Request, request))
	}
	r.Request = request
	seen := map[string]bool{}
	valid := make([]Annotation, 0, len(r.Annotations))
	for i, entry := range r.Annotations {
		entry.Path = strings.TrimSpace(entry.Path)
		entry.Severity = strings.ToLower(strings.TrimSpace(entry.Severity))
		entry.Confidence = strings.ToLower(strings.TrimSpace(entry.Confidence))
		entry.Label = strings.ToLower(strings.TrimSpace(entry.Label))
		entry.Body = strings.TrimSpace(entry.Body)
		entry.ReportedStart, entry.ReportedEnd = entry.StartLine, entry.EndLine
		snap, known := snapshots[entry.Path]
		switch {
		case entry.Path == "":
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: path がありません", i))
			continue
		case !known:
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: 対象外の path %q です", i, entry.Path))
			continue
		case !severities[entry.Severity]:
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: 不明な severity %q", i, entry.Severity))
			continue
		case !confidences[entry.Confidence]:
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: 不明な confidence %q", i, entry.Confidence))
			continue
		case entry.Body == "":
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: body がありません", i))
			continue
		case len(entry.Quote) == 0:
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: quote がありません", i))
			continue
		}
		key := entry.Path + "\x00" + strings.Join(entry.Quote, "\n") + "\x00" + entry.Body
		if seen[key] {
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: 指摘が重複しています（最初のものを使います）", i))
			continue
		}
		seen[key] = true
		if entry.StartLine >= 1 && entry.EndLine == entry.StartLine+len(entry.Quote)-1 && entry.EndLine <= len(snap.Lines) && equalLines(snap.Lines[entry.StartLine-1:entry.EndLine], entry.Quote) {
			entry.Located = true
		} else if at := findClosest(snap.Lines, entry.Quote, entry.StartLine); at >= 0 {
			entry.StartLine, entry.EndLine = at+1, at+len(entry.Quote)
			entry.Located = true
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: quote に合わせて位置を L%d-%d に移しました", i, entry.StartLine, entry.EndLine))
		} else {
			entry.Located = false
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: quote に一致する箇所が見つかりません", i))
		}
		if entry.Evidence == nil {
			entry.Evidence = []Evidence{}
		}
		note, warn := normalizeSuggestion(&entry)
		entry.SuggestionNote = note
		if warn != "" {
			warnings = append(warnings, fmt.Sprintf("annotations[%d]: %s", i, warn))
		}
		valid = append(valid, entry)
	}
	r.Annotations = valid
	return &r, warnings, nil
}

func findClosest(lines, quote []string, reported int) int {
	best, distance := -1, int(^uint(0)>>1)
	for i := 0; i+len(quote) <= len(lines); i++ {
		if !equalLines(lines[i:i+len(quote)], quote) {
			continue
		}
		d := i + 1 - reported
		if d < 0 {
			d = -d
		}
		if d < distance {
			best, distance = i, d
		}
	}
	return best
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
