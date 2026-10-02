package gitops

import (
	"errors"
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	cases := []struct{ in, leak string }{
		{"fatal: unable to access 'https://user:hunter2@github.com/o/r.git/'", "hunter2"},
		{"remote: https://ghp_abcdefghijklmnopqrstuvwxyz0123@github.com/o/r", "ghp_abc"},
		{"ssh://git:pa55word@host/r.git", "pa55word"},
		{"Authorization: Bearer abc.def.ghi", "abc.def"},
		{"authorization: basic dXNlcjpwYXNz", "dXNlcjpw"},
		{"https://host/r.git?private_token=glpat-aaaaaaaaaaaaaaaaaaaaaa&x=1", "glpat-aaa"},
		{"password=topsecret\nusername=me", "topsecret"},
		{"token: abcd1234", "abcd1234"},
		{"github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz", "github_pat_11"},
		{"key AKIAABCDEFGHIJKLMNOP used", "AKIAABCDEFGHIJKLMNOP"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk\n-----END OPENSSH PRIVATE KEY-----", "b3BlbnNzaC1rZXk"},
	}
	for _, c := range cases {
		got := Mask(c.in)
		if strings.Contains(got, c.leak) || !strings.Contains(got, Masked) {
			t.Errorf("Mask(%q) = %q", c.in, got)
		}
	}
	// Ordinary output stays readable.
	for _, s := range []string{
		"To github.com:o/r.git\n ! [rejected]        HEAD -> main (fetch first)",
		"git@github.com: Permission denied (publickey).",
		"https://github.com/o/r.git",
	} {
		if got := Mask(s); got != s {
			t.Errorf("Mask(%q) = %q", s, got)
		}
	}
}

func TestMessage(t *testing.T) {
	cases := []struct {
		changes []Change
		lang    string
		want    string
	}{
		{[]Change{{Path: "ch1/intro.md", Kind: Modified}}, LangEN, "docs(ch1): update intro.md\n"},
		{[]Change{{Path: "ch1/intro.md", Kind: Modified}}, LangJA, "docs(ch1): intro.md を更新\n"},
		{[]Change{{Path: "README.md", Kind: Added}}, LangEN, "docs: add README.md\n"},
		{[]Change{{Path: "a.md", Kind: Deleted}, {Path: "b.md", Kind: Deleted}}, LangJA, "docs: a.md、b.md を削除\n\n- a.md を削除\n- b.md を削除\n"},
		{[]Change{{Path: "main.go", Kind: Added}, {Path: "doc.md", Kind: Modified}}, LangEN, "feat: update main.go and doc.md\n\n- add main.go\n- update doc.md\n"},
		{[]Change{{Path: "x/a_test.go", Kind: Modified}}, LangEN, "test(x): update a_test.go\n"},
		{[]Change{{Path: "go.mod", Kind: Modified}, {Path: "go.sum", Kind: Modified}}, LangEN, "build: update go.mod and go.sum\n\n- update go.mod\n- update go.sum\n"},
		{[]Change{{Path: "new.md", From: "old.md", Kind: Renamed}}, LangJA, "docs: old.md の名前を new.md に変更\n\n- old.md の名前を new.md に変更\n"},
	}
	for _, c := range cases {
		if got := Message(c.changes, c.lang); got != c.want {
			t.Errorf("Message(%v, %s) =\n%q\nwant\n%q", c.changes, c.lang, got, c.want)
		}
		if _, err := ValidateMessage(Message(c.changes, c.lang)); err != nil {
			t.Errorf("generated message invalid: %v", err)
		}
	}

	var many []Change
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, Change{Path: "book/" + n + ".md", Kind: Modified})
	}
	if got := Message(many, LangJA); !strings.HasPrefix(got, "docs(book): 5件のファイルを更新\n\n- book/a.md を更新\n") {
		t.Errorf("many ja = %q", got)
	}
	if got := Message(many, LangEN); !strings.HasPrefix(got, "docs(book): update 5 files\n") {
		t.Errorf("many en = %q", got)
	}
	if Message(nil, LangEN) != "" {
		t.Error("empty changes produced a message")
	}
}

func TestValidateMessage(t *testing.T) {
	for _, ok := range []string{"docs: fix typo", "feat(api)!: drop v1\n\nBREAKING CHANGE: gone", "fix(第1章): 誤字を直す\r\n\r\n本文"} {
		if _, err := ValidateMessage(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "update stuff", "Docs: x", "docs:x", "docs(): x", "docs: x\nbody without blank line", "docs: a\x00b"} {
		if _, err := ValidateMessage(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	got, _ := ValidateMessage("  docs: x\r\n\r\nbody\r\n ")
	if got != "docs: x\n\nbody\n" {
		t.Errorf("normalized = %q", got)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"git@github.com: Permission denied (publickey).\r\nfatal: Could not read from remote repository.":    FailSSHAuth,
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled":                 FailHTTPSAuth,
		"remote: Invalid username or token.\nfatal: Authentication failed for 'https://github.com/o/r.git/'": FailHTTPSAuth,
		"Host key verification failed.\nfatal: Could not read from remote repository.":                       FailHostKey,
		" ! [rejected]        HEAD -> main (fetch first)\nerror: failed to push some refs":                   FailNonFastForward,
		"remote: error: GH006: Protected branch update failed for refs/heads/main.":                          FailProtected,
		"ERROR: Repository not found.\nfatal: Could not read from remote repository.":                        FailNotFound,
		"fatal: unable to access 'https://example.invalid/r.git/': Could not resolve host: example.invalid":  FailNetwork,
		"something unexpected": FailUnknown,
	}
	for stderr, want := range cases {
		f := Classify(&CmdError{Args: []string{"push"}, ExitCode: 128, Stderr: stderr}, "origin", "git@github.com:o/r.git", Env{})
		if f.Kind != want {
			t.Errorf("%q: kind %s, want %s", stderr, f.Kind, want)
		}
		if f.Message == "" || len(f.Hints) == 0 {
			t.Errorf("%q: no explanation", stderr)
		}
	}
	f := Classify(&CmdError{Stderr: "Permission denied (publickey)."}, "origin", "git@gitlab.com:o/r.git", Env{})
	if !strings.Contains(f.Hints[0].Text, "SSH_AUTH_SOCK") {
		t.Errorf("missing agent hint: %+v", f.Hints[0])
	}
	if !strings.Contains(strings.Join(hintCommands(f), "\n"), "ssh -T git@gitlab.com") {
		t.Errorf("missing ssh -T hint: %+v", f.Hints)
	}
	f = Classify(errors.New("plain"), "origin", "https://github.com/o/r.git", Env{CredentialHelper: true})
	if f.Kind != FailUnknown {
		t.Errorf("kind = %s", f.Kind)
	}
}

func hintCommands(f *Failure) []string {
	var out []string
	for _, h := range f.Hints {
		out = append(out, h.Commands...)
	}
	return out
}
