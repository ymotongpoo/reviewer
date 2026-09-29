package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes/hermestest"
	"github.com/ymotongpoo/reviewer/internal/store"
)

type recNotifier struct {
	mu   sync.Mutex
	msgs []string
	sess []string
}

func (n *recNotifier) Notify(_ context.Context, sessionID, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.msgs = append(n.msgs, text)
	n.sess = append(n.sess, sessionID)
	return nil
}

func (n *recNotifier) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string{}, n.msgs...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSendToAgent(t *testing.T) {
	a, root := setup(t)
	fake := &hermestest.Fake{
		Key:      "k",
		Sessions: []map[string]any{{"id": "d1", "source": "discord", "title": "記事を書く", "last_active": 1.0}},
		Rotate:   map[string]string{"d1": "d1-live"},
		Script: func(string) []hermestest.Step {
			return []hermestest.Step{
				{Event: "message.delta", Fields: map[string]any{"delta": "直しました"}},
				{Approval: true},
			}
		},
		// Act like the agent: fix the file and answer every comment.
		OnRun: func(input, _ string) {
			dir := filepath.Join(a.DataDir, "rounds", "1")
			var fb struct {
				Comments []struct {
					ID string `json:"id"`
				} `json:"comments"`
			}
			b, _ := os.ReadFile(filepath.Join(dir, "feedback.json"))
			json.Unmarshal(b, &fb)
			var rs []map[string]string
			for _, c := range fb.Comments {
				rs = append(rs, map[string]string{"id": c.ID, "status": "addressed", "message": "ok"})
			}
			out, _ := json.Marshal(map[string]any{"round": 1, "responses": rs, "summary": "短くしました"})
			os.WriteFile(filepath.Join(dir, "response.json"), out, 0o644)
			os.WriteFile(filepath.Join(root, "ch2.md"), []byte("# Chapter 2 fixed\n"), 0o644)
		},
	}
	ts := httptest.NewServer(fake)
	defer ts.Close()
	n := &recNotifier{}
	a.ConfigureAgent(hermes.New(hermes.Options{URL: ts.URL, APIKey: "k"}), "", n, "test", "http://devbox.local:7777/")

	ctx := context.Background()
	if _, err := a.SendToAgent(ctx, 1); err == nil {
		t.Error("send without binding accepted")
	}
	if _, err := a.BindAgent(ctx, "nope"); err == nil {
		t.Error("unknown session accepted")
	}
	if b, err := a.BindAgent(ctx, "d1"); err != nil || b.Title != "記事を書く" {
		t.Fatalf("bind: %+v %v", b, err)
	}
	if _, err := a.SendToAgent(ctx, 1); err == nil {
		t.Error("send before submit accepted")
	}
	c, _ := a.CreateComment(NewComment{Scope: "file", Path: "ch2.md", Label: "must", Body: "直して"})
	if _, err := a.Submit(); err != nil {
		t.Fatal(err)
	}
	rec, err := a.SendToAgent(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fake.Requests[0].Input, "reviewer から自動送信") || !strings.Contains(fake.Requests[0].Input, "feedback.md") {
		t.Errorf("prompt = %q", fake.Requests[0].Input)
	}

	// The run waits for an approval: a second send is refused meanwhile.
	waitFor(t, "approval", func() bool {
		info := a.AgentInfo()
		return len(info.Active) == 1 && info.Active[0].Pending != nil
	})
	if _, err := a.SendToAgent(ctx, 1); err == nil {
		t.Error("double send accepted")
	}
	if err := a.AnswerAgentRun(ctx, rec.ID, "", "bogus"); err == nil {
		t.Error("invalid choice accepted")
	}
	if err := a.AnswerAgentRun(ctx, rec.ID, "", "once"); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "run end", func() bool { return len(a.AgentInfo().Active) == 0 })
	runs := a.AgentRuns(1)
	if len(runs) != 1 || runs[0].Status != agent.StatusCompleted || runs[0].Text != "直しました" || runs[0].NoResponse {
		t.Fatalf("runs = %+v", runs[0].AgentRun)
	}
	if len(runs[0].Events) < 4 {
		t.Errorf("events = %+v", runs[0].Events)
	}
	got, _ := a.Store.Comment(c.ID)
	if got.Status != store.StatusAddressed {
		t.Errorf("comment status = %s", got.Status)
	}
	if b := a.Store.AgentBinding(); b.SessionID != "d1-live" {
		t.Errorf("binding not rotated: %+v", b)
	}
	waitFor(t, "notices", func() bool { return len(n.all()) == 2 })
	msgs := n.all()
	if !strings.Contains(msgs[0], "ラウンド1") || !strings.Contains(msgs[0], "devbox.local") {
		t.Errorf("start notice = %q", msgs[0])
	}
	if !strings.Contains(msgs[1], "✅") || !strings.Contains(msgs[1], "短くしました") {
		t.Errorf("done notice = %q", msgs[1])
	}
}

func TestAgentUnavailable(t *testing.T) {
	a, _ := setup(t)
	a.ConfigureAgent(nil, "API サーバーが無効です", nil, "none", "")
	info := a.AgentInfo()
	if info.Available || info.Reason == "" {
		t.Errorf("info = %+v", info)
	}
	if _, err := a.AgentSessions(context.Background()); err == nil {
		t.Error("sessions without agent")
	}
}
