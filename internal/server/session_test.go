package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessions(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.root, "a")
	id := e.open(t, dir)
	main := e.ts.URL + "/p/" + id

	res, body := do(t, "POST", main+"/api/sessions", `{"name":"  第3章  "}`, bearer)
	if res.StatusCode != 200 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var created struct {
		Session struct{ ID, Name string }
		URL     string
	}
	json.Unmarshal([]byte(body), &created)
	if created.Session.Name != "第3章" || created.URL != "/p/"+id+"/s/"+created.Session.ID+"/" {
		t.Fatalf("created = %+v", created)
	}
	other := e.ts.URL + strings.TrimSuffix(created.URL, "/")
	if res, body := do(t, "POST", main+"/api/sessions", `{"name":"  "}`, bearer); res.StatusCode != 400 {
		t.Errorf("empty name: %d %s", res.StatusCode, body)
	}

	// Comments belong to the session they were written in.
	for _, base := range []string{main, other} {
		if res, body := do(t, "POST", base+"/api/comments", `{"scope":"project","label":"must","body":"x"}`, bearer); res.StatusCode != 200 {
			t.Fatalf("comment: %d %s", res.StatusCode, body)
		}
	}
	do(t, "POST", other+"/api/comments", `{"scope":"project","label":"must","body":"y"}`, bearer)
	count := func(base string) int {
		_, body := do(t, "GET", base+"/api/comments", "", bearer)
		var out struct{ Comments []json.RawMessage }
		json.Unmarshal([]byte(body), &out)
		return len(out.Comments)
	}
	if count(main) != 1 || count(other) != 2 {
		t.Errorf("comments main=%d other=%d", count(main), count(other))
	}
	if _, err := os.Stat(filepath.Join(dir, ".reviewer", "sessions", created.Session.ID, "comments.jsonl")); err != nil {
		t.Errorf("session data: %v", err)
	}

	// Submitting in one session leaves the other's round alone.
	if res, body := do(t, "POST", other+"/api/rounds/submit", `{}`, bearer); res.StatusCode != 200 {
		t.Fatalf("submit: %d %s", res.StatusCode, body)
	} else if !strings.Contains(body, filepath.Join("sessions", created.Session.ID, "rounds", "1", "feedback.md")) {
		t.Errorf("feedback path: %s", body)
	}
	info := func(base string) (out struct {
		RoundStatus string
		Session     struct{ ID, Name string }
	}) {
		_, body := do(t, "GET", base+"/api/project", "", bearer)
		json.Unmarshal([]byte(body), &out)
		return out
	}
	if m, o := info(main), info(other); m.RoundStatus != "open" || o.RoundStatus != "submitted" || m.Session.ID != "main" || o.Session.Name != "第3章" {
		t.Errorf("info main=%+v other=%+v", m, o)
	}

	// Presets are shared by the directory.
	if res, body := do(t, "PUT", other+"/api/presets", `{"presets":[{"name":"p","prompt":"q","scope":"all"}]}`, bearer); res.StatusCode != 200 {
		t.Fatalf("presets: %d %s", res.StatusCode, body)
	}
	if _, body := do(t, "GET", main+"/api/presets", "", bearer); !strings.Contains(body, `"name":"p"`) {
		t.Errorf("presets not shared: %s", body)
	}

	// Rename and archive.
	if res, body := do(t, "PATCH", other+"/api/session", `{"name":"第4章","archived":true}`, bearer); res.StatusCode != 200 {
		t.Fatalf("update: %d %s", res.StatusCode, body)
	}
	if res, _ := do(t, "PATCH", main+"/api/session", `{"archived":true}`, bearer); res.StatusCode != 400 {
		t.Errorf("archive main: %d", res.StatusCode)
	}
	_, body = do(t, "GET", main+"/api/sessions", "", bearer)
	if !strings.Contains(body, `"name":"第4章"`) || !strings.Contains(body, `"archived":true`) {
		t.Errorf("sessions: %s", body)
	}
	_, body = do(t, "GET", e.ts.URL+"/api/projects", "", bearer)
	if strings.Contains(body, "第4章") {
		t.Errorf("archived session listed on the picker: %s", body)
	}

	// Sessions survive a restart of the server.
	e.reg.CloseAll()
	if o := info(other); o.Session.Name != "第4章" || o.RoundStatus != "submitted" {
		t.Errorf("after restart: %+v", o)
	}
	if res, _ := do(t, "GET", main+"/s/nosuch/api/project", "", bearer); res.StatusCode != 404 {
		t.Errorf("unknown session: %d", res.StatusCode)
	}
	if res, _ := do(t, "GET", main+"/s/Bad..id/api/project", "", bearer); res.StatusCode != 404 {
		t.Errorf("invalid session: %d", res.StatusCode)
	}
	// The SPA is served for session pages.
	if res, _ := do(t, "GET", other+"/", "", bearer); res.StatusCode != 200 {
		t.Errorf("session page: %d", res.StatusCode)
	}
}

func TestSessionResponseImportedBySession(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.root, "a")
	id := e.open(t, dir)
	main := e.ts.URL + "/p/" + id
	_, body := do(t, "POST", main+"/api/sessions", `{"name":"s"}`, bearer)
	var created struct{ URL string }
	json.Unmarshal([]byte(body), &created)
	other := e.ts.URL + strings.TrimSuffix(created.URL, "/")
	do(t, "POST", other+"/api/comments", `{"scope":"project","label":"must","body":"x"}`, bearer)
	_, body = do(t, "POST", other+"/api/rounds/submit", `{}`, bearer)
	var sub struct{ ResponsePath string }
	json.Unmarshal([]byte(body), &sub)
	resp := `{"round":1,"responses":[{"id":"C-1","status":"addressed","message":"ok"}],"summary":"done"}`
	if err := os.WriteFile(sub.ResponsePath, []byte(resp), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body = do(t, "GET", other+"/api/comments", "", bearer)
		if strings.Contains(body, `"status":"addressed"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("response not imported: %s", body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, body := do(t, "GET", main+"/api/project", "", bearer); strings.Contains(body, `"response"`) {
		t.Errorf("main session got the response: %s", body)
	}
}
