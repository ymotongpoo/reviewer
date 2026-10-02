// Package gitops runs the few Git operations reviewer offers from the web
// UI: inspecting the working tree, committing it, and a plain push. Commands
// are executed directly (no shell) with fixed argument lists, a timeout, a
// bounded stderr and an environment that keeps Git from prompting, so that
// only an existing SSH agent or credential helper can authenticate.
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Timeouts of the commands. Reads are local and quick; commit runs hooks and
// push talks to the network.
const (
	ReadTimeout   = 20 * time.Second
	CommitTimeout = 2 * time.Minute
	PushTimeout   = 2 * time.Minute
)

// Output limits. Stderr is only shown to the user, so a short tail is
// enough; stdout carries status listings and must hold a large tree.
const (
	maxStderr = 16 << 10
	maxStdout = 8 << 20
)

// ErrNotRepo is returned when the directory is not inside a Git work tree.
var ErrNotRepo = errors.New("not a git repository")

// Runner executes git in a directory.
type Runner struct {
	Dir string
	// Bin is the git executable; empty means "git" from PATH.
	Bin string
	// Env overrides the parent environment (for tests); nil uses os.Environ.
	Env []string
}

// CmdError is a failed git command. Stderr is already masked.
type CmdError struct {
	Args     []string
	ExitCode int
	Stderr   string
	TimedOut bool
	Err      error
}

func (e *CmdError) Error() string {
	msg := "git " + strings.Join(e.Args, " ")
	switch {
	case e.TimedOut:
		msg += ": timed out"
	case e.ExitCode > 0:
		msg += fmt.Sprintf(": exit status %d", e.ExitCode)
	case e.Err != nil:
		msg += ": " + e.Err.Error()
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + s
	}
	return Mask(msg)
}

func (e *CmdError) Unwrap() error { return e.Err }

// clearedEnv lists variables that would redirect git away from the project
// or let it ask for secrets interactively.
var clearedEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE",
	"GIT_PREFIX", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
	"GIT_ASKPASS", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "GIT_TERMINAL_PROMPT",
	"GCM_INTERACTIVE", "GIT_EDITOR", "GIT_SEQUENCE_EDITOR", "GIT_PAGER", "PAGER",
	"GIT_TRACE", "GIT_TRACE_CURL", "GIT_TRACE_PACKET", "GIT_CURL_VERBOSE",
	"LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES",
}

// promptlessEnv keeps every authentication non-interactive. An empty
// GIT_ASKPASS stops git from falling back to core.askPass or SSH_ASKPASS.
var promptlessEnv = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_ASKPASS=",
	"SSH_ASKPASS=",
	"SSH_ASKPASS_REQUIRE=never",
	"GCM_INTERACTIVE=never",
	"GIT_EDITOR=true",
	"GIT_SEQUENCE_EDITOR=true",
	"GIT_PAGER=cat",
	"PAGER=cat",
	// Messages are matched to classify failures.
	"LC_ALL=C",
}

func (r *Runner) environ() []string {
	base := r.Env
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base)+len(promptlessEnv))
	for _, kv := range base {
		if k, _, _ := strings.Cut(kv, "="); !slices.Contains(clearedEnv, k) {
			out = append(out, kv)
		}
	}
	return append(out, promptlessEnv...)
}

// baseArgs precede every command: no pager, no colors, and paths printed
// verbatim so that non-ASCII file names survive.
var baseArgs = []string{"--no-pager", "-c", "color.ui=never", "-c", "core.quotePath=false"}

// Run executes git with args and returns its stdout. stdin may be nil.
func (r *Runner) Run(ctx context.Context, timeout time.Duration, stdin []byte, args ...string) ([]byte, error) {
	out, _, err := r.RunStderr(ctx, timeout, stdin, args...)
	return out, err
}

// RunStderr is Run that also returns the masked stderr of a successful
// command; push reports the remote's messages there.
func (r *Runner) RunStderr(ctx context.Context, timeout time.Duration, stdin []byte, args ...string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	bin := r.Bin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, append(append([]string{}, baseArgs...), args...)...)
	cmd.Dir = r.Dir
	cmd.Env = r.environ()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	stdout := &capped{max: maxStdout}
	stderr := &capped{max: maxStderr, tail: true}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	detach(cmd)
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if err == nil {
		if stdout.truncated {
			return nil, "", &CmdError{Args: args, Err: errors.New("output too large")}
		}
		return stdout.buf.Bytes(), Mask(stderr.String()), nil
	}
	ce := &CmdError{Args: args, Stderr: Mask(stderr.String()), Err: err}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		ce.TimedOut = true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		ce.ExitCode = ee.ExitCode()
	}
	return nil, "", ce
}

// capped is a writer that keeps at most max bytes: the head, or the tail
// when tail is set (the end of stderr holds the actual error).
type capped struct {
	buf       bytes.Buffer
	max       int
	tail      bool
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if c.tail {
		c.buf.Write(p)
		if over := c.buf.Len() - c.max; over > 0 {
			b := append([]byte{}, c.buf.Bytes()[over:]...)
			c.buf.Reset()
			c.buf.Write(b)
			c.truncated = true
		}
		return n, nil
	}
	if room := c.max - c.buf.Len(); room < len(p) {
		if room > 0 {
			c.buf.Write(p[:room])
		}
		c.truncated = true
		return n, nil
	}
	c.buf.Write(p)
	return n, nil
}

func (c *capped) String() string {
	s := c.buf.String()
	if c.truncated && c.tail {
		s = "…" + s
	}
	return s
}
