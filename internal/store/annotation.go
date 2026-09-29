package store

import (
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
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ymotongpoo/reviewer/internal/anchor"
	"github.com/ymotongpoo/reviewer/internal/config"
)

const (
	AnnotationPending   = "pending"
	AnnotationAdopted   = "adopted"
	AnnotationDismissed = "dismissed"
)

// Evidence supports an annotation with a source and an optional excerpt.
type Evidence struct {
	URL   string `json:"url,omitempty"`
	Quote string `json:"quote,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Annotation is an agent-authored observation anchored to a file.
type Annotation struct {
	ID         string         `json:"id"`
	Request    string         `json:"request"`
	Path       string         `json:"path"`
	OrigStart  int            `json:"origStart,omitempty"`
	OrigEnd    int            `json:"origEnd,omitempty"`
	OrigBlob   string         `json:"origBlob,omitempty"`
	Anchor     *anchor.Anchor `json:"anchor,omitempty"`
	Loc        *Location      `json:"loc,omitempty"`
	Severity   string         `json:"severity"`
	Confidence string         `json:"confidence"`
	Label      string         `json:"label,omitempty"`
	Body       string         `json:"body"`
	Evidence   []Evidence     `json:"evidence"`
	Suggestion string         `json:"suggestion,omitempty"`
	State      string         `json:"state"`
	AdoptedAs  string         `json:"adoptedAs,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

// AnnotationImport records the latest annotations.json import.
type AnnotationImport struct {
	Hash       string    `json:"hash"`
	ImportedAt time.Time `json:"importedAt"`
	Summary    string    `json:"summary,omitempty"`
	Count      int       `json:"count"`
	Warnings   []string  `json:"warnings,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// AnnotationRequest records one request sent to an agent.
type AnnotationRequest struct {
	ID           string            `json:"id"`
	Preset       string            `json:"preset,omitempty"`
	Prompt       string            `json:"prompt"`
	Files        map[string]string `json:"files"`
	Target       string            `json:"target"`
	SessionID    string            `json:"sessionId,omitempty"`
	Hidden       bool              `json:"hidden,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	CompletedAt  *time.Time        `json:"completedAt,omitempty"`
	ChangedPaths []string          `json:"changedPaths,omitempty"`
	Import       *AnnotationImport `json:"import,omitempty"`
}

type annotationLogRecord struct {
	Op         string      `json:"op"`
	ID         string      `json:"id,omitempty"`
	Annotation *Annotation `json:"annotation,omitempty"`
}

func (s *Store) annotationLogPath() string { return filepath.Join(s.Dir, "annotations.jsonl") }

func (s *Store) loadAnnotations() error {
	return readJSONL(s.annotationLogPath(), func(b []byte) {
		s.annotationLogLines++
		var r annotationLogRecord
		if json.Unmarshal(b, &r) != nil {
			return
		}
		switch r.Op {
		case "put":
			if r.Annotation != nil {
				s.annotations[r.Annotation.ID] = r.Annotation
			}
		case "del":
			delete(s.annotations, r.ID)
		}
	})
}

// NewRequestID allocates a request ID.
func (s *Store) NewRequestID() string {
	id := "Q-" + strconv.Itoa(s.State.NextRequest)
	s.State.NextRequest++
	return id
}

// NewAnnotationID allocates an annotation ID.
func (s *Store) NewAnnotationID() string {
	id := "A-" + strconv.Itoa(s.State.NextAnnotation)
	s.State.NextAnnotation++
	return id
}

// RequestDir returns the directory for an annotation request.
func (s *Store) RequestDir(id string) string { return filepath.Join(s.Dir, "requests", id) }

func validRequestID(id string) bool {
	if !strings.HasPrefix(id, "Q-") || len(id) < 3 {
		return false
	}
	_, err := strconv.Atoi(strings.TrimPrefix(id, "Q-"))
	return err == nil
}

// PutRequest writes request.json.
func (s *Store) PutRequest(r *AnnotationRequest) error {
	if !validRequestID(r.ID) {
		return fmt.Errorf("invalid request id %q", r.ID)
	}
	if err := os.MkdirAll(s.RequestDir(r.ID), 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.RequestDir(r.ID), "request.json"), r); err != nil {
		return err
	}
	return s.SaveState()
}

// Request reads one annotation request.
func (s *Store) Request(id string) (*AnnotationRequest, error) {
	if !validRequestID(id) {
		return nil, fs.ErrNotExist
	}
	var r AnnotationRequest
	if err := readJSON(filepath.Join(s.RequestDir(id), "request.json"), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Requests returns all annotation requests in numeric order.
func (s *Store) Requests() []*AnnotationRequest {
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "requests"))
	out := []*AnnotationRequest{}
	for _, e := range entries {
		if !e.IsDir() || !validRequestID(e.Name()) {
			continue
		}
		if r, err := s.Request(e.Name()); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return numericID(out[i].ID) < numericID(out[j].ID) })
	return out
}

// WriteRequestFile writes a file inside requests/<id>/.
func (s *Store) WriteRequestFile(id, name string, b []byte) (string, error) {
	if !validRequestID(id) || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid request file")
	}
	p := filepath.Join(s.RequestDir(id), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, writeFileAtomic(p, b)
}

// Annotation returns an annotation by ID.
func (s *Store) Annotation(id string) (*Annotation, bool) {
	a, ok := s.annotations[id]
	return a, ok
}

// Annotations returns all annotations ordered by numeric ID.
func (s *Store) Annotations() []*Annotation {
	out := make([]*Annotation, 0, len(s.annotations))
	for _, a := range s.annotations {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return numericID(out[i].ID) < numericID(out[j].ID) })
	return out
}

func numericID(id string) int {
	_, n, _ := strings.Cut(id, "-")
	v, _ := strconv.Atoi(n)
	return v
}

// PutAnnotation appends the current state of a to annotations.jsonl.
func (s *Store) PutAnnotation(a *Annotation) error {
	s.annotations[a.ID] = a
	if err := appendJSONL(s.annotationLogPath(), annotationLogRecord{Op: "put", Annotation: a}); err != nil {
		return err
	}
	return s.SaveState()
}

// DeleteAnnotation removes an annotation.
func (s *Store) DeleteAnnotation(id string) error {
	delete(s.annotations, id)
	return appendJSONL(s.annotationLogPath(), annotationLogRecord{Op: "del", ID: id})
}

// ProjectPresets reads presets.toml. A missing file yields an empty slice.
func (s *Store) ProjectPresets() ([]config.Preset, error) {
	var doc struct {
		Presets []config.Preset `toml:"presets"`
	}
	if _, err := toml.DecodeFile(filepath.Join(s.Dir, "presets.toml"), &doc); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []config.Preset{}, nil
		}
		return nil, err
	}
	if doc.Presets == nil {
		doc.Presets = []config.Preset{}
	}
	return doc.Presets, nil
}

// SaveProjectPresets writes presets.toml atomically.
func (s *Store) SaveProjectPresets(presets []config.Preset) error {
	var b bytes.Buffer
	doc := struct {
		Presets []config.Preset `toml:"presets"`
	}{Presets: presets}
	if err := toml.NewEncoder(&b).Encode(doc); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.Dir, "presets.toml"), b.Bytes())
}
