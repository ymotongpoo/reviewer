package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/app"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/gitops"
	"github.com/ymotongpoo/reviewer/internal/store"
)

// maxRecent bounds the recent project list.
const maxRecent = 50

// Project is an open review session of a directory. The default session
// of directory id is served under /p/<id>/, the others under
// /p/<id>/s/<session>/.
type Project struct {
	ID      string // the directory's id
	Session string
	Root    string
	App     *app.App
	Hub     *Hub
	dir     *Dir
	reg     *Registry

	muxOnce sync.Once
	mux     http.Handler
}

// URL returns the session's page path.
func (p *Project) URL() string { return sessionPath(p.ID, p.Session) }

func sessionPath(id, session string) string {
	if session == store.DefaultSession {
		return "/p/" + id + "/"
	}
	return "/p/" + id + "/s/" + session + "/"
}

// Dir is an open directory with its open sessions, which share one watcher.
type Dir struct {
	ID     string
	Root   string
	WS     *app.Workspace
	cancel context.CancelFunc

	mu       sync.Mutex
	sessions map[string]*Project
}

func (d *Dir) apps() []*app.App {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*app.App, 0, len(d.sessions))
	for _, p := range d.sessions {
		out = append(out, p.App)
	}
	return out
}

func (d *Dir) busy() bool {
	for _, a := range d.apps() {
		if a.AgentBusy() {
			return true
		}
	}
	return false
}

// broadcast sends ev to every open session of the directory.
func (d *Dir) broadcast(ev app.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range d.sessions {
		p.Hub.Publish(ev)
	}
}

// AgentSetup connects a newly opened session to the shared agent. url is
// the session's page, used in notifications.
type AgentSetup func(a *app.App, url string)

type recentEntry struct {
	Path       string    `json:"path"`
	LastOpened time.Time `json:"lastOpened"`
}

// Registry holds the open directories and the list of recent ones.
type Registry struct {
	mu       sync.Mutex
	roots    []string // absolute, symlinks resolved
	stateDir string
	baseURL  string
	setup    AgentSetup
	open     map[string]*Dir
	recent   []recentEntry
}

// ProjectID returns the stable id of a project root.
func ProjectID(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:6])
}

// NewRegistry creates a registry limited to roots (entries may start with
// "~"). The recent list is read from stateDir. baseURL (without a trailing
// slash) is used to build project URLs for notifications.
func NewRegistry(roots []string, stateDir, baseURL string, setup AgentSetup) (*Registry, error) {
	r := &Registry{stateDir: stateDir, baseURL: strings.TrimRight(baseURL, "/"), setup: setup, open: map[string]*Dir{}}
	for _, root := range roots {
		abs, err := filepath.Abs(config.ExpandHome(root))
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		r.roots = append(r.roots, abs)
	}
	if len(r.roots) == 0 {
		return nil, errors.New("roots に開けるディレクトリがありません")
	}
	if b, err := os.ReadFile(r.recentPath()); err == nil {
		json.Unmarshal(b, &r.recent)
	}
	return r, nil
}

func (r *Registry) recentPath() string { return filepath.Join(r.stateDir, "projects.json") }

func (r *Registry) saveRecent() {
	if r.stateDir == "" {
		return
	}
	if err := os.MkdirAll(r.stateDir, 0o700); err != nil {
		log.Printf("state dir: %v", err)
		return
	}
	b, _ := json.MarshalIndent(r.recent, "", "  ")
	tmp := r.recentPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, r.recentPath())
	}
}

// Roots returns the allowed roots.
func (r *Registry) Roots() []string { return append([]string{}, r.roots...) }

func (r *Registry) within(p string) bool {
	for _, root := range r.roots {
		if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Resolve validates a user-supplied directory path ("~" allowed) and
// returns its absolute, symlink-resolved form.
func (r *Registry) Resolve(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", &app.Error{Code: http.StatusBadRequest, Msg: "ディレクトリを指定してください"}
	}
	p = config.ExpandHome(p)
	if !filepath.IsAbs(p) {
		return "", &app.Error{Code: http.StatusBadRequest, Msg: "絶対パスか ~ から始まるパスを指定してください"}
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		return "", &app.Error{Code: http.StatusNotFound, Msg: "ディレクトリがありません: " + p}
	}
	if !r.within(real) {
		return "", &app.Error{Code: http.StatusForbidden, Msg: "このディレクトリは開けません（許可されている場所: " + strings.Join(r.displayRoots(), ", ") + "）"}
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return "", &app.Error{Code: http.StatusBadRequest, Msg: "ディレクトリではありません: " + p}
	}
	return real, nil
}

func (r *Registry) displayRoots() []string {
	out := make([]string, len(r.roots))
	for i, root := range r.roots {
		out[i] = displayPath(root)
	}
	return out
}

// displayPath abbreviates the home directory as "~".
func displayPath(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// Open opens (or returns the already open) default session of the
// directory at path.
func (r *Registry) Open(path string) (*Project, error) {
	root, err := r.Resolve(path)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	d, err := r.openLocked(root)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return r.session(d, store.DefaultSession)
}

// openLocked opens the directory root and its default session.
func (r *Registry) openLocked(root string) (*Dir, error) {
	id := ProjectID(root)
	if d, ok := r.open[id]; ok {
		r.touchLocked(root)
		return d, nil
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		return nil, &app.Error{Code: http.StatusBadRequest, Msg: err.Error()}
	}
	ws, err := app.OpenWorkspace(root, cfg)
	if err != nil {
		return nil, err
	}
	d := &Dir{ID: id, Root: ws.Proj.Root, WS: ws, sessions: map[string]*Project{}}
	if _, err := r.openSession(d, store.DefaultSession); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go func() {
		if err := ws.Watch(ctx, d.apps); err != nil {
			log.Printf("watch %s: %v", root, err)
		}
	}()
	r.open[id] = d
	r.touchLocked(root)
	log.Printf("opened %s (%s)", root, id)
	return d, nil
}

// session returns the open session sid of d, opening it when needed.
func (r *Registry) session(d *Dir, sid string) (*Project, error) {
	if !store.ValidSessionID(sid) {
		return nil, &app.Error{Code: http.StatusNotFound, Msg: "セッションが見つかりません: " + sid}
	}
	return r.openSession(d, sid)
}

func (r *Registry) openSession(d *Dir, sid string) (*Project, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if p, ok := d.sessions[sid]; ok {
		return p, nil
	}
	hub := NewHub()
	a, err := d.WS.Open(sid, hub.Publish)
	if err != nil {
		return nil, err
	}
	if err := a.Init(); err != nil {
		return nil, err
	}
	p := &Project{ID: d.ID, Session: sid, Root: d.Root, App: a, Hub: hub, dir: d, reg: r}
	if r.setup != nil {
		r.setup(a, r.SessionURL(d.ID, sid))
	}
	d.sessions[sid] = p
	return p, nil
}

// ProjectURL returns the page of a directory's default session (without
// the token).
func (r *Registry) ProjectURL(id string) string { return r.SessionURL(id, store.DefaultSession) }

// SessionURL returns the page of a session (without the token).
func (r *Registry) SessionURL(id, sid string) string {
	if r.baseURL == "" {
		return ""
	}
	return r.baseURL + sessionPath(id, sid)
}

func (r *Registry) touchLocked(root string) {
	now := time.Now()
	out := []recentEntry{{Path: root, LastOpened: now}}
	for _, e := range r.recent {
		if e.Path != root {
			out = append(out, e)
		}
	}
	if len(out) > maxRecent {
		out = out[:maxRecent]
	}
	r.recent = out
	r.saveRecent()
}

// dir returns the open directory with id, reopening a recent directory that
// is not loaded (e.g. after a restart of the server).
func (r *Registry) dir(id string) (*Dir, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.open[id]; ok {
		return d, nil
	}
	for _, e := range r.recent {
		if ProjectID(e.Path) == id {
			if !r.within(e.Path) {
				break
			}
			return r.openLocked(e.Path)
		}
	}
	return nil, &app.Error{Code: http.StatusNotFound, Msg: "プロジェクトが見つかりません。選択画面から開き直してください"}
}

// Get returns the default session of the directory with id.
func (r *Registry) Get(id string) (*Project, error) {
	return r.GetSession(id, store.DefaultSession)
}

// GetSession returns the session sid of the directory with id.
func (r *Registry) GetSession(id, sid string) (*Project, error) {
	d, err := r.dir(id)
	if err != nil {
		return nil, err
	}
	return r.session(d, sid)
}

// CreateSession adds a session named name to the directory with id.
func (r *Registry) CreateSession(ctx context.Context, id, name, branch string) (*Project, error) {
	d, err := r.dir(id)
	if err != nil {
		return nil, err
	}
	s, err := d.WS.CreateSession(ctx, name, branch)
	if err != nil {
		return nil, err
	}
	p, err := r.session(d, s.ID)
	if err != nil {
		return nil, err
	}
	d.broadcast(app.Event{Type: "session"})
	return p, nil
}

// Close unloads a directory with all its sessions. It refuses while the
// agent is working on one of them.
func (r *Registry) Close(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.open[id]
	if !ok {
		return nil
	}
	if d.busy() {
		return &app.Error{Code: http.StatusConflict, Msg: "エージェントの対応中は閉じられません"}
	}
	d.cancel()
	delete(r.open, id)
	log.Printf("closed %s", d.Root)
	return nil
}

// Forget closes a project and removes it from the recent list. Review data
// in the directory is kept.
func (r *Registry) Forget(id string) error {
	if err := r.Close(id); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.recent[:0]
	for _, e := range r.recent {
		if ProjectID(e.Path) != id {
			out = append(out, e)
		}
	}
	r.recent = out
	r.saveRecent()
	return nil
}

// CloseAll stops every project.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, d := range r.open {
		d.cancel()
		delete(r.open, id)
	}
}

// SessionSummary describes a review session of a directory.
type SessionSummary struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	URL         string              `json:"url"`
	CreatedAt   time.Time           `json:"createdAt"`
	Archived    bool                `json:"archived,omitempty"`
	Branch      string              `json:"branch,omitempty"`
	Open        bool                `json:"open"`
	Initialized bool                `json:"initialized"`
	Round       int                 `json:"round,omitempty"`
	RoundStatus string              `json:"roundStatus,omitempty"`
	Agent       *store.AgentBinding `json:"agent,omitempty"`
	Busy        bool                `json:"busy,omitempty"`
}

// ProjectSummary describes a recent project for the picker. The round and
// agent fields describe the default session; Sessions lists all sessions
// that are not archived.
type ProjectSummary struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Path        string              `json:"path"`
	Display     string              `json:"display"`
	Open        bool                `json:"open"`
	Exists      bool                `json:"exists"`
	Initialized bool                `json:"initialized"`
	LastOpened  time.Time           `json:"lastOpened"`
	Round       int                 `json:"round,omitempty"`
	RoundStatus string              `json:"roundStatus,omitempty"`
	Agent       *store.AgentBinding `json:"agent,omitempty"`
	Busy        bool                `json:"busy,omitempty"`
	Sessions    []SessionSummary    `json:"sessions,omitempty"`
}

// List summarizes the recent projects, newest first.
func (r *Registry) List() []ProjectSummary {
	r.mu.Lock()
	recent := append([]recentEntry{}, r.recent...)
	open := map[string]*Dir{}
	for id, d := range r.open {
		open[id] = d
	}
	r.mu.Unlock()
	out := make([]ProjectSummary, 0, len(recent))
	for _, e := range recent {
		id := ProjectID(e.Path)
		s := ProjectSummary{ID: id, Name: filepath.Base(e.Path), Path: e.Path, Display: displayPath(e.Path), LastOpened: e.LastOpened}
		if st, err := os.Stat(e.Path); err == nil && st.IsDir() {
			s.Exists = true
		}
		d := open[id]
		if d != nil {
			s.Open, s.Busy = true, d.busy()
		}
		if s.Exists {
			s.fill(d)
		}
		out = append(out, s)
	}
	return out
}

func (s *ProjectSummary) fill(d *Dir) {
	cfg, err := config.Load(s.Path, "")
	if err != nil {
		return
	}
	dataDir, err := cfg.ResolveDataDir(s.Path)
	if err != nil {
		return
	}
	s.Sessions = sessionSummaries(s.ID, dataDir, d, false)
	for _, ss := range s.Sessions {
		if ss.ID == store.DefaultSession {
			s.Initialized, s.Round, s.RoundStatus, s.Agent = ss.Initialized, ss.Round, ss.RoundStatus, ss.Agent
		}
	}
}

// sessionSummaries describes the sessions stored in dataDir. d, when not
// nil, tells which sessions are open.
func sessionSummaries(id, dataDir string, d *Dir, archived bool) []SessionSummary {
	sessions, err := store.ListSessions(dataDir)
	if err != nil {
		return nil
	}
	open := map[string]*Project{}
	if d != nil {
		d.mu.Lock()
		for sid, p := range d.sessions {
			open[sid] = p
		}
		d.mu.Unlock()
	}
	out := []SessionSummary{}
	for _, ss := range sessions {
		if p, ok := open[ss.ID]; ok {
			ss = p.App.SessionInfo()
		}
		if ss.Archived && !archived {
			continue
		}
		sum := SessionSummary{ID: ss.ID, Name: ss.Name, URL: sessionPath(id, ss.ID), CreatedAt: ss.CreatedAt, Archived: ss.Archived, Branch: ss.Branch}
		if st, binding, ok := store.Peek(store.SessionDir(dataDir, ss.ID)); ok {
			sum.Initialized, sum.Round, sum.RoundStatus, sum.Agent = true, st.Round, st.RoundStatus, binding
		}
		if p, ok := open[ss.ID]; ok {
			sum.Open, sum.Busy = true, p.App.AgentBusy()
		}
		out = append(out, sum)
	}
	return out
}

// Sessions describes the sessions of p's directory, archived ones included.
func (p *Project) Sessions() []SessionSummary {
	return sessionSummaries(p.ID, p.dir.WS.DataDir, p.dir, true)
}

// DirEntry is a subdirectory in the browser.
type DirEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Display     string `json:"display"`
	HasReviewer bool   `json:"hasReviewer"`
}

// DirListing is the content of a directory in the browser. With an empty
// path it lists the roots.
type DirListing struct {
	Path    string     `json:"path,omitempty"`
	Display string     `json:"display,omitempty"`
	Parent  string     `json:"parent,omitempty"`
	Entries []DirEntry `json:"entries"`
	// HasReviewer tells whether Path itself holds review data.
	HasReviewer bool `json:"hasReviewer"`
}

func hasReviewer(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ".reviewer", "state.json"))
	return err == nil && !st.IsDir()
}

// ListDir lists the subdirectories of path (or the roots when empty).
func (r *Registry) ListDir(path string) (*DirListing, error) {
	if strings.TrimSpace(path) == "" {
		l := &DirListing{Entries: []DirEntry{}}
		for _, root := range r.roots {
			l.Entries = append(l.Entries, DirEntry{Name: displayPath(root), Path: root, Display: displayPath(root), HasReviewer: hasReviewer(root)})
		}
		return l, nil
	}
	dir, err := r.Resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrPermission) {
		return nil, err
	}
	l := &DirListing{Path: dir, Display: displayPath(dir), Entries: []DirEntry{}, HasReviewer: hasReviewer(dir)}
	if parent := filepath.Dir(dir); parent != dir && r.within(parent) {
		l.Parent = parent
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		full := filepath.Join(dir, name)
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			if real, err := filepath.EvalSymlinks(full); err == nil && r.within(real) {
				if st, err := os.Stat(real); err == nil && st.IsDir() {
					isDir = true
				}
			}
		}
		if !isDir {
			continue
		}
		l.Entries = append(l.Entries, DirEntry{Name: name, Path: full, Display: displayPath(full), HasReviewer: hasReviewer(full)})
	}
	sort.Slice(l.Entries, func(i, j int) bool {
		return strings.ToLower(l.Entries[i].Name) < strings.ToLower(l.Entries[j].Name)
	})
	return l, nil
}

// Server-level handlers.

func (s *Server) handleServer(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	writeJSON(w, map[string]any{"roots": s.Reg.displayRoots(), "home": home, "version": Version})
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"projects": s.Reg.List()})
}

func (s *Server) handleOpenProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		// GitLanguage is the commit message language chosen when the
		// review starts; it only applies to a project without git.toml.
		GitLanguage string `json:"gitLanguage"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	if req.GitLanguage != "" && !gitops.ValidLanguage(req.GitLanguage) {
		writeError(w, &app.Error{Code: http.StatusBadRequest, Msg: "コミットメッセージの言語は ja か en です"})
		return
	}
	p, err := s.Reg.Open(req.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.App.InitGitLanguage(req.GitLanguage); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"id": p.ID, "url": p.URL(), "path": p.Root})
}

func (s *Server) handleCloseProject(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, s.Reg.Close(r.PathValue("id")))
}

func (s *Server) handleForgetProject(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, s.Reg.Forget(r.PathValue("id")))
}

func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	l, err := s.Reg.ListDir(r.URL.Query().Get("path"))
	respond(w, l, err)
}

// Version is reported by /api/server; set by the command.
var Version = "dev"
