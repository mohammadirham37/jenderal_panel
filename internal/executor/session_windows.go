//go:build windows

package executor

import "os/exec"

// configureProcessGroup is a no-op on Windows.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills the session process on Windows.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
