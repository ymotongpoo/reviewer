package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/anchor"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/store"
)

func setup(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "ch1.md", "# Chapter 1\n\nintro\n\nThis sentence is too long and wordy.\n\nend\n")
	write(t, root, "ch2.md", "# Chapter 2\n")
	a, err := New(root, config.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	return a, a.Proj.Root
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTrip(t *testing.T) {
	a, root := setup(t)

	fv, err := a.File("ch1.md")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := a.CreateComment(NewComment{Scope: "project", Label: "must", Body: "全体的に短く"})
	if err != nil {
		t.Fatal(err)
	}
	lc, err := a.CreateComment(NewComment{Scope: "line", Path: "ch1.md", Start: 5, End: 5, Label: "suggestion", Body: "短く\n```suggestion\nThis is short.\n```", Hash: fv.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateComment(NewComment{Scope: "line", Path: "../x", Start: 1, End: 1}); err == nil {
		t.Error("path traversal accepted")
	}

	// The agent inserts lines above while the reviewer is still drafting.
	write(t, root, "ch1.md", "# Chapter 1\n\nnew para\n\nintro\n\nThis sentence is too long and wordy.\n\nend\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	c, _ := a.Store.Comment(lc.ID)
	if c.Loc.Start != 7 || c.Loc.State != anchor.Moved {
		t.Fatalf("loc after insert = %+v", c.Loc)
	}

	res, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(res.FeedbackPath)
	for _, want := range []string{"[" + pc.ID + "][must]", "[" + lc.ID + "][suggestion] L7", "> This sentence is too long and wordy."} {
		if !strings.Contains(string(md), want) {
			t.Errorf("feedback.md lacks %q:\n%s", want, md)
		}
	}
	if !strings.Contains(res.Prompt, res.ResponsePath) {
		t.Errorf("prompt = %q", res.Prompt)
	}

	// The agent edits and responds.
	write(t, root, "ch1.md", "# Chapter 1\n\nnew para\n\nintro\n\nThis is short.\n\nend\n")
	resp, _ := json.Marshal(map[string]any{
		"round": 1,
		"responses": []map[string]string{
			{"id": pc.ID, "status": "addressed", "message": "短くしました"},
			{"id": lc.ID, "status": "addressed", "message": "置換しました"},
		},
		"summary": "done",
	})
	os.WriteFile(res.ResponsePath, resp, 0o644)
	a.HandleChanges([]string{"ch1.md"}, true)

	c, _ = a.Store.Comment(lc.ID)
	if c.Status != store.StatusAddressed || len(c.Replies) != 1 || c.Replies[0].Author != store.AuthorAgent {
		t.Fatalf("after response: %+v", c)
	}
	info := a.Info()
	if info.Response == nil || info.Response.Summary != "done" || len(info.Response.Warnings) != 0 {
		t.Fatalf("response info = %+v", info.Response)
	}

	// While waiting for the agent, the tree marks changed files and the
	// round history compares the submission with the current files.
	if !treeChanged(a, "ch1.md") {
		t.Error("ch1.md not marked changed")
	}
	rc, err := a.RoundChanges(1)
	if err != nil {
		t.Fatal(err)
	}
	if rc.ToRound != 0 || len(rc.Files) != 1 || rc.Files[0].Path != "ch1.md" || rc.Files[0].Insert != 1 || rc.Files[0].Delete != 1 {
		t.Errorf("changes = %+v", rc)
	}
	if d, err := a.RoundDiff(1, "ch1.md"); err != nil || len(d.Ops) < 2 {
		t.Errorf("diff = %+v %v", d, err)
	}
	if _, err := a.RoundChanges(2); err == nil {
		t.Error("changes of an unsubmitted round")
	}

	// Next round: resolve one, reply to the other; it is carried over.
	resolved := store.StatusResolved
	if _, err := a.UpdateComment(pc.ID, CommentPatch{Status: &resolved}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddReply(lc.ID, "もう少し具体的に"); err != nil {
		t.Fatal(err)
	}
	if a.Store.State.Round != 2 || a.Store.State.RoundStatus != store.RoundOpen {
		t.Fatalf("round not auto-opened: %+v", a.Store.State)
	}
	// In the new round, no change markers; history compares with round 2's start.
	if treeChanged(a, "ch1.md") {
		t.Error("change marker shown in a new round")
	}
	write(t, root, "ch1.md", "# Chapter 1\n\nnew para\n\nintro\n\nThis is short.\n\nend\nmore\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	if rc, _ := a.RoundChanges(1); rc.ToRound != 2 || len(rc.Files) != 1 || rc.Files[0].Insert != 1 {
		t.Errorf("round 1 changes after round 2 opened = %+v", rc)
	}
	res2, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	md2, _ := os.ReadFile(res2.FeedbackPath)
	if strings.Contains(string(md2), pc.ID) {
		t.Errorf("resolved comment included:\n%s", md2)
	}
	if !strings.Contains(string(md2), "前ラウンドから持ち越した") || !strings.Contains(string(md2), "もう少し具体的に") {
		t.Errorf("carried comment missing:\n%s", md2)
	}
	c, _ = a.Store.Comment(lc.ID)
	if c.Status != store.StatusOpen || c.Loc.State != anchor.Fuzzy && c.Loc.State != anchor.Outdated {
		t.Errorf("carried comment = status %s loc %+v", c.Status, c.Loc)
	}
}

func treeChanged(a *App, path string) bool {
	for _, f := range a.Tree() {
		if f.Path == path {
			return f.Changed || f.New
		}
	}
	return false
}

func TestDraftRules(t *testing.T) {
	a, _ := setup(t)
	c, err := a.CreateComment(NewComment{Scope: "file", Path: "ch2.md", Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	body := "y"
	if _, err := a.UpdateComment(c.ID, CommentPatch{Body: &body}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddReply(c.ID, "r"); err == nil {
		t.Error("reply to draft accepted")
	}
	if _, err := a.Submit(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateComment(c.ID, CommentPatch{Body: &body}); err == nil {
		t.Error("edit after submit accepted")
	}
	if err := a.DeleteComment(c.ID); err == nil {
		t.Error("delete after submit accepted")
	}
	if _, err := a.Submit(); err == nil {
		t.Error("double submit accepted")
	}
}

func TestSubmitEmpty(t *testing.T) {
	a, _ := setup(t)
	if _, err := a.Submit(); err == nil {
		t.Error("empty submit accepted")
	}
}

func TestBadResponse(t *testing.T) {
	a, _ := setup(t)
	a.CreateComment(NewComment{Scope: "project", Body: "x"})
	res, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(res.ResponsePath, []byte("{not json"), 0o644)
	a.HandleChanges(nil, true)
	if info := a.Info(); info.Response == nil || info.Response.Error == "" {
		t.Errorf("expected parse error, got %+v", info.Response)
	}
}
