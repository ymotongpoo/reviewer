package hermes

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
)

// readSSE calls fn with the data of each event until fn returns false or the
// stream ends. Comment lines (": keepalive") are skipped.
func readSSE(r io.Reader, fn func(data []byte) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var data bytes.Buffer
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0:
			if data.Len() > 0 {
				if !fn(bytes.TrimSuffix(data.Bytes(), []byte("\n"))) {
					return nil
				}
				data.Reset()
			}
		case line[0] == ':':
		case bytes.HasPrefix(line, []byte("data:")):
			data.Write(bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" ")))
			data.WriteByte('\n')
		}
	}
	if data.Len() > 0 {
		fn(bytes.TrimSuffix(data.Bytes(), []byte("\n")))
	}
	return sc.Err()
}

type rawEvent struct {
	Event       string   `json:"event"`
	Timestamp   float64  `json:"timestamp"`
	Delta       string   `json:"delta"`
	Text        string   `json:"text"`
	Tool        string   `json:"tool"`
	Preview     string   `json:"preview"`
	Error       any      `json:"error"`
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Choices     []string `json:"choices"`
	RequestID   string   `json:"request_id"`
	Choice      string   `json:"choice"`
	Output      string   `json:"output"`
	ExitReason  string   `json:"turn_exit_reason"`
}

// convert maps a Hermes run event to an agent.Event.
func convert(data []byte) (agent.Event, bool) {
	var e rawEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return agent.Event{}, false
	}
	ev := agent.Event{At: time.Now()}
	if e.Timestamp > 0 {
		ev.At = parseTime(e.Timestamp)
	}
	switch e.Event {
	case "run.started":
		ev.Type = agent.EventStarted
	case "message.delta":
		ev.Type, ev.Text = agent.EventText, e.Delta
	case "reasoning.available":
		ev.Type, ev.Text = agent.EventThinking, e.Text
	case "tool.started":
		ev.Type, ev.Tool, ev.ToolState, ev.Text = agent.EventTool, e.Tool, "started", e.Preview
	case "tool.completed":
		ev.Type, ev.Tool, ev.ToolState, ev.Text = agent.EventTool, e.Tool, "completed", e.Preview
		if b, ok := e.Error.(bool); ok && b {
			ev.ToolState = "failed"
		}
	case "tool.failed":
		ev.Type, ev.Tool, ev.ToolState, ev.Text = agent.EventTool, e.Tool, "failed", e.Preview
	case "subagent.start", "subagent.complete":
		ev.Type, ev.Tool, ev.Text = agent.EventTool, "subagent", e.Preview
		ev.ToolState = map[bool]string{true: "started", false: "completed"}[e.Event == "subagent.start"]
	case "approval.request":
		ev.Type = agent.EventApproval
		ev.Approval = &agent.Approval{ID: e.RequestID, Command: e.Command, Description: e.Description, Choices: e.Choices}
		if len(ev.Approval.Choices) == 0 {
			ev.Approval.Choices = []string{"once", "deny"}
		}
	case "approval.responded":
		ev.Type, ev.Text = agent.EventAnswered, e.Choice
	case "run.completed", "run.failed", "run.cancelled", "run.interrupted":
		s, _ := terminalStatus(strings.TrimPrefix(e.Event, "run."))
		ev.Type, ev.Status, ev.Text = agent.EventDone, s, e.ExitReason
	default:
		return agent.Event{}, false
	}
	return ev, true
}

// coalescer merges consecutive text and thinking chunks so that the UI and
// the event log are not flooded with one event per token.
type coalescer struct {
	mu      sync.Mutex
	out     chan<- agent.Event
	pending *agent.Event
	timer   *time.Timer
}

const coalesceWindow = 300 * time.Millisecond

func newCoalescer(out chan<- agent.Event) *coalescer { return &coalescer{out: out} }

func (c *coalescer) emit(ev agent.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ev.Type == agent.EventText || ev.Type == agent.EventThinking {
		if c.pending != nil && c.pending.Type == ev.Type {
			c.pending.Text += ev.Text
			return
		}
		c.flushLocked()
		p := ev
		c.pending = &p
		c.timer = time.AfterFunc(coalesceWindow, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.flushLocked()
		})
		return
	}
	c.flushLocked()
	c.out <- ev
}

func (c *coalescer) flushLocked() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.pending != nil {
		if c.pending.Text != "" {
			c.out <- *c.pending
		}
		c.pending = nil
	}
}

func (c *coalescer) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked()
}
