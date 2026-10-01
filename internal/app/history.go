package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/ymotongpoo/reviewer/internal/feedback"
	"github.com/ymotongpoo/reviewer/internal/textutil"
)

// Change kinds of a file between two snapshots.
const (
	ChangeModified  = "modified"
	ChangeAdded     = "added"
	ChangeDeleted   = "deleted"
	ChangeUnchanged = "unchanged" // listed because it has comments
)

// RoundChange is a file changed in response to a round.
type RoundChange struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Insert   int    `json:"insert"`
	Delete   int    `json:"delete"`
	Comments int    `json:"comments"`
}

// RoundComment is where a comment pointed when round n was submitted; line
// numbers refer to the submitted snapshot, i.e. the old side of the diff.
type RoundComment struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Path      string `json:"path,omitempty"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	Located   bool   `json:"located"`
	Carried   bool   `json:"carriedOver"`
}

// RoundChanges lists what changed in response to round n: from its
// submitted snapshot to the start of round n+1, or to the current files
// when round n+1 has not started.
type RoundChanges struct {
	Round int `json:"round"`
	// ToRound is n+1, or 0 when compared with the current files.
	ToRound  int            `json:"toRound"`
	Phase    string         `json:"phase"`
	Files    []RoundChange  `json:"files"`
	Comments []RoundComment `json:"comments"`
}

// roundComments reads the comments sent in round n from its feedback.json.
func (a *App) roundComments(n int) []RoundComment {
	out := []RoundComment{}
	b, err := os.ReadFile(filepath.Join(a.Store.RoundDir(n), "feedback.json"))
	if err != nil {
		return out
	}
	var doc feedback.Doc
	if json.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, it := range doc.Items {
		out = append(out, RoundComment{
			ID: it.ID, Scope: it.Scope, Path: it.Path, StartLine: it.StartLine, EndLine: it.EndLine,
			Located: it.Located, Carried: it.CarriedOver,
		})
	}
	return out
}

// snapshotPair returns the before and after snapshots of round n. The after
// side maps paths to blob hashes; for the current files, contents are
// stored as blobs so that diffs can be computed from blobs alone.
func (a *App) snapshotPair(n int) (from, to map[string]string, toRound int, err error) {
	m, err := a.Store.Manifest(n)
	if err != nil || m.SubmittedAt == nil {
		return nil, nil, 0, notFound("ラウンド%dはまだ提出されていません", n)
	}
	if next, err := a.Store.Manifest(n + 1); err == nil {
		return m.SubmitFiles, next.OpenFiles, n + 1, nil
	}
	if err := a.rescan(); err != nil {
		return nil, nil, 0, err
	}
	to = make(map[string]string, len(a.files))
	for _, f := range a.files {
		if m.SubmitFiles[f.Path] == f.Hash {
			to[f.Path] = f.Hash
			continue
		}
		b, err := a.Proj.Read(f.Path)
		if err != nil {
			continue
		}
		h, err := a.Store.PutBlob(b)
		if err != nil {
			return nil, nil, 0, err
		}
		to[f.Path] = h
	}
	return m.SubmitFiles, to, 0, nil
}

func (a *App) blobLines(h string) []string {
	if h == "" {
		return nil
	}
	b, err := a.Store.Blob(h)
	if err != nil {
		return nil
	}
	return textutil.SplitLines(string(b))
}

// RoundChangesMode lists changes for either the review or agent phase.
func (a *App) RoundChangesMode(n int, phase string) (*RoundChanges, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if phase == "" {
		phase = "agent"
	}
	var from, to map[string]string
	var toRound int
	var err error
	if phase == "review" {
		from, to, err = a.reviewSnapshotPair(n)
		toRound = n
	} else if phase == "agent" {
		from, to, toRound, err = a.snapshotPair(n)
	} else {
		return nil, badRequest("不明な差分種別です: %s", phase)
	}
	if err != nil {
		return nil, err
	}
	rc := &RoundChanges{Round: n, ToRound: toRound, Phase: phase, Files: []RoundChange{}, Comments: a.roundComments(n)}
	commented := map[string]int{}
	for _, c := range rc.Comments {
		if c.Path != "" {
			commented[c.Path]++
		}
	}
	paths := map[string]bool{}
	for p := range from {
		paths[p] = true
	}
	for p := range to {
		paths[p] = true
	}
	for p := range paths {
		hf, ht := from[p], to[p]
		if hf == ht {
			if commented[p] > 0 {
				rc.Files = append(rc.Files, RoundChange{Path: p, Kind: ChangeUnchanged, Comments: commented[p]})
			}
			continue
		}
		c := RoundChange{Path: p, Kind: ChangeModified, Comments: commented[p]}
		switch {
		case hf == "":
			c.Kind = ChangeAdded
		case ht == "":
			c.Kind = ChangeDeleted
		}
		for _, op := range textutil.LineDiff(a.blobLines(hf), a.blobLines(ht)) {
			switch op.Kind {
			case textutil.OpInsert:
				c.Insert += len(op.Lines)
			case textutil.OpDelete:
				c.Delete += len(op.Lines)
			}
		}
		rc.Files = append(rc.Files, c)
	}
	sort.Slice(rc.Files, func(i, j int) bool { return rc.Files[i].Path < rc.Files[j].Path })
	return rc, nil
}

// RoundChanges returns the agent's changes after round n.
func (a *App) RoundChanges(n int) (*RoundChanges, error) {
	return a.RoundChangesMode(n, "agent")
}

// RoundDiff is the diff of one file in response to a round.
type RoundDiff struct {
	Round   int           `json:"round"`
	ToRound int           `json:"toRound"`
	Path    string        `json:"path"`
	Kind    string        `json:"kind"`
	Phase   string        `json:"phase"`
	Ops     []textutil.Op `json:"ops"`
}

// reviewSnapshotPair returns the files at the start and submission of round n.
func (a *App) reviewSnapshotPair(n int) (from, to map[string]string, err error) {
	m, err := a.Store.Manifest(n)
	if err != nil || m.SubmittedAt == nil {
		return nil, nil, notFound("ラウンド%dはまだ提出されていません", n)
	}
	return m.OpenFiles, m.SubmitFiles, nil
}

// RoundDiffMode returns the diff for either the review phase (start -> submit)
// or the agent phase (submit -> next round/current files).
func (a *App) RoundDiffMode(n int, path, phase string) (*RoundDiff, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if phase == "" {
		phase = "agent"
	}
	var from, to map[string]string
	var toRound int
	var err error
	if phase == "review" {
		from, to, err = a.reviewSnapshotPair(n)
		toRound = n
	} else if phase == "agent" {
		from, to, toRound, err = a.snapshotPair(n)
	} else {
		return nil, badRequest("不明な差分種別です: %s", phase)
	}
	if err != nil {
		return nil, err
	}
	hf, okf := from[path]
	ht, okt := to[path]
	if !okf && !okt {
		return nil, notFound("ラウンド%dの前後にファイルがありません: %s", n, path)
	}
	d := &RoundDiff{Round: n, ToRound: toRound, Path: path, Phase: phase, Kind: ChangeModified}
	switch {
	case hf == ht:
		d.Kind = ChangeUnchanged
	case !okf:
		d.Kind = ChangeAdded
	case !okt:
		d.Kind = ChangeDeleted
	}
	d.Ops = textutil.LineDiff(a.blobLines(hf), a.blobLines(ht))
	return d, nil
}

// RoundDiff returns the agent's changes after round n.
func (a *App) RoundDiff(n int, path string) (*RoundDiff, error) {
	return a.RoundDiffMode(n, path, "agent")
}
