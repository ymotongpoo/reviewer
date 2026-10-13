package t3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

// fake is a T3 Code MCP endpoint with scripted runs.
type fake struct {
	t     *testing.T
	token string
	sse   bool

	mu       sync.Mutex
	calls    []call
	reads    int
	launched map[string]any
	// items returns the activity timeline after the given number of reads.
	items  func(reads int) ([]item, string)
	expire bool // reject the current token once
}

type call struct {
	Tool string
	Args map[string]any
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	f := &fake{t: t, token: "tok"}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)
	return f, ts
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	expire, token := f.expire, f.token
	f.expire = false
	f.mu.Unlock()
	if expire || r.Header.Get("Authorization") != "Bearer "+token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	switch req.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "s1")
		f.reply(w, req.ID, map[string]any{"protocolVersion": mcpProtocolVersion})
		return
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if r.Header.Get("Mcp-Session-Id") != "s1" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{req.Params.Name, req.Params.Arguments})
	f.mu.Unlock()
	out, err := f.tool(req.Params.Name, req.Params.Arguments)
	if err != "" {
		f.reply(w, req.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": err}}, "isError": true})
		return
	}
	b, _ := json.Marshal(out)
	f.reply(w, req.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}, "structuredContent": out})
}

func (f *fake) reply(w http.ResponseWriter, id json.RawMessage, result any) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if f.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func (f *fake) tool(name string, args map[string]any) (any, string) {
	switch name {
	case "t3_project_list":
		return map[string]any{"projects": []map[string]any{
			{"id": "p-repo", "title": "zenn", "workspaceRoot": "/r/zenn"},
			{"id": "p-book", "title": "book-a", "workspaceRoot": "/r/zenn/books/a"},
			{"id": "p-other", "title": "other", "workspaceRoot": "/r/other"},
		}, "nextCursor": nil}, ""
	case "t3_thread_list":
		switch args["projectId"] {
		case "p-repo":
			return map[string]any{"threads": []map[string]any{
				{"threadId": "t-old", "title": "old", "status": "completed", "providerInstanceId": "codex", "model": "m-old", "updatedAt": "2026-10-01T00:00:00Z"},
				{"threadId": "t-sub", "title": "sub", "status": "completed", "relationshipToParent": "subagent", "updatedAt": "2026-10-09T00:00:00Z"},
			}}, ""
		case "p-book":
			return map[string]any{"threads": []map[string]any{
				{"threadId": "t-new", "title": "new", "status": "running", "providerInstanceId": "claudeAgent", "model": "m-new", "updatedAt": "2026-10-05T00:00:00Z"},
			}}, ""
		}
		return map[string]any{"threads": []any{}}, ""
	case "orchestrator_capabilities":
		return map[string]any{"providers": []map[string]any{
			{"providerInstanceId": "codex", "displayName": "Codex", "models": []map[string]any{{"id": "gpt-x", "label": "GPT-X"}}},
			{"providerInstanceId": "cursor", "displayName": "Cursor", "models": []any{}},
			{"providerInstanceId": "claudeAgent", "displayName": "Claude", "models": []map[string]any{{"id": "m-new", "label": "M New"}, {"id": "m-raw"}}},
		}}, ""
	case "t3_thread_launch":
		f.mu.Lock()
		f.launched = args
		f.mu.Unlock()
		return map[string]any{"threadId": "t-launched", "projectId": args["projectId"]}, ""
	case "t3_thread_send":
		if args["threadId"] == "t-busy" {
			return nil, `{"message":"The target thread has broader permission modes than the caller."}`
		}
		return map[string]any{"threadId": args["threadId"], "runId": "run-1", "status": "queued", "delivery": "queued"}, ""
	case "t3_thread_read":
		f.mu.Lock()
		f.reads++
		n := f.reads
		f.mu.Unlock()
		items, status := f.items(n)
		after := -1
		if v, ok := args["afterPosition"].(float64); ok {
			after = int(v)
		}
		var out []item
		for _, it := range items {
			if it.Position > after {
				out = append(out, it)
			}
		}
		return map[string]any{
			"thread":     map[string]any{"status": status, "latestRunId": "run-1"},
			"recentRuns": []map[string]any{{"runId": "run-1", "status": status}},
			"items":      out, "hasMore": false,
		}, ""
	case "t3_thread_wait":
		return map[string]any{"threadId": args["threadId"], "runId": args["runId"], "status": "running", "timedOut": true}, ""
	case "t3_thread_interrupt":
		return map[string]any{"threadId": args["threadId"], "runId": args["runId"], "status": "interrupt_requested"}, ""
	}
	return nil, "unknown tool " + name
}

func (f *fake) called(tool string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, c := range f.calls {
		if c.Tool == tool {
			out = append(out, c.Args)
		}
	}
	return out
}

func client(ts *httptest.Server, token string) *Client {
	return New(Options{URL: ts.URL, Token: token, Access: DefaultAccess, PollInterval: 5 * time.Millisecond})
}

func TestSessionsOfRelatedProjects(t *testing.T) {
	for _, sse := range []bool{false, true} {
		f, ts := newFake(t)
		f.sse = sse
		ss, err := client(ts, "tok").Sessions(context.Background(), "/r/zenn")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range ss {
			got = append(got, s.ID+":"+s.Preview)
		}
		// The book project inside the directory is listed, the unrelated one
		// and the subagent are not; newest first.
		if want := "t-new:book-a · 実行中,t-old:zenn"; strings.Join(got, ",") != want {
			t.Errorf("sse=%v sessions = %v, want %s", sse, got, want)
		}
		if ss[0].Source != "t3" {
			t.Errorf("source = %q", ss[0].Source)
		}
	}
}

func TestRelated(t *testing.T) {
	for _, tt := range []struct {
		root, dir string
		want      bool
	}{
		{"/r/zenn", "/r/zenn", true},
		{"/r/zenn", "/r/zenn/books/a", true},
		{"/r/zenn/books/a", "/r/zenn", true},
		{"/r/zenn", "/r/zenn.dev", false},
		{"/r/other", "/r/zenn", false},
	} {
		if got := related(tt.root, tt.dir); got != tt.want {
			t.Errorf("related(%q, %q) = %v", tt.root, tt.dir, got)
		}
	}
}

func TestCreateSessionWithChosenModel(t *testing.T) {
	f, ts := newFake(t)
	_, err := client(ts, "tok").CreateSession(context.Background(), "/r/zenn", "x",
		agent.SessionOptions{Model: agent.ModelRef{Provider: "codex", Model: "gpt-x"}})
	if err != nil {
		t.Fatal(err)
	}
	sel, _ := f.launched["modelSelection"].(map[string]any)
	if f.launched["projectId"] != "p-repo" || sel["instanceId"] != "codex" || sel["model"] != "gpt-x" {
		t.Errorf("launch = %v", f.launched)
	}
}

func TestModels(t *testing.T) {
	_, ts := newFake(t)
	models, recent, err := client(ts, "tok").Models(context.Background(), "/r/zenn/books/a")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range models {
		got = append(got, m.Provider+"/"+m.Model+"="+m.ProviderName+":"+m.Label)
	}
	// Providers without models are left out; a missing label falls back to the id.
	if want := "codex/gpt-x=Codex:GPT-X,claudeAgent/m-new=Claude:M New,claudeAgent/m-raw=Claude:m-raw"; strings.Join(got, ",") != want {
		t.Errorf("models = %v", got)
	}
	if recent == nil || *recent != (agent.ModelRef{Provider: "claudeAgent", Model: "m-new"}) {
		t.Errorf("recent = %v", recent)
	}
	// Outside every project, related projects still tell the recent model.
	if _, recent, _ := client(ts, "tok").Models(context.Background(), "/r"); recent == nil || recent.Model != "m-new" {
		t.Errorf("recent outside projects = %v", recent)
	}
}

func TestCreateSessionInInnermostProject(t *testing.T) {
	f, ts := newFake(t)
	s, err := client(ts, "tok").CreateSession(context.Background(), "/r/zenn/books/a/ch1", "reviewer: Q-1", agent.SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "t-launched" {
		t.Errorf("session = %+v", s)
	}
	if f.launched["projectId"] != "p-book" || f.launched["title"] != "reviewer: Q-1" {
		t.Errorf("launch = %v", f.launched)
	}
	sel, _ := f.launched["modelSelection"].(map[string]any)
	if sel["model"] != "m-new" || sel["instanceId"] != "claudeAgent" {
		t.Errorf("modelSelection = %v", sel)
	}
	if _, err := client(ts, "tok").CreateSession(context.Background(), "/elsewhere", "x", agent.SessionOptions{}); err == nil {
		t.Error("created a thread outside every project")
	}
}

func collect(t *testing.T, run agent.Run) []string {
	t.Helper()
	var out []string
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-run.Events():
			if !ok {
				return out
			}
			s := ev.Type
			switch ev.Type {
			case agent.EventTool:
				s += ":" + ev.Tool + ":" + ev.ToolState
			case agent.EventText, agent.EventThinking:
				s += ":" + ev.Text
			case agent.EventDone:
				s += ":" + ev.Status
			}
			out = append(out, s)
		case <-timeout:
			t.Fatalf("run did not end; events so far: %v", out)
		}
	}
}

func TestRun(t *testing.T) {
	f, ts := newFake(t)
	f.items = func(n int) ([]item, string) {
		items := []item{
			{Position: 0, ItemID: "old", RunID: "run-0", Type: "assistant_message", Status: "completed", Text: "earlier turn"},
			{Position: 1, ItemID: "u", RunID: "run-1", Type: "user_message", Status: "completed"},
			{Position: 2, ItemID: "tool", RunID: "run-1", Type: "command_execution", Status: "inProgress", Title: "Run tests"},
		}
		switch {
		case n == 1:
			return items, "running"
		case n == 2:
			return items, "waiting"
		}
		items[2].Status = "completed"
		items = append(items,
			item{Position: 3, ItemID: "think", RunID: "run-1", Type: "reasoning", Status: "completed", Text: "hmm"},
			item{Position: 4, ItemID: "a", RunID: "run-1", Type: "assistant_message", Status: "completed", Text: "直しました"})
		return items, "completed"
	}
	run, err := client(ts, "tok").Start(context.Background(), agent.Request{SessionID: "t-1", Prompt: "fix", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	got := collect(t, run)
	want := []string{"started", "tool:Run tests:started", "approval", "answered", "tool:Run tests:completed", "thinking:hmm", "text:直しました", "done:completed"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events =\n %v\nwant\n %v", got, want)
	}
	send := f.called("t3_thread_send")
	if len(send) != 1 || send[0]["mode"] != "queue" || send[0]["clientRequestId"] != "k-1" || send[0]["message"] != "fix" {
		t.Errorf("send = %v", send)
	}
	// Reads continue after the last final item of the run.
	reads := f.called("t3_thread_read")
	if last := reads[len(reads)-1]; last["afterPosition"] != float64(1) && last["afterPosition"] != float64(4) {
		t.Errorf("last read = %v", last)
	}
}

func TestRunStopAndFailures(t *testing.T) {
	f, ts := newFake(t)
	stopped := false
	f.items = func(int) ([]item, string) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if stopped {
			return nil, "interrupted"
		}
		return nil, "running"
	}
	c := client(ts, "tok")
	run, err := c.Start(context.Background(), agent.Request{SessionID: "t-1", Prompt: "fix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	stopped = true
	f.mu.Unlock()
	if got := collect(t, run); got[len(got)-1] != "done:cancelled" {
		t.Errorf("events = %v", got)
	}
	if in := f.called("t3_thread_interrupt"); len(in) != 1 || in[0]["runId"] != "run-1" {
		t.Errorf("interrupt = %v", in)
	}
	if err := run.Answer(context.Background(), "", "once"); err == nil {
		t.Error("answered an approval through MCP")
	}
	_, err = c.Start(context.Background(), agent.Request{SessionID: "t-busy", Prompt: "fix"})
	if err == nil || !strings.Contains(err.Error(), "broader permission modes") {
		t.Errorf("tool error = %v", err)
	}
}

func TestAttachSkipsExistingItems(t *testing.T) {
	f, ts := newFake(t)
	f.items = func(n int) ([]item, string) {
		items := []item{{Position: 0, ItemID: "a", RunID: "run-1", Type: "assistant_message", Status: "completed", Text: "before restart"}}
		if n == 1 {
			return items, "running"
		}
		return append(items, item{Position: 1, ItemID: "b", RunID: "run-1", Type: "assistant_message", Status: "completed", Text: "after"}), "completed"
	}
	run, err := client(ts, "tok").Attach(context.Background(), "t-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(collect(t, run), "|"); got != "text:after|done:completed" {
		t.Errorf("events = %s", got)
	}
}

// fakeCLI writes a t3 stand-in that prints a pairing code after a log line.
func fakeCLI(t *testing.T) (cmd, log string) {
	dir := t.TempDir()
	log = filepath.Join(dir, "args")
	cmd = filepath.Join(dir, "t3")
	script := "#!/bin/sh\necho \"$T3CODE_HOME $*\" >> " + log + "\necho '[00:00] WARN migration'\necho '{\"id\":\"x\",\"credential\":\"PAIR\"}'\n"
	if err := os.WriteFile(cmd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return cmd, log
}

func TestAuthorizeWithPairingCode(t *testing.T) {
	f, ts := newFake(t)
	var mu sync.Mutex
	issued := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/mcp/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"client_id":"cid"}`)
	})
	mux.HandleFunc("POST /oauth/mcp/decision", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Authorization map[string]string `json:"authorization"`
			Decision      map[string]string `json:"decision"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Decision["code"] != "PAIR" || req.Decision["access"] == "" || req.Decision["_tag"] != "pairing-code" || req.Authorization["code_challenge_method"] != "S256" {
			http.Error(w, `{"message":"bad decision"}`, http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"redirectTo":%q}`, req.Authorization["redirect_uri"]+"?code=CODE&state="+req.Authorization["state"])
	})
	mux.HandleFunc("POST /oauth/mcp/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("code") != "CODE" || r.Form.Get("code_verifier") == "" || r.Form.Get("client_id") != "cid" {
			http.Error(w, `{"error_description":"bad grant"}`, http.StatusBadRequest)
			return
		}
		mu.Lock()
		issued++
		tok := fmt.Sprintf("tok%d", issued)
		mu.Unlock()
		f.mu.Lock()
		f.token = tok
		f.mu.Unlock()
		fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":2592000,"scope":"x"}`, tok)
	})
	mux.Handle("/mcp", ts.Config.Handler)
	auth := httptest.NewServer(mux)
	defer auth.Close()

	cmd, log := fakeCLI(t)
	home := t.TempDir()
	tokenFile := filepath.Join(t.TempDir(), "state", "t3-token.json")
	opts := Options{URL: auth.URL, Command: cmd, Home: home, TokenFile: tokenFile, PollInterval: time.Millisecond}
	opts, err := Resolve(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(opts).Sessions(context.Background(), "/r/zenn"); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(log)
	if !strings.HasPrefix(string(args), home+" auth pairing create") || !strings.Contains(string(args), "--scope source-control:write") {
		t.Errorf("cli args = %q", args)
	}
	st, err := os.Stat(tokenFile)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("token file: %v %v", st, err)
	}
	// A new client reuses the saved token.
	if _, err := New(opts).Sessions(context.Background(), "/r/zenn"); err != nil || issued != 1 {
		t.Fatalf("reuse: issued=%d err=%v", issued, err)
	}
	// A rejected token is replaced.
	f.mu.Lock()
	f.expire = true
	f.mu.Unlock()
	if _, err := New(opts).Sessions(context.Background(), "/r/zenn"); err != nil || issued != 2 {
		t.Fatalf("renew: issued=%d err=%v", issued, err)
	}
	// A token for another access level is not reused.
	opts.Access = "auto"
	New(opts).Sessions(context.Background(), "/r/zenn")
	if issued != 3 {
		t.Errorf("access change: issued=%d", issued)
	}
}

func TestResolve(t *testing.T) {
	home := t.TempDir()
	if _, err := Resolve(Options{Home: home}); err != ErrNotConfigured {
		t.Errorf("no T3: %v", err)
	}
	os.MkdirAll(filepath.Join(home, "userdata"), 0o755)
	os.WriteFile(filepath.Join(home, "userdata", "server-runtime.json"), []byte(`{"pid":0,"origin":"http://127.0.0.1:3773/"}`), 0o644)
	o, err := Resolve(Options{Home: home, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Access != DefaultAccess {
		t.Errorf("access = %q", o.Access)
	}
	if origin, _ := New(o).origin(); origin != "http://127.0.0.1:3773" {
		t.Errorf("origin = %q", origin)
	}
	if _, err := Resolve(Options{Home: home, Token: "t", Access: "root"}); err == nil {
		t.Error("accepted an unknown access level")
	}
}
