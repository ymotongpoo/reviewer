// Package server exposes an App over HTTP and serves the web UI.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/app"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/store"
)

//go:embed all:dist
var distFS embed.FS

// Server is the HTTP front end. It serves the project picker at "/" and
// each open project under "/p/<id>/".
type Server struct {
	Token  string
	Port   int
	Reg    *Registry
	static http.Handler
}

// NewToken returns a random token.
func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// New creates a server for the projects in reg.
func New(reg *Registry, token string, port int) *Server {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return &Server{Token: token, Port: port, Reg: reg, static: spa(sub)}
}

func (s *Server) cookieName() string { return fmt.Sprintf("reviewer_token_%d", s.Port) }

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/server", s.handleServer)
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("POST /api/projects/open", s.handleOpenProject)
	mux.HandleFunc("POST /api/projects/{id}/close", s.handleCloseProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleForgetProject)
	mux.HandleFunc("GET /api/fs", s.handleFS)
	mux.HandleFunc("/p/{id}/api/", s.handleProjectAPI)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &app.Error{Code: http.StatusNotFound, Msg: "not found"})
	})
	mux.Handle("/p/{id}/", s.static)
	mux.Handle("/", s.static)
	return s.auth(securityHeaders(mux))
}

// handleProjectAPI dispatches /p/<id>/api/... to the project, opening it
// again when it is known (e.g. after a server restart).
func (s *Server) handleProjectAPI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.Reg.Get(id)
	if err != nil {
		writeError(w, err)
		return
	}
	http.StripPrefix("/p/"+id, p.Handler()).ServeHTTP(w, r)
}

// Handler returns the per-project API handler; paths start with /api/.
func (p *Project) Handler() http.Handler {
	p.muxOnce.Do(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/project", p.handleProject)
		mux.HandleFunc("GET /api/tree", p.handleTree)
		mux.HandleFunc("GET /api/file", p.handleFile)
		mux.HandleFunc("GET /api/rounds/{n}/changes", p.handleRoundChanges)
		mux.HandleFunc("GET /api/rounds/{n}/diff", p.handleRoundDiff)
		mux.HandleFunc("GET /api/blob/{hash}", p.handleBlob)
		mux.HandleFunc("GET /api/comments", p.handleComments)
		mux.HandleFunc("POST /api/comments", p.handleCreateComment)
		mux.HandleFunc("PATCH /api/comments/{id}", p.handleUpdateComment)
		mux.HandleFunc("DELETE /api/comments/{id}", p.handleDeleteComment)
		mux.HandleFunc("POST /api/comments/{id}/replies", p.handleAddReply)
		mux.HandleFunc("PATCH /api/comments/{id}/replies/{rid}", p.handleUpdateReply)
		mux.HandleFunc("DELETE /api/comments/{id}/replies/{rid}", p.handleDeleteReply)
		mux.HandleFunc("POST /api/rounds/submit", p.handleSubmit)
		mux.HandleFunc("POST /api/rounds/open", p.handleOpenRound)
		mux.HandleFunc("GET /api/export", p.handleExport)
		mux.HandleFunc("GET /api/agent", p.handleAgent)
		mux.HandleFunc("GET /api/agent/sessions", p.handleAgentSessions)
		mux.HandleFunc("PUT /api/agent/binding", p.handleAgentBinding)
		mux.HandleFunc("POST /api/agent/send", p.handleAgentSend)
		mux.HandleFunc("GET /api/agent/runs", p.handleAgentRuns)
		mux.HandleFunc("POST /api/agent/runs/{id}/stop", p.handleAgentStop)
		mux.HandleFunc("POST /api/agent/runs/{id}/approval", p.handleAgentApproval)
		mux.HandleFunc("GET /api/presets", p.handlePresets)
		mux.HandleFunc("PUT /api/presets", p.handleSavePresets)
		mux.HandleFunc("POST /api/annotate", p.handleAnnotate)
		mux.HandleFunc("GET /api/annotate/requests", p.handleAnnotationRequests)
		mux.HandleFunc("GET /api/annotate/requests/{id}", p.handleAnnotationRequest)
		mux.HandleFunc("PATCH /api/annotate/requests/{id}", p.handleUpdateAnnotationRequest)
		mux.HandleFunc("POST /api/annotate/requests/{id}/discard", p.handleDiscardAnnotationRequest)
		mux.HandleFunc("GET /api/annotate/requests/{id}/diff", p.handleAnnotationRequestDiff)
		mux.HandleFunc("GET /api/annotations", p.handleAnnotations)
		mux.HandleFunc("POST /api/annotations/{id}/adopt", p.handleAdoptAnnotation)
		mux.HandleFunc("PATCH /api/annotations/{id}", p.handleUpdateAnnotation)
		mux.HandleFunc("GET /api/git", p.handleGitStatus)
		mux.HandleFunc("PUT /api/git/settings", p.handleGitSettings)
		mux.HandleFunc("POST /api/git/commit", p.handleGitCommit)
		mux.HandleFunc("POST /api/git/push", p.handleGitPush)
		mux.HandleFunc("GET /api/events", p.handleEvents)
		mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, &app.Error{Code: http.StatusNotFound, Msg: "not found"})
		})
		p.mux = mux
	})
	return p.mux
}

// auth accepts the token from the query (then stored in a cookie), the
// cookie, or an Authorization: Bearer header. Mutating requests must carry
// the X-Reviewer header, which cross-site forms cannot set.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("token"); t != "" && r.Method == http.MethodGet {
			if !s.validToken(t) {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: s.cookieName(), Value: t, Path: "/", HttpOnly: true,
				SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600,
			})
			q := r.URL.Query()
			q.Del("token")
			u := *r.URL
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.RequestURI(), http.StatusFound)
			return
		}
		tok := ""
		if c, err := r.Cookie(s.cookieName()); err == nil {
			tok = c.Value
		}
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimPrefix(h, "Bearer ")
		}
		if !s.validToken(tok) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, &app.Error{Code: http.StatusUnauthorized, Msg: "認証が必要です。起動時に表示された URL を開いてください"})
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, unauthorizedPage)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Reviewer") == "" {
			writeError(w, &app.Error{Code: http.StatusForbidden, Msg: "X-Reviewer header required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validToken(t string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(s.Token)) == 1
}

const unauthorizedPage = `<!doctype html><meta charset="utf-8"><title>reviewer</title>
<body style="font-family:system-ui;padding:2rem"><h1>認証が必要です</h1>
<p>reviewer の起動時にターミナルへ表示された <code>?token=</code> 付きの URL を開いてください。</p></body>`

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// spa serves static files and falls back to index.html.
func spa(fsys fs.FS) http.Handler {
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := fsys.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "web UI is not built; run `make web`", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	var ae *app.Error
	if errors.As(err, &ae) {
		code = ae.Code
	} else {
		log.Printf("error: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		return &app.Error{Code: http.StatusBadRequest, Msg: "invalid JSON: " + err.Error()}
	}
	return nil
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, v)
}

func (p *Project) handleProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, p.App.Info())
}

func (p *Project) handleTree(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"files": p.App.Tree()})
}

func (p *Project) handleFile(w http.ResponseWriter, r *http.Request) {
	fv, err := p.App.File(r.URL.Query().Get("path"))
	respond(w, fv, err)
}

func roundParam(r *http.Request) int {
	var n int
	fmt.Sscan(r.PathValue("n"), &n)
	return n
}

func (p *Project) handleRoundChanges(w http.ResponseWriter, r *http.Request) {
	rc, err := p.App.RoundChangesMode(roundParam(r), r.URL.Query().Get("phase"))
	respond(w, rc, err)
}

func (p *Project) handleRoundDiff(w http.ResponseWriter, r *http.Request) {
	d, err := p.App.RoundDiffMode(roundParam(r), r.URL.Query().Get("path"), r.URL.Query().Get("phase"))
	respond(w, d, err)
}

func (p *Project) handleBlob(w http.ResponseWriter, r *http.Request) {
	b, err := p.App.Blob(r.PathValue("hash"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Write(b)
}

func (p *Project) handleComments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"comments": p.App.Comments()})
}

func (p *Project) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	var req app.NewComment
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := p.App.CreateComment(req)
	respond(w, c, err)
}

func (p *Project) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	var patch app.CommentPatch
	if err := decode(r, &patch); err != nil {
		writeError(w, err)
		return
	}
	c, err := p.App.UpdateComment(r.PathValue("id"), patch)
	respond(w, c, err)
}

func (p *Project) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	err := p.App.DeleteComment(r.PathValue("id"))
	respond(w, map[string]bool{"ok": true}, err)
}

type replyReq struct {
	Body string `json:"body"`
}

func (p *Project) handleAddReply(w http.ResponseWriter, r *http.Request) {
	var req replyReq
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := p.App.AddReply(r.PathValue("id"), req.Body)
	respond(w, c, err)
}

func (p *Project) handleUpdateReply(w http.ResponseWriter, r *http.Request) {
	var req replyReq
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := p.App.UpdateReply(r.PathValue("id"), r.PathValue("rid"), req.Body)
	respond(w, c, err)
}

func (p *Project) handleDeleteReply(w http.ResponseWriter, r *http.Request) {
	c, err := p.App.UpdateReply(r.PathValue("id"), r.PathValue("rid"), "")
	respond(w, c, err)
}

type submitReq struct {
	SendToAgent bool `json:"sendToAgent"`
}

type submitRes struct {
	*app.SubmitResult
	AgentRun   *store.AgentRun `json:"agentRun,omitempty"`
	AgentError string          `json:"agentError,omitempty"`
}

func (p *Project) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req submitReq
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			writeError(w, err)
			return
		}
	}
	res, err := p.App.Submit()
	if err != nil {
		writeError(w, err)
		return
	}
	out := submitRes{SubmitResult: res}
	if req.SendToAgent {
		// The round is submitted either way; a failed send can be retried.
		run, err := p.App.SendToAgent(r.Context(), res.Round)
		if err != nil {
			out.AgentError = err.Error()
		}
		out.AgentRun = run
	}
	writeJSON(w, out)
}

func (p *Project) handleAgent(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, p.App.AgentInfo())
}

func (p *Project) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	ss, err := p.App.AgentSessions(r.Context())
	respond(w, map[string]any{"sessions": ss}, err)
}

func (p *Project) handleAgentBinding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	b, err := p.App.BindAgent(r.Context(), req.SessionID)
	respond(w, map[string]any{"binding": b}, err)
}

func (p *Project) handleAgentSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Round int `json:"round"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	run, err := p.App.SendToAgent(r.Context(), req.Round)
	respond(w, run, err)
}

func (p *Project) handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	var n int
	fmt.Sscan(r.URL.Query().Get("round"), &n)
	writeJSON(w, map[string]any{"runs": p.App.AgentRuns(n)})
}

func (p *Project) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	err := p.App.StopAgentRun(r.Context(), r.PathValue("id"))
	respond(w, map[string]bool{"ok": true}, err)
}

func (p *Project) handleAgentApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ApprovalID string `json:"approvalId"`
		Choice     string `json:"choice"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	err := p.App.AnswerAgentRun(r.Context(), r.PathValue("id"), req.ApprovalID, req.Choice)
	respond(w, map[string]bool{"ok": true}, err)
}

func (p *Project) handlePresets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"presets": p.App.Presets()})
}

func (p *Project) handleSavePresets(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Presets []config.Preset `json:"presets"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	presets, err := p.App.SaveProjectPresets(req.Presets)
	respond(w, map[string]any{"presets": presets}, err)
}

func (p *Project) handleAnnotate(w http.ResponseWriter, r *http.Request) {
	var req app.AnnotateInput
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	result, err := p.App.Annotate(r.Context(), req)
	respond(w, result, err)
}

func (p *Project) handleAnnotationRequests(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"requests": p.App.AnnotationRequests()})
}

func (p *Project) handleAnnotationRequest(w http.ResponseWriter, r *http.Request) {
	request, err := p.App.AnnotationRequest(r.PathValue("id"))
	respond(w, request, err)
}

func (p *Project) handleUpdateAnnotationRequest(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		Hidden *bool `json:"hidden"`
	}
	if err := decode(r, &patch); err != nil {
		writeError(w, err)
		return
	}
	if patch.Hidden == nil {
		writeError(w, &app.Error{Code: http.StatusBadRequest, Msg: "hidden を指定してください"})
		return
	}
	request, err := p.App.SetRequestHidden(r.PathValue("id"), *patch.Hidden)
	respond(w, request, err)
}

func (p *Project) handleDiscardAnnotationRequest(w http.ResponseWriter, r *http.Request) {
	err := p.App.DiscardRequest(r.PathValue("id"))
	respond(w, map[string]bool{"ok": true}, err)
}

func (p *Project) handleAnnotationRequestDiff(w http.ResponseWriter, r *http.Request) {
	diff, err := p.App.AnnotationRequestDiff(r.PathValue("id"), r.URL.Query().Get("path"))
	respond(w, diff, err)
}

func (p *Project) handleAnnotations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"annotations": p.App.Annotations(r.URL.Query().Get("all") == "1")})
}

func (p *Project) handleAdoptAnnotation(w http.ResponseWriter, r *http.Request) {
	var patch app.AnnotationAdoptPatch
	if r.ContentLength != 0 {
		if err := decode(r, &patch); err != nil {
			writeError(w, err)
			return
		}
	}
	comment, err := p.App.AdoptAnnotation(r.PathValue("id"), patch)
	respond(w, comment, err)
}

func (p *Project) handleUpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		State string `json:"state"`
	}
	if err := decode(r, &patch); err != nil {
		writeError(w, err)
		return
	}
	annotation, err := p.App.SetAnnotationState(r.PathValue("id"), patch.State)
	respond(w, annotation, err)
}

func (p *Project) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, err := p.App.GitStatus(r.Context(), q.Get("remote"), q.Get("branch"))
	respond(w, st, err)
}

func (p *Project) handleGitSettings(w http.ResponseWriter, r *http.Request) {
	var req config.GitConfig
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	g, err := p.App.SaveGitSettings(r.Context(), req)
	respond(w, g, err)
}

// Commit and push keep running when the browser goes away: interrupting git
// half-way would leave a lock file or an unknown push state behind. They
// have their own timeouts.

func (p *Project) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	var req app.GitCommitInput
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	res, err := p.App.GitCommit(context.WithoutCancel(r.Context()), req)
	respond(w, res, err)
}

func (p *Project) handleGitPush(w http.ResponseWriter, r *http.Request) {
	var req app.GitPushInput
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	res, err := p.App.GitPush(context.WithoutCancel(r.Context()), req)
	respond(w, res, err)
}

func (p *Project) handleOpenRound(w http.ResponseWriter, r *http.Request) {
	err := p.App.OpenNextRound()
	respond(w, map[string]bool{"ok": true}, err)
}

func (p *Project) handleExport(w http.ResponseWriter, r *http.Request) {
	var n int
	fmt.Sscan(r.URL.Query().Get("round"), &n)
	format := r.URL.Query().Get("format")
	b, err := p.App.Export(n, format)
	if err != nil {
		writeError(w, err)
		return
	}
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	}
	w.Write(b)
}

func (p *Project) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := p.Hub.Subscribe()
	defer p.Hub.Unsubscribe(ch)
	fmt.Fprint(w, "retry: 2000\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// Hub fans out events to SSE subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[chan app.Event]bool
}

// NewHub creates a hub.
func NewHub() *Hub { return &Hub{subs: map[chan app.Event]bool{}} }

// Publish sends ev to every subscriber without blocking.
func (h *Hub) Publish(ev app.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribe registers a subscriber.
func (h *Hub) Subscribe() chan app.Event {
	ch := make(chan app.Event, 64)
	h.mu.Lock()
	h.subs[ch] = true
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber.
func (h *Hub) Unsubscribe(ch chan app.Event) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}
