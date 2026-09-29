package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/app"
	"github.com/ymotongpoo/reviewer/internal/config"
)

func newTestServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.md"), []byte("# a\nline\n"), 0o644)
	hub := NewHub()
	a, err := app.New(root, config.Default(), hub.Publish)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	s := New(a, "secret", 7777, hub)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s
}

func do(t *testing.T, method, url, body string, hdr map[string]string) *http.Response {
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
	res.Body.Close()
	return res
}

func TestAuth(t *testing.T) {
	ts, _ := newTestServer(t)
	bearer := map[string]string{"Authorization": "Bearer secret"}

	if res := do(t, "GET", ts.URL+"/api/tree", "", nil); res.StatusCode != 401 {
		t.Errorf("no token: %d", res.StatusCode)
	}
	if res := do(t, "GET", ts.URL+"/api/tree", "", map[string]string{"Authorization": "Bearer nope"}); res.StatusCode != 401 {
		t.Errorf("bad token: %d", res.StatusCode)
	}
	res := do(t, "GET", ts.URL+"/?token=secret", "", nil)
	if res.StatusCode != 302 || len(res.Cookies()) != 1 || res.Header.Get("Location") != "/" {
		t.Errorf("token login: %d %v %q", res.StatusCode, res.Cookies(), res.Header.Get("Location"))
	}
	cookie := map[string]string{"Cookie": "reviewer_token_7777=secret"}
	if res := do(t, "GET", ts.URL+"/api/tree", "", cookie); res.StatusCode != 200 {
		t.Errorf("cookie: %d", res.StatusCode)
	}
	body := `{"scope":"project","body":"x"}`
	if res := do(t, "POST", ts.URL+"/api/comments", body, bearer); res.StatusCode != 403 {
		t.Errorf("missing X-Reviewer: %d", res.StatusCode)
	}
	bearer["X-Reviewer"] = "1"
	if res := do(t, "POST", ts.URL+"/api/comments", body, bearer); res.StatusCode != 200 {
		t.Errorf("create: %d", res.StatusCode)
	}
	for _, p := range []string{"../etc/passwd", "/etc/passwd", ".reviewer/state.json"} {
		if res := do(t, "GET", ts.URL+"/api/file?path="+p, "", bearer); res.StatusCode != 400 && res.StatusCode != 404 {
			t.Errorf("file %s: %d", p, res.StatusCode)
		}
	}
	if res := do(t, "GET", ts.URL+"/api/file?path=a.md", "", bearer); res.StatusCode != 200 {
		t.Errorf("file a.md: %d", res.StatusCode)
	}
	if res := do(t, "GET", ts.URL+"/some/spa/route", "", bearer); res.StatusCode != 200 {
		t.Errorf("spa fallback: %d", res.StatusCode)
	}
}
