package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// isolate keeps the user's git configuration (signing, hooks, helpers) out
// of the tests.
func isolate(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("SSH_AUTH_SOCK", "")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture creates a work tree with a bare "origin" and a project directory
// docs/ whose data directory is docs/.reviewer.
func fixture(t *testing.T) (top, project, bare string) {
	t.Helper()
	isolate(t)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	bare = filepath.Join(base, "origin.git")
	top = filepath.Join(base, "work")
	git(t, base, "init", "-q", "--bare", "-b", "main", bare)
	git(t, base, "init", "-q", "-b", "main", top)
	git(t, top, "config", "user.name", "Tester")
	git(t, top, "config", "user.email", "tester@example.com")
	writeFile(t, filepath.Join(top, "docs", "ch1.md"), "# ch1\n")
	writeFile(t, filepath.Join(top, "other", "x.txt"), "x\n")
	git(t, top, "add", "-A")
	git(t, top, "commit", "-q", "-m", "init")
	git(t, top, "remote", "add", "origin", bare)
	git(t, top, "push", "-q", "origin", "main")
	return top, filepath.Join(top, "docs"), bare
}

func TestStatusAndCommitKeepOutsideChanges(t *testing.T) {
	top, project, _ := fixture(t)
	ctx := context.Background()
	writeFile(t, filepath.Join(project, "ch1.md"), "# ch1\nmore\n")
	writeFile(t, filepath.Join(project, "日本語.md"), "新規\n")
	writeFile(t, filepath.Join(project, ".reviewer", "state.json"), "{}\n")
	writeFile(t, filepath.Join(top, "other", "x.txt"), "x\nchanged outside\n")

	r, err := Open(ctx, project, ".reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if r.Prefix != "docs/" || r.Toplevel != top {
		t.Fatalf("prefix %q toplevel %q", r.Prefix, r.Toplevel)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Unborn || st.Operation != "" || len(st.OutsideStaged) != 0 {
		t.Fatalf("state = %+v", st)
	}
	want := []Change{{Path: "ch1.md", Kind: Modified}, {Path: "日本語.md", Kind: Added, Untracked: true}}
	if !slices.Equal(st.Changes, want) {
		t.Fatalf("changes = %+v", st.Changes)
	}
	if st.Upstream.Remote != "" {
		t.Fatalf("upstream = %+v", st.Upstream)
	}

	// Editing a file that is already modified changes the fingerprint.
	time.Sleep(10 * time.Millisecond)
	writeFile(t, filepath.Join(project, "ch1.md"), "# ch1\nmore\nand more\n")
	st2, _ := r.Status(ctx)
	if st2.Fingerprint == st.Fingerprint {
		t.Fatal("fingerprint did not change")
	}

	if _, err := r.Commit(ctx, "docs: update ch1\n"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, top, "log", "-1", "--format=%s"); got != "docs: update ch1\n" {
		t.Fatalf("subject = %q", got)
	}
	files := git(t, top, "-c", "core.quotePath=false", "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "docs/ch1.md") || !strings.Contains(files, "docs/日本語.md") || strings.Contains(files, ".reviewer") || strings.Contains(files, "other/") {
		t.Fatalf("committed files = %q", files)
	}
	// The change outside the project is still there and unstaged.
	if got := git(t, top, "status", "--porcelain"); got != " M other/x.txt\n?? docs/.reviewer/\n" {
		t.Fatalf("status after commit = %q", got)
	}
	b, _ := os.ReadFile(filepath.Join(top, "other", "x.txt"))
	if string(b) != "x\nchanged outside\n" {
		t.Fatalf("outside file = %q", b)
	}
}

func TestStatusReportsOutsideStagedAndOperations(t *testing.T) {
	top, project, _ := fixture(t)
	ctx := context.Background()
	writeFile(t, filepath.Join(top, "other", "x.txt"), "staged\n")
	git(t, top, "add", "other/x.txt")
	r, err := Open(ctx, project, ".reviewer")
	if err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(st.OutsideStaged, []string{"other/x.txt"}) {
		t.Fatalf("outside staged = %v", st.OutsideStaged)
	}
	writeFile(t, filepath.Join(r.GitDir, "MERGE_HEAD"), "0000\n")
	if st, _ := r.Status(ctx); st.Operation != "merge" {
		t.Fatalf("operation = %q", st.Operation)
	}
}

func TestOpenNotRepo(t *testing.T) {
	isolate(t)
	if _, err := Open(context.Background(), t.TempDir(), ""); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemotesAndPush(t *testing.T) {
	top, project, bare := fixture(t)
	ctx := context.Background()
	git(t, top, "fetch", "-q", "origin")
	git(t, top, "branch", "-q", "--set-upstream-to=origin/main")
	r, _ := Open(ctx, project, ".reviewer")
	remotes, err := r.Remotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 || remotes[0].Name != "origin" || !slices.Equal(remotes[0].Branches, []string{"main"}) || remotes[0].URL != bare {
		t.Fatalf("remotes = %+v", remotes)
	}
	st, _ := r.Status(ctx)
	if st.Upstream.Remote != "origin" || st.Upstream.Branch != "main" {
		t.Fatalf("upstream = %+v", st.Upstream)
	}

	writeFile(t, filepath.Join(project, "ch2.md"), "# ch2\n")
	if _, err := r.Commit(ctx, "docs: add ch2\n"); err != nil {
		t.Fatal(err)
	}
	if ahead, behind, ok := r.Divergence(ctx, "origin", "main"); !ok || ahead != 1 || behind != 0 {
		t.Fatalf("divergence = %d %d %v", ahead, behind, ok)
	}
	if _, err := r.Push(ctx, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, bare, "log", "-1", "--format=%s", "main"); got != "docs: add ch2\n" {
		t.Fatalf("remote head = %q", got)
	}

	// A diverged remote is rejected, never forced.
	other := filepath.Join(filepath.Dir(bare), "other")
	git(t, filepath.Dir(bare), "clone", "-q", bare, other)
	git(t, other, "-c", "user.name=O", "-c", "user.email=o@example.com", "commit", "-q", "--allow-empty", "-m", "remote work")
	git(t, other, "push", "-q", "origin", "main")
	writeFile(t, filepath.Join(project, "ch3.md"), "# ch3\n")
	if _, err := r.Commit(ctx, "docs: add ch3\n"); err != nil {
		t.Fatal(err)
	}
	_, err = r.Push(ctx, "origin", "main")
	if f := Classify(err, "origin", bare, Env{}); err == nil || f.Kind != FailNonFastForward {
		t.Fatalf("push over remote work: err=%v failure=%+v", err, f)
	}
	if got := git(t, bare, "log", "-1", "--format=%s", "main"); got != "remote work\n" {
		t.Fatalf("remote was overwritten: %q", got)
	}

	for _, bad := range [][2]string{{"-f", "main"}, {"origin", "+main"}, {"origin", "a:b"}, {"https://x.example/r.git", "main"}} {
		if _, err := r.Push(ctx, bad[0], bad[1]); err == nil {
			t.Errorf("push %v accepted", bad)
		}
	}
	if err := r.CheckBranchName(ctx, "feature/ok"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"-x", "a..b", "refs/heads/x", "a b", "x.lock"} {
		if err := r.CheckBranchName(ctx, bad); err == nil {
			t.Errorf("branch %q accepted", bad)
		}
	}
}

func TestPushFailureIsMasked(t *testing.T) {
	top, project, _ := fixture(t)
	ctx := context.Background()
	// Nothing listens on port 1; the URL carries a token that must not leak.
	git(t, top, "remote", "add", "leaky", "https://user:ghp_abcdefghijklmnopqrstuvwxyz0123456789@127.0.0.1:1/r.git")
	r, _ := Open(ctx, project, ".reviewer")
	remotes, _ := r.Remotes(ctx)
	for _, rm := range remotes {
		if strings.Contains(rm.URL, "ghp_") || strings.Contains(rm.URL, "user:") {
			t.Fatalf("remote URL not masked: %q", rm.URL)
		}
	}
	_, err := r.Push(ctx, "leaky", "main")
	if err == nil {
		t.Fatal("push succeeded")
	}
	f := Classify(err, "leaky", remotes[0].URL, Env{})
	for _, s := range []string{err.Error(), f.Detail, f.Message} {
		if strings.Contains(s, "ghp_") || strings.Contains(s, "abcdefghijklmnop") {
			t.Fatalf("secret leaked: %q", s)
		}
	}
	if f.Kind != FailNetwork {
		t.Fatalf("kind = %s (%s)", f.Kind, f.Detail)
	}
}

func TestEnvironmentDisablesPrompts(t *testing.T) {
	r := &Runner{Env: []string{"PATH=/bin", "GIT_DIR=/elsewhere", "GIT_ASKPASS=/usr/bin/ksshaskpass", "SSH_AUTH_SOCK=/run/agent", "LANG=ja_JP.UTF-8"}}
	env := r.environ()
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=", "SSH_ASKPASS_REQUIRE=never", "GCM_INTERACTIVE=never", "LC_ALL=C", "SSH_AUTH_SOCK=/run/agent", "PATH=/bin"} {
		if !slices.Contains(env, want) {
			t.Errorf("missing %s in %v", want, env)
		}
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_DIR=") || kv == "GIT_ASKPASS=/usr/bin/ksshaskpass" || strings.HasPrefix(kv, "LANG=") {
			t.Errorf("kept %s", kv)
		}
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "git")
	// A fake git that starts a child and hangs, like ssh waiting for input.
	writeFile(t, bin, "#!/bin/sh\nsleep 30 &\nsleep 30\n")
	os.Chmod(bin, 0o755)
	r := &Runner{Dir: dir, Bin: bin}
	start := time.Now()
	_, err := r.Run(context.Background(), 200*time.Millisecond, nil, "push")
	var ce *CmdError
	if !errors.As(err, &ce) || !ce.TimedOut {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
	if f := Classify(err, "origin", "git@github.com:o/r.git", Env{}); f.Kind != FailTimeout {
		t.Fatalf("kind = %s", f.Kind)
	}
}

func TestStderrIsCapped(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "git")
	writeFile(t, bin, "#!/bin/sh\nhead -c 100000 /dev/zero | tr '\\0' a >&2\necho 'fatal: token=s3cr3t' >&2\nexit 1\n")
	os.Chmod(bin, 0o755)
	_, err := (&Runner{Dir: dir, Bin: bin}).Run(context.Background(), 5*time.Second, nil, "status")
	var ce *CmdError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if len(ce.Stderr) > maxStderr+8 || !strings.HasSuffix(strings.TrimSpace(ce.Stderr), "token=***") {
		t.Fatalf("stderr len %d tail %q", len(ce.Stderr), ce.Stderr[len(ce.Stderr)-30:])
	}
}

func TestPushRefusesMirrorAndReportsRemoteMessages(t *testing.T) {
	top, project, bare := fixture(t)
	ctx := context.Background()
	// A post-receive hook stands in for a forge printing a pull request link.
	writeFile(t, filepath.Join(bare, "hooks", "post-receive"), "#!/bin/sh\necho 'Create a pull request: https://example.com/pr?token=s3cr3t'\n")
	os.Chmod(filepath.Join(bare, "hooks", "post-receive"), 0o755)
	r, _ := Open(ctx, project, ".reviewer")
	writeFile(t, filepath.Join(project, "ch2.md"), "# ch2\n")
	if _, err := r.Commit(ctx, "docs: add ch2\n"); err != nil {
		t.Fatal(err)
	}
	out, err := r.Push(ctx, "origin", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Create a pull request: https://example.com/pr") || strings.Contains(out, "s3cr3t") {
		t.Fatalf("remote messages = %q", out)
	}

	// remote.<name>.mirror would force-push every ref; it is refused.
	git(t, top, "config", "remote.origin.mirror", "true")
	remotes, err := r.Remotes(ctx)
	if err != nil || len(remotes) != 1 || !remotes[0].Mirror {
		t.Fatalf("remotes = %+v, %v", remotes, err)
	}
	if _, err := r.Push(ctx, "origin", "main"); !errors.Is(err, ErrMirror) {
		t.Fatalf("mirror push: %v", err)
	}
	git(t, top, "config", "remote.origin.mirror", "not-a-bool")
	if _, err := r.Push(ctx, "origin", "main"); !errors.Is(err, ErrMirror) {
		t.Fatalf("unreadable mirror setting: %v", err)
	}
}

func TestCommitChecksIndexAfterStaging(t *testing.T) {
	top, project, _ := fixture(t)
	ctx := context.Background()
	writeFile(t, filepath.Join(project, "ch1.md"), "# ch1\nedited\n")
	writeFile(t, filepath.Join(top, "other", "x.txt"), "staged elsewhere\n")
	git(t, top, "add", "other/x.txt")
	r, _ := Open(ctx, project, ".reviewer")
	head := git(t, top, "rev-parse", "HEAD")
	if _, err := r.Commit(ctx, "docs: x\n"); !errors.Is(err, ErrOutsideStaged) {
		t.Fatalf("err = %v", err)
	}
	if got := git(t, top, "rev-parse", "HEAD"); got != head {
		t.Fatal("a commit was created")
	}
}

func TestFirstCommitOnUnbornBranch(t *testing.T) {
	isolate(t)
	ctx := context.Background()
	top, _ := filepath.EvalSymlinks(t.TempDir())
	git(t, top, "init", "-q", "-b", "main")
	git(t, top, "config", "user.name", "Tester")
	git(t, top, "config", "user.email", "tester@example.com")
	writeFile(t, filepath.Join(top, "a.md"), "a\n")
	writeFile(t, filepath.Join(top, ".reviewer", "state.json"), "{}\n")
	r, err := Open(ctx, top, ".reviewer")
	if err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Unborn || st.Branch != "main" || !slices.Equal(st.Changes, []Change{{Path: "a.md", Kind: Added, Untracked: true}}) {
		t.Fatalf("state = %+v", st)
	}
	if _, err := r.Commit(ctx, "docs: add a.md\n"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, top, "show", "--name-only", "--format=", "HEAD"); got != "a.md\n" {
		t.Fatalf("committed = %q", got)
	}
}
