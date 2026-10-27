package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

type feedbackModelAgent struct {
	mu      sync.Mutex
	calls   []string
	session string
	model   agent.ModelRef
	fail    bool
}

func (*feedbackModelAgent) Kind() string { return "model-test" }
func (*feedbackModelAgent) Name() string { return "Model Test" }
func (*feedbackModelAgent) Sessions(context.Context, string) ([]agent.Session, error) {
	return []agent.Session{{ID: "bound", Title: "Existing conversation"}}, nil
}
func (*feedbackModelAgent) Models(context.Context, string) ([]agent.Model, *agent.ModelRef, error) {
	return nil, nil, nil
}
func (a *feedbackModelAgent) ConfigureSession(_ context.Context, session string, model agent.ModelRef) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "configure")
	a.session, a.model = session, model
	if a.fail {
		return errors.New("configuration rejected")
	}
	return nil
}
func (a *feedbackModelAgent) Start(_ context.Context, req agent.Request) (agent.Run, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "send")
	if req.SessionID != "bound" || !strings.Contains(req.Prompt, "feedback.md") {
		return nil, errors.New("wrong feedback destination")
	}
	events := make(chan agent.Event, 1)
	events <- agent.Event{Type: agent.EventDone, Status: agent.StatusCompleted, At: time.Now()}
	close(events)
	return &feedbackModelRun{id: fmt.Sprintf("run-%d", len(a.calls)), events: events}, nil
}

type feedbackModelRun struct {
	id     string
	events chan agent.Event
}

func (r *feedbackModelRun) ID() string                                 { return r.id }
func (r *feedbackModelRun) Events() <-chan agent.Event                 { return r.events }
func (*feedbackModelRun) Stop(context.Context) error                   { return nil }
func (*feedbackModelRun) Answer(context.Context, string, string) error { return nil }

func feedbackModelEnv(t *testing.T) (*env, *Project, *feedbackModelAgent, string) {
	t.Helper()
	e := newEnv(t)
	id := e.open(t, filepath.Join(e.root, "a"))
	p, err := e.reg.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	ag := &feedbackModelAgent{}
	p.App.ConfigureAgent(ag, "", nil, "none", "")
	if _, err := p.App.BindAgent(context.Background(), "bound"); err != nil {
		t.Fatal(err)
	}
	base := e.ts.URL + "/p/" + id
	if res, body := do(t, "POST", base+"/api/comments", `{"scope":"project","body":"Please fix this","label":"must"}`, bearer); res.StatusCode != 200 {
		t.Fatalf("comment: %d %s", res.StatusCode, body)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(2 * time.Second)
		for p.App.AgentBusy() && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if p.App.AgentBusy() {
			t.Error("feedback run did not finish")
		}
	})
	return e, p, ag, base
}

func TestFeedbackModelSelection(t *testing.T) {
	model := agent.ModelRef{Provider: "codex", Model: "gpt-test", Options: []agent.OptionValue{{ID: "reasoningEffort", Value: "xhigh"}}}
	for _, tc := range []struct {
		name, path string
		selected   bool
		fail       bool
		wantCalls  []string
	}{
		{"keep destination settings", "/api/rounds/submit", false, false, []string{"send"}},
		{"configure before sending", "/api/rounds/submit", true, false, []string{"configure", "send"}},
		{"configuration failure", "/api/rounds/submit", true, true, []string{"configure"}},
		{"manual resend", "/api/agent/send", true, false, []string{"configure", "send"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, ag, base := feedbackModelEnv(t)
			ag.fail = tc.fail
			if !p.App.AgentInfo().CanConfigureModel {
				t.Fatal("model selection capability is missing")
			}
			input := map[string]any{"sendToAgent": true}
			if tc.path == "/api/agent/send" {
				if _, err := p.App.Submit(); err != nil {
					t.Fatal(err)
				}
				input = map[string]any{"round": 1}
			}
			if tc.selected {
				input["model"] = model
			}
			encoded, _ := json.Marshal(input)
			res, body := do(t, "POST", base+tc.path, string(encoded), bearer)
			if res.StatusCode != 200 {
				t.Fatalf("send: %d %s", res.StatusCode, body)
			}
			if tc.fail && !strings.Contains(body, "configuration rejected") {
				t.Fatalf("configuration error was hidden: %s", body)
			}
			if tc.fail && len(p.App.AgentRuns(1)) != 0 {
				t.Fatal("failed configuration left an agent run")
			}
			ag.mu.Lock()
			calls, selected, session := append([]string(nil), ag.calls...), ag.model, ag.session
			ag.mu.Unlock()
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
			}
			if tc.selected && (!reflect.DeepEqual(selected, model) || session != "bound") {
				t.Fatalf("selection = %+v, session = %q", selected, session)
			}
			if p.App.Info().RoundStatus != "submitted" || p.App.AgentInfo().Binding.SessionID != "bound" {
				t.Fatal("submission or existing session binding changed")
			}
		})
	}
}

func TestInvalidFeedbackModelLeavesRoundOpen(t *testing.T) {
	_, p, ag, base := feedbackModelEnv(t)
	for _, input := range []string{
		`{"sendToAgent":false,"model":{"provider":"codex","model":"gpt-test"}}`,
		`{"sendToAgent":true,"model":{"model":"gpt-test"}}`,
		`{"sendToAgent":true,"model":{"provider":"codex"}}`,
		`{"sendToAgent":true,"model":{"provider":"codex","model":"gpt-test","options":[{"id":"reasoningEffort"}]}}`,
	} {
		res, body := do(t, "POST", base+"/api/rounds/submit", input, bearer)
		if res.StatusCode != 400 || p.App.Info().RoundStatus != "open" {
			t.Fatalf("invalid selection submitted a round: %d %s", res.StatusCode, body)
		}
	}
	ag.mu.Lock()
	defer ag.mu.Unlock()
	if len(ag.calls) != 0 {
		t.Fatalf("invalid selection invoked the agent: %v", ag.calls)
	}
}

// Embedding only Agent hides the optional model configuration capability.
type fixedFeedbackAgent struct{ agent.Agent }

func TestUnsupportedFeedbackModelLeavesRoundOpen(t *testing.T) {
	_, p, ag, base := feedbackModelEnv(t)
	p.App.ConfigureAgent(fixedFeedbackAgent{ag}, "", nil, "none", "")
	if p.App.AgentInfo().CanConfigureModel {
		t.Fatal("unsupported agent advertises model configuration")
	}
	res, body := do(t, "POST", base+"/api/rounds/submit", `{"sendToAgent":true,"model":{"provider":"codex","model":"gpt-test"}}`, bearer)
	if res.StatusCode != 400 || p.App.Info().RoundStatus != "open" {
		t.Fatalf("unsupported model silently submitted: %d %s", res.StatusCode, body)
	}
}
