package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessions(t *testing.T) {
	root := t.TempDir()
	ss, err := ListSessions(root)
	if err != nil || len(ss) != 1 || ss[0].ID != DefaultSession || ss[0].Name != DefaultSessionName {
		t.Fatalf("initial sessions = %+v, %v", ss, err)
	}
	if SessionDir(root, DefaultSession) != root {
		t.Errorf("default session dir = %s", SessionDir(root, DefaultSession))
	}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	b, err := CreateSession(root, " book  B ", "review/b", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	a, err := CreateSession(root, "book A", "", t0)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "book B" || !ValidSessionID(a.ID) || a.ID == b.ID {
		t.Errorf("created %+v %+v", a, b)
	}
	if _, err := CreateSession(root, "   ", "", t0); err == nil {
		t.Error("created a session without a name")
	}
	// Directories without session.json are ignored.
	os.MkdirAll(filepath.Join(root, "sessions", "stray"), 0o755)
	ss, _ = ListSessions(root)
	if len(ss) != 3 || ss[1].ID != a.ID || ss[2].ID != b.ID {
		t.Fatalf("sessions = %+v", ss)
	}

	main := ss[0]
	main.Name = "全体"
	if err := SaveSession(root, main); err != nil {
		t.Fatal(err)
	}
	b.Archived = true
	SaveSession(root, b)
	if got, _ := ReadSession(root, DefaultSession); got.Name != "全体" {
		t.Errorf("renamed default = %+v", got)
	}
	if got, _ := ReadSession(root, b.ID); !got.Archived || got.Branch != "review/b" {
		t.Errorf("archived = %+v", got)
	}
	if _, err := ReadSession(root, "../x"); err == nil {
		t.Error("read an invalid session id")
	}

	// A session's store keeps shared settings in the directory's data dir.
	st, err := Open(SessionDir(root, a.ID))
	if err != nil {
		t.Fatal(err)
	}
	st.Shared = root
	if err := st.SaveProjectPresets(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "presets.toml")); err != nil {
		t.Errorf("presets not shared: %v", err)
	}
}
