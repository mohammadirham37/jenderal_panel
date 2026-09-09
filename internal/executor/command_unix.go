//go:build unix

package executor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const processGroupKillDelay = 2 * time.Second

// configureCommandCancellation gives each command its own process group. A
// canceled context first lets shell traps clean up, then kills descendants that
// ignored TERM. This prevents children retaining stdout/stderr pipes after the
// direct command exits.
func configureCommandCancellation(cmd *exec.Cmd, done <-chan struct{}) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		pid := cmd.Process.Pid
		if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		go func() {
			timer := time.NewTimer(processGroupKillDelay)
			defer timer.Stop()
			select {
			case <-done:
				return
			case <-timer.C:
			}
			select {
			case <-done:
				return
			default:
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
		return nil
	}
}

// finalizeCommandCancellation synchronously removes any group members that
// ignored TERM before run returns and the leader PID can be reused.
func finalizeCommandCancellation(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
