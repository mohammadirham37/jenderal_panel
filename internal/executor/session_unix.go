//go:build unix

package executor

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the session process in its own process group so
// Stop can kill the whole tree (shell plus anything it spawned).
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup SIGKILLs the session's process group.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		// The group may already be gone if the process exited on its own.
		return nil
	}
	return nil
}
