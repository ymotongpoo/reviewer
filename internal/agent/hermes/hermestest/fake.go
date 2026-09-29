// Package hermestest provides a fake Hermes API server for tests and for
// trying reviewer without a real Hermes installation.
package hermestest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Step is one scripted event of a run. A Step with Approval set blocks
// until the client answers it.
type Step struct {
	Event    string         // e.g. "message.delta", "tool.started"
	Fields   map[string]any // extra fields of the event
	Approval bool           // wait for POST /v1/runs/{id}/approval
	Delay    time.Duration
}

// Request records a POST /v1/runs call.
type Request struct {
	Input          string
	SessionID      string
	IdempotencyKey string
}

// Fake is a fake Hermes API server. Zero values are usable except Key.
type Fake struct {
	Key      string
	Sessions []map[string]any
	// Script returns the steps of a run for the given input. The default
	// streams a short answer.
	Script func(input string) []Step
	// OnRun is called before the run completes, e.g. to edit files.
	OnRun func(input, sessionID string)
	// Rotate maps a session id to the live id reported by the run status.
	Rotate map[string]string
	// Keepalive is sent between steps when non-zero.
	Keepalive time.Duration

	mu       sync.Mutex
	runs     map[string]*run
	Requests []Request
	seq      int
}

type run struct {
	id, session, status, input string
	approval                   chan string
	stop                       chan struct{}
	stopOnce                   sync.Once
}

func (f *Fake) init() {
	if f.runs == nil {
		f.runs = map[string]*run{}
	}
}

// ServeHTTP implements http.Handler.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.Key {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Invalid API key"}})
		return
	}
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/health":
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	case r.Method == http.MethodGet && p == "/api/sessions":
		data := f.Sessions
		if data == nil {
			data = []map[string]any{}
		}
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	case r.Method == http.MethodPost && p == "/v1/runs":
		f.handleStart(w, r)
	case strings.HasPrefix(p, "/v1/runs/"):
		rest := strings.TrimPrefix(p, "/v1/runs/")
		id, action, _ := strings.Cut(rest, "/")
		f.mu.Lock()
		f.init()
		rn := f.runs[id]
		f.mu.Unlock()
		if rn == nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Run not found: " + id}})
			return
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			f.mu.Lock()
			st := map[string]any{"run_id": id, "status": rn.status, "session_id": rn.session}
			f.mu.Unlock()
			json.NewEncoder(w).Encode(st)
		case action == "events":
			f.stream(w, r, rn)
		case action == "approval" && r.Method == http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			choice, _ := body["choice"].(string)
			select {
			case rn.approval <- choice:
				json.NewEncoder(w).Encode(map[string]any{"run_id": id, "choice": choice, "resolved": 1})
			default:
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Run has no pending approval"}})
			}
		case action == "stop" && r.Method == http.MethodPost:
			rn.stopOnce.Do(func() { close(rn.stop) })
			json.NewEncoder(w).Encode(map[string]any{"run_id": id, "status": "stopping"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *Fake) handleStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input     string `json:"input"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Input == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Missing 'input' field"}})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	f.seq++
	id := fmt.Sprintf("run_%04d", f.seq)
	session := body.SessionID
	if live, ok := f.Rotate[session]; ok {
		session = live
	}
	if session == "" {
		session = id
	}
	f.Requests = append(f.Requests, Request{Input: body.Input, SessionID: body.SessionID, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	f.runs[id] = &run{id: id, session: session, input: body.Input, status: "queued", approval: make(chan string), stop: make(chan struct{})}
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{"run_id": id, "status": "queued", "replayed": false})
}

func (f *Fake) steps(input string) []Step {
	if f.Script != nil {
		return f.Script(input)
	}
	return []Step{
		{Event: "message.delta", Fields: map[string]any{"delta": "レビューを"}},
		{Event: "message.delta", Fields: map[string]any{"delta": "確認しました。"}},
		{Event: "tool.started", Fields: map[string]any{"tool": "read_file", "preview": "feedback.md"}},
		{Event: "tool.completed", Fields: map[string]any{"tool": "read_file", "duration": 0.1, "error": false}},
	}
}

func (f *Fake) stream(w http.ResponseWriter, r *http.Request, rn *run) {
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(name string, fields map[string]any) {
		ev := map[string]any{"event": name, "run_id": rn.id, "timestamp": float64(time.Now().UnixNano()) / 1e9}
		for k, v := range fields {
			ev[k] = v
		}
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	setStatus := func(s string) {
		f.mu.Lock()
		rn.status = s
		f.mu.Unlock()
	}
	input := rn.input
	setStatus("running")
	send("run.started", nil)
	for _, st := range f.steps(input) {
		if f.Keepalive > 0 {
			fmt.Fprint(w, ": keepalive\n\n")
		}
		select {
		case <-rn.stop:
			setStatus("cancelled")
			send("run.cancelled", map[string]any{"completed": false, "interrupted": true})
			return
		case <-r.Context().Done():
			return
		case <-time.After(st.Delay):
		}
		if st.Approval {
			setStatus("waiting_for_approval")
			fields := map[string]any{"command": "rm -rf build", "description": "delete build dir", "choices": []string{"once", "session", "always", "deny"}}
			for k, v := range st.Fields {
				fields[k] = v
			}
			send("approval.request", fields)
			select {
			case choice := <-rn.approval:
				send("approval.responded", map[string]any{"choice": choice, "resolved": 1})
				setStatus("running")
			case <-rn.stop:
				setStatus("cancelled")
				send("run.cancelled", map[string]any{"completed": false, "interrupted": true})
				return
			case <-r.Context().Done():
				return
			}
			continue
		}
		send(st.Event, st.Fields)
	}
	if f.OnRun != nil {
		f.OnRun(input, rn.session)
	}
	setStatus("completed")
	send("run.completed", map[string]any{"completed": true, "partial": false, "interrupted": false})
}
