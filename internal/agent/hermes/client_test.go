package hermes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes/hermestest"
)

func TestResolveFromEnvFile(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".env"), []byte("# hermes\nDISCORD_BOT_TOKEN=x\nexport API_SERVER_ENABLED=true\nAPI_SERVER_KEY=\"0123456789abcdef0123\"\nAPI_SERVER_PORT=9000 # custom\n"), 0o600)
	o, err := Resolve(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if o.URL != "http://127.0.0.1:9000" || o.APIKey != "0123456789abcdef0123" {
		t.Errorf("resolved %+v", o)
	}
	if _, err := Resolve(Options{Home: t.TempDir()}); err != ErrNotConfigured {
		t.Errorf("missing config: %v", err)
	}
}

func TestHomeDirCandidates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERMES_HOME", "")
	os.MkdirAll(filepath.Join(home, ".hermes"), 0o700)
	os.WriteFile(filepath.Join(home, ".hermes", ".env"), []byte("DISCORD_BOT_TOKEN=x\n"), 0o600)
	os.MkdirAll(filepath.Join(home, "hermes"), 0o700)
	os.WriteFile(filepath.Join(home, "hermes", ".env"), []byte("API_SERVER_KEY=0123456789abcdef0123\n"), 0o600)
	if got := HomeDir(); got != filepath.Join(home, "hermes") {
		t.Errorf("HomeDir = %q", got)
	}
	t.Setenv("HERMES_HOME", "/custom")
	if got := HomeDir(); got != "/custom" {
		t.Errorf("HomeDir with env = %q", got)
	}
}

func newClient(t *testing.T, f *hermestest.Fake) *Client {
	t.Helper()
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	return New(Options{URL: ts.URL, APIKey: f.Key})
}

func collect(t *testing.T, r agent.Run, onApproval func(*agent.Approval)) []agent.Event {
	t.Helper()
	var out []agent.Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-r.Events():
			if !ok {
				return out
			}
			out = append(out, ev)
			if ev.Type == agent.EventApproval && onApproval != nil {
				onApproval(ev.Approval)
			}
		case <-timeout:
			t.Fatalf("timeout; got %+v", out)
		}
	}
}

func TestSessions(t *testing.T) {
	f := &hermestest.Fake{Key: "k", Sessions: []map[string]any{
		{"id": "cli1", "source": "cli", "title": "CLI", "last_active": 300.0},
		{"id": "d1", "source": "discord", "title": "Old thread", "last_active": 100.0},
		{"id": "d2", "source": "discord", "title": "New thread", "last_active": "200"},
		{"id": "sub", "source": "subagent", "title": "child", "last_active": 400.0},
		{"id": "h", "source": "discord", "title": "hidden", "hidden": true},
	}}
	c := newClient(t, f)
	ss, err := c.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range ss {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "d2,d1,cli1" {
		t.Errorf("order = %v", ids)
	}

	bad := New(Options{URL: c.opts.URL, APIKey: "wrong"})
	if _, err := bad.Sessions(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("auth error = %v", err)
	}
}

func TestCreateSession(t *testing.T) {
	f := &hermestest.Fake{Key: "k"}
	c := newClient(t, f)
	session, err := c.CreateSession(context.Background(), "reviewer: Q-1 test")
	if err != nil {
		t.Fatal(err)
	}
	if session.ID == "" || session.Title != "reviewer: Q-1 test" || session.Source != "api_server" {
		t.Fatalf("session = %+v", session)
	}
	if len(f.SessionRequests) != 1 || f.SessionRequests[0].Source != "api_server" {
		t.Fatalf("requests = %+v", f.SessionRequests)
	}
	_, err = c.CreateSession(context.Background(), session.Title)
	var apiErr *APIError
	if !errors.Is(err, agent.ErrTitleInUse) || !errors.As(err, &apiErr) {
		t.Fatalf("duplicate title error = %v", err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Code != "invalid_title" || apiErr.Message != "Title already in use by session "+session.ID {
		t.Fatalf("API error = %+v", apiErr)
	}
	if len(f.Sessions) != 1 {
		t.Fatalf("sessions = %+v", f.Sessions)
	}
}

func TestCreateSessionTitleConflictErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		code     string
		conflict bool
	}{
		{"code", 400, `{"error":{"message":"conflict","code":"invalid_title"}}`, "invalid_title", true},
		{"message", 400, `{"error":{"message":"Title already in use by session s1"}}`, "", true},
		{"legacy string", 400, `{"error":"Title already in use by session s1"}`, "", true},
		{"other error", 400, `{"error":{"message":"Invalid session ID","code":"invalid_session_id"}}`, "invalid_session_id", false},
		{"other status", 500, `{"error":{"message":"Title already in use by session s1","code":"invalid_title"}}`, "invalid_title", false},
		{"plain text", 502, "Bad gateway", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer ts.Close()
			c := New(Options{URL: ts.URL})
			_, err := c.CreateSession(context.Background(), "title")
			if errors.Is(fmt.Errorf("create session: %w", err), agent.ErrTitleInUse) != tt.conflict {
				t.Fatalf("title conflict = %v, error = %v", tt.conflict, err)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != tt.status || apiErr.Code != tt.code || apiErr.Message == "" {
				t.Fatalf("API error = %+v", err)
			}
		})
	}
}

func TestRunEvents(t *testing.T) {
	f := &hermestest.Fake{
		Key:       "k",
		Keepalive: time.Millisecond,
		Rotate:    map[string]string{"s1": "s1-live"},
		Script: func(string) []hermestest.Step {
			return []hermestest.Step{
				{Event: "message.delta", Fields: map[string]any{"delta": "a"}},
				{Event: "message.delta", Fields: map[string]any{"delta": "b"}},
				{Event: "tool.started", Fields: map[string]any{"tool": "patch", "preview": "ch1.md"}},
				{Event: "tool.completed", Fields: map[string]any{"tool": "patch", "error": true}},
				{Approval: true},
				{Event: "message.delta", Fields: map[string]any{"delta": "c"}},
			}
		},
	}
	c := newClient(t, f)
	run, err := c.Start(context.Background(), agent.Request{SessionID: "s1", Prompt: "do it", IdempotencyKey: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, run, func(a *agent.Approval) {
		if len(a.Choices) != 4 || a.Command == "" {
			t.Errorf("approval = %+v", a)
		}
		if err := run.Answer(context.Background(), a.ID, "deny"); err != nil {
			t.Error(err)
		}
	})
	var kinds []string
	for _, e := range evs {
		k := e.Type
		if e.Type == agent.EventText {
			k += "(" + e.Text + ")"
		}
		if e.Type == agent.EventTool {
			k += "(" + e.ToolState + ")"
		}
		if e.Type == agent.EventDone {
			k += "(" + e.Status + ")"
		}
		kinds = append(kinds, k)
	}
	want := "started,text(ab),tool(started),tool(failed),approval,answered,text(c),done(completed)"
	if strings.Join(kinds, ",") != want {
		t.Errorf("events = %v\nwant %v", kinds, want)
	}
	if got := run.(*Run).LiveSessionID(); got != "s1-live" {
		t.Errorf("live session = %q", got)
	}
	if f.Requests[0].SessionID != "s1" || f.Requests[0].IdempotencyKey != "r1" {
		t.Errorf("request = %+v", f.Requests[0])
	}
}

func TestStop(t *testing.T) {
	f := &hermestest.Fake{Key: "k", Script: func(string) []hermestest.Step {
		return []hermestest.Step{{Event: "message.delta", Fields: map[string]any{"delta": "x"}, Delay: 5 * time.Second}}
	}}
	c := newClient(t, f)
	run, err := c.Start(context.Background(), agent.Request{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		run.Stop(context.Background())
	}()
	evs := collect(t, run, nil)
	last := evs[len(evs)-1]
	if last.Type != agent.EventDone || last.Status != agent.StatusCancelled {
		t.Errorf("last = %+v", last)
	}
}
