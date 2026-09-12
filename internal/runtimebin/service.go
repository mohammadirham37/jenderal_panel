// Package runtimebin installs the server-wide Deno and Bun single-binary
// runtimes used by app services. Each release is pinned to a version with
// sha256 checksums per architecture for supply-chain safety.
package runtimebin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const (
	DenoVersion = "2.9.6"
	DenoBin     = "/usr/local/bin/deno"

	BunVersion = "1.4.2"
	BunBin     = "/usr/local/bin/bun"

	denoReleaseURL = "https://github.com/denoland/deno/releases/download/v" + DenoVersion + "/"
	bunReleaseURL  = "https://github.com/oven-sh/bun/releases/download/bun-v" + BunVersion + "/"
)

type artifact struct {
	zipURL  string
	binName string // binary name inside the extracted zip
}

// artifacts maps runtime → arch (uname -m) → release zip + inner binary.
var artifacts = map[string]map[string]artifact{
	"deno": {
		"x86_64":  {zipURL: denoReleaseURL + "deno-x86_64-unknown-linux-gnu.zip", binName: "deno"},
		"aarch64": {zipURL: denoReleaseURL + "deno-aarch64-unknown-linux-gnu.zip", binName: "deno"},
	},
	"bun": {
		"x86_64":  {zipURL: bunReleaseURL + "bun-linux-x64.zip", binName: "bun"},
		"aarch64": {zipURL: bunReleaseURL + "bun-linux-aarch64.zip", binName: "bun"},
	},
}

// checksums pins the sha256 of each zip per runtime and architecture,
// computed from the official release assets when the version was pinned.
var checksums = map[string]map[string]string{
	"deno": {
		"x86_64":  "394f07f4da2bebe6ce6f1e7ce0fa16429b29b08c35e3fac3fe25972676dff4b2",
		"aarch64": "9a46afc6c392c7cd2ff71a31558935545b46408d0e87f7a86908c712721c046e",
	},
	"bun": {
		"x86_64":  "36368faef7527875d5ffa52e53cd48021741f2a83eb6208a8dd64068d422a913",
		"aarch64": "54328bbc2d9c8e0c9f892c544d66c57a83b84139e34909e5ee81758f1ac8fda7",
	},
}

// Service installs the Deno and Bun runtimes.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewService creates a new runtimebin Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// SetTaskRunner wires the task runner used for installs.
func (s *Service) SetTaskRunner(tr *taskrunner.Runner) {
	s.tasks = tr
}

// RuntimeStatus is the API view of one runtime installation.
type RuntimeStatus struct {
	Runtime   string `json:"runtime"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Pinned    string `json:"pinned_version"`
}

func (s *Service) binaryPath(runtime string) string {
	if runtime == "deno" {
		return "/usr/local/bin/deno"
	}
	return "/usr/local/bin/bun"
}

func versionArgs(runtime string) []string {
	if runtime == "deno" {
		return []string{"version"}
	}
	return []string{"--version"}
}

// Status reports installation state for both runtimes.
func (s *Service) Status(ctx context.Context) ([]RuntimeStatus, error) {
	out := make([]RuntimeStatus, 0, 2)
	for _, rt := range []string{"deno", "bun"} {
		st := RuntimeStatus{Runtime: rt, Pinned: pinnedVersion(rt)}
		result, err := s.exec.Run(ctx, s.binaryPath(rt), versionArgs(rt)...)
		if err == nil && result != nil && result.ExitCode == 0 {
			st.Installed = true
			st.Version = firstLine(result.Stdout)
			if st.Version == "" {
				st.Version = st.Pinned
			}
		}
		out = append(out, st)
	}
	return out, nil
}

func pinnedVersion(runtime string) string {
	if runtime == "deno" {
		return DenoVersion
	}
	return BunVersion
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// installScript renders the root install script for one runtime. It contains
// no user input: the version, URLs, and checksums are pinned in this file.
// Both runtimes ship zips whose single executable is extracted and installed.
func (s *Service) installScript(runtime string) (string, error) {
	arts, ok := artifacts[runtime]
	if !ok {
		return "", model.NewValidationError("unknown runtime: " + runtime)
	}
	sums := checksums[runtime]

	var b strings.Builder
	fmt.Fprintf(&b, "set -eu\n")
	fmt.Fprintf(&b, "arch=$(/usr/bin/uname -m)\n")
	fmt.Fprintf(&b, "case $arch in\n")

	archs := make([]string, 0, len(arts))
	for arch := range arts {
		archs = append(archs, arch)
	}
	sort.Strings(archs)
	for _, arch := range archs {
		fmt.Fprintf(&b, "  %s)\n", arch)
		fmt.Fprintf(&b, "    url=%q\n", arts[arch].zipURL)
		fmt.Fprintf(&b, "    checksum=%q\n", sums[arch])
		fmt.Fprintf(&b, "    binname=%q\n", arts[arch].binName)
		fmt.Fprintf(&b, "    ;;\n")
	}
	fmt.Fprintf(&b, "  *)\n")
	fmt.Fprintf(&b, "    echo \"unsupported architecture: $arch\" >&2\n")
	fmt.Fprintf(&b, "    exit 1\n")
	fmt.Fprintf(&b, "    ;;\n")
	fmt.Fprintf(&b, "esac\n")
	fmt.Fprintf(&b, "tmpzip=$(mktemp /tmp/%s-XXXXXX.zip)\n", runtime)
	fmt.Fprintf(&b, "tmpdir=$(mktemp -d /tmp/%s-XXXXXX)\n", runtime)
	fmt.Fprintf(&b, "trap 'rm -rf \"$tmpzip\" \"$tmpdir\"' EXIT\n")
	fmt.Fprintf(&b, "/usr/bin/curl -fsSL --retry 3 -o \"$tmpzip\" \"$url\"\n")
	fmt.Fprintf(&b, "echo \"$checksum  $tmpzip\" | /usr/bin/sha256sum -c -\n")
	fmt.Fprintf(&b, "/usr/bin/unzip -o -q \"$tmpzip\" -d \"$tmpdir\"\n")
	fmt.Fprintf(&b, "/usr/bin/find \"$tmpdir\" -type f -name %q -exec /usr/bin/install -m 0755 {} %s \\;\n",
		arts["x86_64"].binName, s.binaryPath(runtime))
	fmt.Fprintf(&b, "%s %s\n", s.binaryPath(runtime), strings.Join(versionArgs(runtime), " "))
	return b.String(), nil
}

// Install downloads, verifies, and installs one runtime ("deno" or "bun")
// as a background task. Returns the task ID for polling.
func (s *Service) Install(ctx context.Context, runtime string) (string, error) {
	switch runtime {
	case "deno", "bun":
	default:
		return "", model.NewValidationError("unknown runtime: " + runtime)
	}
	if s.tasks == nil {
		return "", model.NewDomainError("TASK_RUNNER_UNAVAILABLE", "task runner is not available", nil)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "runtime_install", Module: "services", Target: runtime,
		Detail: "installing " + runtime + " " + pinnedVersion(runtime)})

	script, err := s.installScript(runtime)
	if err != nil {
		return "", err
	}
	return s.tasks.Run("Install "+strings.Title(runtime)+" "+pinnedVersion(runtime), "bash", "-c", script), nil
}
