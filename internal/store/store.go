// Package store persists review data as plain files so that both humans and
// agents can read them directly.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ymotongpoo/reviewer/internal/project"
)

const stateVersion = 1

// Store is the on-disk review database rooted at a data directory.
type Store struct {
	Dir                string
	State              State
	comments           map[string]*Comment
	logLines           int
	annotations        map[string]*Annotation
	annotationLogLines int
}

// ErrNotInitialized is returned by OpenExisting for a missing data dir.
var ErrNotInitialized = errors.New("no review data")

// Open opens or creates the store at dir.
func Open(dir string) (*Store, error) {
	for _, d := range []string{dir, filepath.Join(dir, "blobs"), filepath.Join(dir, "rounds"), filepath.Join(dir, "requests")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return load(dir)
}

// OpenExisting opens the store without creating it.
func OpenExisting(dir string) (*Store, error) {
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotInitialized
		}
		return nil, err
	}
	return load(dir)
}

func load(dir string) (*Store, error) {
	s := &Store{Dir: dir, comments: map[string]*Comment{}, annotations: map[string]*Annotation{}}
	if err := readJSON(filepath.Join(dir, "state.json"), &s.State); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if s.State.Version == 0 {
		s.State = State{Version: stateVersion, NextComment: 1, NextReply: 1, NextRequest: 1, NextAnnotation: 1}
	}
	if s.State.NextRequest == 0 {
		s.State.NextRequest = 1
	}
	if s.State.NextAnnotation == 0 {
		s.State.NextAnnotation = 1
	}
	if err := s.loadComments(); err != nil {
		return nil, err
	}
	if err := s.loadAnnotations(); err != nil {
		return nil, err
	}
	if s.logLines > 4*len(s.comments)+100 {
		if err := s.compact(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

type logRecord struct {
	Op      string   `json:"op"`
	ID      string   `json:"id,omitempty"`
	Comment *Comment `json:"comment,omitempty"`
}

func (s *Store) logPath() string { return filepath.Join(s.Dir, "comments.jsonl") }

func (s *Store) loadComments() error {
	f, err := os.Open(s.logPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		s.logLines++
		var r logRecord
		if err := json.Unmarshal(line, &r); err != nil {
			// A torn final write must not make the whole log unreadable.
			continue
		}
		switch r.Op {
		case "put":
			if r.Comment != nil {
				s.comments[r.Comment.ID] = r.Comment
			}
		case "del":
			delete(s.comments, r.ID)
		}
	}
	return sc.Err()
}

func (s *Store) compact() error {
	var buf bytes.Buffer
	for _, c := range s.Comments() {
		b, err := json.Marshal(logRecord{Op: "put", Comment: c})
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if err := writeFileAtomic(s.logPath(), buf.Bytes()); err != nil {
		return err
	}
	s.logLines = len(s.comments)
	return nil
}

func (s *Store) appendLog(r logRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	s.logLines++
	return nil
}

// SaveState writes state.json.
func (s *Store) SaveState() error {
	return writeJSON(filepath.Join(s.Dir, "state.json"), s.State)
}

// NewCommentID allocates a comment ID.
func (s *Store) NewCommentID() string {
	id := "C-" + strconv.Itoa(s.State.NextComment)
	s.State.NextComment++
	return id
}

// NewReplyID allocates a reply ID.
func (s *Store) NewReplyID() string {
	id := "R-" + strconv.Itoa(s.State.NextReply)
	s.State.NextReply++
	return id
}

// Comment returns a comment by ID.
func (s *Store) Comment(id string) (*Comment, bool) {
	c, ok := s.comments[id]
	return c, ok
}

// Comments returns all comments ordered by numeric ID.
func (s *Store) Comments() []*Comment {
	out := make([]*Comment, 0, len(s.comments))
	for _, c := range s.comments {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return idNum(out[i].ID) < idNum(out[j].ID) })
	return out
}

func idNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "C-"))
	return n
}

// PutComment stores c. The state is saved as well since IDs may have been
// allocated.
func (s *Store) PutComment(c *Comment) error {
	s.comments[c.ID] = c
	if err := s.appendLog(logRecord{Op: "put", Comment: c}); err != nil {
		return err
	}
	return s.SaveState()
}

// DeleteComment removes a comment.
func (s *Store) DeleteComment(id string) error {
	delete(s.comments, id)
	return s.appendLog(logRecord{Op: "del", ID: id})
}

// PutBlob stores content addressed by its hash.
func (s *Store) PutBlob(b []byte) (string, error) {
	h := project.HashBytes(b)
	p := filepath.Join(s.Dir, "blobs", h)
	if _, err := os.Stat(p); err == nil {
		return h, nil
	}
	return h, writeFileAtomic(p, b)
}

// Blob reads content by hash.
func (s *Store) Blob(h string) ([]byte, error) {
	if len(h) != 64 || strings.ContainsAny(h, "/\\.") {
		return nil, fmt.Errorf("invalid blob id %q", h)
	}
	return os.ReadFile(filepath.Join(s.Dir, "blobs", h))
}

// RoundDir returns the directory of round n.
func (s *Store) RoundDir(n int) string {
	return filepath.Join(s.Dir, "rounds", strconv.Itoa(n))
}

// Manifest reads the manifest of round n.
func (s *Store) Manifest(n int) (*Manifest, error) {
	var m Manifest
	if err := readJSON(filepath.Join(s.RoundDir(n), "manifest.json"), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// SaveManifest writes the manifest of its round.
func (s *Store) SaveManifest(m *Manifest) error {
	if err := os.MkdirAll(s.RoundDir(m.Round), 0o755); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.RoundDir(m.Round), "manifest.json"), m)
}

// WriteRoundFile writes a file into the directory of round n.
func (s *Store) WriteRoundFile(n int, name string, b []byte) (string, error) {
	p := filepath.Join(s.RoundDir(n), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, writeFileAtomic(p, b)
}

func readJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(p, append(b, '\n'))
}

func writeFileAtomic(p string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}
