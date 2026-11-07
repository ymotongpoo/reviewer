package app

import (
	"os"
	"strings"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/pdfimport"
	"github.com/ymotongpoo/reviewer/internal/store"
)

func pdfInput() PDFInput {
	return PDFInput{DocumentHash: strings.Repeat("a", 64), Name: "annotated.pdf", Highlights: []pdfimport.Highlight{{ID: "h1", Page: 2, Quote: "This sentence is too long and wordy.", Body: "短くしてください。"}}}
}

func TestPDFImportRoundTrip(t *testing.T) {
	a, root := setup(t)
	in := pdfInput()
	ms, err := a.MatchPDF(in)
	if err != nil || len(ms) != 1 || !ms[0].Automatic {
		t.Fatalf("match=%+v err=%v", ms, err)
	}
	if len(a.Store.Comments()) != 0 {
		t.Fatal("preview wrote comments")
	}
	ci := PDFCommentInput{in.DocumentHash, in.Name, in.Highlights[0], ms[0].Candidates[0]}
	c, err := a.ImportPDFComment(ci)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != store.StatusDraft || c.Range.Text != in.Highlights[0].Quote || c.PDFSource.Page != 2 {
		t.Fatalf("comment=%+v", c)
	}
	// Both duplicate request retries and a restart preserve idempotency.
	a, err = New(root, a.Cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Init(); err != nil {
		t.Fatal(err)
	}
	again, err := a.ImportPDFComment(ci)
	if err != nil || again.ID != c.ID || len(a.Store.Comments()) != 1 {
		t.Fatalf("duplicate=%+v err=%v", again, err)
	}
	ms, err = a.MatchPDF(in)
	if err != nil || ms[0].ExistingID != c.ID {
		t.Fatalf("duplicate preview=%+v err=%v", ms, err)
	}
	res, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(res.FeedbackPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"短くしてください。", "annotated.pdf", "2ページ", "This sentence is too long and wordy."} {
		if !strings.Contains(string(b), want) {
			t.Errorf("feedback missing %q", want)
		}
	}
}

func TestPDFRejectStaleAndInvalidSelection(t *testing.T) {
	a, root := setup(t)
	in := pdfInput()
	ms, err := a.MatchPDF(in)
	if err != nil {
		t.Fatal(err)
	}
	ci := PDFCommentInput{in.DocumentHash, in.Name, in.Highlights[0], ms[0].Candidates[0]}
	bad := ci
	bad.Candidate.Range.StartColumn++
	if _, err := a.ImportPDFComment(bad); err == nil {
		t.Fatal("forged range accepted")
	}
	bad = ci
	bad.Candidate.Path = "../outside.md"
	if _, err := a.ImportPDFComment(bad); err == nil {
		t.Fatal("outside path accepted")
	}
	write(t, root, "ch1.md", "Changed manuscript.\n")
	if _, err := a.ImportPDFComment(ci); err == nil {
		t.Fatal("stale snapshot accepted")
	}
	if len(a.Store.Comments()) != 0 {
		t.Fatal("invalid imports created comments")
	}
}

func TestPDFScopeAndValidation(t *testing.T) {
	a, root := setup(t)
	write(t, root, "notes.txt", "A text-only target")
	write(t, root, ".gitignore", "excluded.md\n")
	write(t, root, "excluded.md", "A text-only target")
	in := pdfInput()
	in.Highlights[0].Quote = "A text-only target"
	ms, err := a.MatchPDF(in)
	if err != nil || len(ms[0].Candidates) != 0 {
		t.Fatalf("out-of-scope candidates: %+v err=%v", ms, err)
	}
	in.Highlights = append(in.Highlights, in.Highlights[0])
	if _, err := a.MatchPDF(in); err == nil {
		t.Fatal("duplicate input IDs accepted")
	}
}
