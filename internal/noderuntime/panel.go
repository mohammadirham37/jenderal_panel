package noderuntime

import (
	"context"
	"strings"
)

const PanelHome = "/var/lib/jenderal"

// The panel build runtime has a fixed account/home and never borrows a tenant's
// Node version. No request can choose these values.
func panelEnvironment() []string {
	return []string{"jenderal", "--", "/usr/bin/env", "-i", "HOME=" + PanelHome, "USER=jenderal", "LOGNAME=jenderal", "NVM_DIR=" + PanelHome + "/.nvm", "NODE_VERSION=24", "PATH=/usr/local/bin:/usr/bin:/bin"}
}

func panelInstallArgs() []string {
	return append(panelEnvironment(), "/usr/bin/timeout", "--signal=TERM", "--kill-after=5s", installCommandTimeout, "/bin/bash", "-c", installScript, "--", "jenderal", "24", PanelHome)
}

func (s *Service) InstallPanel(ctx context.Context, log func(string)) error {
	if log != nil {
		log("Preparing the panel build runtime (Node.js 24).")
	}
	return s.install(ctx, panelInstallArgs(), log)
}

func quotedCommand(args []string) string {
	quoted := make([]string, len(args))
	for n, arg := range args {
		quoted[n] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return strings.Join(quoted, " ")
}

// PanelBootstrapShell is a literal command for root-owned installer/update tasks;
// downloaded NVM and package scripts execute as the unprivileged panel account.
func PanelBootstrapShell() string {
	return quotedCommand(append([]string{"/usr/bin/sudo", "-u"}, panelInstallArgs()...))
}

func PanelNPMCommand(args ...string) string {
	command := append(panelEnvironment(), PanelHome+"/.nvm/nvm-exec", "npm")
	command = append(command, args...)
	return quotedCommand(append([]string{"/usr/bin/sudo", "-u"}, command...))
}
