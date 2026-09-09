//go:build windows

package executor

import "os/exec"

func configureCommandCancellation(_ *exec.Cmd, _ <-chan struct{}) {}

func finalizeCommandCancellation(_ *exec.Cmd) {}
