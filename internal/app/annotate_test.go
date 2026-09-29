package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent/hermes"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes/hermestest"
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

type annotationResponseFixture struct {
	Request     string           `json:"request"`
	Annotations []map[string]any `json:"annotations"`
	Summary     string           `json:"summary"`
}

func readAnnotationResponse(t *testing.T, a *App, requestID string) annotationResponseFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.Store.RequestDir(requestID), "annotations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var response annotationResponseFixture
	if err := json.Unmarshal(b, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func writeAnnotationResponse(t *testing.T, a *App, requestID string, response annotationResponseFixture) {
	t.Helper()
	b, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Store.RequestDir(requestID), "annotations.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReimportIdenticalAnnotationsKeepsIDs(t *testing.T) {
	a, _ := setup(t)
	req := annotationFixture(t, a)
	responsePath := filepath.Join(a.Store.RequestDir(req.ID), "annotations.json")
	b, err := os.ReadFile(responsePath)
	if err != nil {
		t.Fatal(err)
	}
	if imported, err := a.ImportAnnotations(req.ID); err != nil || !imported {
		t.Fatalf("first import = %v, %v", imported, err)
	}

	if err := os.WriteFile(responsePath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if imported, err := a.ImportAnnotations(req.ID); err != nil || imported {
		t.Fatalf("second import = %v, %v", imported, err)
	}
	annotations := a.Annotations(true)
	if len(annotations) != 2 || annotations[0].ID != "A-1" || annotations[1].ID != "A-2" {
		t.Fatalf("annotations = %+v", annotations)
	}
	for _, ann := range annotations {
		if ann.State != store.AnnotationPending {
			t.Errorf("annotation %s state = %s", ann.ID, ann.State)
		}
	}
}

func TestReimportReplacesPendingAnnotationsAndReusesIDs(t *testing.T) {
	a, _ := setup(t)
	req := annotationFixture(t, a)
	if _, err := a.ImportAnnotations(req.ID); err != nil {
		t.Fatal(err)
	}

	response := readAnnotationResponse(t, a, req.ID)
	response.Annotations = response.Annotations[:1]
	response.Annotations[0]["body"] = "更新された指摘"
	response.Summary = "1件確認しました"
	writeAnnotationResponse(t, a, req.ID, response)
	if imported, err := a.ImportAnnotations(req.ID); err != nil || !imported {
		t.Fatalf("reimport = %v, %v", imported, err)
	}

	annotations := a.Annotations(true)
	if len(annotations) != 1 || annotations[0].ID != "A-1" || annotations[0].Body != "更新された指摘" || annotations[0].State != store.AnnotationPending {
		t.Fatalf("annotations = %+v", annotations)
	}
	gotReq, err := a.Store.Request(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotReq.Import.Count != 1 {
		t.Fatalf("import = %+v", gotReq.Import)
	}
	if next := a.Store.NewAnnotationID(); next != "A-3" {
		t.Fatalf("next annotation ID = %s", next)
	}
}

func TestReimportPreservesReviewedAnnotationsWithoutIDCollision(t *testing.T) {
	a, _ := setup(t)
	req := annotationFixture(t, a)
	response := readAnnotationResponse(t, a, req.ID)
	response.Annotations = append(response.Annotations, map[string]any{
		"path": "ch1.md", "startLine": 7, "endLine": 7,
		"quote": []string{"end"}, "severity": "minor", "confidence": "medium",
		"body": "末尾を確認してください",
	})
	response.Summary = "3件確認しました"
	writeAnnotationResponse(t, a, req.ID, response)
	if _, err := a.ImportAnnotations(req.ID); err != nil {
		t.Fatal(err)
	}

	annotations := a.Annotations()
	if _, err := a.AdoptAnnotation(annotations[0].ID, AnnotationAdoptPatch{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetAnnotationState(annotations[1].ID, store.AnnotationDismissed); err != nil {
		t.Fatal(err)
	}

	response.Annotations = response.Annotations[:2]
	response.Annotations[0]["body"] = "差し替え後の指摘1"
	response.Annotations[1]["body"] = "差し替え後の指摘2"
	response.Summary = "2件確認しました"
	writeAnnotationResponse(t, a, req.ID, response)
	if imported, err := a.ImportAnnotations(req.ID); err != nil || !imported {
		t.Fatalf("reimport = %v, %v", imported, err)
	}

	all := a.Annotations(true)
	if len(all) != 4 {
		t.Fatalf("annotations = %+v", all)
	}
	wantStates := map[string]string{
		"A-1": store.AnnotationAdopted,
		"A-2": store.AnnotationDismissed,
		"A-3": store.AnnotationPending,
		"A-4": store.AnnotationPending,
	}
	for _, ann := range all {
		if ann.State != wantStates[ann.ID] {
			t.Errorf("annotation %s state = %s, want %s", ann.ID, ann.State, wantStates[ann.ID])
		}
	}
	if all[0].Body != "技術的に誤っています" || all[1].Body != "確認してください" {
		t.Fatalf("reviewed annotations changed = %+v", all[:2])
	}
	if all[2].Body != "差し替え後の指摘1" || all[3].Body != "差し替え後の指摘2" {
		t.Fatalf("replacement annotations = %+v", all[2:])
	}
	if next := a.Store.NewAnnotationID(); next != "A-5" {
		t.Fatalf("next annotation ID = %s", next)
	}
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

func TestAnnotateWithFakeHermes(t *testing.T) {
	a, _ := setup(t)
	fake := &hermestest.Fake{Key: "k", ModifyAnnotationTarget: true}
	ts := httptest.NewServer(fake)
	defer ts.Close()
	notifier := &recNotifier{}
	a.ConfigureAgent(hermes.New(hermes.Options{URL: ts.URL, APIKey: "k"}), "", notifier, "test", "http://reviewer/")

	result, err := a.Annotate(context.Background(), AnnotateInput{
		Preset: "技術的な誤りの検出", Paths: []string{"ch1.md"}, Target: AnnotationTargetNew,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "annotation run", func() bool { return len(a.AgentInfo().Active) == 0 })
	req, err := a.Store.Request(result.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if req.Import == nil || req.Import.Count != 2 || len(req.ChangedPaths) != 1 || req.ChangedPaths[0] != "ch1.md" {
		t.Fatalf("request = %+v", req)
	}
	if len(fake.SessionRequests) != 1 || fake.SessionRequests[0].Title != "reviewer: "+a.Info().Name+" "+req.ID+" 技術的な誤りの検出" {
		t.Fatalf("session requests = %+v", fake.SessionRequests)
	}
	if len(fake.Requests) != 1 || fake.Requests[0].SessionID == "" || !strings.Contains(fake.Requests[0].Input, "instructions.md") {
		t.Fatalf("run requests = %+v", fake.Requests)
	}
	if len(notifier.all()) != 0 {
		t.Fatalf("new-session notices = %v", notifier.all())
	}
	annotations := a.Annotations()
	if len(annotations) != 2 {
		t.Fatalf("annotations = %+v", annotations)
	}
	if _, err := a.AdoptAnnotation(annotations[0].ID, AnnotationAdoptPatch{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetAnnotationState(annotations[1].ID, store.AnnotationDismissed); err != nil {
		t.Fatal(err)
	}
	submitted, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	feedback, _ := os.ReadFile(submitted.FeedbackPath)
	if !strings.Contains(string(feedback), "（fakehermes）技術的な記述を確認してください") {
		t.Fatalf("feedback = %s", feedback)
	}
}

func TestAnnotateRetriesSessionTitleConflicts(t *testing.T) {
	for _, conflicts := range []int{1, 4, 5} {
		t.Run(fmt.Sprintf("%d conflicts", conflicts), func(t *testing.T) {
			a, _ := setup(t)
			baseTitle := "reviewer: " + a.Info().Name + " Q-1 test"
			titles := []string{baseTitle}
			for i := 2; i <= 5; i++ {
				titles = append(titles, fmt.Sprintf("%s (%d)", baseTitle, i))
			}
			fake := &hermestest.Fake{Key: "k"}
			for i := 0; i < conflicts; i++ {
				fake.Sessions = append(fake.Sessions, map[string]any{
					"id": fmt.Sprintf("existing-%d", i), "title": titles[i],
				})
			}
			ts := httptest.NewServer(fake)
			defer ts.Close()
			a.ConfigureAgent(hermes.New(hermes.Options{URL: ts.URL, APIKey: "k"}), "", nil, "test", "")

			result, err := a.Annotate(context.Background(), AnnotateInput{
				Preset: "test", Prompt: "確認", Paths: []string{"ch1.md"},
			})
			if conflicts == 5 {
				var appErr *Error
				if !errors.As(err, &appErr) || appErr.Code != 502 || !strings.Contains(appErr.Msg, "Title already in use") {
					t.Fatalf("Annotate error = %v", err)
				}
				if requests := a.AnnotationRequests(); len(requests) != 0 {
					t.Fatalf("requests = %+v", requests)
				}
				if _, err := os.Stat(a.Store.RequestDir("Q-1")); !os.IsNotExist(err) {
					t.Fatalf("request directory still exists: %v", err)
				}
				if len(fake.Requests) != 0 || len(fake.Sessions) != conflicts {
					t.Fatalf("runs = %+v, sessions = %+v", fake.Requests, fake.Sessions)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				waitFor(t, "annotation run", func() bool { return len(a.AgentInfo().Active) == 0 })
				created := fake.Sessions[len(fake.Sessions)-1]
				if created["title"] != titles[conflicts] || created["id"] != result.Request.SessionID {
					t.Fatalf("created session = %+v, request = %+v", created, result.Request)
				}
				if len(fake.Requests) != 1 || fake.Requests[0].SessionID != result.Request.SessionID {
					t.Fatalf("run requests = %+v", fake.Requests)
				}
			}
			if len(fake.SessionRequests) != min(conflicts+1, 5) {
				t.Fatalf("session requests = %+v", fake.SessionRequests)
			}
			for i, req := range fake.SessionRequests {
				if req.Title != titles[i] {
					t.Errorf("attempt %d title = %q, want %q", i+1, req.Title, titles[i])
				}
			}
		})
	}
}

func TestAnnotateSessionTitlesIncludeProjectName(t *testing.T) {
	fake := &hermestest.Fake{Key: "k"}
	ts := httptest.NewServer(fake)
	defer ts.Close()
	parent := t.TempDir()
	for i, name := range []string{"project-one", "project-two"} {
		root := filepath.Join(parent, name)
		write(t, root, "ch1.md", "# Chapter 1\n")
		a, err := New(root, config.Default(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Init(); err != nil {
			t.Fatal(err)
		}
		a.ConfigureAgent(hermes.New(hermes.Options{URL: ts.URL, APIKey: "k"}), "", nil, "test", "")
		result, err := a.Annotate(context.Background(), AnnotateInput{Prompt: "確認"})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "annotation run", func() bool { return len(a.AgentInfo().Active) == 0 })
		if result.Request.ID != "Q-1" || a.Info().Name != name {
			t.Fatalf("request ID = %q, project name = %q", result.Request.ID, a.Info().Name)
		}
		wantTitle := "reviewer: " + name + " Q-1 AI確認"
		if len(fake.SessionRequests) != i+1 || fake.SessionRequests[i].Title != wantTitle {
			t.Fatalf("session requests = %+v, want title %q", fake.SessionRequests, wantTitle)
		}
	}
}

func TestAnnotateDeletesRequestWhenHermesSetupFails(t *testing.T) {
	tests := []struct {
		name              string
		failCreateSession bool
		failStart         bool
	}{
		{name: "create session", failCreateSession: true},
		{name: "start run", failStart: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := setup(t)
			var events []Event
			a.notify = func(event Event) { events = append(events, event) }
			fake := &hermestest.Fake{
				Key:               "k",
				FailCreateSession: tt.failCreateSession,
				FailStart:         tt.failStart,
			}
			ts := httptest.NewServer(fake)
			defer ts.Close()
			a.ConfigureAgent(hermes.New(hermes.Options{URL: ts.URL, APIKey: "k"}), "", nil, "test", "")

			_, err := a.Annotate(context.Background(), AnnotateInput{
				Preset: "技術的な誤りの検出", Paths: []string{"ch1.md"}, Target: AnnotationTargetNew,
			})
			var appErr *Error
			if !errors.As(err, &appErr) || appErr.Code != 502 {
				t.Fatalf("Annotate error = %v", err)
			}
			if requests := a.AnnotationRequests(); len(requests) != 0 {
				t.Fatalf("requests = %+v", requests)
			}
			if _, err := os.Stat(a.Store.RequestDir("Q-1")); !os.IsNotExist(err) {
				t.Fatalf("request directory still exists: %v", err)
			}
			for _, event := range events {
				if event.Type == "annotate" {
					t.Fatalf("annotate event emitted for deleted request: %+v", event)
				}
			}
			if len(fake.SessionRequests) != 1 {
				t.Fatalf("session requests = %+v", fake.SessionRequests)
			}
			wantRunRequests := 0
			if tt.failStart {
				wantRunRequests = 1
			}
			if len(fake.Requests) != wantRunRequests {
				t.Fatalf("run requests = %+v", fake.Requests)
			}
		})
	}
}
