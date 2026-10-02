package store

import (
	"bytes"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/ymotongpoo/reviewer/internal/config"
)

func (s *Store) gitPath() string { return filepath.Join(s.Dir, "git.toml") }

// GitSettings reads the project's git.toml. ok is false when it is missing.
func (s *Store) GitSettings() (config.GitConfig, bool, error) {
	var doc struct {
		Git config.GitConfig `toml:"git"`
	}
	if _, err := toml.DecodeFile(s.gitPath(), &doc); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return config.GitConfig{}, false, nil
		}
		return config.GitConfig{}, false, err
	}
	return doc.Git, true, nil
}

// SaveGitSettings writes git.toml atomically.
func (s *Store) SaveGitSettings(g config.GitConfig) error {
	var b bytes.Buffer
	doc := struct {
		Git config.GitConfig `toml:"git"`
	}{Git: g}
	if err := toml.NewEncoder(&b).Encode(doc); err != nil {
		return err
	}
	return writeFileAtomic(s.gitPath(), b.Bytes())
}
