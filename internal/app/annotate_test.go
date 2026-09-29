package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/anchor"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/store"
)

func annotationFixture(t *testing.T, a *App) *store.AnnotationRequest {
	t.Helper()
	fv, err := a.File("ch1.md")
	if err != nil {
		t.Fatal(err)
	}
	req := &store.AnnotationRequest{
		ID: a.Store.NewRequestID(), Prompt: "確認", Preset: "test",
		Files: map[string]string{"ch1.md": fv.Hash}, Target: AnnotationTargetNew,
		CreatedAt: time.Unix(1, 0),
	}
	if err := a.Store.PutRequest(req); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{
		"request": req.ID,
		"annotations": []map[string]any{
			{
				"path": "ch1.md", "startLine": 2, "endLine": 2,
				"quote":    []string{"This sentence is too long and wordy."},
				"severity": "major", "confidence": "high", "label": "must",
				"body": "技術的に誤っています", "suggestion": "This sentence is correct.",
				"evidence": []map[string]string{{"url": "https://example.com/spec", "quote": "The correct rule", "note": "仕様"}},
			},
			{
				"path": "ch1.md", "startLine": 3, "endLine": 3,
				"quote": []string{"not in the file"}, "severity": "info", "confidence": "low",
				"body": "確認してください",
			},
		},
		"summary": "2件確認しました",
	}
	b, _ := json.Marshal(out)
	if err := os.WriteFile(filepath.Join(a.Store.RequestDir(req.ID), "annotations.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestImportAdoptDismissAndReanchorAnnotations(t *testing.T) {
	a, root := setup(t)
	req := annotationFixture(t, a)
	imported, err := a.ImportAnnotations(req.ID)
	if err != nil || !imported {
		t.Fatalf("import = %v, %v", imported, err)
	}
	gotReq, _ := a.Store.Request(req.ID)
	if gotReq.Import == nil || gotReq.Import.Count != 2 || len(gotReq.Import.Warnings) != 2 {
		t.Fatalf("request import = %+v", gotReq.Import)
	}
	anns := a.Annotations()
	if len(anns) != 2 || anns[0].Loc.Start != 5 || anns[0].Loc.State != anchor.Exact || anns[1].Loc.State != anchor.Outdated {
		t.Fatalf("annotations = %+v", anns)
	}

	write(t, root, "ch1.md", "inserted\n# Chapter 1\n\nintro\n\nThis sentence is too long and wordy.\n\nend\n")
	a.HandleChanges([]string{"ch1.md"}, false)
	anns = a.Annotations()
	if anns[0].Loc.Start != 6 || anns[0].Loc.State != anchor.Moved {
		t.Fatalf("reanchored = %+v", anns[0].Loc)
	}
	c, err := a.AdoptAnnotation(anns[0].ID, AnnotationAdoptPatch{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Scope != store.ScopeLine || c.Loc.Start != 6 || !strings.Contains(c.Body, "```suggestion") || !strings.Contains(c.Body, "https://example.com/spec") {
		t.Fatalf("adopted comment = %+v", c)
	}
	if _, err := a.SetAnnotationState(anns[1].ID, store.AnnotationDismissed); err != nil {
		t.Fatal(err)
	}
	if visible := a.Annotations(); len(visible) != 0 {
		t.Fatalf("visible annotations = %+v", visible)
	}
	res, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	feedback, _ := os.ReadFile(res.FeedbackPath)
	if !strings.Contains(string(feedback), "技術的に誤っています") || !strings.Contains(string(feedback), "The correct rule") {
		t.Errorf("feedback = %s", feedback)
	}
}

func TestAdoptUnlocatedAnnotationAsFileComment(t *testing.T) {
	a, _ := setup(t)
	req := annotationFixture(t, a)
	if _, err := a.ImportAnnotations(req.ID); err != nil {
		t.Fatal(err)
	}
	anns := a.Annotations()
	c, err := a.AdoptAnnotation(anns[1].ID, AnnotationAdoptPatch{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Scope != store.ScopeFile || !strings.Contains(c.Body, "not in the file") || !strings.Contains(c.Body, "位置不明") {
		t.Fatalf("comment = %+v", c)
	}
}

func TestAnnotationEditDetectionAndDiff(t *testing.T) {
	a, root := setup(t)
	req := annotationFixture(t, a)
	write(t, root, "ch1.md", "# changed\n")
	a.mu.Lock()
	changed := a.changedRequestPaths(req)
	a.mu.Unlock()
	if len(changed) != 1 || changed[0] != "ch1.md" {
		t.Fatalf("changed = %v", changed)
	}
	diff, err := a.AnnotationRequestDiff(req.ID, "ch1.md")
	if err != nil || diff.Kind != ChangeModified || len(diff.Ops) < 2 {
		t.Fatalf("diff = %+v, %v", diff, err)
	}
}

func TestPresetMergingAndSaving(t *testing.T) {
	root := t.TempDir()
	write(t, root, "doc.md", "text\n")
	cfg := config.Default()
	cfg.Presets = append(cfg.Presets, config.Preset{Name: "用語", Prompt: "用語を確認", Scope: "all"})
	a, err := New(root, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	merged, err := a.SaveProjectPresets([]config.Preset{
		{Name: "用語", Prompt: "プロジェクトの用語を確認", Scope: "selected"},
		{Name: "リンク", Prompt: "リンクを確認", Scope: "current"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 3 || merged[0].Origin != "builtin" || merged[1].Origin != "project" || merged[1].Prompt != "プロジェクトの用語を確認" || merged[2].Origin != "project" {
		t.Fatalf("presets = %+v", merged)
	}
	if _, err := os.Stat(filepath.Join(a.DataDir, "presets.toml")); err != nil {
		t.Fatal(err)
	}
}
