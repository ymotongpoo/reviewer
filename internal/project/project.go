// Package project enumerates and reads the reviewable files of a directory.
package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// MaxFileSize is the largest file considered reviewable.
const MaxFileSize = 2 << 20

// ErrOutside is returned for paths that escape the project root.
var ErrOutside = errors.New("path is outside of the project")

// ErrNotText is returned for binary or oversized files.
var ErrNotText = errors.New("not a reviewable text file")

// ErrChanged is returned by Write when the file no longer has the hash the
// caller based its edit on.
var ErrChanged = errors.New("file changed since it was opened")

// File describes a reviewable file.
type File struct {
	Path string `json:"path"` // slash-separated, relative to the root
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

type cacheEntry struct {
	mtime  time.Time
	size   int64
	hash   string
	isText bool
}

// Project is a reviewed directory.
type Project struct {
	Root    string // absolute, symlinks resolved
	dataDir string // absolute; excluded when inside Root
	exclude []gitignore.Pattern

	mu       sync.Mutex
	patterns []gitignore.Pattern
	cache    map[string]cacheEntry
	dirs     []string
}

// New creates a project rooted at root. extraExclude are gitignore-style
// patterns relative to the root.
func New(root, dataDir string, extraExclude []string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	p := &Project{Root: real, cache: map[string]cacheEntry{}}
	if dataDir != "" {
		if d, err := filepath.Abs(dataDir); err == nil {
			p.dataDir = d
			if rd, err := filepath.EvalSymlinks(filepath.Dir(d)); err == nil {
				p.dataDir = filepath.Join(rd, filepath.Base(d))
			}
		}
	}
	p.exclude = append(p.exclude, gitignore.ParsePattern(".git", nil))
	for _, e := range extraExclude {
		p.exclude = append(p.exclude, gitignore.ParsePattern(e, nil))
	}
	return p, nil
}

// Name returns the directory name of the project.
func (p *Project) Name() string { return filepath.Base(p.Root) }

// Rel converts an absolute path into a slash-separated relative path.
func (p *Project) Rel(abs string) (string, bool) {
	rel, err := filepath.Rel(p.Root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// Resolve validates a relative path and returns its absolute location. Paths
// escaping the root, directly or through symlinks, are rejected.
func (p *Project) Resolve(rel string) (string, error) {
	if rel == "" || strings.Contains(rel, "\x00") || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", ErrOutside
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrOutside
	}
	abs := filepath.Join(p.Root, clean)
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if _, ok := p.Rel(real); !ok || real == p.Root {
		return "", ErrOutside
	}
	if p.inDataDir(real) {
		return "", ErrOutside
	}
	return abs, nil
}

func (p *Project) inDataDir(abs string) bool {
	if p.dataDir == "" {
		return false
	}
	return abs == p.dataDir || strings.HasPrefix(abs, p.dataDir+string(filepath.Separator))
}

// DataDirInside reports whether the data directory lives inside the root.
func (p *Project) DataDirInside() (string, bool) {
	if p.dataDir == "" {
		return "", false
	}
	rel, ok := p.Rel(p.dataDir)
	return rel, ok
}

// GitIgnored reports whether rel would be ignored by .gitignore rules alone.
func (p *Project) GitIgnored(rel string, isDir bool) bool {
	p.mu.Lock()
	pats := p.patterns
	p.mu.Unlock()
	return gitignore.NewMatcher(pats).Match(strings.Split(rel, "/"), isDir)
}

// Ignored reports whether rel is excluded from review.
func (p *Project) Ignored(rel string, isDir bool) bool {
	parts := strings.Split(rel, "/")
	p.mu.Lock()
	pats := append(append([]gitignore.Pattern{}, p.exclude...), p.patterns...)
	p.mu.Unlock()
	if gitignore.NewMatcher(pats).Match(parts, isDir) {
		return true
	}
	return p.inDataDir(filepath.Join(p.Root, filepath.FromSlash(rel)))
}

// Walk lists reviewable text files, refreshing the ignore rules and the
// content hash cache. It also records the directories visited so that a
// watcher can subscribe to them.
func (p *Project) Walk() ([]File, error) {
	var patterns []gitignore.Pattern
	if b, err := os.ReadFile(filepath.Join(p.Root, ".git", "info", "exclude")); err == nil {
		patterns = append(patterns, parsePatterns(b, nil)...)
	}
	var files []File
	var dirs []string
	seen := map[string]bool{}
	err := filepath.WalkDir(p.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == p.Root {
				return err
			}
			return nil
		}
		rel, _ := p.Rel(path)
		if d.IsDir() {
			if path != p.Root {
				if p.inDataDir(path) || p.match(patterns, rel, true) {
					return filepath.SkipDir
				}
			}
			var domain []string
			if rel != "." {
				domain = strings.Split(rel, "/")
			}
			if b, err := os.ReadFile(filepath.Join(path, ".gitignore")); err == nil {
				patterns = append(patterns, parsePatterns(b, domain)...)
			}
			dirs = append(dirs, path)
			return nil
		}
		if !d.Type().IsRegular() || p.match(patterns, rel, false) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxFileSize {
			return nil
		}
		e, err := p.entry(rel, path, info)
		if err != nil || !e.isText {
			return nil
		}
		seen[rel] = true
		files = append(files, File{Path: rel, Size: info.Size(), Hash: e.hash})
		return nil
	})
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.patterns = patterns
	p.dirs = dirs
	for k := range p.cache {
		if !seen[k] {
			delete(p.cache, k)
		}
	}
	p.mu.Unlock()
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// Dirs returns the directories visited by the last Walk.
func (p *Project) Dirs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.dirs...)
}

func (p *Project) match(patterns []gitignore.Pattern, rel string, isDir bool) bool {
	all := append(append([]gitignore.Pattern{}, p.exclude...), patterns...)
	return gitignore.NewMatcher(all).Match(strings.Split(rel, "/"), isDir)
}

func parsePatterns(b []byte, domain []string) []gitignore.Pattern {
	var out []gitignore.Pattern
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, gitignore.ParsePattern(line, domain))
	}
	return out
}

func (p *Project) entry(rel, abs string, info fs.FileInfo) (cacheEntry, error) {
	p.mu.Lock()
	e, ok := p.cache[rel]
	p.mu.Unlock()
	if ok && e.mtime.Equal(info.ModTime()) && e.size == info.Size() {
		return e, nil
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return cacheEntry{}, err
	}
	e = cacheEntry{mtime: info.ModTime(), size: info.Size(), isText: IsText(b), hash: HashBytes(b)}
	p.mu.Lock()
	p.cache[rel] = e
	p.mu.Unlock()
	return e, nil
}

// Read returns the content of a reviewable file.
func (p *Project) Read(rel string) ([]byte, error) {
	abs, err := p.Resolve(rel)
	if err != nil {
		return nil, err
	}
	if p.Ignored(rel, false) {
		return nil, ErrOutside
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileSize {
		return nil, ErrNotText
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if !IsText(b) {
		return nil, ErrNotText
	}
	return b, nil
}

// Write replaces a reviewable text file only when its current content has the
// expected hash. It writes beside the file and renames atomically so a failed
// save cannot leave a partial file behind. A write by another program between
// the hash check and the rename is not detected.
func (p *Project) Write(rel, expectedHash string, content []byte) error {
	abs, err := p.Resolve(rel)
	if err != nil {
		return err
	}
	if p.Ignored(rel, false) {
		return ErrOutside
	}
	if int64(len(content)) > MaxFileSize || !IsText(content) {
		return ErrNotText
	}
	current, err := p.Read(rel)
	if err != nil {
		return err
	}
	if expectedHash == "" || HashBytes(current) != expectedHash {
		return ErrChanged
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".reviewer-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, abs)
}

// IsText reports whether b looks like text (no NUL byte in the first 8KB).
func IsText(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) < 0
}

// HashBytes returns the hex SHA-256 of b.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
