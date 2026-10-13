package app

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounce = 200 * time.Millisecond

// Watch watches the project and the data directory until ctx is done,
// calling HandleChanges for batches of changes.
func (a *App) Watch(ctx context.Context) error {
	return a.ws.Watch(ctx, func() []*App { return []*App{a} })
}

// Watch watches the directory and the data of the sessions returned by
// sessions until ctx is done. Changes to the files are handed to every
// session; a change to a session's own data (an agent's response.json or
// annotations.json) only to that session.
func (w *Workspace) Watch(ctx context.Context, sessions func() []*App) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	watched := map[string]bool{}
	add := func(dir string) {
		if watched[dir] {
			return
		}
		if err := fw.Add(dir); err != nil {
			log.Printf("watch %s: %v", dir, err)
			return
		}
		watched[dir] = true
	}
	addTree := func(dir string) {
		add(dir)
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					add(filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	sync := func() {
		for _, d := range w.Proj.Dirs() {
			add(d)
		}
		for _, a := range sessions() {
			addTree(filepath.Join(a.DataDir, "rounds"))
			addTree(filepath.Join(a.DataDir, "requests"))
		}
	}
	sync()

	var timer *time.Timer
	fire := make(chan struct{}, 1)
	pending := map[string]bool{}
	dataChanged := map[*App]bool{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.resync:
			sync()
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			log.Printf("watch: %v", err)
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if w.inData(ev.Name) {
				if !strings.HasSuffix(ev.Name, "response.json") && !strings.HasSuffix(ev.Name, "annotations.json") && !ev.Has(fsnotify.Create) {
					continue
				}
				if a := owner(sessions(), ev.Name); a != nil {
					dataChanged[a] = true
				}
			} else if rel, ok := w.Proj.Rel(ev.Name); ok {
				pending[rel] = true
			}
			if timer == nil {
				timer = time.AfterFunc(debounce, func() { fire <- struct{}{} })
			} else {
				timer.Reset(debounce)
			}
		case <-fire:
			timer = nil
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			pending = map[string]bool{}
			dc := dataChanged
			dataChanged = map[*App]bool{}
			for _, a := range sessions() {
				if len(paths) > 0 || dc[a] {
					a.HandleChanges(paths, dc[a])
				}
			}
			sync()
		}
	}
}

func (w *Workspace) inData(p string) bool { return within(w.DataDir, p) }

// owner returns the session whose data directory holds p. The default
// session's directory contains the others, so the deepest one wins.
func owner(sessions []*App, p string) *App {
	var best *App
	for _, a := range sessions {
		if within(a.DataDir, p) && (best == nil || len(a.DataDir) > len(best.DataDir)) {
			best = a
		}
	}
	return best
}

func within(dir, p string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}
