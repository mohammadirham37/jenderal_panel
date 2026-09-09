//go:build windows

package executor

import "os/exec"

func configureGracefulCancellation(_ *exec.Cmd) {}
