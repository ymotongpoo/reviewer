package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

// Client talks to the Hermes API server.
type Client struct {
	opts Options
	http *http.Client
	// stream has no overall timeout because runs can take a long time.
	stream *http.Client
}

// New creates a client. opts must already be resolved.
func New(opts Options) *Client {
	return &Client{
		opts:   opts,
		http:   &http.Client{Timeout: 30 * time.Second},
		stream: &http.Client{},
	}
}

// Kind implements agent.Agent.
func (c *Client) Kind() string { return "hermes" }

// Name implements agent.Agent.
func (c *Client) Name() string { return "Hermes Agent" }

// Options returns the resolved options.
func (c *Client) Options() Options { return c.opts }

// APIError is an error response of the API server.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Hermes API %d: %s", e.Status, e.Message)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any, hdr map[string]string) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.opts.base()+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Hermes API に接続できません（%s）: %w", c.opts.URL, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode/100 != 2 {
		return &APIError{Status: res.StatusCode, Message: errorMessage(b)}
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("Hermes API の応答を解析できません: %w", err)
		}
	}
	return nil
}

func errorMessage(b []byte) string {
	var e struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != nil {
		switch v := e.Error.(type) {
		case string:
			return v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				return m
			}
		}
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

type apiSession struct {
	ID         string  `json:"id"`
	Source     string  `json:"source"`
	Title      *string `json:"title"`
	Preview    *string `json:"preview"`
	LastActive any     `json:"last_active"`
	StartedAt  any     `json:"started_at"`
	Archived   bool    `json:"archived"`
	Hidden     bool    `json:"hidden"`
	Parent     *string `json:"parent_session_id"`
}

// Sessions implements agent.Agent. Discord sessions come first, then the
// rest, each ordered by last activity.
func (c *Client) Sessions(ctx context.Context) ([]agent.Session, error) {
	var res struct {
		Data []apiSession `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/sessions?limit=200", nil, &res, nil); err != nil {
		return nil, err
	}
	var out []agent.Session
	for _, s := range res.Data {
		if s.Archived || s.Hidden || s.Source == "subagent" || s.Parent != nil && *s.Parent != "" {
			continue
		}
		ss := agent.Session{ID: s.ID, Source: s.Source, UpdatedAt: parseTime(s.LastActive)}
		if ss.UpdatedAt.IsZero() {
			ss.UpdatedAt = parseTime(s.StartedAt)
		}
		if s.Title != nil {
			ss.Title = *s.Title
		}
		if s.Preview != nil {
			ss.Preview = *s.Preview
		}
		if ss.Title == "" {
			ss.Title = ss.ID
		}
		out = append(out, ss)
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := out[i].Source == "discord", out[j].Source == "discord"
		if di != dj {
			return di
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// CreateSession creates an empty Hermes session with a title.
func (c *Client) CreateSession(ctx context.Context, title string) (agent.Session, error) {
	var res struct {
		Session apiSession `json:"session"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/sessions", map[string]string{
		"title": title, "source": "api_server",
	}, &res, nil); err != nil {
		return agent.Session{}, err
	}
	if res.Session.ID == "" {
		return agent.Session{}, fmt.Errorf("Hermes API が session.id を返しませんでした")
	}
	s := agent.Session{ID: res.Session.ID, Source: res.Session.Source, UpdatedAt: parseTime(res.Session.LastActive)}
	if res.Session.Title != nil {
		s.Title = *res.Session.Title
	}
	return s, nil
}

// parseTime accepts epoch seconds (number or numeric string) and RFC 3339.
func parseTime(v any) time.Time {
	switch t := v.(type) {
	case float64:
		sec := int64(t)
		return time.Unix(sec, int64((t-float64(sec))*1e9))
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return parseTime(f)
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			if tt, err := time.Parse(layout, t); err == nil {
				return tt
			}
		}
	}
	return time.Time{}
}

// Start implements agent.Agent.
func (c *Client) Start(ctx context.Context, r agent.Request) (agent.Run, error) {
	body := map[string]any{"input": r.Prompt}
	if r.SessionID != "" {
		body["session_id"] = r.SessionID
	}
	hdr := map[string]string{}
	if r.IdempotencyKey != "" {
		hdr["Idempotency-Key"] = r.IdempotencyKey
	}
	var res struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/runs", body, &res, hdr); err != nil {
		return nil, err
	}
	if res.RunID == "" {
		return nil, fmt.Errorf("Hermes API が run_id を返しませんでした")
	}
	run := &Run{c: c, id: res.RunID, events: make(chan agent.Event, 256), done: make(chan struct{})}
	go run.consume()
	return run, nil
}

// Attach follows an existing run, e.g. after reviewer restarted.
func (c *Client) Attach(ctx context.Context, runID string) (agent.Run, error) {
	run := &Run{c: c, id: runID, events: make(chan agent.Event, 256), done: make(chan struct{})}
	st, err := run.status(ctx)
	if err != nil {
		return nil, err
	}
	if _, terminal := terminalStatus(st); terminal {
		return nil, fmt.Errorf("run %s already ended (%s)", runID, st)
	}
	go run.consume()
	return run, nil
}

// Run is a Hermes run.
type Run struct {
	c      *Client
	id     string
	events chan agent.Event
	done   chan struct{}

	mu          sync.Mutex
	liveSession string
}

// LiveSessionID returns the session the run wrote to, which differs from
// the requested one when Hermes rotated the session (e.g. after context
// compression). It is known once the run has finished.
func (r *Run) LiveSessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.liveSession
}

// ID implements agent.Run.
func (r *Run) ID() string { return r.id }

// Events implements agent.Run.
func (r *Run) Events() <-chan agent.Event { return r.events }

// Stop implements agent.Run.
func (r *Run) Stop(ctx context.Context) error {
	return r.c.do(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(r.id)+"/stop", map[string]any{}, nil, nil)
}

// Answer implements agent.Run.
func (r *Run) Answer(ctx context.Context, approvalID, choice string) error {
	body := map[string]any{"choice": choice}
	if approvalID != "" {
		body["request_id"] = approvalID
	}
	return r.c.do(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(r.id)+"/approval", body, nil, nil)
}

// status polls the run status; it is used when the event stream breaks.
func (r *Run) status(ctx context.Context) (string, error) {
	var st struct {
		Status    string `json:"status"`
		SessionID string `json:"session_id"`
	}
	err := r.c.do(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(r.id), nil, &st, nil)
	if st.SessionID != "" {
		r.mu.Lock()
		r.liveSession = st.SessionID
		r.mu.Unlock()
	}
	return st.Status, err
}

// consume reads the SSE stream until a terminal event, reconnecting when
// the stream breaks and falling back to polling the status.
func (r *Run) consume() {
	defer close(r.events)
	co := newCoalescer(r.events)
	defer co.close()
	ctx := context.Background()
	for attempt := 0; ; attempt++ {
		done, err := r.stream(ctx, co)
		if done != nil {
			// Learn the live session id before announcing the end.
			r.status(ctx)
			co.emit(*done)
			return
		}
		st, serr := r.status(ctx)
		if serr == nil {
			if s, ok := terminalStatus(st); ok {
				// The stream ended without its terminal event.
				co.emit(agent.Event{Type: agent.EventDone, Status: s})
				return
			}
		}
		if attempt >= 20 {
			msg := "イベントストリームが切断されました"
			if err != nil {
				msg += ": " + err.Error()
			}
			co.emit(agent.Event{Type: agent.EventError, Text: msg})
			return
		}
		time.Sleep(time.Duration(min(attempt+1, 5)) * time.Second)
	}
}

func terminalStatus(s string) (string, bool) {
	switch s {
	case "completed":
		return agent.StatusCompleted, true
	case "failed":
		return agent.StatusFailed, true
	case "cancelled", "interrupted":
		return agent.StatusCancelled, true
	}
	return "", false
}

// stream reads one connection of the event stream. It returns the terminal
// event, if one was seen, without emitting it.
func (r *Run) stream(ctx context.Context, co *coalescer) (*agent.Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.c.opts.base()+"/v1/runs/"+url.PathEscape(r.id)+"/events", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.c.opts.APIKey)
	req.Header.Set("Accept", "text/event-stream")
	res, err := r.c.stream.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, &APIError{Status: res.StatusCode, Message: errorMessage(b)}
	}
	var done *agent.Event
	err = readSSE(res.Body, func(data []byte) bool {
		ev, ok := convert(data)
		if !ok {
			return true
		}
		if ev.Type == agent.EventDone {
			done = &ev
			return false
		}
		co.emit(ev)
		return true
	})
	return done, err
}
