package t3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

// Client is a connection to T3 Code. It implements agent.Agent,
// agent.SessionCreator and app.Attacher.
type Client struct {
	opts Options
	http *http.Client

	tokMu sync.Mutex
	tok   cachedToken

	sessMu    sync.Mutex
	sid       string
	sidOrigin string
	sidToken  string
	nextID    atomic.Int64
}

// New creates a client; opts should come from Resolve.
func New(opts Options) *Client {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	// Long enough for t3_thread_wait, which blocks for up to PollInterval.
	return &Client{opts: opts, http: &http.Client{Timeout: time.Minute}}
}

// Kind implements agent.Agent.
func (c *Client) Kind() string { return "t3" }

// Name implements agent.Agent.
func (c *Client) Name() string { return "T3 Code" }

type project struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	WorkspaceRoot string `json:"workspaceRoot"`
}

func (c *Client) projects(ctx context.Context) ([]project, error) {
	var out []project
	cursor := 0
	for page := 0; page < 20; page++ {
		var res struct {
			Projects   []project `json:"projects"`
			NextCursor *int      `json:"nextCursor"`
		}
		if err := c.call(ctx, "t3_project_list", map[string]any{"cursor": cursor, "limit": 100}, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Projects...)
		if res.NextCursor == nil || *res.NextCursor <= cursor {
			break
		}
		cursor = *res.NextCursor
	}
	return out, nil
}

// related reports whether the T3 project at root may work on dir: the
// same directory, a directory containing it, or one inside it. A
// repository with several books or talks often has a T3 project for each
// of them as well as one for the whole repository.
func related(root, dir string) bool {
	root, dir = filepath.Clean(root), filepath.Clean(dir)
	return root == dir || within(root, dir) || within(dir, root)
}

func within(parent, p string) bool {
	return strings.HasPrefix(p, parent+string(filepath.Separator))
}

type thread struct {
	ThreadID             string    `json:"threadId"`
	Title                string    `json:"title"`
	Status               string    `json:"status"`
	RelationshipToParent string    `json:"relationshipToParent"`
	ProviderInstanceID   string    `json:"providerInstanceId"`
	Model                string    `json:"model"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

func (c *Client) threads(ctx context.Context, projectID string) ([]thread, error) {
	var res struct {
		Threads []thread `json:"threads"`
	}
	if err := c.call(ctx, "t3_thread_list", map[string]any{"projectId": projectID, "limit": 100, "includeSubagents": false}, &res); err != nil {
		return nil, err
	}
	out := res.Threads[:0]
	for _, t := range res.Threads {
		// Subagents work for a parent thread; feedback goes to the parent.
		if t.RelationshipToParent != "subagent" {
			out = append(out, t)
		}
	}
	return out, nil
}

var statusLabels = map[string]string{
	"preparing": "準備中", "queued": "待機中", "starting": "開始中",
	"running": "実行中", "waiting": "回答待ち",
}

// Sessions implements agent.Agent: the threads of the T3 projects related
// to dir, most recently updated first.
func (c *Client) Sessions(ctx context.Context, dir string) ([]agent.Session, error) {
	ps, err := c.projects(ctx)
	if err != nil {
		return nil, err
	}
	out := []agent.Session{}
	for _, p := range ps {
		if dir != "" && !related(p.WorkspaceRoot, dir) {
			continue
		}
		ts, err := c.threads(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			preview := p.Title
			if l := statusLabels[t.Status]; l != "" {
				preview += " · " + l
			}
			out = append(out, agent.Session{ID: t.ThreadID, Title: t.Title, Source: "t3", Preview: preview, UpdatedAt: t.UpdatedAt})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// CreateSession implements agent.SessionCreator: an idle thread in the
// innermost T3 project containing dir, using the model of opts or else the
// model of the latest thread working on dir.
func (c *Client) CreateSession(ctx context.Context, dir, title string, opts agent.SessionOptions) (agent.Session, error) {
	ps, err := c.projects(ctx)
	if err != nil {
		return agent.Session{}, err
	}
	best := innermost(ps, dir)
	if best == nil {
		return agent.Session{}, fmt.Errorf("%s を含む T3 Code のプロジェクトがありません。T3 Code でプロジェクトとして追加してください", dir)
	}
	args := map[string]any{"projectId": best.ID, "title": title}
	// A client outside T3 gets the project's default model, which may be
	// unset.
	sel := opts.Model
	if sel.Model == "" {
		if recent := c.recentModel(ctx, ps, best.ID, dir); recent != nil {
			sel = *recent
		}
	}
	if sel.Model != "" {
		args["modelSelection"] = map[string]any{"instanceId": sel.Provider, "model": sel.Model}
	}
	var res struct {
		ThreadID string `json:"threadId"`
	}
	if err := c.call(ctx, "t3_thread_launch", args, &res); err != nil {
		return agent.Session{}, err
	}
	return agent.Session{ID: res.ThreadID, Title: title, Source: "t3", Preview: best.Title, UpdatedAt: time.Now()}, nil
}

// innermost returns the deepest project containing dir, or nil.
func innermost(ps []project, dir string) *project {
	dir = filepath.Clean(dir)
	var best *project
	for i, p := range ps {
		root := filepath.Clean(p.WorkspaceRoot)
		if root == dir || within(root, dir) {
			if best == nil || len(root) > len(filepath.Clean(best.WorkspaceRoot)) {
				best = &ps[i]
			}
		}
	}
	return best
}

// Models implements agent.ModelLister with the catalog T3 Code offers for
// new threads. Providers without models are left out.
func (c *Client) Models(ctx context.Context, dir string) ([]agent.Model, *agent.ModelRef, error) {
	var res struct {
		Providers []struct {
			ProviderInstanceID string `json:"providerInstanceId"`
			DisplayName        string `json:"displayName"`
			Models             []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := c.call(ctx, "orchestrator_capabilities", map[string]any{}, &res); err != nil {
		return nil, nil, err
	}
	models := []agent.Model{}
	for _, p := range res.Providers {
		for _, m := range p.Models {
			label := m.Label
			if label == "" {
				label = m.ID
			}
			models = append(models, agent.Model{ModelRef: agent.ModelRef{Provider: p.ProviderInstanceID, Model: m.ID}, ProviderName: p.DisplayName, Label: label})
		}
	}
	ps, err := c.projects(ctx)
	if err != nil {
		return models, nil, nil
	}
	var recent *agent.ModelRef
	if best := innermost(ps, dir); best != nil {
		recent = c.recentModel(ctx, ps, best.ID, dir)
	} else {
		recent = c.recentModel(ctx, ps, "", dir)
	}
	return models, recent, nil
}

// Start implements agent.Agent. The message is queued behind a turn that
// is in progress rather than steering it.
func (c *Client) Start(ctx context.Context, r agent.Request) (agent.Run, error) {
	if r.SessionID == "" {
		return nil, errors.New("送信先のスレッドがありません")
	}
	args := map[string]any{"threadId": r.SessionID, "message": r.Prompt, "mode": "queue"}
	if r.IdempotencyKey != "" {
		args["clientRequestId"] = r.IdempotencyKey
	}
	var res struct {
		RunID string `json:"runId"`
	}
	if err := c.call(ctx, "t3_thread_send", args, &res); err != nil {
		return nil, err
	}
	if res.RunID == "" {
		return nil, errors.New("T3 Code が実行 ID を返しませんでした")
	}
	run := newRun(c, r.SessionID, res.RunID, false)
	go run.loop()
	return run, nil
}

// Attach implements app.Attacher. Timeline items that already exist are
// not reported again.
func (c *Client) Attach(ctx context.Context, sessionID, runID string) (agent.Run, error) {
	var res struct {
		Status string `json:"status"`
	}
	if err := c.call(ctx, "t3_thread_wait", map[string]any{"threadId": sessionID, "runId": runID, "timeoutMs": 1}, &res); err != nil {
		return nil, err
	}
	run := newRun(c, sessionID, runID, true)
	go run.loop()
	return run, nil
}

// recentModel returns the model of the most recently updated thread of the
// project projectID, or else of a project related to dir.
func (c *Client) recentModel(ctx context.Context, ps []project, projectID, dir string) *agent.ModelRef {
	var latest *thread
	consider := func(id string) {
		ts, err := c.threads(ctx, id)
		if err != nil {
			return
		}
		for i, t := range ts {
			if t.Model != "" && (latest == nil || t.UpdatedAt.After(latest.UpdatedAt)) {
				latest = &ts[i]
			}
		}
	}
	if projectID != "" {
		consider(projectID)
	}
	if latest == nil {
		for _, p := range ps {
			if p.ID != projectID && related(p.WorkspaceRoot, dir) {
				consider(p.ID)
			}
		}
	}
	if latest == nil {
		return nil
	}
	return &agent.ModelRef{Provider: latest.ProviderInstanceID, Model: latest.Model}
}
