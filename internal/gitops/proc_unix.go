//go:build unix

package gitops

import (
	"os/exec"
	"syscall"
)

// detach starts git in its own session, without a controlling terminal, so
// that neither git nor ssh can prompt on the server's terminal. On timeout
// the whole process group (git, ssh, hooks) is killed.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
