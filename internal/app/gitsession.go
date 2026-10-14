package app

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ymotongpoo/reviewer/internal/gitops"
)

// Git offers to commit only what the session changed. A directory may hold
// changes of other sessions, other tools and older work, which are left
// alone.

// mtimeSlack allows for file systems that keep modification times in whole
// seconds.
const mtimeSlack = 2 * time.Second

// span is a time when the session's agent worked on a round.
type span struct {
	from, to time.Time
	// open spans last until now.
	open bool
}

func (s span) contains(t time.Time) bool {
	return !t.Before(s.from.Add(-mtimeSlack)) && !t.After(s.to)
}

// agentSpans returns when the session's agent worked on its rounds: each
// run, and the time from a submission until the response was read in, the
// next round opened or, with runs, the last of them ended.
func (a *App) agentSpans(now time.Time) []span {
	var out []span
	for n := 1; n <= a.Store.State.Round; n++ {
		m, err := a.Store.Manifest(n)
		if err != nil || m.SubmittedAt == nil {
			continue
		}
		runs := a.Store.AgentRuns(n)
		running := false
		var lastEnd time.Time
		for _, r := range runs {
			if r.EndedAt == nil {
				running = true
				out = append(out, span{from: r.StartedAt, to: now, open: true})
				continue
			}
			out = append(out, span{from: r.StartedAt, to: *r.EndedAt})
			if r.EndedAt.After(lastEnd) {
				lastEnd = *r.EndedAt
			}
		}
		s := span{from: *m.SubmittedAt, to: now, open: true}
		if m.Response != nil && !m.Response.ImportedAt.Before(*m.SubmittedAt) {
			s.to, s.open = m.Response.ImportedAt, false
		} else if next, err := a.Store.Manifest(n + 1); err == nil {
			s.to, s.open = next.OpenedAt, false
		} else if len(runs) > 0 && !running {
			s.to, s.open = lastEnd, false
		}
		out = append(out, s)
	}
	return out
}

// agentWorking reports whether the session's agent is working on a round.
func (a *App) agentWorking() bool {
	return slices.ContainsFunc(a.agentSpans(a.now()), func(s span) bool { return s.open })
}

// touch records paths, relative to the project, as changed by the session.
func (a *App) touch(paths []string) {
	var files []string
	for _, p := range paths {
		if fi, err := os.Lstat(filepath.Join(a.Proj.Root, filepath.FromSlash(p))); err == nil && fi.IsDir() {
			continue
		}
		files = append(files, p)
	}
	_ = a.Store.Touch(files...)
}

// sessionStart returns when the session started and the blobs of its files
// then.
func (a *App) sessionStart() (time.Time, map[string]string) {
	if m, err := a.Store.Manifest(1); err == nil {
		return m.OpenedAt, m.OpenFiles
	}
	return a.Session.CreatedAt, nil
}

// sessionChanges keeps the changes the session made: the file differs from
// when the session started, and the session commented on it, saved it from
// its editor, or its agent changed it while working on a round.
func (a *App) sessionChanges(changes []gitops.Change) []gitops.Change {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A failed scan leaves the last one, which is still a fair guess.
	_ = a.rescan()
	start, base := a.sessionStart()
	spans := a.agentSpans(a.now())
	touched, _ := a.Store.Touched()
	commented := map[string]bool{}
	for _, c := range a.Store.Comments() {
		if c.Path != "" {
			commented[c.Path] = true
		}
	}
	current := make(map[string]string, len(a.files))
	for _, f := range a.files {
		current[f.Path] = f.Hash
	}
	made := func(p string) bool {
		fi, err := os.Lstat(filepath.Join(a.Proj.Root, filepath.FromSlash(p)))
		exists := err == nil
		// Text files are compared with the start; others, which were not
		// kept then, by when they changed.
		if h, ok := base[p]; ok {
			if current[p] == h {
				return false
			}
		} else if exists {
			if fi.ModTime().Before(start.Add(-mtimeSlack)) {
				return false
			}
		} else if !touched[p] {
			return false
		}
		if commented[p] || touched[p] {
			return true
		}
		return exists && slices.ContainsFunc(spans, func(s span) bool { return s.contains(fi.ModTime()) })
	}
	out := []gitops.Change{}
	for _, c := range changes {
		if made(c.Path) || (c.From != "" && made(c.From)) {
			out = append(out, c)
		}
	}
	return out
}

// otherStaged lists the staged paths, relative to the repository, that a
// commit of changes leaves out.
func otherStaged(repo *gitops.Repo, st *gitops.State, changes []gitops.Change) []string {
	in := map[string]bool{}
	for _, c := range changes {
		in[repo.RepoPath(c.Path)] = true
		if c.From != "" {
			in[repo.RepoPath(c.From)] = true
		}
	}
	out := []string{}
	for _, p := range st.Staged {
		if !in[p] {
			out = append(out, p)
		}
	}
	return out
}
