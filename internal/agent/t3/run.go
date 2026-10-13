package t3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

// maxPollErrors ends a run that cannot be read for this many polls in a row.
const maxPollErrors = 15

// maxLogText bounds the text of thinking and tool log entries.
const maxLogText = 300

// Run follows a T3 run by reading its thread's activity timeline.
type Run struct {
	c        *Client
	threadID string
	id       string
	events   chan agent.Event
	stop     context.CancelFunc
	ctx      context.Context

	// skip makes the first read only record what already exists.
	skip bool
	once sync.Once
}

func newRun(c *Client, threadID, runID string, skip bool) *Run {
	ctx, cancel := context.WithCancel(context.Background())
	return &Run{c: c, threadID: threadID, id: runID, events: make(chan agent.Event, 256), ctx: ctx, stop: cancel, skip: skip}
}

// ID implements agent.Run.
func (r *Run) ID() string { return r.id }

// Events implements agent.Run.
func (r *Run) Events() <-chan agent.Event { return r.events }

// Stop implements agent.Run. The loop reports the interrupted run.
func (r *Run) Stop(ctx context.Context) error {
	var res struct {
		Status string `json:"status"`
	}
	return r.c.call(ctx, "t3_thread_interrupt", map[string]any{
		"threadId": r.threadID, "runId": r.id, "reason": "reviewer から停止しました",
		"clientRequestId": "reviewer-stop-" + r.id,
	}, &res)
}

// Answer implements agent.Run. T3 Code does not let MCP clients answer
// approvals, so they are answered in T3 Code.
func (r *Run) Answer(context.Context, string, string) error {
	return errors.New("T3 Code の承認や質問には T3 Code の画面で回答してください")
}

type item struct {
	Position int    `json:"position"`
	ItemID   string `json:"itemId"`
	RunID    string `json:"runId"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	Text     string `json:"text"`
}

type readResult struct {
	Thread struct {
		Status      string `json:"status"`
		LatestRunID string `json:"latestRunId"`
	} `json:"thread"`
	RecentRuns []struct {
		RunID  string `json:"runId"`
		Status string `json:"status"`
	} `json:"recentRuns"`
	Items        []item `json:"items"`
	NextPosition *int   `json:"nextPosition"`
	HasMore      bool   `json:"hasMore"`
}

// done reports whether a timeline item will not change any more.
func done(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "interrupted", "declined", "error":
		return true
	}
	return false
}

// terminal maps a terminal run status to agent.Status*, or "".
func terminal(status string) string {
	switch status {
	case "completed":
		return agent.StatusCompleted
	case "interrupted", "cancelled":
		return agent.StatusCancelled
	case "failed", "rolled_back":
		return agent.StatusFailed
	}
	return ""
}

func (r *Run) emit(ev agent.Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	r.events <- ev
}

func (r *Run) finish(ev agent.Event) {
	r.once.Do(func() {
		r.emit(ev)
		close(r.events)
		r.stop()
	})
}

func (r *Run) loop() {
	after := -1
	seen := map[string]string{} // itemId -> last reported state
	started, waiting := r.skip, false
	failures := 0
	text := false
	for {
		res, err := r.read(after)
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			if failures++; failures >= maxPollErrors {
				r.finish(agent.Event{Type: agent.EventError, Text: "T3 Code から実行の状態を読めません: " + err.Error()})
				return
			}
			r.sleep()
			continue
		}
		failures = 0
		if !started {
			r.emit(agent.Event{Type: agent.EventStarted})
			started = true
		}
		status := r.status(res)
		if waiting && status != "waiting" {
			waiting = false
			r.emit(agent.Event{Type: agent.EventAnswered, Text: "T3 Code で回答"})
		}
		after = r.report(res.Items, after, seen, &text)
		r.skip = false
		if !waiting && status == "waiting" {
			waiting = true
			r.emit(agent.Event{Type: agent.EventApproval, Approval: &agent.Approval{
				Description: "T3 Code が回答を待っています。T3 Code のスレッドで回答してください。",
				Choices:     []string{},
			}})
		}
		if st := terminal(status); st != "" {
			// Items written just before the run ended may not have been read.
			if more, err := r.read(after); err == nil {
				r.report(more.Items, after, seen, &text)
			}
			ev := agent.Event{Type: agent.EventDone, Status: st}
			if st == agent.StatusFailed {
				ev.Text = "T3 Code の実行が失敗しました（" + status + "）"
			}
			r.finish(ev)
			return
		}
		if res.HasMore {
			continue
		}
		r.wait()
	}
}

func (r *Run) read(after int) (*readResult, error) {
	args := map[string]any{"threadId": r.threadID, "view": "activity", "limit": 100, "runLimit": 20, "maxCharsPerItem": 20000}
	if after >= 0 {
		args["afterPosition"] = after
	}
	var res readResult
	if err := r.c.call(r.ctx, "t3_thread_read", args, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// status returns the status of the run.
func (r *Run) status(res *readResult) string {
	for _, run := range res.RecentRuns {
		if run.RunID == r.id {
			return run.Status
		}
	}
	if res.Thread.LatestRunID == r.id {
		return res.Thread.Status
	}
	return ""
}

// report turns the run's new and finished items into events and returns
// the position up to which every item is final, where the next read starts.
func (r *Run) report(items []item, after int, seen map[string]string, text *bool) int {
	next, blocked := after, false
	for _, it := range items {
		final := done(it.Status)
		if it.RunID == r.id && !r.skip {
			r.reportItem(it, final, seen, text)
		} else if it.RunID == r.id {
			seen[it.ItemID] = "done"
			if !final {
				seen[it.ItemID] = "started"
			}
		}
		if !final && it.RunID == r.id {
			blocked = true
		}
		if !blocked && it.Position > next {
			next = it.Position
		}
	}
	return next
}

func (r *Run) reportItem(it item, final bool, seen map[string]string, text *bool) {
	state := seen[it.ItemID]
	if state == "done" {
		return
	}
	switch it.Type {
	case "user_message", "checkpoint":
		seen[it.ItemID] = "done"
	case "assistant_message":
		if !final {
			return
		}
		seen[it.ItemID] = "done"
		t := strings.TrimSpace(it.Text)
		if t == "" {
			return
		}
		if *text {
			t = "\n\n" + t
		}
		*text = true
		r.emit(agent.Event{Type: agent.EventText, Text: t})
	case "reasoning":
		if !final {
			return
		}
		seen[it.ItemID] = "done"
		if t := cut(strings.TrimSpace(it.Text)); t != "" {
			r.emit(agent.Event{Type: agent.EventThinking, Text: t})
		}
	default:
		name := it.Title
		if name == "" {
			name = it.Type
		}
		if !final {
			if state == "" {
				seen[it.ItemID] = "started"
				r.emit(agent.Event{Type: agent.EventTool, Tool: name, ToolState: "started"})
			}
			return
		}
		seen[it.ItemID] = "done"
		st := "completed"
		if it.Status != "completed" {
			st = "failed"
		}
		r.emit(agent.Event{Type: agent.EventTool, Tool: name, ToolState: st})
	}
}

func cut(s string) string {
	if r := []rune(s); len(r) > maxLogText {
		return string(r[:maxLogText]) + "…"
	}
	return s
}

// wait blocks until the run ends or the poll interval passes.
func (r *Run) wait() {
	var res struct {
		TimedOut bool `json:"timedOut"`
	}
	if err := r.c.call(r.ctx, "t3_thread_wait", map[string]any{
		"threadId": r.threadID, "runId": r.id, "timeoutMs": r.c.opts.PollInterval.Milliseconds(),
	}, &res); err != nil {
		r.sleep()
	}
}

func (r *Run) sleep() {
	select {
	case <-r.ctx.Done():
	case <-time.After(r.c.opts.PollInterval):
	}
}
