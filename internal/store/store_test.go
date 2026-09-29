package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/config"
)

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := &Comment{ID: s.NewCommentID(), Scope: ScopeProject, Body: "a", Status: StatusDraft}
	if err := s.PutComment(c); err != nil {
		t.Fatal(err)
	}
	c2 := &Comment{ID: s.NewCommentID(), Scope: ScopeProject, Body: "b", Status: StatusDraft}
	if err := s.PutComment(c2); err != nil {
		t.Fatal(err)
	}
	c.Body = "a2"
	if err := s.PutComment(c); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteComment(c2.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate a torn write.
	f, _ := os.OpenFile(s.logPath(), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"op":"put","comm`)
	f.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cs := s2.Comments()
	if len(cs) != 1 || cs[0].Body != "a2" {
		t.Fatalf("comments = %+v", cs)
	}
	if s2.State.NextComment != 3 {
		t.Errorf("NextComment = %d", s2.State.NextComment)
	}
	h, err := s2.PutBlob([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s2.Blob(h)
	if err != nil || string(b) != "hello" {
		t.Fatalf("blob = %q %v", b, err)
	}
	if _, err := s2.Blob("../state.json"); err == nil {
		t.Error("invalid blob id accepted")
	}
}

func TestAnnotationPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	req := &AnnotationRequest{
		ID: s.NewRequestID(), Prompt: "確認", Files: map[string]string{"ch.md": "blob"},
		Target: "new", CreatedAt: time.Unix(1, 0),
	}
	if err := s.PutRequest(req); err != nil {
		t.Fatal(err)
	}
	a := &Annotation{
		ID: s.NewAnnotationID(), Request: req.ID, Path: "ch.md", Severity: "major",
		Confidence: "high", Body: "誤り", State: AnnotationPending,
	}
	if err := s.PutAnnotation(a); err != nil {
		t.Fatal(err)
	}
	run := &AgentRun{ID: "run-1", Purpose: "annotate", Request: req.ID, Status: RunRunning}
	if err := s.PutAgentRun(run); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAgentEvent(run, agent.Event{Type: agent.EventStarted}); err != nil {
		t.Fatal(err)
	}
	presets := []config.Preset{{Name: "固有名詞", Prompt: "確認", Scope: "selected"}}
	if err := s.SaveProjectPresets(presets); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.Request(req.ID); got.Prompt != "確認" || s2.State.NextRequest != 2 {
		t.Fatalf("request = %+v, state = %+v", got, s2.State)
	}
	if got := s2.Annotations(); len(got) != 1 || got[0].ID != "A-1" || s2.State.NextAnnotation != 2 {
		t.Fatalf("annotations = %+v, state = %+v", got, s2.State)
	}
	if got := s2.RequestAgentRuns(req.ID); len(got) != 1 || got[0].Purpose != "annotate" {
		t.Fatalf("runs = %+v", got)
	}
	if got := s2.AgentEvents(run); len(got) != 1 || got[0].Type != agent.EventStarted {
		t.Fatalf("events = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "requests", "Q-1", "agent-runs.jsonl")); err != nil {
		t.Fatal(err)
	}
	gotPresets, err := s2.ProjectPresets()
	if err != nil || !reflect.DeepEqual(gotPresets, presets) {
		t.Fatalf("presets = %+v, %v", gotPresets, err)
	}
}

func TestOldStateDefaultsAnnotationCounters(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"version":1,"round":1,"nextComment":4,"nextReply":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.NewRequestID() != "Q-1" || s.NewAnnotationID() != "A-1" {
		t.Fatalf("state = %+v", s.State)
	}
}
