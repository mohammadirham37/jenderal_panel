// Package hardening applies host-level protections the panel does not need
// to keep switchable, converging them on every panel start.
package hardening

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// KsmbdBlockPath is the modprobe drop-in that stops the in-kernel SMB server
// from loading.
const KsmbdBlockPath = "/etc/modprobe.d/jenderal-disable-ksmbd.conf"

// ksmbdBlockConfig keeps ksmbd from loading. The panel never serves SMB, and
// ksmbd had unpatched, pre-auth remote code execution flaws reachable on
// port 445 (ZDI-CAN-33774, ZDI-CAN-33549, disclosed October 2026).
const ksmbdBlockConfig = `# Managed by Jenderal Panel. The panel does not use the in-kernel SMB
# server (ksmbd), which had unpatched remote code execution flaws
# (ZDI-CAN-33774, ZDI-CAN-33549, October 2026). Delete this file to allow
# loading ksmbd again.
blacklist ksmbd
install ksmbd /bin/false
`

// KsmbdReport says what EnsureKsmbdDisabled found and did.
type KsmbdReport struct {
	// InUse means a ksmbd file server is deliberately running; it is left
	// alone so an existing share is not cut off.
	InUse     bool
	Installed bool // the modprobe block was written this run
	Unloaded  bool // a loaded, unused module was removed
}

// EnsureKsmbdDisabled blocks the ksmbd kernel module and unloads it when it
// is loaded but not serving. A server deliberately running ksmbd (the
// ksmbd-tools service is active) is not touched.
func EnsureKsmbdDisabled(ctx context.Context, exec executor.CommandExecutor) (KsmbdReport, error) {
	var report KsmbdReport

	if res, err := exec.RunSudo(ctx, "systemctl", "is-active", "--quiet", "ksmbd.service"); err == nil && res != nil && res.ExitCode == 0 {
		report.InUse = true
		return report, nil
	}

	current, err := exec.RunSudo(ctx, "cat", KsmbdBlockPath)
	if err != nil {
		return report, fmt.Errorf("read %s: %w", KsmbdBlockPath, err)
	}
	if current == nil || current.ExitCode != 0 || current.Stdout != ksmbdBlockConfig {
		res, err := exec.RunSudoWithInput(ctx, ksmbdBlockConfig, "tee", KsmbdBlockPath)
		if err != nil {
			return report, fmt.Errorf("write %s: %w", KsmbdBlockPath, err)
		}
		if res == nil || res.ExitCode != 0 {
			return report, fmt.Errorf("write %s: %s", KsmbdBlockPath, stderrOf(res))
		}
		report.Installed = true
	}

	loaded, err := exec.RunSudo(ctx, "test", "-d", "/sys/module/ksmbd")
	if err != nil {
		return report, fmt.Errorf("check ksmbd module: %w", err)
	}
	if loaded != nil && loaded.ExitCode == 0 {
		res, err := exec.RunSudo(ctx, "modprobe", "-r", "ksmbd")
		if err != nil {
			return report, fmt.Errorf("unload ksmbd: %w", err)
		}
		if res == nil || res.ExitCode != 0 {
			return report, fmt.Errorf("unload ksmbd: %s", stderrOf(res))
		}
		report.Unloaded = true
	}
	return report, nil
}

func stderrOf(res *executor.Result) string {
	if res == nil {
		return "no result"
	}
	if msg := strings.TrimSpace(res.Stderr); msg != "" {
		return msg
	}
	return fmt.Sprintf("exit status %d", res.ExitCode)
}
