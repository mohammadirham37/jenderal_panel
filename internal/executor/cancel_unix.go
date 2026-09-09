//go:build unix

package executor

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureGracefulCancellation signals only the still-owned direct process.
// This lets supervisors such as sudo and GNU timeout forward TERM and run shell
// cleanup. Cmd applies the bounded KILL fallback before Wait returns/reaps it.
func configureGracefulCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = 7 * time.Second
}
