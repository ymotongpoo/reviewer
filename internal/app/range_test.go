package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/anchor"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/store"
)

func rangeComment(a *App, path, hash string, sl, sc, el, ec int, text string) (*store.Comment, error) {
	return a.CreateComment(NewComment{
		Scope: "line", Path: path, Label: "must", Body: "x", Hash: hash,
		Range: &anchor.TextRange{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec, Text: text},
	})
}

func TestCreateRangeComment(t *testing.T) {
	a, _ := setup(t)
	fv, err := a.File("ch1.md")
	if err != nil {
		t.Fatal(err)
	}
	// One word.
	c, err := rangeComment(a, "ch1.md", fv.Hash, 5, 21, 5, 25, "long")
	if err != nil {
		t.Fatal(err)
	}
	if c.Scope != store.ScopeLine || c.Range == nil || c.Range.Text != "long" || c.OrigStart != 5 || c.OrigEnd != 5 {
		t.Fatalf("comment = %+v range %+v", c, c.Range)
	}
	if c.Range.Before != " Chapter 1\n\nintro\n\nThis sentence is too " || c.Range.After != " and wordy.\n\nend" {
		t.Fatalf("context = %q / %q", c.Range.Before, c.Range.After)
	}
	if c.Loc == nil || c.Loc.Range == nil || c.Loc.Range.Text != "" || c.Loc.Range.StartColumn != 21 || c.Loc.State != anchor.Exact {
		t.Fatalf("loc = %+v", c.Loc)
	}

	// Several lines; Start and End come from the range, not from the request.
	c, err = a.CreateComment(NewComment{
		Scope: "line", Path: "ch1.md", Start: 1, End: 1, Label: "must", Body: "x", Hash: fv.Hash,
		Range: &anchor.TextRange{StartLine: 3, StartColumn: 2, EndLine: 5, EndColumn: 4, Text: "tro\n\nThis"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Loc.Start != 3 || c.Loc.End != 5 || c.Range.Text != "tro\n\nThis" {
		t.Fatalf("multi-line = %+v %+v", c.Loc, c.Range)
	}

	// Only white space.
	if c, err = rangeComment(a, "ch1.md", fv.Hash, 5, 4, 5, 5, " "); err != nil || c.Range == nil {
		t.Fatalf("white space = %+v, %v", c, err)
	}

	// An end at column 0 of the next line ends at the end of the previous one.
	c, err = rangeComment(a, "ch1.md", fv.Hash, 3, 0, 4, 0, "intro")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Range.SamePosition(anchor.TextRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 5}) || c.Loc.End != 3 {
		t.Fatalf("normalized = %+v", c.Range)
	}

	// Only a line break: a line comment.
	c, err = rangeComment(a, "ch1.md", fv.Hash, 3, 5, 4, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Range != nil || c.Loc.Range != nil || c.Loc.Start != 3 || c.Loc.End != 3 {
		t.Fatalf("line break only = %+v %+v", c.Range, c.Loc)
	}
	c, err = rangeComment(a, "ch1.md", fv.Hash, 3, 5, 5, 0, "\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Range != nil || c.Loc.Start != 3 || c.Loc.End != 4 {
		t.Fatalf("line breaks only = %+v %+v", c.Range, c.Loc)
	}

	// Without a range, line comments are unchanged.
	c, err = a.CreateComment(NewComment{Scope: "line", Path: "ch1.md", Start: 5, End: 5, Label: "must", Body: "x", Hash: fv.Hash})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	if c.Range != nil || c.Loc.Range != nil || strings.Contains(string(b), `"range"`) {
		t.Fatalf("line comment = %s", b)
	}
}

func TestCreateRangeCommentRejectsMismatch(t *testing.T) {
	a, root := setup(t)
	fv, err := a.File("ch1.md")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		hash string
		r    anchor.TextRange
		code int
	}{
		{name: "text differs", hash: fv.Hash, r: anchor.TextRange{StartLine: 5, StartColumn: 21, EndLine: 5, EndColumn: 25, Text: "LONG"}, code: http.StatusConflict},
		{name: "column past the line", hash: fv.Hash, r: anchor.TextRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 6, Text: "intro"}, code: http.StatusConflict},
		{name: "line past the file", hash: fv.Hash, r: anchor.TextRange{StartLine: 7, StartColumn: 0, EndLine: 8, EndColumn: 1, Text: "end"}, code: http.StatusConflict},
		{name: "reversed", hash: fv.Hash, r: anchor.TextRange{StartLine: 3, StartColumn: 3, EndLine: 3, EndColumn: 1, Text: ""}, code: http.StatusConflict},
		{name: "unknown hash", hash: strings.Repeat("0", 64), r: anchor.TextRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 5, Text: "intro"}, code: http.StatusConflict},
		{name: "no hash", r: anchor.TextRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 5, Text: "intro"}, code: http.StatusConflict},
		{name: "too long", hash: fv.Hash, r: anchor.TextRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 5, Text: strings.Repeat("x", anchor.MaxRangeTextBytes+1)}, code: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := rangeComment(a, "ch1.md", tt.hash, tt.r.StartLine, tt.r.StartColumn, tt.r.EndLine, tt.r.EndColumn, tt.r.Text)
			if errCode(err) != tt.code {
				t.Fatalf("err = %v, want %d", err, tt.code)
			}
		})
	}

	// The text of the selection is that of the content the client saw, not
	// of the current file.
	write(t, root, "ch1.md", "# Chapter 1\n\nINTRO\n\nThis sentence is too long and wordy.\n\nend\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	if _, err := rangeComment(a, "ch1.md", fv.Hash, 3, 0, 3, 5, "INTRO"); errCode(err) != http.StatusConflict {
		t.Fatalf("current text against an old hash = %v", err)
	}
	c, err := rangeComment(a, "ch1.md", fv.Hash, 5, 21, 5, 25, "long")
	if err != nil {
		t.Fatalf("old hash with matching text: %v", err)
	}
	if c.OrigBlob != fv.Hash || c.Loc.Blob == fv.Hash || c.Loc.State != anchor.Exact {
		t.Fatalf("re-anchored onto the current file = %+v", c.Loc)
	}
	if n := len(a.Comments()); n != 1 {
		t.Fatalf("rejected comments were stored: %d", n)
	}
}

func TestRangeColumnsAreCodePointsWithoutBOMAndCR(t *testing.T) {
	root := t.TempDir()
	write(t, root, "j.md", "\uFEFF日本語😀の文\r\n二行目です\r\n")
	a, err := New(root, config.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	fv, err := a.File("j.md")
	if err != nil {
		t.Fatal(err)
	}
	c, err := rangeComment(a, "j.md", fv.Hash, 1, 3, 1, 5, "😀の")
	if err != nil {
		t.Fatal(err)
	}
	if c.Range.Before != "日本語" || c.Range.After != "文\n二行目です" {
		t.Fatalf("context = %q / %q", c.Range.Before, c.Range.After)
	}
	// The end of line 1 is column 6; the CR is not a column.
	if _, err := rangeComment(a, "j.md", fv.Hash, 1, 5, 2, 2, "文\n二行"); err != nil {
		t.Fatal(err)
	}
	if _, err := rangeComment(a, "j.md", fv.Hash, 1, 5, 1, 7, "文\r"); errCode(err) != http.StatusConflict {
		t.Fatalf("CR as a column = %v", err)
	}
	if _, err := rangeComment(a, "j.md", fv.Hash, 1, 0, 1, 1, "\uFEFF"); errCode(err) != http.StatusConflict {
		t.Fatalf("BOM as a column = %v", err)
	}
}

func TestRangeCommentRejectsInvalidUTF8(t *testing.T) {
	a, _ := setup(t)
	h, err := a.Store.PutBlob([]byte("ok\na\xed\xa0\x80b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rangeComment(a, "ch1.md", h, 2, 0, 2, 1, "a"); errCode(err) != http.StatusConflict {
		t.Fatalf("range on a surrogate = %v", err)
	}
}

func TestReanchorRangeComment(t *testing.T) {
	a, root := setup(t)
	write(t, root, "ch1.md", "# Chapter 1\n\nthe cat sat\n\nthe dog sat\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	fv, _ := a.File("ch1.md")
	unique, err := rangeComment(a, "ch1.md", fv.Hash, 5, 4, 5, 7, "dog")
	if err != nil {
		t.Fatal(err)
	}
	ambiguous, err := rangeComment(a, "ch1.md", fv.Hash, 5, 8, 5, 11, "sat")
	if err != nil {
		t.Fatal(err)
	}
	line, err := a.CreateComment(NewComment{Scope: "line", Path: "ch1.md", Start: 5, End: 5, Label: "must", Body: "x", Hash: fv.Hash})
	if err != nil {
		t.Fatal(err)
	}

	// Lines are inserted above and the words move within their lines.
	write(t, root, "ch1.md", "# Chapter 1\n\nnew\nmore\n\nthe cat sat\n\nthe big dog sat\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	c, _ := a.Store.Comment(unique.ID)
	if c.Loc.State != anchor.Moved || !c.Loc.Range.SamePosition(anchor.TextRange{StartLine: 8, StartColumn: 8, EndLine: 8, EndColumn: 11}) || c.Loc.Start != 8 {
		t.Fatalf("unique = %+v %+v", c.Loc, c.Loc.Range)
	}
	// "sat" is twice in the file; the context still tells which.
	c, _ = a.Store.Comment(ambiguous.ID)
	if c.Loc.State != anchor.Moved || c.Loc.Start != 8 || c.Loc.Range.StartColumn != 12 {
		t.Fatalf("by context = %+v %+v", c.Loc, c.Loc.Range)
	}
	if c, _ := a.Store.Comment(line.ID); c.Loc.Start != 8 || c.Range != nil {
		t.Fatalf("line comment = %+v", c.Loc)
	}

	// Two identical sentences: nothing tells them apart.
	write(t, root, "ch1.md", "# Chapter 1\n\nthe big dog sat\n\nthe big dog sat\n\nthe big dog sat\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	if c, _ := a.Store.Comment(ambiguous.ID); c.Loc.State != anchor.Outdated {
		t.Fatalf("ambiguous = %+v %+v", c.Loc, c.Loc.Range)
	}

	// The word is gone.
	write(t, root, "ch1.md", "# Chapter 1\n\nthe cat sat\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	if c, _ := a.Store.Comment(unique.ID); c.Loc.State != anchor.Outdated {
		t.Fatalf("missing = %+v", c.Loc)
	}

	// It comes back where it was first written.
	write(t, root, "ch1.md", "# Chapter 1\n\nthe cat sat\n\nthe dog sat\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	if c, _ := a.Store.Comment(unique.ID); c.Loc.State != anchor.Exact || !c.Loc.Range.SamePosition(*c.Range) {
		t.Fatalf("restored = %+v %+v", c.Loc, c.Loc.Range)
	}
}

func TestRangeCommentFeedback(t *testing.T) {
	a, _ := setup(t)
	fv, _ := a.File("ch1.md")
	c, err := rangeComment(a, "ch1.md", fv.Hash, 5, 21, 5, 25, "long")
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(res.FeedbackPath)
	for _, want := range []string{"[" + c.ID + "][must] L5:22-5:25", "対象の文字列:\n\n> long\n"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("feedback.md lacks %q:\n%s", want, md)
		}
	}
	js, _ := os.ReadFile(res.FeedbackJSON)
	if !strings.Contains(string(js), `"range": {`) || !strings.Contains(string(js), `"startColumn": 21`) {
		t.Errorf("feedback.json lacks the range:\n%s", js)
	}
}

func TestLineCommentJSONWithoutRangeLoads(t *testing.T) {
	dir := t.TempDir()
	// A comment as written before range comments existed.
	old := `{"op":"put","comment":{"id":"C-1","round":1,"scope":"line","path":"ch1.md","label":"must","body":"x","status":"draft",` +
		`"anchor":{"lines":["intro"],"before":[],"after":[],"hash":"h"},"origStart":3,"origEnd":3,"origBlob":"b",` +
		`"loc":{"start":3,"end":3,"state":"exact","blob":"b","anchor":{"lines":["intro"],"before":[],"after":[],"hash":"h"}},` +
		`"replies":[],"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "comments.jsonl"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := s.Comment("C-1")
	if !ok || c.Scope != store.ScopeLine || c.Range != nil || c.Loc == nil || c.Loc.Range != nil || c.Loc.Start != 3 {
		t.Fatalf("loaded = %+v", c)
	}
}
