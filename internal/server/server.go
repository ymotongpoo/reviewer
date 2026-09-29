// Package server exposes an App over HTTP and serves the web UI.
package server

import (
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
	"github.com/ymotongpoo/reviewer/internal/store"
)

//go:embed all:dist
var distFS embed.FS

// Server is the HTTP front end.
type Server struct {
	App    *app.App
	Token  string
	Port   int
	hub    *Hub
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

// New creates a server. The returned Hub must be connected to the App's
// notifications.
func New(a *app.App, token string, port int, hub *Hub) *Server {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return &Server{App: a, Token: token, Port: port, hub: hub, static: spa(sub)}
}

func (s *Server) cookieName() string { return fmt.Sprintf("reviewer_token_%d", s.Port) }

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/project", s.handleProject)
	mux.HandleFunc("GET /api/tree", s.handleTree)
	mux.HandleFunc("GET /api/file", s.handleFile)
	mux.HandleFunc("GET /api/diff", s.handleDiff)
	mux.HandleFunc("GET /api/blob/{hash}", s.handleBlob)
	mux.HandleFunc("GET /api/comments", s.handleComments)
	mux.HandleFunc("POST /api/comments", s.handleCreateComment)
	mux.HandleFunc("PATCH /api/comments/{id}", s.handleUpdateComment)
	mux.HandleFunc("DELETE /api/comments/{id}", s.handleDeleteComment)
	mux.HandleFunc("POST /api/comments/{id}/replies", s.handleAddReply)
	mux.HandleFunc("PATCH /api/comments/{id}/replies/{rid}", s.handleUpdateReply)
	mux.HandleFunc("DELETE /api/comments/{id}/replies/{rid}", s.handleDeleteReply)
	mux.HandleFunc("POST /api/rounds/submit", s.handleSubmit)
	mux.HandleFunc("POST /api/rounds/open", s.handleOpenRound)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("GET /api/agent", s.handleAgent)
	mux.HandleFunc("GET /api/agent/sessions", s.handleAgentSessions)
	mux.HandleFunc("PUT /api/agent/binding", s.handleAgentBinding)
	mux.HandleFunc("POST /api/agent/send", s.handleAgentSend)
	mux.HandleFunc("GET /api/agent/runs", s.handleAgentRuns)
	mux.HandleFunc("POST /api/agent/runs/{id}/stop", s.handleAgentStop)
	mux.HandleFunc("POST /api/agent/runs/{id}/approval", s.handleAgentApproval)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &app.Error{Code: http.StatusNotFound, Msg: "not found"})
	})
	mux.Handle("/", s.static)
	return s.auth(securityHeaders(mux))
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

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.App.Info())
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"files": s.App.Tree()})
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	fv, err := s.App.File(r.URL.Query().Get("path"))
	respond(w, fv, err)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	dv, err := s.App.Diff(r.URL.Query().Get("path"))
	respond(w, dv, err)
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	b, err := s.App.Blob(r.PathValue("hash"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Write(b)
}

func (s *Server) handleComments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"comments": s.App.Comments()})
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	var req app.NewComment
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.App.CreateComment(req)
	respond(w, c, err)
}

func (s *Server) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	var p app.CommentPatch
	if err := decode(r, &p); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.App.UpdateComment(r.PathValue("id"), p)
	respond(w, c, err)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	err := s.App.DeleteComment(r.PathValue("id"))
	respond(w, map[string]bool{"ok": true}, err)
}

type replyReq struct {
	Body string `json:"body"`
}

func (s *Server) handleAddReply(w http.ResponseWriter, r *http.Request) {
	var req replyReq
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.App.AddReply(r.PathValue("id"), req.Body)
	respond(w, c, err)
}

func (s *Server) handleUpdateReply(w http.ResponseWriter, r *http.Request) {
	var req replyReq
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.App.UpdateReply(r.PathValue("id"), r.PathValue("rid"), req.Body)
	respond(w, c, err)
}

func (s *Server) handleDeleteReply(w http.ResponseWriter, r *http.Request) {
	c, err := s.App.UpdateReply(r.PathValue("id"), r.PathValue("rid"), "")
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

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req submitReq
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			writeError(w, err)
			return
		}
	}
	res, err := s.App.Submit()
	if err != nil {
		writeError(w, err)
		return
	}
	out := submitRes{SubmitResult: res}
	if req.SendToAgent {
		// The round is submitted either way; a failed send can be retried.
		run, err := s.App.SendToAgent(r.Context(), res.Round)
		if err != nil {
			out.AgentError = err.Error()
		}
		out.AgentRun = run
	}
	writeJSON(w, out)
}

func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.App.AgentInfo())
}

func (s *Server) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	ss, err := s.App.AgentSessions(r.Context())
	respond(w, map[string]any{"sessions": ss}, err)
}

func (s *Server) handleAgentBinding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	b, err := s.App.BindAgent(r.Context(), req.SessionID)
	respond(w, map[string]any{"binding": b}, err)
}

func (s *Server) handleAgentSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Round int `json:"round"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	run, err := s.App.SendToAgent(r.Context(), req.Round)
	respond(w, run, err)
}

func (s *Server) handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	var n int
	fmt.Sscan(r.URL.Query().Get("round"), &n)
	writeJSON(w, map[string]any{"runs": s.App.AgentRuns(n)})
}

func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	err := s.App.StopAgentRun(r.Context(), r.PathValue("id"))
	respond(w, map[string]bool{"ok": true}, err)
}

func (s *Server) handleAgentApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ApprovalID string `json:"approvalId"`
		Choice     string `json:"choice"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	err := s.App.AnswerAgentRun(r.Context(), r.PathValue("id"), req.ApprovalID, req.Choice)
	respond(w, map[string]bool{"ok": true}, err)
}

func (s *Server) handleOpenRound(w http.ResponseWriter, r *http.Request) {
	err := s.App.OpenNextRound()
	respond(w, map[string]bool{"ok": true}, err)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var n int
	fmt.Sscan(r.URL.Query().Get("round"), &n)
	format := r.URL.Query().Get("format")
	b, err := s.App.Export(n, format)
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

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)
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
