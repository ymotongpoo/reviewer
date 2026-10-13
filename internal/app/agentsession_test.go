package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

// creatorAgent creates sessions and lists models, recording what it got.
type creatorAgent struct {
	dir, title string
	opts       agent.SessionOptions
	fail       error
}

func (c *creatorAgent) Kind() string { return "fake" }
func (c *creatorAgent) Name() string { return "Fake" }
func (c *creatorAgent) Sessions(context.Context, string) ([]agent.Session, error) {
	return nil, nil
}
func (c *creatorAgent) Start(context.Context, agent.Request) (agent.Run, error) {
	return nil, errors.New("not used")
}
func (c *creatorAgent) CreateSession(_ context.Context, dir, title string, opts agent.SessionOptions) (agent.Session, error) {
	if c.fail != nil {
		return agent.Session{}, c.fail
	}
	c.dir, c.title, c.opts = dir, title, opts
	return agent.Session{ID: "new-1", Source: "fake"}, nil
}
func (c *creatorAgent) Models(context.Context, string) ([]agent.Model, *agent.ModelRef, error) {
	return []agent.Model{{ModelRef: agent.ModelRef{Provider: "p", Model: "m"}, ProviderName: "P", Label: "M"}}, &agent.ModelRef{Provider: "p", Model: "m"}, nil
}

func TestCreateAgentSession(t *testing.T) {
	a, root := setup(t)
	ag := &creatorAgent{}
	a.ConfigureAgent(ag, "", nil, "none", "")
	if info := a.AgentInfo(); !info.CanCreate || !info.CanPickModel {
		t.Fatalf("info = %+v", info)
	}
	m, err := a.AgentModels(context.Background())
	if err != nil || len(m.Models) != 1 || m.Recent == nil || m.Recent.Model != "m" {
		t.Fatalf("models = %+v, %v", m, err)
	}

	b, err := a.CreateAgentSession(context.Background(), NewAgentSession{Model: agent.ModelRef{Provider: "p", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "reviewer: " + a.Proj.Name()
	if ag.dir != root || ag.title != wantTitle || ag.opts.Model.Model != "m" {
		t.Errorf("created dir=%q title=%q opts=%+v", ag.dir, ag.title, ag.opts)
	}
	if b.SessionID != "new-1" || b.Title != wantTitle || b.Kind != "fake" {
		t.Errorf("binding = %+v", b)
	}
	if got := a.AgentInfo().Binding; got == nil || got.SessionID != "new-1" {
		t.Errorf("stored binding = %+v", got)
	}

	if _, err := a.CreateAgentSession(context.Background(), NewAgentSession{Title: "a\nb"}); err == nil {
		t.Error("accepted a title with a newline")
	}
	ag.fail = agent.ErrTitleInUse
	var ae *Error
	if _, err := a.CreateAgentSession(context.Background(), NewAgentSession{Title: "taken"}); !errors.As(err, &ae) || ae.Code != 409 {
		t.Errorf("title in use: %v", err)
	}
}
