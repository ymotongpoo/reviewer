package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent/hermes"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes/hermestest"
	"github.com/ymotongpoo/reviewer/internal/app"
	"github.com/ymotongpoo/reviewer/internal/store"
)

func TestSecurityHeadersKeepSameOriginImages(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, r)
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "img-src 'self' data:") || strings.Contains(csp, "img-src 'self' data: https:") {
		t.Fatalf("CSP image sources = %q", csp)
	}
}

type env struct {
	ts    *httptest.Server
	reg   *Registry
	root  string // allowed root
	state string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	state := t.TempDir()
	for _, d := range []string{"a", "b"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
		os.WriteFile(filepath.Join(root, d, d+".md"), []byte("# "+d+"\nline\n"), 0o644)
	}
	reg, err := NewRegistry([]string{root}, state, "http://host:7777", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New(reg, "secret", 7777)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { ts.Close(); reg.CloseAll() })
	return &env{ts: ts, reg: reg, root: root, state: state}
}

var bearer = map[string]string{"Authorization": "Bearer secret", "X-Reviewer": "1"}

func do(t *testing.T, method, url, body string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

func (e *env) open(t *testing.T, path string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"path": path})
	res, body := do(t, "POST", e.ts.URL+"/api/projects/open", string(b), bearer)
	if res.StatusCode != 200 {
		t.Fatalf("open %s: %d %s", path, res.StatusCode, body)
	}
	var out struct{ ID string }
	json.Unmarshal([]byte(body), &out)
	return out.ID
}

func TestAuth(t *testing.T) {
	e := newEnv(t)
	if res, _ := do(t, "GET", e.ts.URL+"/api/projects", "", nil); res.StatusCode != 401 {
		t.Errorf("no token: %d", res.StatusCode)
	}
	res, _ := do(t, "GET", e.ts.URL+"/?token=secret", "", nil)
	if res.StatusCode != 302 || len(res.Cookies()) != 1 || res.Header.Get("Location") != "/" {
		t.Errorf("token login: %d %v %q", res.StatusCode, res.Cookies(), res.Header.Get("Location"))
	}
	if res, _ := do(t, "GET", e.ts.URL+"/api/projects", "", map[string]string{"Cookie": "reviewer_token_7777=secret"}); res.StatusCode != 200 {
		t.Errorf("cookie: %d", res.StatusCode)
	}
	if res, _ := do(t, "POST", e.ts.URL+"/api/projects/open", `{"path":"x"}`, map[string]string{"Authorization": "Bearer secret"}); res.StatusCode != 403 {
		t.Errorf("missing X-Reviewer: %d", res.StatusCode)
	}
}

func TestProjects(t *testing.T) {
	e := newEnv(t)
	idA := e.open(t, filepath.Join(e.root, "a"))
	if err := os.WriteFile(filepath.Join(e.root, "a", "pixel.bin"), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	assetRes, assetBody := do(t, "GET", e.ts.URL+"/p/"+idA+"/api/asset?path=pixel.bin", "", bearer)
	if assetRes.StatusCode != 200 || assetBody != "binary" {
		t.Fatalf("asset: %d %q", assetRes.StatusCode, assetBody)
	}
	if res, _ := do(t, "GET", e.ts.URL+"/p/"+idA+"/api/asset?path=../pixel.bin", "", bearer); res.StatusCode != 400 {
		t.Fatalf("asset traversal: %d", res.StatusCode)
	}
	if again := e.open(t, filepath.Join(e.root, "a", ".")); again != idA {
		t.Errorf("id not stable: %s vs %s", idA, again)
	}
	idB := e.open(t, filepath.Join(e.root, "b"))

	res, body := do(t, "GET", e.ts.URL+"/p/"+idA+"/api/tree", "", bearer)
	if res.StatusCode != 200 || !strings.Contains(body, "a.md") || strings.Contains(body, "b.md") {
		t.Errorf("tree a: %d %s", res.StatusCode, body)
	}
	if res, _ := do(t, "GET", e.ts.URL+"/p/000000000000/api/tree", "", bearer); res.StatusCode != 404 {
		t.Errorf("unknown project: %d", res.StatusCode)
	}
	for _, p := range []string{"../etc/passwd", "/etc/passwd", ".reviewer/state.json"} {
		if res, _ := do(t, "GET", e.ts.URL+"/p/"+idA+"/api/file?path="+p, "", bearer); res.StatusCode != 400 && res.StatusCode != 404 {
			t.Errorf("file %s: %d", p, res.StatusCode)
		}
	}
	if res, _ := do(t, "GET", e.ts.URL+"/p/"+idA+"/", "", bearer); res.StatusCode != 200 {
		t.Errorf("spa under project: %d", res.StatusCode)
	}

	// Outside the roots, directly or through a symlink.
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(e.root, "link"))
	for _, p := range []string{outside, filepath.Join(e.root, "link"), filepath.Join(e.root, "..")} {
		b, _ := json.Marshal(map[string]string{"path": p})
		if res, _ := do(t, "POST", e.ts.URL+"/api/projects/open", string(b), bearer); res.StatusCode != 403 {
			t.Errorf("open outside %s: %d", p, res.StatusCode)
		}
	}

	// The list shows both, newest first, with their state.
	_, body = do(t, "GET", e.ts.URL+"/api/projects", "", bearer)
	var list struct{ Projects []ProjectSummary }
	json.Unmarshal([]byte(body), &list)
	if len(list.Projects) != 2 || list.Projects[0].ID != idB || !list.Projects[1].Open || list.Projects[1].Round != 1 || !list.Projects[1].Initialized {
		t.Errorf("list = %+v", list.Projects)
	}

	// Directory browser.
	_, body = do(t, "GET", e.ts.URL+"/api/fs?path="+e.root, "", bearer)
	if !strings.Contains(body, `"name":"a","path"`) || !strings.Contains(body, `"hasReviewer":true`) {
		t.Errorf("fs = %s", body)
	}
	if res, _ := do(t, "GET", e.ts.URL+"/api/fs?path=/", "", bearer); res.StatusCode != 403 {
		t.Errorf("fs outside: %d", res.StatusCode)
	}

	// Close, then a request reopens it from the recent list (as after a restart).
	if res, _ := do(t, "POST", e.ts.URL+"/api/projects/"+idA+"/close", "", bearer); res.StatusCode != 200 {
		t.Errorf("close: %d", res.StatusCode)
	}
	reg2, _ := NewRegistry([]string{e.root}, e.state, "", nil)
	defer reg2.CloseAll()
	if p, err := reg2.Get(idA); err != nil || p.Root != filepath.Join(e.root, "a") {
		t.Errorf("reopen from recent: %v %v", p, err)
	}
	// Forget removes it from the list but keeps the data.
	do(t, "DELETE", e.ts.URL+"/api/projects/"+idA, "", bearer)
	if _, err := e.reg.Get(idA); err == nil {
		t.Error("forgotten project still reachable")
	}
	if _, err := os.Stat(filepath.Join(e.root, "a", ".reviewer", "state.json")); err != nil {
		t.Errorf("data removed: %v", err)
	}
}

func TestEventsAreScoped(t *testing.T) {
	e := newEnv(t)
	idA := e.open(t, filepath.Join(e.root, "a"))
	idB := e.open(t, filepath.Join(e.root, "b"))

	req, _ := http.NewRequest("GET", e.ts.URL+"/p/"+idB+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data:") {
				lines <- sc.Text()
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	do(t, "POST", e.ts.URL+"/p/"+idA+"/api/comments", `{"scope":"project","body":"for a"}`, bearer)
	select {
	case l := <-lines:
		t.Errorf("project b received an event of a: %s", l)
	case <-time.After(300 * time.Millisecond):
	}
	do(t, "POST", e.ts.URL+"/p/"+idB+"/api/comments", `{"scope":"project","body":"for b"}`, bearer)
	select {
	case <-lines:
	case <-time.After(2 * time.Second):
		t.Error("project b did not receive its own event")
	}
}

func TestAnnotateSessionTitleAPI(t *testing.T) {
	fake := &hermestest.Fake{Key: "k", Sessions: []map[string]any{{"id": "existing", "title": "使用中"}}}
	ts := httptest.NewServer(fake)
	defer ts.Close()
	e := newEnv(t)
	id := e.open(t, filepath.Join(e.root, "a"))
	p, err := e.reg.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	p.App.ConfigureAgent(hermes.New(hermes.Options{URL: ts.URL, APIKey: "k"}), "", nil, "test", "")
	base := e.ts.URL + "/p/" + id + "/api/annotate"
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"target":"new","sessionTitle":"使用中"}`, http.StatusConflict},
		{`{"target":"new","sessionTitle":"改\n行"}`, http.StatusBadRequest},
		{`{"target":"bound","sessionTitle":""}`, http.StatusBadRequest},
		{`{"target":"session","sessionTitle":"名前"}`, http.StatusBadRequest},
	} {
		res, body := do(t, "POST", base, tc.body, bearer)
		if res.StatusCode != tc.code {
			t.Fatalf("annotate %s: %d %s, want %d", tc.body, res.StatusCode, body, tc.code)
		}
	}
	res, body := do(t, "GET", base+"/requests", "", bearer)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"requests":[]`) {
		t.Fatalf("failed requests: %d %s", res.StatusCode, body)
	}
	res, body = do(t, "POST", base, `{"target":"new","sessionTitle":"指定名"}`, bearer)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("annotate: %d %s", res.StatusCode, body)
	}
	var result app.AnnotateResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	if result.Request == nil || result.Request.SessionTitle != "指定名" {
		t.Fatalf("annotate response: %s", body)
	}
	for _, path := range []string{"/requests", "/requests/" + result.Request.ID} {
		res, body = do(t, "GET", base+path, "", bearer)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, `"sessionTitle":"指定名"`) {
			t.Fatalf("GET %s: %d %s", path, res.StatusCode, body)
		}
	}
	// Let the run finish writing into the temporary directory before cleanup.
	deadline := time.Now().Add(10 * time.Second)
	for len(p.App.AgentInfo().Active) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("annotation run did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAnnotationAPI(t *testing.T) {
	e := newEnv(t)
	id := e.open(t, filepath.Join(e.root, "a"))
	p, err := e.reg.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	fv, err := p.App.File("a.md")
	if err != nil {
		t.Fatal(err)
	}
	req := &store.AnnotationRequest{
		ID: p.App.Store.NewRequestID(), Prompt: "確認", Preset: "test",
		Files: map[string]string{"a.md": fv.Hash}, Target: "new", CreatedAt: time.Now(),
	}
	if err := p.App.Store.PutRequest(req); err != nil {
		t.Fatal(err)
	}
	annotations := `{"request":"` + req.ID + `","annotations":[{"path":"a.md","startLine":2,"endLine":2,"quote":["line"],"severity":"major","confidence":"high","body":"wrong"}]}`
	if err := os.WriteFile(filepath.Join(p.App.Store.RequestDir(req.ID), "annotations.json"), []byte(annotations), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.App.ImportAnnotations(req.ID); err != nil {
		t.Fatal(err)
	}
	base := e.ts.URL + "/p/" + id

	res, body := do(t, "GET", base+"/api/presets", "", bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"origin":"builtin"`) {
		t.Fatalf("presets: %d %s", res.StatusCode, body)
	}
	res, body = do(t, "PUT", base+"/api/presets", `{"presets":[{"name":"project","prompt":"check","scope":"all"}]}`, bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"origin":"project"`) {
		t.Fatalf("save presets: %d %s", res.StatusCode, body)
	}
	res, body = do(t, "GET", base+"/api/annotate/requests", "", bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"id":"`+req.ID+`"`) {
		t.Fatalf("requests: %d %s", res.StatusCode, body)
	}
	res, body = do(t, "GET", base+"/api/annotate/requests/"+req.ID, "", bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"runs":[]`) {
		t.Fatalf("request: %d %s", res.StatusCode, body)
	}
	res, body = do(t, "GET", base+"/api/annotations", "", bearer)
	var list struct {
		Annotations []store.Annotation `json:"annotations"`
	}
	json.Unmarshal([]byte(body), &list)
	if res.StatusCode != 200 || len(list.Annotations) != 1 {
		t.Fatalf("annotations: %d %s", res.StatusCode, body)
	}
	annotationID := list.Annotations[0].ID
	res, _ = do(t, "PATCH", base+"/api/annotations/"+annotationID, `{"state":"dismissed"}`, bearer)
	if res.StatusCode != 200 {
		t.Fatalf("dismiss: %d", res.StatusCode)
	}
	_, body = do(t, "GET", base+"/api/annotations", "", bearer)
	if !strings.Contains(body, `"annotations":[]`) {
		t.Fatalf("dismissed annotations: %s", body)
	}
	res, body = do(t, "GET", base+"/api/annotations?all=1", "", bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"state":"dismissed"`) {
		t.Fatalf("all annotations: %d %s", res.StatusCode, body)
	}
	do(t, "PATCH", base+"/api/annotations/"+annotationID, `{"state":"pending"}`, bearer)
	res, body = do(t, "POST", base+"/api/annotations/"+annotationID+"/adopt", `{"label":"question","body":"override"}`, bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"label":"question"`) || !strings.Contains(body, `"body":"override"`) {
		t.Fatalf("adopt: %d %s", res.StatusCode, body)
	}

	if err := os.WriteFile(filepath.Join(e.root, "a", "a.md"), []byte("# a\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, body = do(t, "GET", base+"/api/annotate/requests/"+req.ID+"/diff?path=a.md", "", bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"kind":"modified"`) {
		t.Fatalf("diff: %d %s", res.StatusCode, body)
	}
	res, body = do(t, "PATCH", base+"/api/annotate/requests/"+req.ID, `{"hidden":true}`, bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"hidden":true`) {
		t.Fatalf("hide: %d %s", res.StatusCode, body)
	}
	res, body = do(t, "POST", base+"/api/annotate/requests/"+req.ID+"/discard", "", bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("discard: %d %s", res.StatusCode, body)
	}
}

func TestGitAPI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	e := newEnv(t)
	dir := filepath.Join(e.root, "a")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Tester"},
		{"config", "user.email", "tester@example.com"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	b, _ := json.Marshal(map[string]string{"path": dir, "gitLanguage": "fr"})
	if res, _ := do(t, "POST", e.ts.URL+"/api/projects/open", string(b), bearer); res.StatusCode != 400 {
		t.Fatalf("bad language: %d", res.StatusCode)
	}
	b, _ = json.Marshal(map[string]string{"path": dir, "gitLanguage": "en"})
	res, body := do(t, "POST", e.ts.URL+"/api/projects/open", string(b), bearer)
	if res.StatusCode != 200 {
		t.Fatalf("open: %d %s", res.StatusCode, body)
	}
	var opened struct{ ID string }
	json.Unmarshal([]byte(body), &opened)
	base := e.ts.URL + "/p/" + opened.ID

	res, body = do(t, "GET", base+"/api/git", "", bearer)
	var st app.GitStatus
	if err := json.Unmarshal([]byte(body), &st); res.StatusCode != 200 || err != nil {
		t.Fatalf("status: %d %s", res.StatusCode, body)
	}
	if !st.Repo || st.Settings.Language != "en" || len(st.Changes) != 1 || st.Changes[0].Path != "a.md" || st.Messages["en"] != "docs: add a.md\n" {
		t.Fatalf("status = %s", body)
	}
	if !st.Unborn || len(st.PushBlockers) == 0 {
		t.Fatalf("push should be blocked before the first commit: %s", body)
	}

	if res, _ := do(t, "PUT", base+"/api/git/settings", `{"language":"xx"}`, bearer); res.StatusCode != 400 {
		t.Fatalf("settings: %d", res.StatusCode)
	}
	if res, _ := do(t, "POST", base+"/api/git/commit", `{"message":"docs: x"}`, map[string]string{"Authorization": "Bearer secret"}); res.StatusCode != 403 {
		t.Fatalf("commit without X-Reviewer: %d", res.StatusCode)
	}
	if res, _ := do(t, "POST", base+"/api/git/commit", `{"message":"add a","fingerprint":"`+st.Fingerprint+`"}`, bearer); res.StatusCode != 400 {
		t.Fatalf("bad message: %d", res.StatusCode)
	}
	res, body = do(t, "POST", base+"/api/git/commit", `{"message":"docs: add a.md","fingerprint":"`+st.Fingerprint+`"}`, bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"subject":"docs: add a.md"`) {
		t.Fatalf("commit: %d %s", res.StatusCode, body)
	}
	if res, body := do(t, "POST", base+"/api/git/push", `{"remote":"origin","branch":"main"}`, bearer); res.StatusCode != 400 {
		t.Fatalf("push without remotes: %d %s", res.StatusCode, body)
	}

	// Reopening with another language keeps the saved choice.
	b, _ = json.Marshal(map[string]string{"path": dir, "gitLanguage": "ja"})
	if res, body := do(t, "POST", e.ts.URL+"/api/projects/open", string(b), bearer); res.StatusCode != 200 {
		t.Fatalf("reopen: %d %s", res.StatusCode, body)
	}
	_, body = do(t, "GET", base+"/api/git", "", bearer)
	if err := json.Unmarshal([]byte(body), &st); err != nil || st.Settings.Language != "en" {
		t.Fatalf("reopen changed the language: %s", body)
	}
	res, body = do(t, "PUT", base+"/api/git/settings", `{"language":"ja","remote":"","branch":"main"}`, bearer)
	if res.StatusCode != 200 || !strings.Contains(body, `"language":"ja"`) || !strings.Contains(body, `"branch":"main"`) {
		t.Fatalf("save settings: %d %s", res.StatusCode, body)
	}
	_, body = do(t, "GET", base+"/api/git", "", bearer)
	if err := json.Unmarshal([]byte(body), &st); err != nil || st.Settings.Language != "ja" || st.Unborn || len(st.Changes) != 0 {
		t.Fatalf("after commit: %s", body)
	}
}

func TestSaveFileAPI(t *testing.T) {
	e := newEnv(t)
	id := e.open(t, filepath.Join(e.root, "a"))
	_, body := do(t, "GET", e.ts.URL+"/p/"+id+"/api/file?path=a.md", "", bearer)
	var fv struct{ Content, Hash string }
	json.Unmarshal([]byte(body), &fv)

	put := func(content, hash string) (*http.Response, string) {
		b, _ := json.Marshal(map[string]string{"path": "a.md", "content": content, "hash": hash})
		return do(t, "PUT", e.ts.URL+"/p/"+id+"/api/file", string(b), bearer)
	}
	if res, body := put("# a\nedited\n", "stale"); res.StatusCode != http.StatusConflict {
		t.Fatalf("stale save: %d %s", res.StatusCode, body)
	}
	res, body := put("# a\nedited\n", fv.Hash)
	if res.StatusCode != 200 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	var saved struct {
		OK   bool
		Hash string
	}
	json.Unmarshal([]byte(body), &saved)
	_, body = do(t, "GET", e.ts.URL+"/p/"+id+"/api/file?path=a.md", "", bearer)
	var after struct{ Content, Hash string }
	json.Unmarshal([]byte(body), &after)
	if !saved.OK || saved.Hash == "" || saved.Hash != after.Hash || after.Content != "# a\nedited\n" {
		t.Fatalf("saved %+v, file %+v", saved, after)
	}
}

func TestRangeCommentAPI(t *testing.T) {
	e := newEnv(t)
	id := e.open(t, filepath.Join(e.root, "a"))
	_, body := do(t, "GET", e.ts.URL+"/p/"+id+"/api/file?path=a.md", "", bearer)
	var fv struct{ Hash string }
	json.Unmarshal([]byte(body), &fv)

	create := func(hash, text string) (*http.Response, string) {
		b, _ := json.Marshal(map[string]any{
			"scope": "line", "path": "a.md", "label": "must", "body": "x", "hash": hash,
			"range": map[string]any{"startLine": 2, "startColumn": 1, "endLine": 2, "endColumn": 3, "text": text},
		})
		return do(t, "POST", e.ts.URL+"/p/"+id+"/api/comments", string(b), bearer)
	}
	res, body := create(fv.Hash, "in")
	if res.StatusCode != 200 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var c struct {
		Range struct {
			StartColumn int
			Text        string
		}
		Loc struct{ Start, End int }
	}
	json.Unmarshal([]byte(body), &c)
	if c.Range.Text != "in" || c.Range.StartColumn != 1 || c.Loc.Start != 2 || c.Loc.End != 2 {
		t.Fatalf("created %s", body)
	}
	if res, body := create(fv.Hash, "ne"); res.StatusCode != http.StatusConflict {
		t.Fatalf("mismatched text: %d %s", res.StatusCode, body)
	}
	if res, body := create(strings.Repeat("0", 64), "in"); res.StatusCode != http.StatusConflict {
		t.Fatalf("unknown hash: %d %s", res.StatusCode, body)
	}
}
