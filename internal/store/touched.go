package store

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
)

// The files a session changed: saved from its editor, or changed while its
// agent worked on a round. Git offers to commit them.

func (s *Store) touchedPath() string { return filepath.Join(s.Dir, "touched.json") }

type touchedDoc struct {
	Paths []string `json:"paths"`
}

func (s *Store) loadTouched() error {
	if s.touched != nil {
		return nil
	}
	var doc touchedDoc
	if err := readJSON(s.touchedPath(), &doc); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	s.touched = map[string]bool{}
	for _, p := range doc.Paths {
		s.touched[p] = true
	}
	return nil
}

// Touched reports the files recorded by Touch, relative to the project.
func (s *Store) Touched() (map[string]bool, error) {
	if err := s.loadTouched(); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(s.touched))
	for p := range s.touched {
		out[p] = true
	}
	return out, nil
}

// Touch records that the session changed paths.
func (s *Store) Touch(paths ...string) error {
	if err := s.loadTouched(); err != nil {
		return err
	}
	added := false
	for _, p := range paths {
		if p != "" && !s.touched[p] {
			s.touched[p] = true
			added = true
		}
	}
	if !added {
		return nil
	}
	doc := touchedDoc{Paths: make([]string, 0, len(s.touched))}
	for p := range s.touched {
		doc.Paths = append(doc.Paths, p)
	}
	sort.Strings(doc.Paths)
	return writeJSON(s.touchedPath(), doc)
}
