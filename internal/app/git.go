package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/gitops"
)

// GitTarget is the remote branch a push goes to.
type GitTarget struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	// Tracked tells whether a remote-tracking ref exists for the branch;
	// Ahead and Behind are only meaningful then.
	Tracked bool `json:"tracked"`
	Ahead   int  `json:"ahead"`
	Behind  int  `json:"behind"`
}

// GitStatus is what the commit dialog shows.
type GitStatus struct {
	Repo     bool   `json:"repo"`
	Toplevel string `json:"toplevel,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	*gitops.State
	Identity string `json:"identity,omitempty"`
	// Messages are generated commit messages by language.
	Messages map[string]string `json:"messages"`
	// Settings are the saved project defaults (language and push target).
	Settings config.GitConfig `json:"settings"`
	Remotes  []gitops.Remote  `json:"remotes"`
	Target   GitTarget        `json:"target"`
	// CommitBlockers and PushBlockers explain why an operation is not
	// possible right now.
	CommitBlockers []string   `json:"commitBlockers"`
	PushBlockers   []string   `json:"pushBlockers"`
	Env            gitops.Env `json:"env"`
}

// openRepo opens the project's repository; the data directory is excluded
// from every commit.
func (a *App) openRepo(ctx context.Context) (*gitops.Repo, error) {
	exclude, _ := a.Proj.DataDirInside()
	return gitops.Open(ctx, a.Proj.Root, exclude)
}

// gitRepo reports whether the project is inside a Git work tree. It is
// checked once and updated by every GitStatus.
func (a *App) gitRepo() bool {
	a.gitOnce.Do(func() {
		_, err := a.openRepo(context.Background())
		a.isRepo.Store(err == nil)
	})
	return a.isRepo.Load()
}

// GitSettings returns the configured defaults overridden by the project's
// git.toml.
func (a *App) GitSettings() config.GitConfig {
	g := a.Cfg.Git
	if saved, ok, err := a.Store.GitSettings(); err == nil && ok {
		if saved.Language != "" {
			g.Language = saved.Language
		}
		if saved.Remote != "" {
			g.Remote = saved.Remote
		}
		if saved.Branch != "" {
			g.Branch = saved.Branch
		}
	}
	if !gitops.ValidLanguage(g.Language) {
		g.Language = gitops.LangJA
	}
	return g
}

// InitGitLanguage records the commit message language chosen when the
// review data is created. An existing git.toml is left alone.
func (a *App) InitGitLanguage(lang string) error {
	if lang == "" {
		return nil
	}
	if !gitops.ValidLanguage(lang) {
		return badRequest("コミットメッセージの言語は ja か en です: %s", lang)
	}
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	if _, ok, err := a.Store.GitSettings(); err != nil || ok {
		return err
	}
	return a.Store.SaveGitSettings(config.GitConfig{Language: lang})
}

func remoteNamed(remotes []gitops.Remote, name string) *gitops.Remote {
	i := slices.IndexFunc(remotes, func(r gitops.Remote) bool { return r.Name == name })
	if i < 0 {
		return nil
	}
	return &remotes[i]
}

// allowedBranch reports whether branch may be pushed to on remote: the
// current branch, or one the remote is known to have.
func allowedBranch(st *gitops.State, remote *gitops.Remote, branch string) bool {
	if branch == "" {
		return false
	}
	if branch == st.Branch {
		return true
	}
	return remote != nil && slices.Contains(remote.Branches, branch)
}

// resolveTarget picks the push target: the requested one, then the saved
// defaults, then the upstream of the current branch.
func resolveTarget(st *gitops.State, remotes []gitops.Remote, settings config.GitConfig, remote, branch string) (GitTarget, error) {
	if remote != "" {
		if remoteNamed(remotes, remote) == nil {
			return GitTarget{}, badRequest("remote がありません: %s", remote)
		}
	} else {
		for _, name := range []string{settings.Remote, st.Upstream.Remote, "origin"} {
			if name != "" && remoteNamed(remotes, name) != nil {
				remote = name
				break
			}
		}
		if remote == "" && len(remotes) > 0 {
			remote = remotes[0].Name
		}
	}
	if remote == "" {
		return GitTarget{}, nil
	}
	rm := remoteNamed(remotes, remote)
	if branch != "" {
		if !allowedBranch(st, rm, branch) {
			return GitTarget{}, badRequest("push 先のブランチは、現在のブランチか %s の既存のブランチから選んでください: %s", remote, branch)
		}
	} else {
		candidates := []string{settings.Branch}
		if remote == st.Upstream.Remote {
			candidates = append(candidates, st.Upstream.Branch)
		}
		for _, name := range append(candidates, st.Branch) {
			if allowedBranch(st, rm, name) {
				branch = name
				break
			}
		}
	}
	return GitTarget{Remote: remote, Branch: branch}, nil
}

// GitStatus inspects the repository. remote and branch select a push
// target other than the default for this operation only.
func (a *App) GitStatus(ctx context.Context, remote, branch string) (*GitStatus, error) {
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	out := &GitStatus{
		Messages: map[string]string{}, Settings: a.GitSettings(), Remotes: []gitops.Remote{},
		CommitBlockers: []string{}, PushBlockers: []string{},
		Env: gitops.Env{SSHAgent: sshAgent()},
	}
	repo, err := a.openRepo(ctx)
	if err == nil || errors.Is(err, gitops.ErrNotRepo) {
		a.gitOnce.Do(func() {})
		a.isRepo.Store(err == nil)
	}
	if errors.Is(err, gitops.ErrNotRepo) {
		out.State = &gitops.State{Changes: []gitops.Change{}, OutsideStaged: []string{}}
		out.CommitBlockers = append(out.CommitBlockers, "このディレクトリは Git のリポジトリの中にありません")
		out.PushBlockers = append(out.PushBlockers, "このディレクトリは Git のリポジトリの中にありません")
		return out, nil
	}
	if err != nil {
		return nil, gitError("Git を実行できませんでした", err)
	}
	out.Repo, out.Toplevel, out.Prefix = true, repo.Toplevel, repo.Prefix
	st, err := repo.Status(ctx)
	if err != nil {
		return nil, gitError("Git の状態を読めませんでした", err)
	}
	out.State = st
	out.Env.CredentialHelper = repo.CredentialHelper(ctx)
	if ident, err := repo.Identity(ctx); err == nil {
		out.Identity = ident
	}
	if remotes, err := repo.Remotes(ctx); err == nil {
		out.Remotes = remotes
	} else {
		return nil, gitError("remote の一覧を読めませんでした", err)
	}
	if out.Target, err = resolveTarget(st, out.Remotes, out.Settings, remote, branch); err != nil {
		return nil, err
	}
	if out.Target.Remote != "" && out.Target.Branch != "" && !st.Unborn {
		out.Target.Ahead, out.Target.Behind, out.Target.Tracked = repo.Divergence(ctx, out.Target.Remote, out.Target.Branch)
		if !out.Target.Tracked {
			out.Target.Ahead = repo.CountCommits(ctx)
		}
	}
	for _, lang := range []string{gitops.LangJA, gitops.LangEN} {
		out.Messages[lang] = gitops.Message(st.Changes, lang)
	}
	out.CommitBlockers = commitBlockers(st, out.Identity)
	out.PushBlockers = pushBlockers(st, out.Remotes, out.Target)
	return out, nil
}

func sshAgent() bool {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return false
	}
	_, err := os.Stat(sock)
	return err == nil
}

func commitBlockers(st *gitops.State, identity string) []string {
	out := []string{}
	if st.Operation != "" {
		out = append(out, fmt.Sprintf("%s の途中です。ターミナルで完了するか中止してから、もう一度開いてください", st.Operation))
	}
	if st.Branch == "" {
		out = append(out, "HEAD がブランチを指していません（detached HEAD）。ターミナルでブランチに切り替えてください")
	}
	if slices.ContainsFunc(st.Changes, func(c gitops.Change) bool { return c.Kind == gitops.Conflict }) {
		out = append(out, "コンフリクトしているファイルがあります。ターミナルで解決してください")
	}
	if len(st.OutsideStaged) > 0 {
		out = append(out, fmt.Sprintf("このプロジェクトの外（またはデータディレクトリ）にステージ済みの変更があり、コミットに含まれてしまいます: %s。ターミナルで `git restore --staged <path>` するか、先にコミットしてください", summarize(st.OutsideStaged, 5)))
	}
	if identity == "" {
		out = append(out, "コミットする人の名前とメールアドレスが Git に設定されていません。`git config --global user.name \"名前\"` と `git config --global user.email you@example.com` で設定してください")
	}
	return out
}

func pushBlockers(st *gitops.State, remotes []gitops.Remote, t GitTarget) []string {
	out := []string{}
	rm := remoteNamed(remotes, t.Remote)
	switch {
	case len(remotes) == 0:
		out = append(out, "remote が設定されていません。ターミナルで `git remote add origin <URL>` を実行してください")
	case rm != nil && rm.Mirror:
		out = append(out, fmt.Sprintf("%s は mirror として設定されています（remote.%s.mirror）。mirror への push は強制 push になるため、reviewer からは push しません", t.Remote, t.Remote))
	case st.Branch == "":
		out = append(out, "HEAD がブランチを指していません（detached HEAD）")
	case st.Unborn:
		out = append(out, "まだコミットがありません")
	case t.Branch == "":
		out = append(out, "push 先のブランチを選んでください")
	}
	return out
}

func summarize(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" ほか%d件", len(items)-n)
}

// gitError wraps a git failure; the message is already masked.
func gitError(msg string, err error) error {
	return &Error{Code: http.StatusInternalServerError, Msg: msg + ": " + gitops.Mask(err.Error())}
}

// SaveGitSettings stores the project defaults. remote must be a configured
// remote and branch the current branch or one of the remote's branches.
func (a *App) SaveGitSettings(ctx context.Context, g config.GitConfig) (config.GitConfig, error) {
	if !gitops.ValidLanguage(g.Language) {
		return config.GitConfig{}, badRequest("コミットメッセージの言語は ja か en です: %s", g.Language)
	}
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	if g.Remote != "" || g.Branch != "" {
		repo, err := a.openRepo(ctx)
		if err != nil {
			return config.GitConfig{}, badRequest("Git のリポジトリではないため、push 先は保存できません")
		}
		st, err := repo.Status(ctx)
		if err != nil {
			return config.GitConfig{}, gitError("Git の状態を読めませんでした", err)
		}
		remotes, err := repo.Remotes(ctx)
		if err != nil {
			return config.GitConfig{}, gitError("remote の一覧を読めませんでした", err)
		}
		rm := remoteNamed(remotes, g.Remote)
		if g.Remote != "" && rm == nil {
			return config.GitConfig{}, badRequest("remote がありません: %s", g.Remote)
		}
		if g.Branch != "" {
			if !allowedBranch(st, rm, g.Branch) {
				return config.GitConfig{}, badRequest("既定のブランチは、現在のブランチか remote の既存のブランチから選んでください: %s", g.Branch)
			}
			if err := repo.CheckBranchName(ctx, g.Branch); err != nil {
				return config.GitConfig{}, badRequest("ブランチ名が不正です: %s", g.Branch)
			}
		}
	}
	if err := a.Store.SaveGitSettings(g); err != nil {
		return config.GitConfig{}, err
	}
	a.notify(Event{Type: "git"})
	return a.GitSettings(), nil
}

// GitCommitInput is a commit request. Fingerprint is the one the dialog
// showed; the commit is refused when the changes differ from it.
type GitCommitInput struct {
	Message     string `json:"message"`
	Fingerprint string `json:"fingerprint"`
}

// GitCommitResult describes a new commit.
type GitCommitResult struct {
	Commit  string `json:"commit"`
	Branch  string `json:"branch"`
	Subject string `json:"subject"`
	Files   int    `json:"files"`
}

// GitCommit stages the project's changes and commits them. Changes outside
// the project and the data directory are never staged.
func (a *App) GitCommit(ctx context.Context, in GitCommitInput) (*GitCommitResult, error) {
	msg, err := gitops.ValidateMessage(in.Message)
	if err != nil {
		return nil, badRequest("%s", err.Error())
	}
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	repo, err := a.openRepo(ctx)
	if errors.Is(err, gitops.ErrNotRepo) {
		return nil, conflict("このディレクトリは Git のリポジトリの中にありません")
	}
	if err != nil {
		return nil, gitError("Git を実行できませんでした", err)
	}
	st, err := repo.Status(ctx)
	if err != nil {
		return nil, gitError("Git の状態を読めませんでした", err)
	}
	ident, _ := repo.Identity(ctx)
	if b := commitBlockers(st, ident); len(b) > 0 {
		return nil, conflict("%s", b[0])
	}
	if len(st.Changes) == 0 {
		return nil, conflict("コミットする変更がありません")
	}
	if in.Fingerprint != st.Fingerprint {
		return nil, conflict("確認したあとに変更内容が変わりました。内容を確認し直してください")
	}
	if err := repo.Stage(ctx); err != nil {
		return nil, &Error{Code: http.StatusUnprocessableEntity, Msg: "変更をステージできませんでした（ファイルの内容は変わっていません）: " + gitops.Mask(err.Error())}
	}
	// Something else may have staged files since Status; git would commit them.
	if outside, err := repo.OutsideStaged(ctx, st.Unborn); err != nil || len(outside) > 0 {
		if err != nil {
			return nil, gitError("ステージした内容を確認できませんでした（変更はステージされた状態で残っています）", err)
		}
		return nil, conflict("このプロジェクトの外にステージ済みの変更が増えたため、コミットを中止しました（このプロジェクトの変更はステージされた状態で残っています）: %s", summarize(outside, 5))
	}
	commit, err := repo.CommitStaged(ctx, msg)
	if err != nil {
		return nil, &Error{Code: http.StatusUnprocessableEntity, Msg: "コミットできませんでした（変更はステージされた状態で残っています。ファイルの内容は変わっていません）: " + gitops.Mask(err.Error())}
	}
	subject, _, _ := strings.Cut(msg, "\n")
	a.notify(Event{Type: "git"})
	return &GitCommitResult{Commit: commit, Branch: st.Branch, Subject: subject, Files: len(st.Changes)}, nil
}

// GitPushInput selects the push target by remote name and branch name.
type GitPushInput struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

// GitPushResult is the outcome of a push. A failed push is reported in
// Failure rather than as an error, so that the UI can show the fix.
type GitPushResult struct {
	OK     bool   `json:"ok"`
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	Commit string `json:"commit,omitempty"`
	// Messages are the remote's masked "remote:" lines of a successful
	// push, such as a link to open a pull request.
	Messages string          `json:"messages,omitempty"`
	Failure  *gitops.Failure `json:"failure,omitempty"`
}

// GitPush pushes HEAD to a branch of a configured remote without force.
func (a *App) GitPush(ctx context.Context, in GitPushInput) (*GitPushResult, error) {
	if in.Remote == "" || in.Branch == "" {
		return nil, badRequest("push 先の remote とブランチを選んでください")
	}
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	repo, err := a.openRepo(ctx)
	if errors.Is(err, gitops.ErrNotRepo) {
		return nil, conflict("このディレクトリは Git のリポジトリの中にありません")
	}
	if err != nil {
		return nil, gitError("Git を実行できませんでした", err)
	}
	st, err := repo.Status(ctx)
	if err != nil {
		return nil, gitError("Git の状態を読めませんでした", err)
	}
	remotes, err := repo.Remotes(ctx)
	if err != nil {
		return nil, gitError("remote の一覧を読めませんでした", err)
	}
	target, err := resolveTarget(st, remotes, a.GitSettings(), in.Remote, in.Branch)
	if err != nil {
		return nil, err
	}
	if b := pushBlockers(st, remotes, target); len(b) > 0 {
		return nil, conflict("%s", b[0])
	}
	if err := repo.CheckBranchName(ctx, target.Branch); err != nil {
		return nil, badRequest("ブランチ名が不正です: %s", target.Branch)
	}
	res := &GitPushResult{Remote: target.Remote, Branch: target.Branch, Commit: st.Head}
	msgs, err := repo.Push(ctx, target.Remote, target.Branch)
	if errors.Is(err, gitops.ErrMirror) {
		return nil, conflict("%s は mirror として設定されているため push しません", target.Remote)
	}
	if err != nil {
		url := remoteNamed(remotes, target.Remote).URL
		res.Failure = gitops.Classify(err, target.Remote, url, gitops.Env{SSHAgent: sshAgent(), CredentialHelper: repo.CredentialHelper(ctx)})
		return res, nil
	}
	res.OK, res.Messages = true, msgs
	a.notify(Event{Type: "git"})
	return res, nil
}
