package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DefaultSession is the review session whose data lives directly in the
// data directory. Data written before sessions existed belongs to it.
const DefaultSession = "main"

// DefaultSessionName is shown for the default session until it is renamed.
const DefaultSessionName = "メイン"

// maxSessionName bounds the length of a session name in runes.
const maxSessionName = 80

var sessionIDPattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// Session describes a review session of a directory. Each session has its
// own rounds, comments, annotation requests and agent binding; the files,
// presets and Git settings of the directory are shared.
type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// Archived sessions are hidden from the session lists.
	Archived bool `json:"archived,omitempty"`
}

// ValidSessionID reports whether id can name a session directory.
func ValidSessionID(id string) bool { return sessionIDPattern.MatchString(id) }

// SessionDir returns the data directory of session id under the data
// directory root.
func SessionDir(root, id string) string {
	if id == DefaultSession {
		return root
	}
	return filepath.Join(root, "sessions", id)
}

func sessionInfoPath(root, id string) string {
	return filepath.Join(SessionDir(root, id), "session.json")
}

// ReadSession returns the metadata of session id. The default session
// exists even without metadata; other sessions need their session.json.
func ReadSession(root, id string) (Session, error) {
	if !ValidSessionID(id) {
		return Session{}, fmt.Errorf("invalid session id %q", id)
	}
	var s Session
	err := readJSON(sessionInfoPath(root, id), &s)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist) && id == DefaultSession:
	case errors.Is(err, fs.ErrNotExist):
		return Session{}, fs.ErrNotExist
	default:
		return Session{}, err
	}
	s.ID = id
	if strings.TrimSpace(s.Name) == "" {
		if id == DefaultSession {
			s.Name = DefaultSessionName
		} else {
			s.Name = id
		}
	}
	return s, nil
}

// ListSessions returns the sessions of the data directory root, the default
// session first and the others by creation time.
func ListSessions(root string) ([]Session, error) {
	main, err := ReadSession(root, DefaultSession)
	if err != nil {
		return nil, err
	}
	out := []Session{main}
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var others []Session
	for _, e := range entries {
		if !e.IsDir() || !ValidSessionID(e.Name()) || e.Name() == DefaultSession {
			continue
		}
		s, err := ReadSession(root, e.Name())
		if err != nil {
			continue
		}
		others = append(others, s)
	}
	sort.Slice(others, func(i, j int) bool {
		if !others[i].CreatedAt.Equal(others[j].CreatedAt) {
			return others[i].CreatedAt.Before(others[j].CreatedAt)
		}
		return others[i].ID < others[j].ID
	})
	return append(out, others...), nil
}

// CleanSessionName trims name and rejects empty or overlong names.
func CleanSessionName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("セッション名を入力してください")
	}
	if len([]rune(name)) > maxSessionName {
		return "", fmt.Errorf("セッション名は%d文字以内にしてください", maxSessionName)
	}
	return name, nil
}

// CreateSession creates a session named name under the data directory root.
func CreateSession(root, name string, now time.Time) (Session, error) {
	name, err := CleanSessionName(name)
	if err != nil {
		return Session{}, err
	}
	for range 5 {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return Session{}, err
		}
		id := hex.EncodeToString(b)
		dir := SessionDir(root, id)
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return Session{}, err
		}
		if err := os.Mkdir(dir, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return Session{}, err
		}
		s := Session{ID: id, Name: name, CreatedAt: now}
		return s, SaveSession(root, s)
	}
	return Session{}, errors.New("セッションの ID を割り当てられませんでした")
}

// SaveSession writes the metadata of s.
func SaveSession(root string, s Session) error {
	if !ValidSessionID(s.ID) {
		return fmt.Errorf("invalid session id %q", s.ID)
	}
	p := sessionInfoPath(root, s.ID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return writeJSON(p, s)
}
