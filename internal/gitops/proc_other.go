//go:build !unix

package gitops

import "os/exec"

// detach is a no-op where sessions are not available; the prompt-disabling
// environment still applies.
func detach(cmd *exec.Cmd) {}
