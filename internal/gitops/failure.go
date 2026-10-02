package gitops

import (
	"errors"
	"strings"
)

// Failure kinds of a push.
const (
	FailSSHAuth        = "ssh-auth"
	FailHTTPSAuth      = "https-auth"
	FailHostKey        = "host-key"
	FailNonFastForward = "non-fast-forward"
	FailProtected      = "protected"
	FailNotFound       = "not-found"
	FailNetwork        = "network"
	FailTimeout        = "timeout"
	FailUnknown        = "unknown"
)

// Hint is a suggested fix with commands to run in a terminal.
type Hint struct {
	Text     string   `json:"text"`
	Commands []string `json:"commands,omitempty"`
}

// Failure explains a failed push. Detail is git's masked stderr.
type Failure struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
	Hints   []Hint `json:"hints"`
}

// Env describes how the server process can authenticate, without values.
type Env struct {
	SSHAgent         bool `json:"sshAgent"`
	CredentialHelper bool `json:"credentialHelper"`
}

var patterns = []struct {
	kind string
	subs []string
}{
	{FailHostKey, []string{"host key verification failed", "remote host identification has changed", "no matching host key", "host key is known for"}},
	{FailSSHAuth, []string{"permission denied (publickey", "permission denied (keyboard-interactive", "permission denied (password", "sign_and_send_pubkey", "agent refused operation", "no such identity", "enter passphrase"}},
	{FailHTTPSAuth, []string{"could not read username", "could not read password", "terminal prompts disabled", "authentication failed", "invalid username or password", "http basic: access denied", "the requested url returned error: 401", "the requested url returned error: 403", "support for password authentication was removed", "invalid credentials"}},
	{FailProtected, []string{"protected branch", "gh006", "pre-receive hook declined", "push declined", "not allowed to push", "you are not allowed to"}},
	{FailNonFastForward, []string{"non-fast-forward", "fetch first", "updates were rejected", "[rejected]"}},
	{FailNotFound, []string{"repository not found", "does not appear to be a git repository", "the requested url returned error: 404", "project you were looking for could not be found"}},
	{FailNetwork, []string{"could not resolve host", "could not resolve hostname", "connection timed out", "connection refused", "network is unreachable", "no route to host", "connection reset", "failed to connect", "ssl certificate problem", "from proxy", "operation timed out", "early eof", "the remote end hung up"}},
}

// Classify turns a push error into a Failure with fix suggestions.
func Classify(err error, remote, remoteURL string, env Env) *Failure {
	f := &Failure{Kind: FailUnknown}
	var ce *CmdError
	if errors.As(err, &ce) {
		f.Detail = strings.TrimSpace(ce.Stderr)
		if ce.TimedOut {
			f.Kind = FailTimeout
		}
	} else if err != nil {
		f.Detail = Mask(err.Error())
	}
	if f.Kind == FailUnknown {
		lower := strings.ToLower(f.Detail)
		for _, p := range patterns {
			for _, s := range p.subs {
				if strings.Contains(lower, s) {
					f.Kind = p.kind
					break
				}
			}
			if f.Kind != FailUnknown {
				break
			}
		}
	}
	f.Message, f.Hints = explain(f.Kind, remote, remoteURL, env)
	return f
}

func isHTTP(u string) bool {
	u = strings.ToLower(u)
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// sshHost guesses the "user@host" to test from an SSH remote URL.
func sshHost(u string) string {
	if rest, ok := strings.CutPrefix(u, "ssh://"); ok {
		host, _, _ := strings.Cut(rest, "/")
		if h, _, ok := strings.Cut(host, ":"); ok && !strings.Contains(h, "[") {
			host = h
		}
		return host
	}
	if i := strings.Index(u, ":"); i > 0 && !strings.Contains(u[:i], "/") {
		return u[:i]
	}
	return "git@github.com"
}

func httpHost(u string) string {
	rest := u[strings.Index(u, "://")+3:]
	host, _, _ := strings.Cut(rest, "/")
	if _, h, ok := strings.Cut(host, "@"); ok {
		host = h
	}
	return host
}

var agentHints = []Hint{
	{Text: "reviewer は SSH の鍵を読み込まず、ssh-agent に登録済みの鍵だけを使います。ターミナルで鍵を ssh-agent に登録してください。", Commands: []string{"ssh-add -l", "ssh-add ~/.ssh/id_ed25519"}},
	{Text: "reviewer を systemd のユーザーサービスで動かしている場合は、サービスに SSH_AUTH_SOCK を渡してから再起動してください。", Commands: []string{
		"systemctl --user set-environment SSH_AUTH_SOCK=\"$SSH_AUTH_SOCK\"",
		"reviewer service restart",
	}},
}

func explain(kind, remote, remoteURL string, env Env) (string, []Hint) {
	retry := Hint{Text: "設定できたら、ターミナルで同じ操作が確認なしで通ることを確かめてから「再試行」を押してください。", Commands: []string{"git ls-remote " + remote}}
	switch kind {
	case FailSSHAuth:
		hints := []Hint{}
		if !env.SSHAgent {
			hints = append(hints, Hint{Text: "reviewer のプロセスから ssh-agent が見えていません（SSH_AUTH_SOCK が未設定です）。"})
		}
		hints = append(hints, agentHints...)
		hints = append(hints, Hint{Text: "鍵がリモートに登録されているかを確かめてください。", Commands: []string{"ssh -T " + sshHost(remoteURL)}}, retry)
		return "SSH の認証に失敗しました。", hints
	case FailHTTPSAuth:
		host := "github.com"
		if isHTTP(remoteURL) {
			host = httpHost(remoteURL)
		}
		hints := []Hint{}
		if !env.CredentialHelper {
			hints = append(hints, Hint{Text: "credential helper が設定されていません。reviewer はユーザー名やトークンの入力を求めません。"})
		}
		hints = append(hints,
			Hint{Text: "GitHub なら gh で credential helper を設定できます。", Commands: []string{"gh auth login", "gh auth setup-git"}},
			Hint{Text: "OS のキーチェーンや Git Credential Manager を credential helper にして、一度ターミナルで認証すると保存されます。", Commands: []string{
				"git config --global credential.helper libsecret   # Linux（git-credential-libsecret）",
				"git config --global credential.helper osxkeychain # macOS",
				"git config --global credential.helper manager     # Git Credential Manager",
				"git ls-remote " + remote + "   # ここで認証して保存する",
			}},
			Hint{Text: "SSH で push する場合は、remote の URL を SSH に変えることもできます（ターミナルで実行します）。", Commands: []string{"git remote set-url --push " + remote + " git@" + host + ":<owner>/<repo>.git"}},
			retry,
		)
		return "HTTPS の認証に失敗しました。credential helper に保存された認証情報がないか、無効です。", hints
	case FailHostKey:
		return "SSH のホスト鍵を確認できませんでした。", []Hint{
			{Text: "ターミナルで一度接続し、表示されるフィンガープリントを公式の値と照合してから known_hosts に登録してください。", Commands: []string{"ssh -T " + sshHost(remoteURL)}},
			{Text: "ホスト鍵が変わったと表示された場合は、公式の告知を確認してから古い鍵を消してください。", Commands: []string{"ssh-keygen -R <host>"}},
			retry,
		}
	case FailNonFastForward:
		return "リモートのブランチに手元にないコミットがあるため、push を拒否されました。", []Hint{
			{Text: "reviewer は force push、pull、merge を行いません。ターミナルでリモートの変更を取り込んでから再試行するか、別のブランチへ push してください。", Commands: []string{"git fetch " + remote, "git status"}},
		}
	case FailProtected:
		return "リモートの設定でこのブランチへの push が拒否されました（保護ブランチなど）。", []Hint{
			{Text: "別のブランチを選んで push し、プルリクエストを作ってください。"},
		}
	case FailNotFound:
		return "リモートのリポジトリが見つからないか、アクセスする権限がありません。", []Hint{
			{Text: "remote の URL と、認証に使うアカウントの権限を確かめてください。", Commands: []string{"git remote -v", "git ls-remote " + remote}},
		}
	case FailNetwork:
		return "リモートに接続できませんでした。", []Hint{
			{Text: "ネットワークやプロキシの設定を確かめてください。", Commands: []string{"git ls-remote " + remote}},
		}
	case FailTimeout:
		hints := []Hint{{Text: "認証の入力待ちで止まっている可能性があります。パスフレーズ付きの鍵は ssh-agent に登録してください。"}}
		hints = append(hints, agentHints...)
		return "push が時間内に終わりませんでした。", append(hints, retry)
	default:
		hints := []Hint{{Text: "下の詳細を確認してください。"}}
		if isHTTP(remoteURL) {
			hints = append(hints, Hint{Text: "HTTPS の remote では credential helper が必要です。", Commands: []string{"gh auth setup-git"}})
		} else {
			hints = append(hints, agentHints[0])
		}
		return "push に失敗しました。", append(hints, retry)
	}
}
