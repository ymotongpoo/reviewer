package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/gitops"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// gitSetup creates a repository with a bare origin and opens docs/ as the
// project. The data directory docs/.reviewer is not ignored on purpose.
func gitSetup(t *testing.T) (a *App, top, bare string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	base, _ := filepath.EvalSymlinks(t.TempDir())
	top, bare = filepath.Join(base, "work"), filepath.Join(base, "origin.git")
	runGit(t, base, "init", "-q", "--bare", "-b", "main", bare)
	runGit(t, base, "init", "-q", "-b", "main", top)
	runGit(t, top, "config", "user.name", "Tester")
	runGit(t, top, "config", "user.email", "tester@example.com")
	write(t, top, "docs/ch1.md", "# ch1\n")
	write(t, top, "src/main.go", "package main\n")
	runGit(t, top, "add", "-A")
	runGit(t, top, "commit", "-q", "-m", "init")
	runGit(t, top, "remote", "add", "origin", bare)
	runGit(t, top, "push", "-q", "-u", "origin", "main")
	a, err := New(filepath.Join(top, "docs"), config.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	return a, top, bare
}

func errCode(err error) int {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return 0
}

func TestGitCommitAndPush(t *testing.T) {
	a, top, bare := gitSetup(t)
	ctx := context.Background()
	if !a.Info().Git {
		t.Fatal("Info().Git = false")
	}
	write(t, top, "docs/ch1.md", "# ch1\nedited\n")
	write(t, top, "src/main.go", "package main\n\n// untouched by reviewer\n")

	st, err := a.GitStatus(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Changes) != 1 || st.Changes[0].Path != "ch1.md" || st.Prefix != "docs/" {
		t.Fatalf("changes = %+v prefix %q", st.Changes, st.Prefix)
	}
	if st.Target.Remote != "origin" || st.Target.Branch != "main" || !st.Target.Tracked || st.Target.Ahead != 0 {
		t.Fatalf("target = %+v", st.Target)
	}
	if len(st.CommitBlockers) != 0 || len(st.PushBlockers) != 0 {
		t.Fatalf("blockers = %v %v", st.CommitBlockers, st.PushBlockers)
	}
	if st.Messages["ja"] != "docs: ch1.md を更新\n" || st.Messages["en"] != "docs: update ch1.md\n" {
		t.Fatalf("messages = %q", st.Messages)
	}

	if _, err := a.GitCommit(ctx, GitCommitInput{Message: "update ch1", Fingerprint: st.Fingerprint}); errCode(err) != http.StatusBadRequest {
		t.Fatalf("non-conventional message: %v", err)
	}
	if _, err := a.GitCommit(ctx, GitCommitInput{Message: st.Messages["en"], Fingerprint: "stale"}); errCode(err) != http.StatusConflict {
		t.Fatalf("stale fingerprint: %v", err)
	}
	res, err := a.GitCommit(ctx, GitCommitInput{Message: st.Messages["en"], Fingerprint: st.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if res.Subject != "docs: update ch1.md" || res.Branch != "main" || res.Files != 1 {
		t.Fatalf("commit = %+v", res)
	}
	// The data directory and the change outside the project stay out.
	files := runGit(t, top, "show", "--name-only", "--format=", "HEAD")
	if files != "docs/ch1.md\n" {
		t.Fatalf("committed = %q", files)
	}
	if got := runGit(t, top, "status", "--porcelain", "--", "src"); got != " M src/main.go\n" {
		t.Fatalf("outside change = %q", got)
	}

	st, _ = a.GitStatus(ctx, "", "")
	if len(st.Changes) != 0 || st.Target.Ahead != 1 {
		t.Fatalf("after commit: changes %+v target %+v", st.Changes, st.Target)
	}
	if _, err := a.GitCommit(ctx, GitCommitInput{Message: "docs: x", Fingerprint: st.Fingerprint}); errCode(err) != http.StatusConflict {
		t.Fatalf("empty commit: %v", err)
	}

	push, err := a.GitPush(ctx, GitPushInput{Remote: "origin", Branch: "main"})
	if err != nil || !push.OK {
		t.Fatalf("push = %+v, %v", push, err)
	}
	if got := runGit(t, bare, "log", "-1", "--format=%s", "main"); got != "docs: update ch1.md\n" {
		t.Fatalf("remote = %q", got)
	}
}

func TestGitPushTargets(t *testing.T) {
	a, top, _ := gitSetup(t)
	ctx := context.Background()
	runGit(t, top, "remote", "add", "backup", filepath.Join(filepath.Dir(top), "missing.git"))

	// Branches are limited to the current one and the remote's branches.
	if _, err := a.GitStatus(ctx, "origin", "release"); errCode(err) != http.StatusBadRequest {
		t.Fatalf("unknown branch: %v", err)
	}
	if _, err := a.GitStatus(ctx, "https://example.com/r.git", ""); errCode(err) != http.StatusBadRequest {
		t.Fatalf("URL as remote: %v", err)
	}
	if _, err := a.GitPush(ctx, GitPushInput{Remote: "elsewhere", Branch: "main"}); errCode(err) != http.StatusBadRequest {
		t.Fatalf("unknown remote: %v", err)
	}
	st, err := a.GitStatus(ctx, "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Target.Remote != "backup" || st.Target.Branch != "main" || st.Target.Tracked {
		t.Fatalf("one-off target = %+v", st.Target)
	}

	// A failed push is a result with an explanation, not an error.
	res, err := a.GitPush(ctx, GitPushInput{Remote: "backup", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Failure == nil || res.Failure.Kind != gitops.FailNotFound || len(res.Failure.Hints) == 0 {
		t.Fatalf("push = %+v", res)
	}

	// Saved defaults are used until the dialog picks something else.
	if _, err := a.SaveGitSettings(ctx, config.GitConfig{Language: "en", Remote: "nope"}); errCode(err) != http.StatusBadRequest {
		t.Fatalf("unknown default remote: %v", err)
	}
	if _, err := a.SaveGitSettings(ctx, config.GitConfig{Language: "fr"}); errCode(err) != http.StatusBadRequest {
		t.Fatalf("bad language: %v", err)
	}
	g, err := a.SaveGitSettings(ctx, config.GitConfig{Language: "en", Remote: "backup", Branch: "main"})
	if err != nil || g.Language != "en" || g.Remote != "backup" {
		t.Fatalf("settings = %+v, %v", g, err)
	}
	st, _ = a.GitStatus(ctx, "", "")
	if st.Target.Remote != "backup" || st.Settings.Language != "en" {
		t.Fatalf("default target = %+v settings %+v", st.Target, st.Settings)
	}
	if b, err := os.ReadFile(filepath.Join(a.DataDir, "git.toml")); err != nil || !strings.Contains(string(b), `remote = "backup"`) {
		t.Fatalf("git.toml = %q, %v", b, err)
	}
}

func TestGitCommitRefusesOutsideStaged(t *testing.T) {
	a, top, _ := gitSetup(t)
	ctx := context.Background()
	write(t, top, "docs/ch1.md", "# ch1\nedited\n")
	write(t, top, "src/main.go", "package main // staged\n")
	runGit(t, top, "add", "src/main.go")
	st, err := a.GitStatus(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.CommitBlockers) != 1 || !strings.Contains(st.CommitBlockers[0], "src/main.go") {
		t.Fatalf("blockers = %v", st.CommitBlockers)
	}
	if _, err := a.GitCommit(ctx, GitCommitInput{Message: "docs: x", Fingerprint: st.Fingerprint}); errCode(err) != http.StatusConflict {
		t.Fatalf("commit: %v", err)
	}
	// Nothing was staged or committed.
	if got := runGit(t, top, "status", "--porcelain"); got != " M docs/ch1.md\nM  src/main.go\n?? docs/.reviewer/\n" {
		t.Fatalf("status = %q", got)
	}
}

func TestGitNotRepoAndLanguage(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	a, _ := setup(t)
	st, err := a.GitStatus(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Repo || len(st.CommitBlockers) == 0 || a.Info().Git {
		t.Fatalf("status = %+v", st)
	}
	if err := a.InitGitLanguage("de"); errCode(err) != http.StatusBadRequest {
		t.Fatalf("bad language: %v", err)
	}
	if err := a.InitGitLanguage("en"); err != nil {
		t.Fatal(err)
	}
	// The choice at creation does not override later settings.
	if err := a.InitGitLanguage("ja"); err != nil {
		t.Fatal(err)
	}
	if g := a.GitSettings(); g.Language != "en" {
		t.Fatalf("language = %s", g.Language)
	}
}

func TestGitPushFailureKeepsCommit(t *testing.T) {
	a, top, bare := gitSetup(t)
	ctx := context.Background()
	// Another clone moves origin/main ahead; reviewer must not overwrite it.
	other := filepath.Join(filepath.Dir(bare), "other")
	runGit(t, filepath.Dir(bare), "clone", "-q", bare, other)
	runGit(t, other, "-c", "user.name=O", "-c", "user.email=o@example.com", "commit", "-q", "--allow-empty", "-m", "remote work")
	runGit(t, other, "push", "-q", "origin", "main")
	runGit(t, top, "fetch", "-q", "origin")

	write(t, top, "docs/ch2.md", "# ch2\n")
	st, err := a.GitStatus(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Target.Behind != 1 {
		t.Fatalf("target = %+v", st.Target)
	}
	res, err := a.GitCommit(ctx, GitCommitInput{Message: st.Messages["ja"], Fingerprint: st.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	push, err := a.GitPush(ctx, GitPushInput{Remote: "origin", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if push.OK || push.Failure == nil || push.Failure.Kind != gitops.FailNonFastForward {
		t.Fatalf("push = %+v", push)
	}
	// The commit stays; the remote keeps its own history.
	if head := strings.TrimSpace(runGit(t, top, "rev-parse", "HEAD")); head != res.Commit {
		t.Fatalf("HEAD %s, commit %s", head, res.Commit)
	}
	if got := runGit(t, bare, "log", "-1", "--format=%s", "main"); got != "remote work\n" {
		t.Fatalf("remote = %q", got)
	}
	st, _ = a.GitStatus(ctx, "", "")
	if len(st.Changes) != 0 || st.Target.Ahead != 1 || st.Target.Behind != 1 || len(st.PushBlockers) != 0 {
		t.Fatalf("after failed push: %+v %v", st.Target, st.PushBlockers)
	}
}

func TestGitBlockers(t *testing.T) {
	a, top, _ := gitSetup(t)
	ctx := context.Background()
	runGit(t, top, "config", "remote.origin.mirror", "true")
	st, err := a.GitStatus(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PushBlockers) != 1 || !strings.Contains(st.PushBlockers[0], "mirror") {
		t.Fatalf("push blockers = %v", st.PushBlockers)
	}
	if _, err := a.GitPush(ctx, GitPushInput{Remote: "origin", Branch: "main"}); errCode(err) != http.StatusConflict {
		t.Fatalf("mirror push: %v", err)
	}
	runGit(t, top, "config", "--unset", "remote.origin.mirror")

	runGit(t, top, "checkout", "-q", "--detach")
	write(t, top, "docs/ch1.md", "# ch1\ndetached\n")
	st, _ = a.GitStatus(ctx, "", "")
	if len(st.CommitBlockers) == 0 || len(st.PushBlockers) == 0 || st.Branch != "" {
		t.Fatalf("detached: %v %v", st.CommitBlockers, st.PushBlockers)
	}
	if _, err := a.GitCommit(ctx, GitCommitInput{Message: "docs: x", Fingerprint: st.Fingerprint}); errCode(err) != http.StatusConflict {
		t.Fatalf("detached commit: %v", err)
	}
}

func TestGitSettingsMergeConfig(t *testing.T) {
	a, _, _ := gitSetup(t)
	ctx := context.Background()
	a.Cfg.Git = config.GitConfig{Language: "en", Remote: "origin", Branch: "main"}
	if g := a.GitSettings(); g != a.Cfg.Git {
		t.Fatalf("defaults = %+v", g)
	}
	// git.toml overrides only what it sets.
	if _, err := a.SaveGitSettings(ctx, config.GitConfig{Language: "ja"}); err != nil {
		t.Fatal(err)
	}
	if g := a.GitSettings(); g.Language != "ja" || g.Remote != "origin" || g.Branch != "main" {
		t.Fatalf("merged = %+v", g)
	}
	// An unusable language from config.toml falls back to Japanese.
	a.Cfg.Git.Language = "fr"
	os.Remove(filepath.Join(a.DataDir, "git.toml"))
	if g := a.GitSettings(); g.Language != "ja" {
		t.Fatalf("fallback = %+v", g)
	}
}

func TestSaveFileChecksHashAndReanchors(t *testing.T) {
	a, top, _ := gitSetup(t)
	write(t, top, "docs/ch1.md", "# ch1\nline\n")
	view, err := a.File("ch1.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveFile(SaveFileInput{Path: "ch1.md", Content: "# ch1\nchanged\n", Hash: "stale"}); errCode(err) != http.StatusConflict {
		t.Fatalf("stale save = %v", err)
	}
	res, err := a.SaveFile(SaveFileInput{Path: "ch1.md", Content: "# ch1\nchanged\n", Hash: view.Hash})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.File("ch1.md")
	if err != nil || got.Content != "# ch1\nchanged\n" {
		t.Fatalf("saved file = %+v, %v", got, err)
	}
	if res.Hash != got.Hash {
		t.Fatalf("returned hash %s, file hash %s", res.Hash, got.Hash)
	}
	// The returned hash is usable as the base of the next save.
	if _, err := a.SaveFile(SaveFileInput{Path: "ch1.md", Content: "# ch1\nagain\n", Hash: res.Hash}); err != nil {
		t.Fatalf("save with returned hash: %v", err)
	}
}

func TestSaveFileConflictKeepsExternalChange(t *testing.T) {
	a, top, _ := gitSetup(t)
	write(t, top, "docs/ch1.md", "# ch1\nline\n")
	view, err := a.File("ch1.md")
	if err != nil {
		t.Fatal(err)
	}
	// Another program writes while the reviewer is editing.
	write(t, top, "docs/ch1.md", "# ch1\nexternal\n")
	if _, err := a.SaveFile(SaveFileInput{Path: "ch1.md", Content: "# ch1\nmine\n", Hash: view.Hash}); errCode(err) != http.StatusConflict {
		t.Fatalf("save over external change = %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(top, "docs/ch1.md"))
	if string(b) != "# ch1\nexternal\n" {
		t.Fatalf("external change overwritten: %q", b)
	}
}

func TestSaveFileReanchorsCommentsBelowInsertedLines(t *testing.T) {
	a, top, _ := gitSetup(t)
	write(t, top, "docs/ch1.md", "# ch1\n\nfirst\n\nsecond paragraph\n\nend\n")
	view, err := a.File("ch1.md")
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.CreateComment(NewComment{Scope: "line", Path: "ch1.md", Start: 5, End: 5, Label: "must", Body: "x", Hash: view.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveFile(SaveFileInput{Path: "ch1.md", Content: "# ch1\n\nfirst\nadded\nadded too\n\nsecond paragraph\n\nend\n", Hash: view.Hash}); err != nil {
		t.Fatal(err)
	}
	got, _ := a.Store.Comment(c.ID)
	if got.Loc == nil || got.Loc.Start != 7 || got.Loc.End != 7 {
		t.Fatalf("loc after save = %+v", got.Loc)
	}
}
