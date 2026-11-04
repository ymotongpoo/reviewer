// Package t3 connects reviewer to T3 Code through the MCP endpoint of its
// server. Feedback is sent to a T3 thread as a new message, and the run it
// starts is followed by polling the thread's timeline.
package t3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Options configure the connection.
type Options struct {
	// URL is the origin of the T3 Code server, e.g. http://127.0.0.1:3773.
	// Empty means the server recorded in <Home>/userdata/server-runtime.json,
	// read again on every reconnect.
	URL string
	// Token is an MCP client token. Empty means reviewer obtains one itself
	// with the t3 CLI and keeps it in TokenFile.
	Token string
	// Command is the t3 CLI. Empty means the binary of the running server,
	// then t3 on PATH or an installed macOS desktop app.
	Command string
	// Home is the T3 Code data directory ($T3CODE_HOME, ~/.t3).
	Home string
	// Access is the permission ceiling requested for reviewer's token. A
	// thread with a broader runtime mode cannot receive messages from it.
	Access string
	// TokenFile caches the obtained token.
	TokenFile string
	// PollInterval is how often a run's timeline is read.
	PollInterval time.Duration

	commandArgs []string
	commandEnv  []string
}

// Access levels of an MCP client, narrowest first.
var accessLevels = []string{"read-only", "approval-required", "auto-accept-edits", "auto", "full-access"}

// DefaultAccess lets reviewer write to threads of any runtime mode.
const DefaultAccess = "full-access"

// ErrNotConfigured means no T3 Code server was found on this machine.
var ErrNotConfigured = errors.New("T3 Code のサーバーが見つかりません")

// HomeDir returns the T3 Code data directory.
func HomeDir() string {
	if d := os.Getenv("T3CODE_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".t3"
	}
	return filepath.Join(home, ".t3")
}

type runtimeInfo struct {
	PID    int    `json:"pid"`
	Origin string `json:"origin"`
}

func readRuntime(home string) (runtimeInfo, error) {
	var rt runtimeInfo
	b, err := os.ReadFile(filepath.Join(home, "userdata", "server-runtime.json"))
	if err != nil {
		return rt, err
	}
	if err := json.Unmarshal(b, &rt); err != nil {
		return rt, err
	}
	if rt.Origin == "" {
		return rt, errors.New("server-runtime.json has no origin")
	}
	return rt, nil
}

// Resolve fills in defaults and checks that a server can be found.
func Resolve(o Options) (Options, error) {
	if o.Home == "" {
		o.Home = HomeDir()
	}
	if o.Access == "" {
		o.Access = DefaultAccess
	}
	if !slices.Contains(accessLevels, o.Access) {
		return o, fmt.Errorf("agent.t3.access は %s のどれかです: %q", strings.Join(accessLevels, ", "), o.Access)
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 2 * time.Second
	}
	o.URL = strings.TrimRight(o.URL, "/")
	// The server may start after reviewer; its address is read when
	// connecting, so an installed T3 Code is enough.
	rt, rtErr := readRuntime(o.Home)
	if o.URL == "" && rtErr != nil {
		if _, err := os.Stat(filepath.Join(o.Home, "userdata")); err != nil {
			return o, ErrNotConfigured
		}
	}
	if o.Token == "" && o.Command == "" {
		cli := findCommand(o.Home, rt.PID)
		o.Command, o.commandArgs, o.commandEnv = cli.path, cli.args, cli.env
		if o.Command == "" {
			return o, errors.New("t3 コマンドが見つかりません（agent.t3.command で指定できます）")
		}
	}
	return o, nil
}

// cliCommand also supports the CLI bundled in the Electron desktop app.
// Electron must run as Node with the server entry point before CLI arguments.
type cliCommand struct {
	path string
	args []string
	env  []string
}

func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

func commandForExecutable(path string) cliCommand {
	if !executable(path) {
		return cliCommand{}
	}
	contents := filepath.Dir(filepath.Dir(path))
	if filepath.Base(filepath.Dir(path)) == "MacOS" && filepath.Ext(filepath.Dir(contents)) == ".app" {
		archive := filepath.Join(contents, "Resources", "app.asar")
		if st, err := os.Stat(archive); err != nil || !st.Mode().IsRegular() {
			return cliCommand{}
		}
		return cliCommand{path: path,
			args: []string{filepath.Join(archive, "apps", "server", "dist", "bin.mjs")},
			env:  []string{"ELECTRON_RUN_AS_NODE=1"}}
	}
	return cliCommand{path: path}
}

func processExecutable(pid int) string {
	if pid <= 0 {
		return ""
	}
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		return ""
	}
	path, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if strings.HasSuffix(path, " (deleted)") {
		return ""
	}
	return path
}

// Prefer the running server's CLI: it reads that server's database directly.
func findCommand(home string, pid int) cliCommand {
	if cli := commandForExecutable(processExecutable(pid)); cli.path != "" {
		return cli
	}
	if path, err := exec.LookPath("t3"); err == nil {
		return cliCommand{path: path}
	}
	matches, _ := filepath.Glob(filepath.Join(home, "runtime", "versions", "*", "t3"))
	var best string
	var bestTime time.Time
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && executable(m) && st.ModTime().After(bestTime) {
			best, bestTime = m, st.ModTime()
		}
	}
	if best != "" {
		return cliCommand{path: best}
	}
	if runtime.GOOS == "darwin" {
		userHome, _ := os.UserHomeDir()
		for _, dir := range []string{"/Applications", filepath.Join(userHome, "Applications")} {
			apps, _ := filepath.Glob(filepath.Join(dir, "T3 Code*.app", "Contents", "MacOS", "T3 Code*"))
			for _, app := range apps {
				if cli := commandForExecutable(app); cli.path != "" {
					return cli
				}
			}
		}
	}
	return cliCommand{}
}

// origin returns the server to talk to.
func (c *Client) origin() (string, error) {
	if c.opts.URL != "" {
		return c.opts.URL, nil
	}
	rt, err := readRuntime(c.opts.Home)
	if err != nil {
		return "", ErrNotConfigured
	}
	return strings.TrimRight(rt.Origin, "/"), nil
}
