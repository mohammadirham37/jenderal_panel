package hardening

import (
	"context"
	"fmt"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// NginxTokensPath turns off the nginx version in the Server header and on
// error pages for every site on the server (the http context reads
// conf.d/*.conf). A version string only helps match public CVEs.
const NginxTokensPath = "/etc/nginx/conf.d/jenderal-server-tokens.conf"

const nginxTokensConfig = "# Managed by Jenderal Panel: hide the nginx version in responses.\nserver_tokens off;\n"

// EnsureNginxServerTokensOff installs the drop-in unless the administrator
// already set server_tokens in the main config (a second one would be a
// duplicate directive), validates nginx, and reloads it. A failed check
// removes the drop-in again.
func EnsureNginxServerTokensOff(ctx context.Context, exec executor.CommandExecutor) (bool, error) {
	if res, err := exec.RunSudo(ctx, "test", "-f", "/etc/nginx/nginx.conf"); err != nil || res == nil || res.ExitCode != 0 {
		return false, nil // nginx not installed
	}
	if current, err := exec.RunSudo(ctx, "cat", NginxTokensPath); err == nil && current != nil &&
		current.ExitCode == 0 && current.Stdout == nginxTokensConfig {
		return false, nil
	}
	// An active server_tokens elsewhere wins; ours would be a duplicate.
	res, err := exec.RunSudo(ctx, "grep", "-rqsE", `^[[:space:]]*server_tokens[[:space:]]`,
		"/etc/nginx/nginx.conf", "/etc/nginx/conf.d")
	if err != nil {
		return false, fmt.Errorf("check server_tokens: %w", err)
	}
	if res != nil && res.ExitCode == 0 {
		return false, nil
	}

	write, err := exec.RunSudoWithInput(ctx, nginxTokensConfig, "tee", NginxTokensPath)
	if err != nil || write == nil || write.ExitCode != 0 {
		return false, fmt.Errorf("write %s: %s", NginxTokensPath, stderrOf(write))
	}
	if check, err := exec.RunSudo(ctx, "nginx", "-t"); err != nil || check == nil || check.ExitCode != 0 {
		_, _ = exec.RunSudo(ctx, "rm", "-f", NginxTokensPath)
		return false, fmt.Errorf("nginx rejected server_tokens off: %s", stderrOf(check))
	}
	if active, err := exec.RunSudo(ctx, "systemctl", "is-active", "--quiet", "nginx"); err == nil && active != nil && active.ExitCode == 0 {
		if reload, err := exec.RunSudo(ctx, "systemctl", "reload", "nginx"); err != nil || reload == nil || reload.ExitCode != 0 {
			return true, fmt.Errorf("reload nginx: %s", stderrOf(reload))
		}
	}
	return true, nil
}
