// Package goruntime installs and reports on the server-wide Go toolchain
// used to build Go applications on the server. The toolchain is the official
// tarball from go.dev, pinned to a version with sha256 checksums for
// supply-chain safety.
package goruntime

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
	// Version is the pinned Go release the panel installs.
	Version = "1.27.1"
	// Goroot is where the toolchain is extracted ("/usr/local/go").
	Goroot = "/usr/local/go"
	// GoBin is the go binary path after installation.
	GoBin = Goroot + "/bin/go"

	releaseURL = "https://go.dev/dl/go" + Version + ".linux-%s.tar.gz"
)

// checksums pins the sha256 of the official tarballs per architecture
// (uname -m value).
var checksums = map[string]string{
	"x86_64":  "63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445",
	"aarch64": "3450b45a3f9ee8568792736a5c5e70a1f2e9b36c35a8f74958c03e51d7d92bec",
}

var versionRegex = regexp.MustCompile(`go(\d+\.\d+(\.\d+)?)`)

// Service installs and inspects the Go toolchain.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewService creates a new goruntime Service.
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

// Status reports whether the go binary works and which version it reports.
func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{Pinned: Version}
	result, err := s.exec.Run(ctx, GoBin, "version")
	if err != nil || result == nil || result.ExitCode != 0 {
		return status, nil
	}
	status.Installed = true
	status.Version = parseVersion(result.Stdout)
	return status, nil
}

// parseVersion extracts the release version from `go version` output
// (e.g. "go version go1.27.1 linux/amd64").
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
	fmt.Fprintf(&b, "tmp=$(mktemp /tmp/go-%s.XXXXXX.tar.gz)\n", Version)
	fmt.Fprintf(&b, "trap 'rm -f \"$tmp\"' EXIT\n")
	fmt.Fprintf(&b, "/usr/bin/curl -fsSL --retry 3 -o \"$tmp\" \"$url\"\n")
	fmt.Fprintf(&b, "echo \"$checksum  $tmp\" | /usr/bin/sha256sum -c -\n")
	fmt.Fprintf(&b, "rm -rf %s\n", Goroot)
	fmt.Fprintf(&b, "/usr/bin/tar -C /usr/local -xzf \"$tmp\"\n")
	fmt.Fprintf(&b, "%s version\n", GoBin)
	return b.String()
}

// Install downloads, verifies, and installs the pinned Go toolchain as a
// background task. Returns the task ID for polling.
func (s *Service) Install(ctx context.Context) (string, error) {
	if s.tasks == nil {
		return "", model.NewDomainError("TASK_RUNNER_UNAVAILABLE", "task runner is not available", nil)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "go_runtime_install", Module: "services", Detail: "installing Go " + Version})
	return s.tasks.Run("Install Go "+Version, "bash", "-c", installScript()), nil
}

// EnsureInstalled reports a precise error when Go builds are requested
// before the server-wide toolchain was installed.
func (s *Service) EnsureInstalled(ctx context.Context) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return model.NewDomainError("GO_TOOLCHAIN_MISSING",
			"the Go toolchain is not installed; an administrator must install it first (services page)", nil)
	}
	return nil
}
