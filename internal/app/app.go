// Package app implements review rounds on top of the project, the store and
// the anchoring logic. It is shared by the HTTP server and the CLI.
package app

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/anchor"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/feedback"
	"github.com/ymotongpoo/reviewer/internal/project"
	"github.com/ymotongpoo/reviewer/internal/store"
	"github.com/ymotongpoo/reviewer/internal/textutil"
)

// Error is an error with an HTTP status code.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func badRequest(format string, a ...any) error {
	return &Error{Code: http.StatusBadRequest, Msg: fmt.Sprintf(format, a...)}
}

func notFound(format string, a ...any) error {
	return &Error{Code: http.StatusNotFound, Msg: fmt.Sprintf(format, a...)}
}

func conflict(format string, a ...any) error {
	return &Error{Code: http.StatusConflict, Msg: fmt.Sprintf(format, a...)}
}

// Event is published to connected clients.
type Event struct {
	Type    string   `json:"type"` // files | tree | comments | round | response | agent | annotate | annotations
	Paths   []string `json:"paths,omitempty"`
	Round   int      `json:"round,omitempty"`
	Request string   `json:"request,omitempty"`
	// Run and Agent are set for agent events.
	Run   string       `json:"run,omitempty"`
	Agent *agent.Event `json:"agent,omitempty"`
}

// App is a running review session for one project.
type App struct {
	mu      sync.Mutex
	Cfg     config.Config
	Proj    *project.Project
	Store   *store.Store
	DataDir string
	notify  func(Event)
	files   []project.File
	now     func() time.Time
	agents  agentState
}

// New opens the project at root with cfg. notify may be nil.
func New(root string, cfg config.Config, notify func(Event)) (*App, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	dataDir, err := cfg.ResolveDataDir(abs)
	if err != nil {
		return nil, err
	}
	proj, err := project.New(abs, dataDir, cfg.Exclude)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dataDir)
	if err != nil {
		return nil, err
	}
	if notify == nil {
		notify = func(Event) {}
	}
	return &App{Cfg: cfg, Proj: proj, Store: st, DataDir: dataDir, notify: notify, now: time.Now}, nil
}

// Init scans the project, opens round 1 when needed, imports pending agent
// responses and re-anchors comments against the current files.
func (a *App) Init() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.rescan(); err != nil {
		return err
	}
	if a.Store.State.Round == 0 {
		if err := a.openRound(1); err != nil {
			return err
		}
	}
	if _, err := a.importResponses(); err != nil {
		log.Printf("import responses: %v", err)
	}
	a.reanchor(nil)
	return nil
}

func (a *App) rescan() error {
	files, err := a.Proj.Walk()
	if err != nil {
		return err
	}
	a.files = files
	return nil
}

func (a *App) threshold() float64 { return a.Cfg.Anchor.FuzzyThreshold }

// latestSubmitted returns the most recent submitted round, or 0.
func (a *App) latestSubmitted() int {
	st := a.Store.State
	if st.RoundStatus == store.RoundSubmitted {
		return st.Round
	}
	return st.Round - 1
}

// baseFiles returns the submitted snapshot used as the diff base.
func (a *App) baseFiles() (int, map[string]string) {
	n := a.latestSubmitted()
	if n < 1 {
		return 0, nil
	}
	m, err := a.Store.Manifest(n)
	if err != nil {
		return 0, nil
	}
	return n, m.SubmitFiles
}

// Warnings lists configuration problems worth showing to the user.
func (a *App) warnings() []string {
	var out []string
	if rel, inside := a.Proj.DataDirInside(); inside && !a.Proj.GitIgnored(rel, true) {
		if _, err := os.Stat(filepath.Join(a.Proj.Root, ".git")); err == nil {
			out = append(out, fmt.Sprintf("データディレクトリ %s/ が .gitignore に含まれていません", rel))
		}
	}
	return out
}

// Warnings lists configuration problems worth showing to the user.
func (a *App) Warnings() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.warnings()
}

// RoundPaths are the files of a submitted round.
type RoundPaths struct {
	Round        int    `json:"round"`
	FeedbackPath string `json:"feedbackPath"`
	FeedbackJSON string `json:"feedbackJsonPath"`
	ResponsePath string `json:"responsePath"`
	Prompt       string `json:"prompt"`
}

func (a *App) roundPaths(n int) RoundPaths {
	dir := a.Store.RoundDir(n)
	rp := RoundPaths{
		Round:        n,
		FeedbackPath: filepath.Join(dir, "feedback.md"),
		FeedbackJSON: filepath.Join(dir, "feedback.json"),
		ResponsePath: filepath.Join(dir, "response.json"),
	}
	prompt, err := feedback.Prompt(a.Cfg.PromptTemplate, feedback.PromptData{
		Round: n, FeedbackPath: rp.FeedbackPath, FeedbackJSONPath: rp.FeedbackJSON,
		ResponsePath: rp.ResponsePath, Project: a.Proj.Root,
	})
	if err != nil {
		prompt = fmt.Sprintf("prompt_template のエラー: %v", err)
	}
	rp.Prompt = prompt
	return rp
}

// Info summarizes the session.
type Info struct {
	Name        string              `json:"name"`
	Root        string              `json:"root"`
	DataDir     string              `json:"dataDir"`
	Round       int                 `json:"round"`
	RoundStatus string              `json:"roundStatus"`
	OpenedAt    time.Time           `json:"openedAt"`
	BaseRound   int                 `json:"baseRound"`
	Labels      []config.Label      `json:"labels"`
	Warnings    []string            `json:"warnings"`
	Latest      *RoundPaths         `json:"latest,omitempty"`
	Response    *store.ResponseInfo `json:"response,omitempty"`
	Rounds      []RoundSummary      `json:"rounds"`
}

// RoundSummary is a past or current round.
type RoundSummary struct {
	Round       int                 `json:"round"`
	OpenedAt    time.Time           `json:"openedAt"`
	SubmittedAt *time.Time          `json:"submittedAt,omitempty"`
	Comments    int                 `json:"comments"`
	Response    *store.ResponseInfo `json:"response,omitempty"`
}

// Info returns the session summary.
func (a *App) Info() Info {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.Store.State
	info := Info{
		Name: a.Proj.Name(), Root: a.Proj.Root, DataDir: a.DataDir,
		Round: st.Round, RoundStatus: st.RoundStatus, Labels: a.Cfg.Labels,
		Warnings: a.warnings(), Rounds: []RoundSummary{},
	}
	info.BaseRound, _ = a.baseFiles()
	for n := 1; n <= st.Round; n++ {
		m, err := a.Store.Manifest(n)
		if err != nil {
			continue
		}
		if n == st.Round {
			info.OpenedAt = m.OpenedAt
		}
		info.Rounds = append(info.Rounds, RoundSummary{
			Round: n, OpenedAt: m.OpenedAt, SubmittedAt: m.SubmittedAt,
			Comments: len(m.FeedbackIDs), Response: m.Response,
		})
	}
	if n := a.latestSubmitted(); n >= 1 {
		rp := a.roundPaths(n)
		info.Latest = &rp
		if m, err := a.Store.Manifest(n); err == nil {
			info.Response = m.Response
		}
	}
	return info
}

// TreeFile is an entry of the file tree.
type TreeFile struct {
	Path       string `json:"path"`
	Unresolved int    `json:"unresolved"`
	Changed    bool   `json:"changed"`
	New        bool   `json:"new"`
}

// Tree lists reviewable files with comment counts. Change markers (against
// the submission) are only set while the round waits for the agent; once
// the next round opens, changes are reviewed from the round history.
func (a *App) Tree() []TreeFile {
	a.mu.Lock()
	defer a.mu.Unlock()
	baseRound, base := 0, map[string]string(nil)
	if a.Store.State.RoundStatus == store.RoundSubmitted {
		baseRound, base = a.baseFiles()
	}
	counts := map[string]int{}
	for _, c := range a.Store.Comments() {
		if c.Path != "" && c.Status != store.StatusResolved {
			counts[c.Path]++
		}
	}
	out := make([]TreeFile, 0, len(a.files))
	for _, f := range a.files {
		tf := TreeFile{Path: f.Path, Unresolved: counts[f.Path]}
		if baseRound > 0 {
			h, ok := base[f.Path]
			tf.New = !ok
			tf.Changed = ok && h != f.Hash
		}
		out = append(out, tf)
	}
	return out
}

// FileView is the content of a file.
type FileView struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

// File returns the current content of path. The content is also stored as a
// blob so that comments made on it can be re-anchored later.
func (a *App) File(path string) (*FileView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := a.readFile(path)
	if err != nil {
		return nil, err
	}
	h, err := a.Store.PutBlob(b)
	if err != nil {
		return nil, err
	}
	return &FileView{Path: path, Content: string(b), Hash: h}, nil
}

func (a *App) readFile(path string) ([]byte, error) {
	b, err := a.Proj.Read(path)
	switch {
	case err == nil:
		return b, nil
	case errors.Is(err, project.ErrOutside):
		return nil, badRequest("不正なパスです: %s", path)
	case errors.Is(err, project.ErrNotText):
		return nil, badRequest("テキストファイルではありません: %s", path)
	case errors.Is(err, fs.ErrNotExist):
		return nil, notFound("ファイルがありません: %s", path)
	default:
		return nil, err
	}
}

// Blob returns stored content by hash.
func (a *App) Blob(h string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := a.Store.Blob(h)
	if err != nil {
		return nil, notFound("blob がありません")
	}
	return b, nil
}

// Comments returns all comments.
func (a *App) Comments() []*store.Comment {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Store.Comments()
}

// NewComment is a request to create a comment.
type NewComment struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Label string `json:"label"`
	Body  string `json:"body"`
	// Hash is the content hash the client was looking at. When the file has
	// changed since, the range is re-anchored onto the current content.
	Hash string `json:"hash"`
}

func (a *App) validLabel(l string) bool {
	return slices.ContainsFunc(a.Cfg.Labels, func(x config.Label) bool { return x.Name == l })
}

// ensureOpen opens the next round when the current one was submitted, so
// that the human can start commenting right after reading the response.
func (a *App) ensureOpen() error {
	if a.Store.State.RoundStatus == store.RoundOpen {
		return nil
	}
	return a.openRound(a.Store.State.Round + 1)
}

// CreateComment creates a draft comment in the current round.
func (a *App) CreateComment(req NewComment) (*store.Comment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.createComment(req)
}

func (a *App) createComment(req NewComment) (*store.Comment, error) {
	if req.Label == "" {
		req.Label = a.Cfg.Labels[0].Name
	}
	if !a.validLabel(req.Label) {
		return nil, badRequest("不明なラベルです: %s", req.Label)
	}
	now := a.now()
	c := &store.Comment{
		Scope: req.Scope, Label: req.Label, Body: req.Body,
		Status: store.StatusDraft, Replies: []store.Reply{}, CreatedAt: now, UpdatedAt: now,
	}
	switch req.Scope {
	case store.ScopeProject:
	case store.ScopeFile, store.ScopeLine:
		b, err := a.readFile(req.Path)
		if err != nil {
			return nil, err
		}
		c.Path = req.Path
		if req.Scope == store.ScopeLine {
			if err := a.anchorNew(c, b, req); err != nil {
				return nil, err
			}
		}
	default:
		return nil, badRequest("不明なスコープです: %s", req.Scope)
	}
	if err := a.ensureOpen(); err != nil {
		return nil, err
	}
	c.ID = a.Store.NewCommentID()
	c.Round = a.Store.State.Round
	if err := a.Store.PutComment(c); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "comments", Paths: nonEmpty(c.Path)})
	return c, nil
}

func (a *App) anchorNew(c *store.Comment, cur []byte, req NewComment) error {
	h, err := a.Store.PutBlob(cur)
	if err != nil {
		return err
	}
	curLines := textutil.SplitLines(string(cur))
	viewLines, viewHash := curLines, h
	if req.Hash != "" && req.Hash != h {
		if b, err := a.Store.Blob(req.Hash); err == nil {
			viewLines, viewHash = textutil.SplitLines(string(b)), req.Hash
		}
	}
	if req.Start < 1 || req.End < req.Start || req.End > len(viewLines) {
		return badRequest("行範囲が不正です: %d-%d", req.Start, req.End)
	}
	anc := anchor.New(viewLines, req.Start, req.End)
	c.Anchor = &anc
	c.OrigStart, c.OrigEnd, c.OrigBlob = req.Start, req.End, viewHash
	c.Loc = &store.Location{Start: req.Start, End: req.End, State: anchor.Exact, Blob: viewHash, Anchor: anc}
	if viewHash != h {
		a.reanchorComment(c, curLines, h)
	}
	return nil
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// CommentPatch updates a comment. Nil fields are left unchanged.
type CommentPatch struct {
	Label  *string `json:"label"`
	Body   *string `json:"body"`
	Status *string `json:"status"`
}

// UpdateComment edits a draft or changes the resolution of a comment.
func (a *App) UpdateComment(id string, p CommentPatch) (*store.Comment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.Store.Comment(id)
	if !ok {
		return nil, notFound("コメントがありません: %s", id)
	}
	if p.Label != nil || p.Body != nil {
		if c.Status != store.StatusDraft {
			return nil, conflict("提出済みのコメントは編集できません。返信を追加してください")
		}
		if p.Label != nil {
			if !a.validLabel(*p.Label) {
				return nil, badRequest("不明なラベルです: %s", *p.Label)
			}
			c.Label = *p.Label
		}
		if p.Body != nil {
			c.Body = *p.Body
		}
	}
	if p.Status != nil {
		switch *p.Status {
		case store.StatusResolved:
			if c.Status == store.StatusDraft {
				return nil, conflict("下書きのコメントは解決にできません")
			}
			c.Status = store.StatusResolved
			c.ResolvedRound = a.Store.State.Round
		case store.StatusOpen:
			if c.Status != store.StatusResolved {
				return nil, conflict("解決済みのコメントのみ再オープンできます")
			}
			c.Status = store.StatusOpen
			c.ResolvedRound = 0
		default:
			return nil, badRequest("この状態には変更できません: %s", *p.Status)
		}
	}
	c.UpdatedAt = a.now()
	if err := a.Store.PutComment(c); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "comments", Paths: nonEmpty(c.Path)})
	return c, nil
}

// DeleteComment deletes a draft comment.
func (a *App) DeleteComment(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.Store.Comment(id)
	if !ok {
		return notFound("コメントがありません: %s", id)
	}
	if c.Status != store.StatusDraft {
		return conflict("提出済みのコメントは削除できません")
	}
	if err := a.Store.DeleteComment(id); err != nil {
		return err
	}
	a.notify(Event{Type: "comments", Paths: nonEmpty(c.Path)})
	return nil
}

// AddReply adds a draft human reply to a submitted comment.
func (a *App) AddReply(id, body string) (*store.Comment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.Store.Comment(id)
	if !ok {
		return nil, notFound("コメントがありません: %s", id)
	}
	if c.Status == store.StatusDraft {
		return nil, conflict("下書きのコメントには返信できません。本文を編集してください")
	}
	if strings.TrimSpace(body) == "" {
		return nil, badRequest("返信が空です")
	}
	if err := a.ensureOpen(); err != nil {
		return nil, err
	}
	now := a.now()
	c.Replies = append(c.Replies, store.Reply{
		ID: a.Store.NewReplyID(), Author: store.AuthorHuman, Body: body,
		Round: a.Store.State.Round, Draft: true, CreatedAt: now, UpdatedAt: now,
	})
	if c.Status == store.StatusResolved {
		c.Status = store.StatusOpen
		c.ResolvedRound = 0
	}
	c.UpdatedAt = now
	if err := a.Store.PutComment(c); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "comments", Paths: nonEmpty(c.Path)})
	return c, nil
}

// UpdateReply edits a draft reply. An empty body deletes it.
func (a *App) UpdateReply(id, rid, body string) (*store.Comment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.Store.Comment(id)
	if !ok {
		return nil, notFound("コメントがありません: %s", id)
	}
	i := slices.IndexFunc(c.Replies, func(r store.Reply) bool { return r.ID == rid })
	if i < 0 {
		return nil, notFound("返信がありません: %s", rid)
	}
	if !c.Replies[i].Draft || c.Replies[i].Author != store.AuthorHuman {
		return nil, conflict("提出済みの返信は編集できません")
	}
	if strings.TrimSpace(body) == "" {
		c.Replies = slices.Delete(c.Replies, i, i+1)
	} else {
		c.Replies[i].Body = body
		c.Replies[i].UpdatedAt = a.now()
	}
	c.UpdatedAt = a.now()
	if err := a.Store.PutComment(c); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "comments", Paths: nonEmpty(c.Path)})
	return c, nil
}

// OpenNextRound starts a new round after a submission.
func (a *App) OpenNextRound() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Store.State.RoundStatus != store.RoundSubmitted {
		return conflict("ラウンド%dはまだ提出されていません", a.Store.State.Round)
	}
	return a.openRound(a.Store.State.Round + 1)
}

func (a *App) openRound(n int) error {
	if err := a.rescan(); err != nil {
		return err
	}
	files, err := a.snapshot()
	if err != nil {
		return err
	}
	m := &store.Manifest{Round: n, OpenedAt: a.now(), OpenFiles: files}
	if err := a.Store.SaveManifest(m); err != nil {
		return err
	}
	a.Store.State.Round = n
	a.Store.State.RoundStatus = store.RoundOpen
	if err := a.Store.SaveState(); err != nil {
		return err
	}
	a.notify(Event{Type: "round", Round: n})
	return nil
}

// snapshot stores every reviewable file as a blob.
func (a *App) snapshot() (map[string]string, error) {
	out := make(map[string]string, len(a.files))
	for _, f := range a.files {
		b, err := a.Proj.Read(f.Path)
		if err != nil {
			continue
		}
		h, err := a.Store.PutBlob(b)
		if err != nil {
			return nil, err
		}
		out[f.Path] = h
	}
	return out, nil
}
