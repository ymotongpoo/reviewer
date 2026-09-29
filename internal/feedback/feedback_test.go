package feedback

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "update golden files")

func sampleDoc() *Doc {
	return &Doc{
		Version:      Version,
		Round:        2,
		Project:      "/work/docs",
		SubmittedAt:  time.Date(2026, 9, 29, 10, 30, 0, 0, time.UTC),
		ResponsePath: "/work/docs/.reviewer/rounds/2/response.json",
		Items: []Item{
			{ID: "C-12", Scope: "project", Label: "must", Body: "導入が長いので半分程度に削ってください", Round: 2, Status: "open", Located: true},
			{ID: "C-14", Scope: "line", Path: "ch1.md", Label: "nit", Body: "typo", StartLine: 30, EndLine: 30, Located: true, Quote: []string{"teh"}, Round: 2, Status: "open"},
			{ID: "C-13", Scope: "line", Path: "ch1.md", Label: "suggestion", Body: "言い換え\n\n```suggestion\n置換案\n```", StartLine: 12, EndLine: 14, Located: true, Quote: []string{"元の文章", "", "続き"}, Round: 2, Status: "open"},
			{ID: "C-15", Scope: "file", Path: "ch1.md", Label: "question", Body: "章の目的は？", Located: true, Round: 2, Status: "open"},
			{ID: "C-07", Scope: "line", Path: "ch2.md", Label: "must", Body: "冗長", StartLine: 40, EndLine: 40, Located: true, Quote: []string{"とても長い文"}, Round: 1, Status: "addressed", CarriedOver: true,
				Thread:     []ThreadEntry{{Author: "agent", Round: 1, Status: "addressed", Body: "短くしました"}},
				NewReplies: []string{"まだ冗長です"}},
		},
	}
}

func TestMarkdownGolden(t *testing.T) {
	got := sampleDoc().Markdown()
	golden := filepath.Join("testdata", "feedback.md.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("markdown mismatch; run go test -update\n%s", got)
	}
}

func TestSuggestions(t *testing.T) {
	got := Suggestions("a\n```suggestion\nx\ny\n```\nb\n```suggestion\n```\n")
	want := []string{"x\ny", ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseResponse(t *testing.T) {
	known := map[string]bool{"C-1": true, "C-2": true, "C-3": true}
	in := "```json\n" + `{"round":2,"responses":[
		{"id":"C-1","status":"Addressed","message":"ok"},
		{"id":"C-2","status":"done","message":"?"},
		{"id":"C-9","status":"wontfix"},
		{"id":"C-1","status":"wontfix"}
	],"summary":"s"}` + "\n```"
	r, warnings, err := ParseResponse([]byte(in), 2, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Responses) != 1 || r.Responses[0].Status != "addressed" {
		t.Errorf("responses = %+v", r.Responses)
	}
	if len(warnings) != 5 {
		t.Errorf("warnings = %q", warnings)
	}
	if _, _, err := ParseResponse([]byte("{"), 1, nil); err == nil {
		t.Error("expected error")
	}
}
