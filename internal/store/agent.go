package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

// AgentBinding ties the project to an agent session.
type AgentBinding struct {
	Kind      string    `json:"kind"`
	SessionID string    `json:"sessionId"`
	Title     string    `json:"title"`
	Source    string    `json:"source,omitempty"`
	BoundAt   time.Time `json:"boundAt"`
}

// Agent run statuses in addition to agent.Status*.
const (
	RunRunning = "running"
	RunError   = "error"
)

// AgentRun records a turn sent to the agent.
type AgentRun struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Round       int             `json:"round"`
	Purpose     string          `json:"purpose,omitempty"`
	Request     string          `json:"request,omitempty"`
	SessionID   string          `json:"sessionId"`
	Status      string          `json:"status"`
	StartedAt   time.Time       `json:"startedAt"`
	EndedAt     *time.Time      `json:"endedAt,omitempty"`
	Text        string          `json:"text,omitempty"`
	Error       string          `json:"error,omitempty"`
	Pending     *agent.Approval `json:"pending,omitempty"`
	NoResponse  bool            `json:"noResponse,omitempty"`
	NotifyError string          `json:"notifyError,omitempty"`
}

func (s *Store) bindingPath() string { return filepath.Join(s.Dir, "agent.json") }

// AgentBinding returns the binding, or nil.
func (s *Store) AgentBinding() *AgentBinding {
	var b AgentBinding
	if err := readJSON(s.bindingPath(), &b); err != nil || b.SessionID == "" {
		return nil
	}
	return &b
}

// SaveAgentBinding stores b; nil removes the binding.
func (s *Store) SaveAgentBinding(b *AgentBinding) error {
	if b == nil {
		err := os.Remove(s.bindingPath())
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeJSON(s.bindingPath(), b)
}

func appendJSONL(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func readJSONL(p string, fn func([]byte)) error {
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(sc.Bytes())
		}
	}
	return sc.Err()
}

func (s *Store) agentRunDir(r *AgentRun) string {
	if r.Purpose == "annotate" && r.Request != "" {
		return s.RequestDir(r.Request)
	}
	return s.RoundDir(r.Round)
}

// PutAgentRun appends the current state of r to its round or request log.
func (s *Store) PutAgentRun(r *AgentRun) error {
	return appendJSONL(filepath.Join(s.agentRunDir(r), "agent-runs.jsonl"), r)
}

func (s *Store) agentRuns(dir string) []*AgentRun {
	byID := map[string]*AgentRun{}
	var order []string
	readJSONL(filepath.Join(dir, "agent-runs.jsonl"), func(b []byte) {
		var r AgentRun
		if json.Unmarshal(b, &r) != nil || r.ID == "" {
			return
		}
		if _, ok := byID[r.ID]; !ok {
			order = append(order, r.ID)
		}
		byID[r.ID] = &r
	})
	out := make([]*AgentRun, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

// AgentRuns returns feedback runs of round n, oldest first. Runs saved before
// Purpose was added have an empty purpose and are treated as feedback runs.
func (s *Store) AgentRuns(n int) []*AgentRun { return s.agentRuns(s.RoundDir(n)) }

// RequestAgentRuns returns annotation runs of request id, oldest first.
func (s *Store) RequestAgentRuns(id string) []*AgentRun { return s.agentRuns(s.RequestDir(id)) }

type eventRecord struct {
	Run   string      `json:"run"`
	Event agent.Event `json:"event"`
}

// AppendAgentEvent appends an event to the log selected by r's purpose.
func (s *Store) AppendAgentEvent(r *AgentRun, ev agent.Event) error {
	return appendJSONL(filepath.Join(s.agentRunDir(r), "agent-events.jsonl"), eventRecord{Run: r.ID, Event: ev})
}

// AgentEvents returns the events of r.
func (s *Store) AgentEvents(rec *AgentRun) []agent.Event {
	var out []agent.Event
	readJSONL(filepath.Join(s.agentRunDir(rec), "agent-events.jsonl"), func(b []byte) {
		var r eventRecord
		if json.Unmarshal(b, &r) == nil && r.Run == rec.ID {
			out = append(out, r.Event)
		}
	})
	return out
}

// Peek reads the review state and agent binding of the data directory dir
// without opening the store. ok is false when dir holds no review data.
func Peek(dir string) (st State, binding *AgentBinding, ok bool) {
	if err := readJSON(filepath.Join(dir, "state.json"), &st); err != nil {
		return st, nil, false
	}
	var b AgentBinding
	if readJSON(filepath.Join(dir, "agent.json"), &b) == nil && b.SessionID != "" {
		binding = &b
	}
	return st, binding, true
}
