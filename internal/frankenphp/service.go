// Package frankenphp installs and reports on the server-wide FrankenPHP
// binary used by Laravel Octane sites. The binary is the official static
// build, pinned to a version with sha256 checksums for supply-chain safety.
package frankenphp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const (
	// Version is the pinned FrankenPHP release the panel installs.
	Version = "1.12.5"
	// BinaryPath is where the panel installs the binary.
	BinaryPath = "/usr/local/bin/frankenphp"

	releaseURL = "https://github.com/php/frankenphp/releases/download/v" + Version + "/frankenphp-linux-%s"
)

// checksums pins the sha256 of the official static binaries per architecture
// (uname -m value).
var checksums = map[string]string{
	"x86_64":  "58485df60a65b8eb04da879af3814895777e8f3693837319cf85727e87151042",
	"aarch64": "de4073f9d54eb73682c6a5cc204cc95c5cb0a671a94a03aade7b990855cba102",
}

var versionRegex = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)

// Service installs and inspects the FrankenPHP binary.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewService creates a new frankenphp Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// SetTaskRunner wires the persistent task runner used for the install.
func (s *Service) SetTaskRunner(tr *taskrunner.Runner) {
	s.tasks = tr
}

// Status is the API view of the installation state.
type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Pinned    string `json:"pinned_version"`
}

// Status reports whether the binary exists and which version it reports.
func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{Pinned: Version}
	result, err := s.exec.Run(ctx, BinaryPath, "version")
	if err != nil || result == nil || result.ExitCode != 0 {
		return status, nil
	}
	status.Installed = true
	status.Version = parseVersion(result.Stdout)
	return status, nil
}

// parseVersion extracts the release version from `frankenphp version` output.
func parseVersion(output string) string {
	m := versionRegex.FindStringSubmatch(output)
	if m == nil {
		return ""
	}
	return m[1]
}

// installScript renders the root install script. It contains no user input:
// the version, URL, and checksums are pinned in this file.
func installScript() string {
	var b strings.Builder
	fmt.Fprintf(&b, "set -eu\n")
	fmt.Fprintf(&b, "arch=$(/usr/bin/uname -m)\n")
	fmt.Fprintf(&b, "case $arch in\n")
	for arch, sum := range checksums {
		fmt.Fprintf(&b, "  %s)\n", arch)
		fmt.Fprintf(&b, "    url=%q\n", fmt.Sprintf(releaseURL, arch))
		fmt.Fprintf(&b, "    checksum=%q\n", sum)
		fmt.Fprintf(&b, "    ;;\n")
	}
	fmt.Fprintf(&b, "  *)\n")
	fmt.Fprintf(&b, "    echo \"unsupported architecture: $arch\" >&2\n")
	fmt.Fprintf(&b, "    exit 1\n")
	fmt.Fprintf(&b, "    ;;\n")
	fmt.Fprintf(&b, "esac\n")
	fmt.Fprintf(&b, "tmp=$(mktemp /tmp/frankenphp-%s.XXXXXX)\n", Version)
	fmt.Fprintf(&b, "trap 'rm -f \"$tmp\"' EXIT\n")
	fmt.Fprintf(&b, "/usr/bin/curl -fsSL --retry 3 -o \"$tmp\" \"$url\"\n")
	fmt.Fprintf(&b, "echo \"$checksum  $tmp\" | /usr/bin/sha256sum -c -\n")
	fmt.Fprintf(&b, "/usr/bin/install -m 0755 \"$tmp\" %s\n", BinaryPath)
	fmt.Fprintf(&b, "%s version\n", BinaryPath)
	return b.String()
}

// Install downloads, verifies, and installs the pinned FrankenPHP binary as a
// background task. Returns the task ID for polling.
func (s *Service) Install(ctx context.Context) (string, error) {
	if s.tasks == nil {
		return "", model.NewDomainError("TASK_RUNNER_UNAVAILABLE", "task runner is not available", nil)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "frankenphp_install", Module: "services", Detail: "installing FrankenPHP " + Version})
	return s.tasks.Run("Install FrankenPHP "+Version, "bash", "-c", installScript()), nil
}

// EnsureInstalled reports a precise error when Octane operations are requested
// before the server-wide install happened.
func (s *Service) EnsureInstalled(ctx context.Context) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return model.NewDomainError("FRANKENPHP_MISSING",
			"FrankenPHP is not installed; an administrator must install it first (services page)", nil)
	}
	return nil
}
