package t3

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestDesktopCLI(t *testing.T) {
	contents := filepath.Join(t.TempDir(), "T3 Code (Nightly).app", "Contents")
	path := filepath.Join(contents, "MacOS", "T3 Code (Nightly)")
	archive := filepath.Join(contents, "Resources", "app.asar")
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(archive)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entry := filepath.Join(archive, "apps", "server", "dist", "bin.mjs")
	// This fake Electron fails unless invoked in Node mode with the bundled
	// entry point, preserving paths with spaces as a single argument.
	script := `#!/bin/sh
[ "$ELECTRON_RUN_AS_NODE" = 1 ] || exit 1
[ "$1" = "$EXPECTED_ENTRY" ] || exit 2
[ "$T3CODE_HOME" = "$EXPECTED_HOME" ] || exit 3
shift
[ "$1 $2 $3" = "auth pairing create" ] || exit 4
printf '{"credential":"test-pairing-code"}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if cli := commandForExecutable(path); cli.path != "" {
		t.Fatal("accepted missing app archive")
	}
	if err := os.WriteFile(archive, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cli := commandForExecutable(path)
	if cli.path != path || !reflect.DeepEqual(cli.args, []string{entry}) {
		t.Fatalf("CLI = %+v", cli)
	}
	home := t.TempDir()
	t.Setenv("EXPECTED_ENTRY", entry)
	t.Setenv("EXPECTED_HOME", home)
	t.Setenv("ELECTRON_RUN_AS_NODE", "0")
	client := New(Options{Command: cli.path, Home: home, Access: DefaultAccess, commandArgs: cli.args, commandEnv: cli.env})
	code, err := client.pairingCode(context.Background())
	if err != nil || code != "test-pairing-code" {
		t.Fatalf("pairingCode = %q, %v", code, err)
	}
}

func TestCommandForStandaloneExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t3")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cli := commandForExecutable(path); cli.path != "" {
		t.Fatal("accepted non-executable file")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	cli := commandForExecutable(path)
	if cli.path != path || len(cli.args) != 0 || len(cli.env) != 0 {
		t.Fatalf("CLI = %+v", cli)
	}
}

func TestProcessExecutable(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("process discovery is supported on macOS and Linux")
	}
	got := processExecutable(os.Getpid())
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// macOS may use /private/var where os.Executable uses /var.
	got, _ = filepath.EvalSymlinks(got)
	want, _ = filepath.EvalSymlinks(want)
	if got == "" || got != want {
		t.Fatalf("processExecutable = %q, want %q", got, want)
	}
	if got := processExecutable(0); got != "" {
		t.Fatalf("zero PID = %q", got)
	}
}
