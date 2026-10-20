package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/ymotongpoo/reviewer/internal/project"
)

var attachmentIDPattern = regexp.MustCompile(`^[a-f0-9]{64}\.(png|jpg|gif)$`)

var attachmentTypes = map[string]string{
	"png": "image/png",
	"jpg": "image/jpeg",
	"gif": "image/gif",
}

// PutAttachment stores an image by content hash and returns its safe filename.
func (s *Store) PutAttachment(data []byte, ext string) (string, error) {
	if _, ok := attachmentTypes[ext]; !ok {
		return "", fmt.Errorf("unsupported attachment extension %q", ext)
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, "attachments"), 0o755); err != nil {
		return "", err
	}
	id := project.HashBytes(data) + "." + ext
	p := filepath.Join(s.Dir, "attachments", id)
	if _, err := os.Stat(p); err == nil {
		return id, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return id, writeFileAtomic(p, data)
}

// AttachmentPath returns the path for a validated attachment id.
func (s *Store) AttachmentPath(id string) (string, error) {
	if !attachmentIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid attachment id %q", id)
	}
	return filepath.Join(s.Dir, "attachments", id), nil
}

// ReadAttachment reads an image saved by PutAttachment and returns its media type.
func (s *Store) ReadAttachment(id string) ([]byte, string, error) {
	p, err := s.AttachmentPath(id)
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	ext := filepath.Ext(id)[1:]
	return b, attachmentTypes[ext], nil
}
