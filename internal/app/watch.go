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
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	watched := map[string]bool{}
	add := func(dir string) {
		if watched[dir] {
			return
		}
		if err := w.Add(dir); err != nil {
			log.Printf("watch %s: %v", dir, err)
			return
		}
		watched[dir] = true
	}
	sync := func() {
		for _, d := range a.Proj.Dirs() {
			add(d)
		}
		rounds := filepath.Join(a.DataDir, "rounds")
		add(rounds)
		if entries, err := os.ReadDir(rounds); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					add(filepath.Join(rounds, e.Name()))
				}
			}
		}
		requests := filepath.Join(a.DataDir, "requests")
		add(requests)
		if entries, err := os.ReadDir(requests); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					add(filepath.Join(requests, e.Name()))
				}
			}
		}
	}
	sync()

	var timer *time.Timer
	fire := make(chan struct{}, 1)
	pending := map[string]bool{}
	dataChanged := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("watch: %v", err)
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if a.inData(ev.Name) {
				if strings.HasSuffix(ev.Name, "response.json") || strings.HasSuffix(ev.Name, "annotations.json") || ev.Has(fsnotify.Create) {
					dataChanged = true
				} else {
					continue
				}
			} else if rel, ok := a.Proj.Rel(ev.Name); ok {
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
			dataChanged = false
			a.HandleChanges(paths, dc)
			sync()
		}
	}
}

func (a *App) inData(p string) bool {
	return p == a.DataDir || strings.HasPrefix(p, a.DataDir+string(filepath.Separator))
}
