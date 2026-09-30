// Package hermestest provides a fake Hermes API server for tests and for
// trying reviewer without a real Hermes installation.
package hermestest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// SessionRequest records a POST /api/sessions call.
type SessionRequest struct {
	Title  string
	Source string
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
	// ModifyAnnotationTarget appends a line to one target file after writing
	// annotations.json, for edit-detection tests.
	ModifyAnnotationTarget bool
	// PartialSuggestions adds annotations that fix one sentence of a line with
	// several: one with edits and one whose suggestion is only that sentence.
	PartialSuggestions bool
	FailCreateSession  bool
	FailStart          bool

	mu              sync.Mutex
	runs            map[string]*run
	Requests        []Request
	SessionRequests []SessionRequest
	seq             int
	sessionSeq      int
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
		f.mu.Lock()
		data := append([]map[string]any(nil), f.Sessions...)
		f.mu.Unlock()
		if data == nil {
			data = []map[string]any{}
		}
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	case r.Method == http.MethodPost && p == "/api/sessions":
		f.handleCreateSession(w, r)
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

func (f *Fake) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title  string `json:"title"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Invalid JSON"}})
		return
	}
	if body.Source == "" {
		body.Source = "api_server"
	}
	f.mu.Lock()
	f.SessionRequests = append(f.SessionRequests, SessionRequest{Title: body.Title, Source: body.Source})
	if f.FailCreateSession {
		f.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "session creation failed"}})
		return
	}
	for _, session := range f.Sessions {
		if body.Title != "" && session["title"] == body.Title {
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"message": fmt.Sprintf("Title already in use by session %v", session["id"]),
				"type":    "invalid_request_error", "param": nil, "code": "invalid_title",
			}})
			return
		}
	}
	f.sessionSeq++
	id := fmt.Sprintf("api_fake_%04d", f.sessionSeq)
	session := map[string]any{"id": id, "source": body.Source, "title": body.Title, "last_active": float64(time.Now().Unix())}
	f.Sessions = append(f.Sessions, session)
	f.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"object": "hermes.session", "session": session})
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
	f.Requests = append(f.Requests, Request{Input: body.Input, SessionID: body.SessionID, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if f.FailStart {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "run start failed"}})
		return
	}
	f.seq++
	id := fmt.Sprintf("run_%04d", f.seq)
	session := body.SessionID
	if live, ok := f.Rotate[session]; ok {
		session = live
	}
	if session == "" {
		session = id
	}
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
		f.writeAnnotations(input)
		f.OnRun(input, rn.session)
	} else {
		f.writeAnnotations(input)
	}
	setStatus("completed")
	send("run.completed", map[string]any{"completed": true, "partial": false, "interrupted": false})
}

var instructionsPathRE = regexp.MustCompile("`([^`]+/instructions\\.md)`")
// backtickPathRE matches absolute paths in backticks on one line, so that
// code fences elsewhere in the instructions cannot shift the pairing.
var backtickPathRE = regexp.MustCompile("`(/[^`\n]+)`")
var requestIDRE = regexp.MustCompile(`(?m)^# AI review request (Q-[0-9]+)$`)

func (f *Fake) writeAnnotations(input string) {
	m := instructionsPathRE.FindStringSubmatch(input)
	if m == nil {
		return
	}
	instructions, err := os.ReadFile(m[1])
	if err != nil {
		return
	}
	var output string
	var targets []string
	for _, match := range backtickPathRE.FindAllStringSubmatch(string(instructions), -1) {
		path := match[1]
		switch {
		case strings.HasSuffix(path, "/annotations.json"):
			output = path
		case filepath.IsAbs(path) && !strings.HasSuffix(path, "/instructions.md"):
			targets = append(targets, path)
		}
	}
	if output == "" || len(targets) == 0 {
		return
	}
	requestID := filepath.Base(filepath.Dir(output))
	if match := requestIDRE.FindSubmatch(instructions); len(match) == 2 {
		requestID = string(match[1])
	}
	var request struct {
		Files map[string]string `json:"files"`
	}
	requestPath := filepath.Join(filepath.Dir(output), "request.json")
	if b, err := os.ReadFile(requestPath); err == nil {
		json.Unmarshal(b, &request)
	}
	type target struct {
		path  string
		lines []string
	}
	var readable []target
	for _, path := range targets {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			readable = append(readable, target{path: path, lines: lines})
		}
	}
	if len(readable) == 0 {
		return
	}
	first := readable[0]
	firstLine := firstNonEmpty(first.lines, 0)
	second := first
	if len(readable) > 1 {
		second = readable[1]
	}
	secondLine := firstNonEmpty(second.lines, firstLine+1)
	rel := func(path string) string {
		clean := filepath.Clean(path)
		for candidate := range request.Files {
			suffix := filepath.FromSlash(candidate)
			if clean == suffix || strings.HasSuffix(clean, string(filepath.Separator)+suffix) {
				return candidate
			}
		}
		return filepath.ToSlash(filepath.Base(path))
	}
	annotations := []map[string]any{
		{
			"path": rel(first.path), "startLine": firstLine + 4, "endLine": firstLine + 4,
			"quote": []string{first.lines[firstLine]}, "severity": "major", "confidence": "high", "label": "must",
			"body": "（fakehermes）技術的な記述を確認してください。", "suggestion": first.lines[firstLine] + "（修正案）",
			"evidence": []map[string]string{{"url": "https://example.com/fake-source", "quote": "fake evidence", "note": "fakehermes のテスト用根拠"}},
		},
		{
			"path": rel(second.path), "startLine": secondLine + 1, "endLine": secondLine + 1,
			"quote": []string{second.lines[secondLine]}, "severity": "minor", "confidence": "medium", "label": "suggestion",
			"body": "（fakehermes）補足説明を確認してください。", "evidence": []map[string]string{},
		},
	}
	if f.PartialSuggestions {
		annotations = append(annotations, partialSuggestionAnnotations(readable[0].lines, rel(readable[0].path))...)
	}
	out, _ := json.MarshalIndent(map[string]any{
		"request": requestID, "annotations": annotations, "summary": fmt.Sprintf("fakehermes が%d件の指摘を作成しました", len(annotations)),
	}, "", "  ")
	if err := os.WriteFile(output, append(out, '\n'), 0o644); err != nil {
		return
	}
	if f.ModifyAnnotationTarget {
		file, err := os.OpenFile(first.path, os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			file.WriteString("\n<!-- fakehermes edit -->\n")
			file.Close()
		}
	}
}

// partialSuggestionAnnotations fixes the second sentence of the first line
// that has at least two Japanese sentences, once with edits and once with a
// suggestion that restates only that sentence.
func partialSuggestionAnnotations(lines []string, path string) []map[string]any {
	for i, line := range lines {
		parts := strings.SplitAfter(line, "。")
		if len(parts) < 3 || strings.TrimSpace(parts[1]) == "" {
			continue
		}
		second := parts[1]
		fixed := strings.TrimSuffix(second, "。") + "（修正済み）。"
		base := map[string]any{"path": path, "startLine": i + 1, "endLine": i + 1, "quote": []string{line},
			"severity": "minor", "confidence": "high", "label": "suggestion", "evidence": []map[string]string{}}
		withEdits := map[string]any{"body": "（fakehermes）2文目の訳を直してください（edits）。", "edits": []map[string]string{{"find": second, "replace": fixed}}}
		partial := map[string]any{"body": "（fakehermes）2文目の訳を直してください（一部だけの suggestion）。", "suggestion": fixed}
		for k, v := range base {
			withEdits[k], partial[k] = v, v
		}
		return []map[string]any{withEdits, partial}
	}
	return nil
}

func firstNonEmpty(lines []string, start int) int {
	if start >= len(lines) {
		start = 0
	}
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	for i := 0; i < start; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return 0
}
