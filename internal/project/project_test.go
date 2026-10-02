package project

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkAndIgnore(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "# a")
	write(t, root, "docs/b.md", "b")
	write(t, root, "docs/.gitignore", "tmp/\n")
	write(t, root, "docs/tmp/x.md", "x")
	write(t, root, ".gitignore", "*.log\n")
	write(t, root, "debug.log", "x")
	write(t, root, "bin.dat", "a\x00b")
	write(t, root, ".git/HEAD", "ref")
	write(t, root, ".reviewer/state.json", "{}")

	p, err := New(root, filepath.Join(root, ".reviewer"), []string{"secret.md"})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "secret.md", "s")
	files, err := p.Walk()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	want := []string{".gitignore", "a.md", "docs/.gitignore", "docs/b.md"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if !p.Ignored("docs/tmp/y.md", false) {
		t.Error("nested gitignore not applied")
	}
}

func TestWriteChecksHashAndPreservesMode(t *testing.T) {
	root := t.TempDir()
	write(t, root, "main.go", "package main\n\n")
	p, err := New(root, filepath.Join(root, ".reviewer"), nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := p.Read("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Write("main.go", "stale", []byte("changed\n")); err == nil {
		t.Fatal("stale write succeeded")
	}
	if err := p.Write("main.go", HashBytes(before), []byte("package main\n\nfunc main() {}\n")); err != nil {
		t.Fatal(err)
	}
	got, err := p.Read("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("content = %q", got)
	}
	if err := p.Write("main.go", HashBytes(got), []byte("a\x00b")); err != ErrNotText {
		t.Fatalf("binary write error = %v", err)
	}
}
func TestResolveRejectsOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, root, "ok.md", "ok")
	write(t, outside, "secret.md", "s")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	write(t, root, ".reviewer/state.json", "{}")
	p, err := New(root, filepath.Join(root, ".reviewer"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Resolve("ok.md"); err != nil {
		t.Errorf("ok.md: %v", err)
	}
	for _, bad := range []string{"../x", "/etc/passwd", "link.md", "a/../../x", ".reviewer/state.json", ""} {
		if _, err := p.Resolve(bad); err == nil {
			t.Errorf("Resolve(%q) succeeded", bad)
		}
	}
	if _, err := p.Read(".git/HEAD"); err == nil {
		t.Error("read .git succeeded")
	}
}
