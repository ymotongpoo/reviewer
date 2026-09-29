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
	"github.com/ymotongpoo/reviewer/internal/store"
)

// maxRecent bounds the recent project list.
const maxRecent = 50

// Project is an open review directory.
type Project struct {
	ID     string
	Root   string
	App    *app.App
	Hub    *Hub
	cancel context.CancelFunc

	muxOnce sync.Once
	mux     http.Handler
}

// AgentSetup connects a newly opened project to the shared agent. url is
// the project's page, used in notifications.
type AgentSetup func(a *app.App, url string)

type recentEntry struct {
	Path       string    `json:"path"`
	LastOpened time.Time `json:"lastOpened"`
}

// Registry holds the open projects and the list of recent ones.
type Registry struct {
	mu       sync.Mutex
	roots    []string // absolute, symlinks resolved
	stateDir string
	baseURL  string
	setup    AgentSetup
	open     map[string]*Project
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
	r := &Registry{stateDir: stateDir, baseURL: strings.TrimRight(baseURL, "/"), setup: setup, open: map[string]*Project{}}
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

// Open opens (or returns the already open) project at path.
func (r *Registry) Open(path string) (*Project, error) {
	root, err := r.Resolve(path)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.openLocked(root)
}

func (r *Registry) openLocked(root string) (*Project, error) {
	id := ProjectID(root)
	if p, ok := r.open[id]; ok {
		r.touchLocked(root)
		return p, nil
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		return nil, &app.Error{Code: http.StatusBadRequest, Msg: err.Error()}
	}
	hub := NewHub()
	a, err := app.New(root, cfg, hub.Publish)
	if err != nil {
		return nil, err
	}
	if err := a.Init(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := a.Watch(ctx); err != nil {
			log.Printf("watch %s: %v", root, err)
		}
	}()
	if r.setup != nil {
		r.setup(a, r.ProjectURL(id))
	}
	p := &Project{ID: id, Root: root, App: a, Hub: hub, cancel: cancel}
	r.open[id] = p
	r.touchLocked(root)
	log.Printf("opened %s (%s)", root, id)
	return p, nil
}

// ProjectURL returns the page of a project (without the token).
func (r *Registry) ProjectURL(id string) string {
	if r.baseURL == "" {
		return ""
	}
	return r.baseURL + "/p/" + id + "/"
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

// Get returns the project with id, reopening a recent project that is not
// loaded (e.g. after a restart of the server).
func (r *Registry) Get(id string) (*Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.open[id]; ok {
		return p, nil
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

// Close unloads a project. It refuses while the agent is working on it.
func (r *Registry) Close(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.open[id]
	if !ok {
		return nil
	}
	if p.App.AgentBusy() {
		return &app.Error{Code: http.StatusConflict, Msg: "エージェントの対応中は閉じられません"}
	}
	p.cancel()
	delete(r.open, id)
	log.Printf("closed %s", p.Root)
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
	for id, p := range r.open {
		p.cancel()
		delete(r.open, id)
	}
}

// ProjectSummary describes a recent project for the picker.
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
}

// List summarizes the recent projects, newest first.
func (r *Registry) List() []ProjectSummary {
	r.mu.Lock()
	recent := append([]recentEntry{}, r.recent...)
	open := map[string]*Project{}
	for id, p := range r.open {
		open[id] = p
	}
	r.mu.Unlock()
	out := make([]ProjectSummary, 0, len(recent))
	for _, e := range recent {
		id := ProjectID(e.Path)
		s := ProjectSummary{ID: id, Name: filepath.Base(e.Path), Path: e.Path, Display: displayPath(e.Path), LastOpened: e.LastOpened}
		if st, err := os.Stat(e.Path); err == nil && st.IsDir() {
			s.Exists = true
		}
		if p, ok := open[id]; ok {
			s.Open, s.Busy = true, p.App.AgentBusy()
		}
		if s.Exists {
			s.fill()
		}
		out = append(out, s)
	}
	return out
}

func (s *ProjectSummary) fill() {
	cfg, err := config.Load(s.Path, "")
	if err != nil {
		return
	}
	dataDir, err := cfg.ResolveDataDir(s.Path)
	if err != nil {
		return
	}
	st, binding, ok := store.Peek(dataDir)
	if !ok {
		return
	}
	s.Initialized, s.Round, s.RoundStatus, s.Agent = true, st.Round, st.RoundStatus, binding
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
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	p, err := s.Reg.Open(req.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"id": p.ID, "url": "/p/" + p.ID + "/", "path": p.Root})
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
