package store

import (
	"os"
	"testing"
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
