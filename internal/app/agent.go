package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/store"
)

// maxRunText bounds the stored final answer of a run.
const maxRunText = 20000

// agentState holds the connection to the agent. Its mutex is separate from
// App.mu so that streaming events never blocks the review UI.
type agentState struct {
	mu       sync.Mutex
	ag       agent.Agent
	reason   string // why the agent is unavailable
	notifier agent.Notifier
	notify   string // notifier name for the UI
	baseURL  string
	active   map[string]*activeRun
}

type activeRun struct {
	rec *store.AgentRun
	run agent.Run
}

// Attacher is implemented by agents that can resume reading a run after a
// restart of reviewer.
type Attacher interface {
	Attach(ctx context.Context, runID string) (agent.Run, error)
}

// liveSessioner is implemented by runs that know the session they ended in.
type liveSessioner interface{ LiveSessionID() string }

// ConfigureAgent sets the agent connection. ag may be nil with a reason
// explaining why no agent is available. baseURL is the reviewer URL shown in
// notifications (without the token).
func (a *App) ConfigureAgent(ag agent.Agent, reason string, n agent.Notifier, notifyName, baseURL string) {
	if n == nil {
		n = agent.NopNotifier{}
	}
	a.agents.mu.Lock()
	a.agents.ag, a.agents.reason, a.agents.notifier, a.agents.notify, a.agents.baseURL = ag, reason, n, notifyName, baseURL
	if a.agents.active == nil {
		a.agents.active = map[string]*activeRun{}
	}
	a.agents.mu.Unlock()
	a.reattach()
}

// reattach resumes following runs that were still running when reviewer
// stopped, or marks them as unknown.
func (a *App) reattach() {
	a.mu.Lock()
	var stale []*store.AgentRun
	for n := 1; n <= a.Store.State.Round; n++ {
		for _, r := range a.Store.AgentRuns(n) {
			if r.Status == store.RunRunning {
				stale = append(stale, r)
			}
		}
	}
	a.mu.Unlock()
	a.agents.mu.Lock()
	ag := a.agents.ag
	a.agents.mu.Unlock()
	for _, rec := range stale {
		if at, ok := ag.(Attacher); ok && ag.Kind() == rec.Kind {
			if run, err := at.Attach(context.Background(), rec.ID); err == nil {
				a.follow(rec, run)
				continue
			}
		}
		now := a.now()
		rec.Status, rec.EndedAt, rec.Error = store.RunError, &now, "reviewer の再起動により追跡できなくなりました"
		a.saveRun(rec)
	}
}

// AgentInfo describes the agent connection for the UI.
type AgentInfo struct {
	Available bool                `json:"available"`
	Kind      string              `json:"kind,omitempty"`
	Name      string              `json:"name,omitempty"`
	Reason    string              `json:"reason,omitempty"`
	Notify    string              `json:"notify"`
	AutoSend  bool                `json:"autoSend"`
	Binding   *store.AgentBinding `json:"binding,omitempty"`
	Active    []*store.AgentRun   `json:"active"`
}

// AgentInfo returns the agent status.
func (a *App) AgentInfo() AgentInfo {
	a.mu.Lock()
	binding := a.Store.AgentBinding()
	a.mu.Unlock()
	a.agents.mu.Lock()
	defer a.agents.mu.Unlock()
	info := AgentInfo{Reason: a.agents.reason, Notify: a.agents.notify, AutoSend: a.Cfg.Agent.AutoSend, Binding: binding, Active: []*store.AgentRun{}}
	if ag := a.agents.ag; ag != nil {
		info.Available, info.Kind, info.Name = true, ag.Kind(), ag.Name()
		if binding != nil && binding.Kind != ag.Kind() {
			info.Binding = nil
		}
	}
	for _, r := range a.agents.active {
		cp := *r.rec
		info.Active = append(info.Active, &cp)
	}
	slices.SortFunc(info.Active, func(x, y *store.AgentRun) int { return x.StartedAt.Compare(y.StartedAt) })
	return info
}

func (a *App) agentOrErr() (agent.Agent, error) {
	a.agents.mu.Lock()
	defer a.agents.mu.Unlock()
	if a.agents.ag == nil {
		msg := "エージェントに接続していません"
		if a.agents.reason != "" {
			msg += ": " + a.agents.reason
		}
		return nil, &Error{Code: http.StatusServiceUnavailable, Msg: msg}
	}
	return a.agents.ag, nil
}

// AgentSessions lists sessions that feedback can be sent to.
func (a *App) AgentSessions(ctx context.Context) ([]agent.Session, error) {
	ag, err := a.agentOrErr()
	if err != nil {
		return nil, err
	}
	ss, err := ag.Sessions(ctx)
	if err != nil {
		return nil, &Error{Code: http.StatusBadGateway, Msg: err.Error()}
	}
	if ss == nil {
		ss = []agent.Session{}
	}
	return ss, nil
}

// BindAgent ties the project to a session; an empty id removes the binding.
func (a *App) BindAgent(ctx context.Context, sessionID string) (*store.AgentBinding, error) {
	if sessionID == "" {
		a.mu.Lock()
		err := a.Store.SaveAgentBinding(nil)
		a.mu.Unlock()
		a.notify(Event{Type: "agent"})
		return nil, err
	}
	ss, err := a.AgentSessions(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(ss, func(s agent.Session) bool { return s.ID == sessionID })
	if i < 0 {
		return nil, notFound("セッションが見つかりません: %s", sessionID)
	}
	ag, _ := a.agentOrErr()
	b := &store.AgentBinding{Kind: ag.Kind(), SessionID: ss[i].ID, Title: ss[i].Title, Source: ss[i].Source, BoundAt: a.now()}
	a.mu.Lock()
	err = a.Store.SaveAgentBinding(b)
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	a.notify(Event{Type: "agent"})
	return b, nil
}

// AgentRunView is a run with its events.
type AgentRunView struct {
	*store.AgentRun
	Events []agent.Event `json:"events"`
}

// AgentRuns returns the runs of round n.
func (a *App) AgentRuns(n int) []AgentRunView {
	// Running runs keep their latest state (e.g. the text so far) in memory.
	a.agents.mu.Lock()
	live := map[string]store.AgentRun{}
	for id, r := range a.agents.active {
		live[id] = *r.rec
	}
	a.agents.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []AgentRunView{}
	for _, r := range a.Store.AgentRuns(n) {
		if l, ok := live[r.ID]; ok {
			r = &l
		}
		evs := a.Store.AgentEvents(n, r.ID)
		if evs == nil {
			evs = []agent.Event{}
		}
		out = append(out, AgentRunView{AgentRun: r, Events: evs})
	}
	return out
}

const autoSentHeader = "（このメッセージは reviewer から自動送信されています）\n\n"

// SendToAgent sends the feedback of submitted round n to the bound session.
func (a *App) SendToAgent(ctx context.Context, n int) (*store.AgentRun, error) {
	ag, err := a.agentOrErr()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	binding := a.Store.AgentBinding()
	m, merr := a.Store.Manifest(n)
	var prompt string
	var attempts int
	if merr == nil {
		prompt = autoSentHeader + a.roundPaths(n).Prompt
		attempts = len(a.Store.AgentRuns(n))
	}
	a.mu.Unlock()
	switch {
	case binding == nil || binding.Kind != ag.Kind():
		return nil, badRequest("送信先のセッションが選ばれていません")
	case merr != nil || m.SubmittedAt == nil:
		return nil, conflict("ラウンド%dはまだ提出されていません", n)
	}

	a.agents.mu.Lock()
	for _, r := range a.agents.active {
		if r.rec.Round == n {
			a.agents.mu.Unlock()
			return nil, conflict("ラウンド%dはすでにエージェントが対応中です", n)
		}
	}
	a.agents.mu.Unlock()

	sum := sha256.Sum256([]byte(a.Proj.Root))
	key := fmt.Sprintf("reviewer-%s-r%d-%d", hex.EncodeToString(sum[:6]), n, attempts+1)
	run, err := ag.Start(ctx, agent.Request{SessionID: binding.SessionID, Prompt: prompt, IdempotencyKey: key})
	if err != nil {
		return nil, &Error{Code: http.StatusBadGateway, Msg: "エージェントに送信できませんでした: " + err.Error()}
	}
	rec := &store.AgentRun{ID: run.ID(), Kind: ag.Kind(), Round: n, SessionID: binding.SessionID, Status: store.RunRunning, StartedAt: a.now()}
	a.saveRun(rec)
	a.follow(rec, run)

	msg := fmt.Sprintf("📝 reviewer: ラウンド%dのレビューコメント（%d件）を送りました。対応をお願いします。", n, len(m.FeedbackIDs))
	if u := a.agents.baseURL; u != "" {
		msg += "\n進捗: " + u
	}
	go a.sendNotice(rec, msg)
	return rec, nil
}

// follow consumes the events of run in the background.
func (a *App) follow(rec *store.AgentRun, run agent.Run) {
	a.agents.mu.Lock()
	if a.agents.active == nil {
		a.agents.active = map[string]*activeRun{}
	}
	a.agents.active[rec.ID] = &activeRun{rec: rec, run: run}
	a.agents.mu.Unlock()
	a.notify(Event{Type: "agent", Round: rec.Round, Run: rec.ID})
	go func() {
		for ev := range run.Events() {
			a.handleEvent(rec, run, ev)
		}
		a.agents.mu.Lock()
		delete(a.agents.active, rec.ID)
		ended := rec.EndedAt != nil
		a.agents.mu.Unlock()
		if !ended {
			a.finish(rec, run, agent.Event{Type: agent.EventError, Text: "イベントストリームが終了しました", At: a.now()})
		}
	}()
}

func (a *App) handleEvent(rec *store.AgentRun, run agent.Run, ev agent.Event) {
	if ev.At.IsZero() {
		ev.At = a.now()
	}
	a.mu.Lock()
	a.Store.AppendAgentEvent(rec.Round, rec.ID, ev)
	a.mu.Unlock()
	a.notify(Event{Type: "agent", Round: rec.Round, Run: rec.ID, Agent: &ev})

	a.agents.mu.Lock()
	changed := false
	switch ev.Type {
	case agent.EventText:
		if len(rec.Text) < maxRunText {
			rec.Text += ev.Text
			if len(rec.Text) > maxRunText {
				rec.Text = rec.Text[:maxRunText]
			}
		}
	case agent.EventApproval:
		rec.Pending, changed = ev.Approval, true
	case agent.EventAnswered:
		rec.Pending, changed = nil, true
	}
	a.agents.mu.Unlock()
	if changed {
		a.saveRun(rec)
	}
	if ev.Type == agent.EventDone || ev.Type == agent.EventError {
		a.finish(rec, run, ev)
	}
}

// finish records the end of a run, imports the agent's response.json and
// posts the completion notice.
func (a *App) finish(rec *store.AgentRun, run agent.Run, ev agent.Event) {
	a.agents.mu.Lock()
	if rec.EndedAt != nil {
		a.agents.mu.Unlock()
		return
	}
	now := a.now()
	rec.EndedAt, rec.Pending = &now, nil
	if ev.Type == agent.EventError {
		rec.Status, rec.Error = store.RunError, ev.Text
	} else {
		rec.Status = ev.Status
		if ev.Status == agent.StatusFailed && ev.Text != "" {
			rec.Error = ev.Text
		}
	}
	a.agents.mu.Unlock()

	// Follow a session rotated by the agent (e.g. context compression).
	if ls, ok := run.(liveSessioner); ok {
		if live := ls.LiveSessionID(); live != "" && live != rec.SessionID {
			a.mu.Lock()
			if b := a.Store.AgentBinding(); b != nil && b.SessionID == rec.SessionID {
				b.SessionID = live
				a.Store.SaveAgentBinding(b)
			}
			a.mu.Unlock()
		}
	}

	// The watcher imports response.json too; import synchronously so the
	// summary is available for the notice.
	a.ImportResponses()
	a.mu.Lock()
	var summary string
	hasResponse := false
	if m, err := a.Store.Manifest(rec.Round); err == nil && m.Response != nil {
		hasResponse = m.Response.Error == "" && m.Response.ImportedAt.After(rec.StartedAt.Add(-time.Second))
		summary = m.Response.Summary
	}
	a.mu.Unlock()
	a.agents.mu.Lock()
	rec.NoResponse = rec.Status == agent.StatusCompleted && !hasResponse
	status, errText, text, noResp := rec.Status, rec.Error, rec.Text, rec.NoResponse
	a.agents.mu.Unlock()
	a.saveRun(rec)
	a.notify(Event{Type: "agent", Round: rec.Round, Run: rec.ID})

	var msg string
	switch status {
	case agent.StatusCompleted:
		msg = fmt.Sprintf("✅ reviewer: ラウンド%dの対応が終わりました。", rec.Round)
		if summary == "" {
			summary = firstLines(text, 300)
		}
		if summary != "" {
			msg += "\n> " + strings.ReplaceAll(summary, "\n", "\n> ")
		}
		if noResp {
			msg += "\n（response.json が書かれていないため、コメントごとの返答はありません）"
		}
	case agent.StatusCancelled:
		msg = fmt.Sprintf("⏹ reviewer: ラウンド%dの対応を停止しました。", rec.Round)
	default:
		msg = fmt.Sprintf("⚠️ reviewer: ラウンド%dの対応が途中で終わりました。", rec.Round)
		if errText != "" {
			msg += "（" + errText + "）"
		}
	}
	a.sendNotice(rec, msg)
}

func firstLines(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func (a *App) sendNotice(rec *store.AgentRun, msg string) {
	a.agents.mu.Lock()
	n := a.agents.notifier
	a.agents.mu.Unlock()
	if n == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := n.Notify(ctx, rec.SessionID, msg); err != nil {
		log.Printf("agent notification: %v", err)
		a.agents.mu.Lock()
		rec.NotifyError = err.Error()
		a.agents.mu.Unlock()
		a.saveRun(rec)
		a.notify(Event{Type: "agent", Round: rec.Round, Run: rec.ID})
	}
}

// saveRun persists a snapshot of rec.
func (a *App) saveRun(rec *store.AgentRun) {
	a.agents.mu.Lock()
	cp := *rec
	a.agents.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.Store.PutAgentRun(&cp); err != nil {
		log.Printf("save agent run: %v", err)
	}
}

func (a *App) activeRun(id string) (*activeRun, error) {
	a.agents.mu.Lock()
	defer a.agents.mu.Unlock()
	r, ok := a.agents.active[id]
	if !ok {
		return nil, notFound("実行中のエージェントの作業がありません: %s", id)
	}
	return r, nil
}

// StopAgentRun interrupts a run.
func (a *App) StopAgentRun(ctx context.Context, id string) error {
	r, err := a.activeRun(id)
	if err != nil {
		return err
	}
	if err := r.run.Stop(ctx); err != nil {
		return &Error{Code: http.StatusBadGateway, Msg: err.Error()}
	}
	return nil
}

// AnswerAgentRun resolves the pending approval of a run.
func (a *App) AnswerAgentRun(ctx context.Context, id, approvalID, choice string) error {
	r, err := a.activeRun(id)
	if err != nil {
		return err
	}
	a.agents.mu.Lock()
	p := r.rec.Pending
	a.agents.mu.Unlock()
	if p == nil {
		return conflict("承認待ちの項目はありません")
	}
	if !slices.Contains(p.Choices, choice) {
		return badRequest("選べない回答です: %s", choice)
	}
	if approvalID == "" {
		approvalID = p.ID
	}
	if err := r.run.Answer(ctx, approvalID, choice); err != nil {
		return &Error{Code: http.StatusBadGateway, Msg: err.Error()}
	}
	return nil
}
