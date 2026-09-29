package app

import (
	"sort"

	"github.com/ymotongpoo/reviewer/internal/textutil"
)

// Change kinds of a file between two snapshots.
const (
	ChangeModified = "modified"
	ChangeAdded    = "added"
	ChangeDeleted  = "deleted"
)

// RoundChange is a file changed in response to a round.
type RoundChange struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Insert int    `json:"insert"`
	Delete int    `json:"delete"`
}

// RoundChanges lists what changed in response to round n: from its
// submitted snapshot to the start of round n+1, or to the current files
// when round n+1 has not started.
type RoundChanges struct {
	Round int `json:"round"`
	// ToRound is n+1, or 0 when compared with the current files.
	ToRound int           `json:"toRound"`
	Files   []RoundChange `json:"files"`
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

// RoundChanges returns the files changed in response to round n.
func (a *App) RoundChanges(n int) (*RoundChanges, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	from, to, toRound, err := a.snapshotPair(n)
	if err != nil {
		return nil, err
	}
	rc := &RoundChanges{Round: n, ToRound: toRound, Files: []RoundChange{}}
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
			continue
		}
		c := RoundChange{Path: p, Kind: ChangeModified}
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

// RoundDiff is the diff of one file in response to a round.
type RoundDiff struct {
	Round   int           `json:"round"`
	ToRound int           `json:"toRound"`
	Path    string        `json:"path"`
	Kind    string        `json:"kind"`
	Ops     []textutil.Op `json:"ops"`
}

// RoundDiff returns how path changed in response to round n.
func (a *App) RoundDiff(n int, path string) (*RoundDiff, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	from, to, toRound, err := a.snapshotPair(n)
	if err != nil {
		return nil, err
	}
	hf, okf := from[path]
	ht, okt := to[path]
	if !okf && !okt {
		return nil, notFound("ラウンド%dの前後にファイルがありません: %s", n, path)
	}
	d := &RoundDiff{Round: n, ToRound: toRound, Path: path, Kind: ChangeModified}
	switch {
	case !okf:
		d.Kind = ChangeAdded
	case !okt:
		d.Kind = ChangeDeleted
	}
	d.Ops = textutil.LineDiff(a.blobLines(hf), a.blobLines(ht))
	return d, nil
}
