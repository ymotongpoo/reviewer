package annotate

import (
	"reflect"
	"strings"
	"testing"
)

const jaLine = "OpenTelemetry はベンダー中立です。トレースは3種類のシグナルの1つです。詳しくは次の節で説明します。"

func TestSegments(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []string
	}{
		{jaLine, []string{"OpenTelemetry はベンダー中立です。", "トレースは3種類のシグナルの1つです。", "詳しくは次の節で説明します。"}},
		{"It works. See docs.example.com for more. Done", []string{"It works. ", "See docs.example.com for more. ", "Done"}},
		{"「はい。」と言った。次へ", []string{"「はい。」", "と言った。", "次へ"}},
		{"一行目\n二行目。三文目。", []string{"一行目\n", "二行目。", "三文目。"}},
	} {
		got := segments(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("segments(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if strings.Join(got, "") != tt.in {
			t.Errorf("segments(%q) do not join back", tt.in)
		}
	}
}

func TestApplyEdits(t *testing.T) {
	got, err := applyEdits(jaLine, []Edit{{Find: "3種類のシグナルの1つ", Replace: "3種類のシグナルのうちの1つ"}})
	want := "OpenTelemetry はベンダー中立です。トレースは3種類のシグナルのうちの1つです。詳しくは次の節で説明します。"
	if err != nil || got != want {
		t.Errorf("single edit = %q, %v", got, err)
	}
	got, err = applyEdits("a b\nc d", []Edit{{Find: "b\nc", Replace: "B\nC"}, {Find: "d", Replace: "D"}})
	if err != nil || got != "a B\nC D" {
		t.Errorf("multi-line edits = %q, %v", got, err)
	}
	if _, err := applyEdits(jaLine, []Edit{{Find: "存在しない", Replace: "x"}}); err == nil {
		t.Error("missing find accepted")
	}
	if _, err := applyEdits("です。です。", []Edit{{Find: "です", Replace: "だ"}}); err == nil || !strings.Contains(err.Error(), "2 箇所") {
		t.Errorf("ambiguous find: %v", err)
	}
	if _, err := applyEdits(jaLine, []Edit{{Find: "", Replace: "x"}}); err == nil {
		t.Error("empty find accepted")
	}
}

func TestNormalizeSuggestion(t *testing.T) {
	fixed := "OpenTelemetry はベンダー中立です。トレースは3種類のシグナルのうちの1つです。詳しくは次の節で説明します。"
	tests := []struct {
		name           string
		in             Annotation
		wantSuggestion string
		wantNote       bool
		wantBody       string // substring
	}{
		{
			name:           "edits build the whole line",
			in:             Annotation{Quote: []string{jaLine}, Edits: []Edit{{Find: "3種類のシグナルの1つ", Replace: "3種類のシグナルのうちの1つ"}}},
			wantSuggestion: fixed,
		},
		{
			name:           "edits win over suggestion",
			in:             Annotation{Quote: []string{jaLine}, Suggestion: "x", Edits: []Edit{{Find: "3種類のシグナルの1つ", Replace: "3種類のシグナルのうちの1つ"}}},
			wantSuggestion: fixed,
		},
		{
			name:     "unusable edits move to the body",
			in:       Annotation{Quote: []string{jaLine}, Body: "誤訳", Edits: []Edit{{Find: "無い", Replace: "x"}}},
			wantNote: true, wantBody: "「無い」→「x」",
		},
		{
			name:           "a fragment is spliced into the line (ja)",
			in:             Annotation{Quote: []string{jaLine}, Suggestion: "トレースは3種類のシグナルのうちの1つです。"},
			wantSuggestion: fixed, wantNote: true,
		},
		{
			name:           "a fragment is spliced into the line (en)",
			in:             Annotation{Quote: []string{"Traces are one signal. Metrics are anothr signal. Logs are the third."}, Suggestion: "Metrics are another signal."},
			wantSuggestion: "Traces are one signal. Metrics are another signal. Logs are the third.", wantNote: true,
		},
		{
			name:           "a fragment of a multi-line quote replaces one line",
			in:             Annotation{Quote: []string{"一行目です", "二行目にはタイポがあるます", "三行目です"}, Suggestion: "二行目にはタイポがあります"},
			wantSuggestion: "一行目です\n二行目にはタイポがあります\n三行目です", wantNote: true,
		},
		{
			name:     "an unrelated fragment is not applied",
			in:       Annotation{Quote: []string{jaLine}, Body: "誤訳", Suggestion: "まったく別の短い文。"},
			wantNote: true, wantBody: "修正案（該当部分のみ）",
		},
		{
			name:           "a whole-line suggestion is kept",
			in:             Annotation{Quote: []string{jaLine}, Suggestion: fixed},
			wantSuggestion: fixed,
		},
		{
			name:           "a single-sentence quote is not second-guessed",
			in:             Annotation{Quote: []string{"長い一文だけの行で、置換案が短くなってもそれが意図である場合です。"}, Suggestion: "短い行です。"},
			wantSuggestion: "短い行です。",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := tt.in
			note, _ := normalizeSuggestion(&a)
			if a.Suggestion != tt.wantSuggestion {
				t.Errorf("suggestion = %q, want %q", a.Suggestion, tt.wantSuggestion)
			}
			if (note != "") != tt.wantNote {
				t.Errorf("note = %q", note)
			}
			if tt.wantBody != "" && !strings.Contains(a.Body, tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", a.Body, tt.wantBody)
			}
		})
	}
}

func TestParseResponseNormalizesSuggestion(t *testing.T) {
	snaps := map[string]Snapshot{"a.md": {Lines: []string{"# t", jaLine}}}
	in := `{"annotations":[{"path":"a.md","startLine":2,"endLine":2,"quote":["` + jaLine + `"],"severity":"minor","confidence":"high","body":"誤訳","suggestion":"トレースは3種類のシグナルのうちの1つです。"}]}`
	r, warnings, err := ParseResponse([]byte(in), "Q-1", snaps)
	if err != nil {
		t.Fatal(err)
	}
	a := r.Annotations[0]
	if !strings.HasPrefix(a.Suggestion, "OpenTelemetry はベンダー中立です。") || a.SuggestionNote == "" {
		t.Errorf("annotation = %+v", a)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "補完") {
		t.Errorf("warnings = %q", warnings)
	}
}
