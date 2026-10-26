package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/app"
)

func TestSwitchReviewSessionToBranchBehindMain(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for _, tc := range []struct {
		name       string
		diverged   bool
		remoteOnly bool
	}{{name: "behind"}, {name: "diverged", diverged: true}, {name: "remote-behind", remoteOnly: true}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			dir := filepath.Join(e.root, "a")
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "-q", "-b", "main")
			git("config", "user.name", "Tester")
			git("config", "user.email", "tester@example.invalid")
			git("config", "commit.gpgsign", "false")
			git("add", "a.md")
			git("commit", "-q", "-m", "initial")
			git("branch", "review/pr")
			git("commit", "-q", "--allow-empty", "-m", "main advanced")
			mainHead := git("rev-parse", "main")
			git("switch", "-q", "review/pr")
			if tc.diverged {
				if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# PR content\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				git("add", "a.md")
				git("commit", "-q", "-m", "PR work")
			}
			head := git("rev-parse", "HEAD")
			if got := git("rev-list", "--count", "HEAD..main"); got != "1" {
				t.Fatalf("commits behind main = %q", got)
			}
			git("switch", "-q", "main")
			remote := ""
			if tc.remoteOnly {
				remote = "origin"
				git("remote", "add", remote, dir)
				git("update-ref", "refs/remotes/origin/review/pr", head)
				git("branch", "-D", "review/pr")
			}
			id := e.open(t, dir)
			base := e.ts.URL + "/p/" + id
			res, body := do(t, "POST", base+"/api/sessions", `{"name":"PR review"}`, bearer)
			if res.StatusCode != 200 {
				t.Fatalf("create: %d %s", res.StatusCode, body)
			}
			var created struct {
				URL     string
				Session struct{ ID string }
			}
			if err := json.Unmarshal([]byte(body), &created); err != nil || created.URL == "" {
				t.Fatalf("created = %s: %v", body, err)
			}
			// Both review sessions share the checked-out branch, and both get
			// notified even when the branches have identical file contents.
			for _, sid := range []string{"main", created.Session.ID} {
				p, err := e.reg.GetSession(id, sid)
				if err != nil {
					t.Fatal(err)
				}
				ch := p.Hub.Subscribe()
				defer p.Hub.Unsubscribe(ch)
				defer func() {
					for _, typ := range []string{"git", "files"} {
						select {
						case ev := <-ch:
							if ev.Type != typ {
								t.Errorf("event = %s, want %s", ev.Type, typ)
							}
						default:
							t.Errorf("missing %s event", typ)
						}
					}
				}()
			}
			input, _ := json.Marshal(app.GitSwitchInput{Branch: "review/pr", Remote: remote, Head: mainHead})
			res, body = do(t, "POST", e.ts.URL+created.URL+"api/git/switch", string(input), bearer)
			if res.StatusCode != 200 {
				t.Fatalf("switch: %d %s", res.StatusCode, body)
			}
			res, body = do(t, "GET", e.ts.URL+created.URL+"api/git", "", bearer)
			var status app.GitStatus
			if err := json.Unmarshal([]byte(body), &status); res.StatusCode != 200 || err != nil {
				t.Fatalf("status: %d %s: %v", res.StatusCode, body, err)
			}
			if status.Branch != "review/pr" || status.Head != head {
				t.Fatalf("session branch = %q, HEAD = %q", status.Branch, status.Head)
			}
			if git("rev-parse", "HEAD") != head || git("rev-parse", "main") != mainHead {
				t.Fatal("switching a review session changed Git history")
			}
		})
	}
}

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
