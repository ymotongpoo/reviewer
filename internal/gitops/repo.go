package gitops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Repo is the part of a Git work tree that reviewer may commit: the project
// directory (Prefix within the repository) minus the data directory.
type Repo struct {
	Runner
	// Toplevel is the root of the work tree; Prefix is the project
	// directory relative to it ("" or ending with "/").
	Toplevel string
	Prefix   string
	GitDir   string
	// Exclude is the data directory relative to the project ("" when it
	// lives outside the project).
	Exclude string
}

// Open locates the repository containing dir. exclude is a path relative to
// dir that is never staged (the data directory). It returns ErrNotRepo when
// dir is not inside a work tree.
func Open(ctx context.Context, dir, exclude string) (*Repo, error) {
	r := &Repo{Runner: Runner{Dir: dir}, Exclude: strings.Trim(filepath.ToSlash(exclude), "/")}
	out, err := r.Run(ctx, ReadTimeout, nil, "rev-parse", "--is-inside-work-tree", "--show-toplevel", "--show-prefix", "--absolute-git-dir")
	if err != nil {
		var ce *CmdError
		if errors.As(err, &ce) && ce.ExitCode == 128 {
			return nil, ErrNotRepo
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	// --show-prefix prints an empty line at the top level.
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	if lines[0] != "true" {
		return nil, ErrNotRepo
	}
	r.Toplevel, r.Prefix, r.GitDir = lines[1], lines[2], lines[3]
	return r, nil
}

// pathspec selects the project directory without the data directory.
// Commands using it run in the project directory.
func (r *Repo) pathspec() []string {
	ps := []string{"--", "."}
	if r.Exclude != "" {
		ps = append(ps, ":(exclude,literal)"+r.Exclude)
	}
	return ps
}

// Change is a changed path, relative to the project directory.
type Change struct {
	Path string `json:"path"`
	// From is the previous path of a rename.
	From string `json:"from,omitempty"`
	// Kind is added, modified, deleted, renamed, typechange or conflict.
	Kind string `json:"kind"`
	// Staged tells whether the index already holds part of the change.
	Staged bool `json:"staged"`
	// Untracked is set for new files not yet known to git.
	Untracked bool `json:"untracked,omitempty"`
}

// Change kinds.
const (
	Added      = "added"
	Modified   = "modified"
	Deleted    = "deleted"
	Renamed    = "renamed"
	TypeChange = "typechange"
	Conflict   = "conflict"
)

// State describes the repository for the commit dialog.
type State struct {
	Branch string `json:"branch"` // empty when detached
	// Unborn is set before the first commit.
	Unborn   bool   `json:"unborn"`
	Head     string `json:"head,omitempty"`
	Upstream struct {
		Remote string `json:"remote,omitempty"`
		Branch string `json:"branch,omitempty"`
	} `json:"upstream"`
	// Operation is a merge, rebase, cherry-pick, revert or bisect in
	// progress.
	Operation string   `json:"operation,omitempty"`
	Changes   []Change `json:"changes"`
	// OutsideStaged lists staged paths (relative to the repository) that a
	// commit would include although they are not part of the project.
	OutsideStaged []string `json:"outsideStaged"`
	// Fingerprint changes whenever the set or content of changes changes.
	Fingerprint string `json:"fingerprint"`
}

// Status inspects the branch, the operation in progress and the changes.
func (r *Repo) Status(ctx context.Context) (*State, error) {
	st := &State{Changes: []Change{}, OutsideStaged: []string{}}
	if out, err := r.Run(ctx, ReadTimeout, nil, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(string(out))
	}
	if out, err := r.Run(ctx, ReadTimeout, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err == nil {
		st.Head = strings.TrimSpace(string(out))
	} else {
		st.Unborn = true
	}
	if st.Branch != "" {
		st.Upstream.Remote, st.Upstream.Branch = r.upstream(ctx, st.Branch)
	}
	st.Operation = r.operation()

	out, err := r.Run(ctx, ReadTimeout, nil, append([]string{"status", "--porcelain=v2", "-z", "--untracked-files=all"}, r.pathspec()...)...)
	if err != nil {
		return nil, err
	}
	st.Changes = r.parseStatus(out)

	index, err := r.staged(ctx, st.Unborn)
	if err != nil {
		return nil, err
	}
	for _, p := range splitZ(index) {
		if !r.inScope(p) {
			st.OutsideStaged = append(st.OutsideStaged, p)
		}
	}
	st.Fingerprint = r.fingerprint(append(out, index...), st.Changes)
	return st, nil
}

// staged lists the staged paths relative to the repository, NUL separated.
func (r *Repo) staged(ctx context.Context, unborn bool) ([]byte, error) {
	args := []string{"diff", "--cached", "--name-only", "-z", "--no-renames"}
	if unborn {
		// An unborn branch has nothing to compare with; list the index.
		args = []string{"ls-files", "-z", "--cached"}
	}
	top := Runner{Dir: r.Toplevel, Bin: r.Bin, Env: r.Env}
	return top.Run(ctx, ReadTimeout, nil, args...)
}

// OutsideStaged lists staged paths that are not part of the project, as
// they would be committed now.
func (r *Repo) OutsideStaged(ctx context.Context, unborn bool) ([]string, error) {
	index, err := r.staged(ctx, unborn)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, p := range splitZ(index) {
		if !r.inScope(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// upstream returns the remote and remote branch name the branch tracks.
func (r *Repo) upstream(ctx context.Context, branch string) (string, string) {
	out, err := r.Run(ctx, ReadTimeout, nil, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+branch)
	if err != nil {
		return "", ""
	}
	remote, ref, _ := strings.Cut(strings.TrimRight(string(out), "\n"), "\x00")
	return remote, strings.TrimPrefix(ref, "refs/heads/")
}

func (r *Repo) operation() string {
	for _, op := range []struct{ file, name string }{
		{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"}, {"BISECT_LOG", "bisect"},
	} {
		if _, err := os.Stat(filepath.Join(r.GitDir, op.file)); err == nil {
			return op.name
		}
	}
	return ""
}

// inScope reports whether a repository-relative path belongs to the project
// and is not in the data directory.
func (r *Repo) inScope(repoPath string) bool {
	rel, ok := strings.CutPrefix(repoPath, r.Prefix)
	if !ok {
		return false
	}
	return r.Exclude == "" || (rel != r.Exclude && !strings.HasPrefix(rel, r.Exclude+"/"))
}

func (r *Repo) rel(repoPath string) string {
	return strings.TrimPrefix(repoPath, r.Prefix)
}

// parseStatus reads `git status --porcelain=v2 -z`. Paths in it are
// relative to the repository root.
func (r *Repo) parseStatus(out []byte) []Change {
	fields := splitZ(out)
	var changes []Change
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case '1', '2', 'u':
			n := map[byte]int{'1': 8, '2': 9, 'u': 10}[f[0]]
			parts := strings.SplitN(f, " ", n+1)
			if len(parts) < n+1 {
				continue
			}
			xy, path := parts[1], parts[n]
			c := Change{Path: r.rel(path), Staged: xy[0] != '.'}
			switch {
			case f[0] == 'u':
				c.Kind = Conflict
			case f[0] == '2':
				c.Kind = Renamed
				if i+1 < len(fields) {
					i++
					c.From = r.rel(fields[i])
				}
			default:
				c.Kind = kindOf(xy)
			}
			changes = append(changes, c)
		case '?':
			changes = append(changes, Change{Path: r.rel(f[2:]), Kind: Added, Untracked: true})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if changes == nil {
		changes = []Change{}
	}
	return changes
}

func kindOf(xy string) string {
	switch {
	case strings.ContainsRune(xy, 'D'):
		return Deleted
	case strings.ContainsRune(xy, 'A'):
		return Added
	case strings.ContainsRune(xy, 'T'):
		return TypeChange
	default:
		return Modified
	}
}

func splitZ(b []byte) []string {
	var out []string
	for _, f := range bytes.Split(b, []byte{0}) {
		if len(f) > 0 {
			out = append(out, string(f))
		}
	}
	return out
}

// fingerprint hashes the status and index listing together with the size and modification
// time of every changed file, so that edits to an already modified file
// change it too.
func (r *Repo) fingerprint(listing []byte, changes []Change) string {
	h := sha256.New()
	h.Write(listing)
	for _, c := range changes {
		fmt.Fprintf(h, "\x00%s\x00%s\x00%s\x00%t", c.Kind, c.Path, c.From, c.Staged)
		if fi, err := os.Lstat(filepath.Join(r.Dir, filepath.FromSlash(c.Path))); err == nil {
			fmt.Fprintf(h, "\x00%d\x00%d\x00%s", fi.Size(), fi.ModTime().UnixNano(), fi.Mode())
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// Remote is a configured remote. URL is masked and only informational: the
// UI selects remotes by name and never accepts a URL.
type Remote struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Branches []string `json:"branches"`
	// Mirror is set for remotes configured with remote.<name>.mirror, on
	// which every push is a forced mirror push. reviewer refuses them.
	Mirror bool `json:"mirror,omitempty"`
}

// Remotes lists the remotes with their remote-tracking branches.
func (r *Repo) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := r.Run(ctx, ReadTimeout, nil, "remote")
	if err != nil {
		return nil, err
	}
	names := strings.Fields(string(out))
	sort.Strings(names)
	refs, err := r.Run(ctx, ReadTimeout, nil, "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if err != nil {
		return nil, err
	}
	all := strings.Fields(string(refs))
	remotes := make([]Remote, 0, len(names))
	for _, name := range names {
		if !validName(name) {
			continue
		}
		rm := Remote{Name: name, Branches: []string{}}
		if u, err := r.Run(ctx, ReadTimeout, nil, "remote", "get-url", "--push", "--", name); err == nil {
			rm.URL = Mask(strings.TrimSpace(string(u)))
		}
		rm.Mirror = r.isMirror(ctx, name)
		prefix := "refs/remotes/" + name + "/"
		for _, ref := range all {
			if b, ok := strings.CutPrefix(ref, prefix); ok && b != "HEAD" {
				rm.Branches = append(rm.Branches, b)
			}
		}
		sort.Strings(rm.Branches)
		remotes = append(remotes, rm)
	}
	return remotes, nil
}

// isMirror reports whether pushes to remote are mirror pushes. An unreadable
// value counts as a mirror, so that the push is refused.
func (r *Repo) isMirror(ctx context.Context, remote string) bool {
	out, err := r.Run(ctx, ReadTimeout, nil, "config", "--bool", "--get", "remote."+remote+".mirror")
	if err != nil {
		var ce *CmdError
		// Exit status 1 means the key is not set.
		return !(errors.As(err, &ce) && ce.ExitCode == 1)
	}
	return strings.TrimSpace(string(out)) == "true"
}

// validName rejects names that git could take for an option or that do not
// fit in a refspec.
func validName(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-") && !strings.ContainsAny(s, " \t\r\n\x00:+^~?*[\\")
}

// CheckBranchName validates a branch name with git's own rules.
func (r *Repo) CheckBranchName(ctx context.Context, name string) error {
	if !validName(name) || strings.HasPrefix(name, "refs/") {
		return fmt.Errorf("invalid branch name: %q", name)
	}
	if _, err := r.Run(ctx, ReadTimeout, nil, "check-ref-format", "refs/heads/"+name); err != nil {
		return fmt.Errorf("invalid branch name: %q", name)
	}
	return nil
}

// Divergence counts the commits HEAD is ahead of and behind a
// remote-tracking branch. ok is false when the tracking ref does not exist.
func (r *Repo) Divergence(ctx context.Context, remote, branch string) (ahead, behind int, ok bool) {
	ref := "refs/remotes/" + remote + "/" + branch
	if _, err := r.Run(ctx, ReadTimeout, nil, "rev-parse", "--verify", "--quiet", ref); err != nil {
		return 0, 0, false
	}
	out, err := r.Run(ctx, ReadTimeout, nil, "rev-list", "--left-right", "--count", "HEAD..."+ref)
	if err != nil {
		return 0, 0, false
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		return 0, 0, false
	}
	ahead, _ = strconv.Atoi(f[0])
	behind, _ = strconv.Atoi(f[1])
	return ahead, behind, true
}

// CountCommits returns the number of commits reachable from HEAD.
func (r *Repo) CountCommits(ctx context.Context) int {
	out, err := r.Run(ctx, ReadTimeout, nil, "rev-list", "--count", "HEAD")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// Identity reports whether git knows the committer's name and email.
func (r *Repo) Identity(ctx context.Context) (string, error) {
	out, err := r.Run(ctx, ReadTimeout, nil, "var", "GIT_COMMITTER_IDENT")
	if err != nil {
		return "", err
	}
	ident := strings.TrimSpace(string(out))
	// Drop the timestamp: "Name <email> 1700000000 +0900".
	if i := strings.LastIndex(ident, ">"); i >= 0 {
		ident = ident[:i+1]
	}
	return ident, nil
}

// CredentialHelper reports whether a credential helper is configured. The
// value is not returned: helpers may embed secrets.
func (r *Repo) CredentialHelper(ctx context.Context) bool {
	out, err := r.Run(ctx, ReadTimeout, nil, "config", "--get-regexp", `^credential\..*helper$`)
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// ErrOutsideStaged is returned by Commit when the index holds changes that
// are not part of the project.
var ErrOutsideStaged = errors.New("changes outside the project are staged")

// Stage adds every change of the project (not the data directory) to the
// index. Changes outside the project are left as they are.
func (r *Repo) Stage(ctx context.Context) error {
	_, err := r.Run(ctx, CommitTimeout, nil, append([]string{"add", "--all"}, r.pathspec()...)...)
	return err
}

// CommitStaged commits the index with message and returns the new HEAD.
func (r *Repo) CommitStaged(ctx context.Context, message string) (string, error) {
	// --cleanup=strip would drop lines starting with '#'; keep the message
	// as written apart from surrounding whitespace.
	if _, err := r.Run(ctx, CommitTimeout, []byte(message), "commit", "--quiet", "--cleanup=whitespace", "--file=-"); err != nil {
		return "", err
	}
	out, err := r.Run(ctx, ReadTimeout, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Commit stages the project and commits it with message. When the index
// also holds changes outside the project the commit is refused (with the
// project's changes left staged), because git would include them.
func (r *Repo) Commit(ctx context.Context, message string) (string, error) {
	unborn := false
	if _, err := r.Run(ctx, ReadTimeout, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		unborn = true
	}
	if err := r.Stage(ctx); err != nil {
		return "", err
	}
	if outside, err := r.OutsideStaged(ctx, unborn); err != nil {
		return "", err
	} else if len(outside) > 0 {
		return "", ErrOutsideStaged
	}
	return r.CommitStaged(ctx, message)
}

// ErrMirror is returned by Push for a remote configured as a mirror.
var ErrMirror = errors.New("the remote is configured as a mirror")

// Push sends HEAD to branch on remote without force. remote must be the
// name of a configured remote, not a URL. It returns the remote's messages
// (the "remote:" lines, such as a link to open a pull request).
func (r *Repo) Push(ctx context.Context, remote, branch string) (string, error) {
	if !validName(remote) || !validName(branch) {
		return "", fmt.Errorf("invalid push target %q %q", remote, branch)
	}
	// remote.<name>.mirror turns any push into a forced mirror push.
	if r.isMirror(ctx, remote) {
		return "", ErrMirror
	}
	// The explicit refspec has no leading '+' and replaces configured push
	// refspecs; no --force, --force-with-lease, --mirror, --delete or
	// --prune is ever passed. Tags and submodules are not pushed along.
	_, stderr, err := r.RunStderr(ctx, PushTimeout, nil, "push", "--no-follow-tags", "--recurse-submodules=check", "--", remote, "HEAD:refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, l := range strings.Split(stderr, "\n") {
		if msg, ok := strings.CutPrefix(strings.TrimRight(l, "\r"), "remote:"); ok && strings.TrimSpace(msg) != "" {
			lines = append(lines, strings.TrimSpace(msg))
		}
	}
	return strings.Join(lines, "\n"), nil
}
