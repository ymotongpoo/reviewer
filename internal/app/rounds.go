package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/ymotongpoo/reviewer/internal/anchor"
	"github.com/ymotongpoo/reviewer/internal/feedback"
	"github.com/ymotongpoo/reviewer/internal/project"
	"github.com/ymotongpoo/reviewer/internal/store"
	"github.com/ymotongpoo/reviewer/internal/textutil"
)

// SubmitResult describes a submitted round.
type SubmitResult struct {
	RoundPaths
	Count int `json:"count"`
}

// Submit finalizes the current round and writes feedback.md/json.
func (a *App) Submit() (*SubmitResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.Store.State
	if st.RoundStatus != store.RoundOpen {
		return nil, conflict("ラウンド%dは提出済みです", st.Round)
	}
	if err := a.rescan(); err != nil {
		return nil, err
	}
	a.reanchor(nil)
	files, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	doc, err := a.buildDoc(st.Round, files)
	if err != nil {
		return nil, err
	}
	if len(doc.Items) == 0 {
		return nil, badRequest("提出するコメントがありません")
	}
	now := a.now()
	doc.SubmittedAt = now

	js, err := doc.JSON()
	if err != nil {
		return nil, err
	}
	if _, err := a.Store.WriteRoundFile(st.Round, "feedback.json", js); err != nil {
		return nil, err
	}
	if _, err := a.Store.WriteRoundFile(st.Round, "feedback.md", doc.Markdown()); err != nil {
		return nil, err
	}

	for _, c := range a.Store.Comments() {
		changed := false
		if c.Status == store.StatusDraft {
			c.Status = store.StatusOpen
			changed = true
		}
		for i := range c.Replies {
			if c.Replies[i].Draft {
				c.Replies[i].Draft = false
				changed = true
				// A new remark from the reviewer reopens the discussion.
				if c.Status != store.StatusResolved {
					c.Status = store.StatusOpen
				}
			}
		}
		if changed {
			c.UpdatedAt = now
			if err := a.Store.PutComment(c); err != nil {
				return nil, err
			}
		}
	}

	m, err := a.Store.Manifest(st.Round)
	if err != nil {
		return nil, err
	}
	m.SubmittedAt = &now
	m.SubmitFiles = files
	m.FeedbackIDs = doc.IDs()
	if err := a.Store.SaveManifest(m); err != nil {
		return nil, err
	}
	a.Store.State.RoundStatus = store.RoundSubmitted
	if err := a.Store.SaveState(); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "round", Round: st.Round})
	return &SubmitResult{RoundPaths: a.roundPaths(st.Round), Count: len(doc.Items)}, nil
}

// buildDoc assembles the feedback of round n. files maps paths to the blobs
// of the snapshot the line numbers refer to.
func (a *App) buildDoc(n int, files map[string]string) (*feedback.Doc, error) {
	doc := &feedback.Doc{
		Version: feedback.Version, Round: n, Project: a.Proj.Root,
		ResponsePath: filepath.Join(a.Store.RoundDir(n), "response.json"),
	}
	cache := map[string][]string{}
	linesOf := func(path string) []string {
		if l, ok := cache[path]; ok {
			return l
		}
		var l []string
		if h, ok := files[path]; ok {
			if b, err := a.Store.Blob(h); err == nil {
				l = textutil.SplitLines(string(b))
			}
		}
		cache[path] = l
		return l
	}
	for _, c := range a.Store.Comments() {
		isNew := c.Round == n && c.Status == store.StatusDraft
		carried := c.Round < n && c.Status != store.StatusResolved && c.Status != store.StatusDraft
		if !isNew && !carried {
			continue
		}
		it := feedback.Item{
			ID: c.ID, Scope: c.Scope, Path: c.Path, Label: c.Label, Body: c.Body,
			Round: c.Round, Status: c.Status, CarriedOver: carried, Located: true,
			Suggestions: feedback.Suggestions(c.Body),
		}
		if c.Scope == store.ScopeLine && c.Loc != nil {
			it.StartLine, it.EndLine = c.Loc.Start, c.Loc.End
			lines := linesOf(c.Path)
			if c.Loc.State == anchor.Outdated || lines == nil || c.Loc.End > len(lines) {
				it.Located = false
				it.Quote = c.Loc.Anchor.Lines
			} else {
				it.Quote = lines[c.Loc.Start-1 : c.Loc.End]
			}
		}
		for _, r := range c.Replies {
			if r.Draft {
				if r.Round == n {
					it.NewReplies = append(it.NewReplies, r.Body)
				}
				continue
			}
			it.Thread = append(it.Thread, feedback.ThreadEntry{Author: r.Author, Round: r.Round, Status: r.Status, Body: r.Body})
		}
		doc.Items = append(doc.Items, it)
	}
	return doc, nil
}

// Export returns the feedback of round n (0 = latest submitted, falling back
// to a preview of the open round) as "md" or "json".
func (a *App) Export(n int, format string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n == 0 {
		n = a.latestSubmitted()
		if n < 1 {
			n = a.Store.State.Round
		}
	}
	name := "feedback.md"
	if format == "json" {
		name = "feedback.json"
	}
	if b, err := os.ReadFile(filepath.Join(a.Store.RoundDir(n), name)); err == nil {
		return b, nil
	}
	if n != a.Store.State.Round || a.Store.State.RoundStatus != store.RoundOpen {
		return nil, notFound("ラウンド%dのフィードバックがありません", n)
	}
	// Preview of the open round against the current files.
	if err := a.rescan(); err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, f := range a.files {
		files[f.Path] = f.Hash
		if b, err := a.Proj.Read(f.Path); err == nil {
			a.Store.PutBlob(b)
		}
	}
	doc, err := a.buildDoc(n, files)
	if err != nil {
		return nil, err
	}
	doc.SubmittedAt = a.now()
	if format == "json" {
		return doc.JSON()
	}
	return doc.Markdown(), nil
}

// ImportResponses reads response.json files of submitted rounds.
func (a *App) ImportResponses() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.importResponses()
}

func (a *App) importResponses() (bool, error) {
	imported := false
	for n := 1; n <= a.Store.State.Round; n++ {
		m, err := a.Store.Manifest(n)
		if err != nil || m.SubmittedAt == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.Store.RoundDir(n), "response.json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return imported, err
		}
		h := project.HashBytes(b)
		if m.Response != nil && m.Response.Hash == h {
			continue
		}
		known := map[string]bool{}
		for _, id := range m.FeedbackIDs {
			known[id] = true
		}
		info := &store.ResponseInfo{Hash: h, ImportedAt: a.now()}
		resp, warnings, err := feedback.ParseResponse(b, n, known)
		if err != nil {
			info.Error = err.Error()
		} else {
			info.Summary = resp.Summary
			info.Warnings = warnings
			info.Count = len(resp.Responses)
			if err := a.applyResponse(n, resp); err != nil {
				return imported, err
			}
		}
		m.Response = info
		if err := a.Store.SaveManifest(m); err != nil {
			return imported, err
		}
		imported = true
		a.notify(Event{Type: "response", Round: n})
	}
	return imported, nil
}

func (a *App) applyResponse(n int, resp *feedback.Response) error {
	for _, e := range resp.Responses {
		c, ok := a.Store.Comment(e.ID)
		if !ok {
			continue
		}
		rid := fmt.Sprintf("A-%d-%s", n, c.ID)
		now := a.now()
		r := store.Reply{ID: rid, Author: store.AuthorAgent, Body: e.Message, Status: e.Status, Round: n, CreatedAt: now, UpdatedAt: now}
		if i := slices.IndexFunc(c.Replies, func(x store.Reply) bool { return x.ID == rid }); i >= 0 {
			r.CreatedAt = c.Replies[i].CreatedAt
			c.Replies[i] = r
		} else {
			// Keep agent replies before the reviewer's replies of later rounds.
			pos := len(c.Replies)
			for i, x := range c.Replies {
				if x.Round > n {
					pos = i
					break
				}
			}
			c.Replies = slices.Insert(c.Replies, pos, r)
		}
		if c.Status != store.StatusResolved && !hasLaterHumanReply(c, n) {
			c.Status = e.Status
		}
		c.UpdatedAt = now
		if err := a.Store.PutComment(c); err != nil {
			return err
		}
	}
	return nil
}

func hasLaterHumanReply(c *store.Comment, n int) bool {
	for _, r := range c.Replies {
		if r.Author == store.AuthorHuman && r.Round > n {
			return true
		}
	}
	return false
}

// HandleChanges reacts to file system changes: it rescans the tree,
// re-anchors comments on changed files and imports agent responses.
func (a *App) HandleChanges(paths []string, dataChanged bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	before := map[string]string{}
	for _, f := range a.files {
		before[f.Path] = f.Hash
	}
	if err := a.rescan(); err != nil {
		return
	}
	var changed []string
	treeChanged := len(before) != len(a.files)
	for _, f := range a.files {
		h, ok := before[f.Path]
		if !ok {
			treeChanged = true
		}
		if h != f.Hash {
			changed = append(changed, f.Path)
		}
		delete(before, f.Path)
	}
	for p := range before {
		changed = append(changed, p)
	}
	if len(changed) > 0 {
		a.reanchor(changed)
		a.notify(Event{Type: "files", Paths: changed})
	}
	if treeChanged {
		a.notify(Event{Type: "tree"})
	}
	if dataChanged {
		a.importResponses()
	}
}

// reanchor updates the location of unresolved line comments on paths
// (all paths when nil).
func (a *App) reanchor(paths []string) {
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	contents := map[string][]byte{}
	var changedPaths []string
	for _, c := range a.Store.Comments() {
		if c.Scope != store.ScopeLine || c.Loc == nil || c.Status == store.StatusResolved {
			continue
		}
		if paths != nil && !want[c.Path] {
			continue
		}
		b, ok := contents[c.Path]
		if !ok {
			var err error
			b, err = a.Proj.Read(c.Path)
			if err != nil {
				b = nil
			}
			contents[c.Path] = b
		}
		prev := *c.Loc
		if b == nil {
			c.Loc.State = anchor.Outdated
		} else {
			h, err := a.Store.PutBlob(b)
			if err != nil || h == c.Loc.Blob {
				continue
			}
			a.reanchorComment(c, textutil.SplitLines(string(b)), h)
		}
		if prev.State != c.Loc.State || prev.Start != c.Loc.Start || prev.End != c.Loc.End || prev.Blob != c.Loc.Blob {
			a.Store.PutComment(c)
			changedPaths = append(changedPaths, c.Path)
		}
	}
	if len(changedPaths) > 0 {
		a.notify(Event{Type: "comments", Paths: changedPaths})
	}
}

func (a *App) reanchorComment(c *store.Comment, newLines []string, h string) {
	var oldLines []string
	if b, err := a.Store.Blob(c.Loc.Blob); err == nil {
		oldLines = textutil.SplitLines(string(b))
	}
	res := anchor.Resolve(c.Loc.Anchor, oldLines, c.Loc.Start, c.Loc.End, newLines, a.threshold())
	if res.State == anchor.Outdated && c.Anchor != nil {
		var orig []string
		if b, err := a.Store.Blob(c.OrigBlob); err == nil {
			orig = textutil.SplitLines(string(b))
		}
		res = anchor.Resolve(*c.Anchor, orig, c.OrigStart, c.OrigEnd, newLines, a.threshold())
	}
	if res.State == anchor.Outdated {
		c.Loc.State = anchor.Outdated
		return
	}
	loc := anchor.New(newLines, res.Start, res.End)
	state := anchor.Exact
	switch {
	case c.Anchor != nil && loc.Hash != c.Anchor.Hash:
		state = anchor.Fuzzy
	case res.Start != c.OrigStart:
		state = anchor.Moved
	}
	c.Loc = &store.Location{Start: res.Start, End: res.End, State: state, Blob: h, Anchor: loc}
}

// Status is a short summary for the CLI.
type Status struct {
	Round       int            `json:"round"`
	RoundStatus string         `json:"roundStatus"`
	ByStatus    map[string]int `json:"byStatus"`
	Unresolved  int            `json:"unresolved"`
	Latest      *RoundPaths    `json:"latest,omitempty"`
}

// StatusSummary counts comments per status.
func (a *App) StatusSummary() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Status{Round: a.Store.State.Round, RoundStatus: a.Store.State.RoundStatus, ByStatus: map[string]int{}}
	for _, c := range a.Store.Comments() {
		s.ByStatus[c.Status]++
		if c.Status != store.StatusResolved {
			s.Unresolved++
		}
	}
	if n := a.latestSubmitted(); n >= 1 {
		rp := a.roundPaths(n)
		s.Latest = &rp
	}
	return s
}
