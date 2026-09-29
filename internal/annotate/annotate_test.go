package annotate

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestInstructionsGolden(t *testing.T) {
	got := Instructions(InstructionData{
		RequestID: "Q-3", Prompt: "技術的な誤りを確認してください。",
		Files:      []string{"/work/docs/ch2.md", "/work/docs/ch1.md"},
		OutputPath: "/work/docs/.reviewer/requests/Q-3/annotations.json",
	})
	golden := filepath.Join("testdata", "instructions.md.golden")
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
		t.Errorf("instructions mismatch; run go test -update\n%s", got)
	}
}

func TestParseResponse(t *testing.T) {
	snapshots := map[string]Snapshot{
		"ch.md": {Blob: "blob", Lines: []string{"zero", "correct quote", "tail"}},
	}
	in := "\xef\xbb\xbf```json\n" + `{
  "request":"wrong",
  "annotations":[
    {"path":"ch.md","startLine":2,"endLine":2,"quote":["correct quote"],"severity":"major","confidence":"high","label":"must","body":"valid","evidence":[]},
    {"path":"ch.md","startLine":1,"endLine":1,"quote":["correct quote"],"severity":"minor","confidence":"medium","body":"relocated"},
    {"path":"ch.md","startLine":1,"endLine":1,"quote":["missing"],"severity":"info","confidence":"low","body":"unlocated"},
    {"path":"ch.md","startLine":2,"endLine":2,"quote":["correct quote"],"severity":"major","confidence":"high","label":"must","body":"valid"},
    {"path":"ch.md","startLine":1,"endLine":1,"quote":["zero"],"severity":"unknown","confidence":"high","body":"bad severity"},
    {"path":"ch.md","startLine":1,"endLine":1,"quote":["zero"],"severity":"major","confidence":"certain","body":"bad confidence"}
  ],
  "summary":"summary"
}` + "\n```"
	r, warnings, err := ParseResponse([]byte(in), "Q-3", snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if r.Request != "Q-3" || r.Summary != "summary" || len(r.Annotations) != 3 {
		t.Fatalf("response = %+v", r)
	}
	if !r.Annotations[0].Located || r.Annotations[0].StartLine != 2 {
		t.Errorf("valid = %+v", r.Annotations[0])
	}
	if !r.Annotations[1].Located || r.Annotations[1].StartLine != 2 || r.Annotations[1].ReportedStart != 1 {
		t.Errorf("relocated = %+v", r.Annotations[1])
	}
	if r.Annotations[2].Located {
		t.Errorf("unlocated = %+v", r.Annotations[2])
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"request が wrong", "位置を L2-2", "一致する箇所が見つかりません", "重複", "不明な severity", "不明な confidence"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings %q do not contain %q", warnings, want)
		}
	}
	if _, _, err := ParseResponse([]byte("{"), "Q-1", snapshots); err == nil {
		t.Error("expected parse error")
	}
}
