// Package cloudflared installs and supervises the Cloudflare Tunnel connector
// (cloudflared) so a server without a public IP can serve its websites and
// the panel itself through an outbound-only tunnel. The tunnel is remotely
// managed: the administrator creates it in the Cloudflare Zero Trust
// dashboard and pastes the connector token into the panel.
package cloudflared

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const (
	// Version is the pinned cloudflared release the panel installs.
	Version = "2026.9.1"
	// BinaryPath is where the panel installs the binary.
	BinaryPath = "/usr/local/bin/cloudflared"
	// TokenEnvPath holds the connector token as TUNNEL_TOKEN (root-only).
	TokenEnvPath = "/etc/cloudflared/jenderal-cloudflared.env"
	// UnitName is the systemd unit supervising the connector.
	UnitName = "jenderal-cloudflared.service"

	releaseURL = "https://github.com/cloudflare/cloudflared/releases/download/" + Version + "/cloudflared-linux-%s"
)

var unitPath = "/etc/systemd/system/" + UnitName

// checksums pins the sha256 of the official release binaries per
// architecture (uname -m value).
var checksums = map[string]string{
	"x86_64":  "03f1f25d1cc93b9ad6c60569d44060bc4f17ed97075760ed8cfca4b12dcd68cc",
	"aarch64": "3d97437c71848bd8df68041e12436b484a661d95073ea1937f01a845ce88faa3",
}

var versionRegex = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// tokenPayload is the JSON carried inside a connector token. Only the fields
// cloudflared needs to even attempt a connection are checked; the secret
// itself is never logged and never leaves the root-only env file.
type tokenPayload struct {
	A string `json:"a"` // account tag
	T string `json:"t"` // tunnel ID
	S string `json:"s"` // secret
}

// Service installs and supervises the cloudflared connector.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewService creates a new cloudflared Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// SetTaskRunner wires the persistent task runner used for install/connect.
func (s *Service) SetTaskRunner(tr *taskrunner.Runner) {
	s.tasks = tr
}

// ValidateToken sanity-checks a pasted connector token: base64 JSON with an
// account tag and a secret. The authoritative check happens when cloudflared
// connects and shows up in the service state and logs.
func ValidateToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return model.NewValidationError("tunnel token is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(token)
	}
	if err != nil {
		return model.NewValidationError("tunnel token is not valid base64")
	}
	var payload tokenPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return model.NewValidationError("tunnel token is not a Cloudflare connector token")
	}
	if payload.A == "" || payload.S == "" {
		return model.NewValidationError("tunnel token is missing account tag or secret")
	}
	return nil
}

// RenderUnit renders the systemd unit that supervises the connector. The
// token comes from the root-only EnvironmentFile; --no-autoupdate keeps the
// binary pinned so updates flow through the panel only.
func RenderUnit() string {
	return fmt.Sprintf(`[Unit]
Description=Jenderal Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%s
ExecStart=%s tunnel --no-autoupdate run
Restart=always
RestartSec=5
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, TokenEnvPath, BinaryPath)
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
	fmt.Fprintf(&b, "tmp=$(mktemp /tmp/cloudflared-%s.XXXXXX)\n", Version)
	fmt.Fprintf(&b, "trap 'rm -f \"$tmp\"' EXIT\n")
	fmt.Fprintf(&b, "/usr/bin/curl -fsSL --retry 3 -o \"$tmp\" \"$url\"\n")
	fmt.Fprintf(&b, "echo \"$checksum  $tmp\" | /usr/bin/sha256sum -c -\n")
	fmt.Fprintf(&b, "/usr/bin/install -m 0755 \"$tmp\" %s\n", BinaryPath)
	fmt.Fprintf(&b, "%s version\n", BinaryPath)
	return b.String()
}

// parseVersion extracts the release version from `cloudflared version` output.
func parseVersion(output string) string {
	m := versionRegex.FindStringSubmatch(output)
	if m == nil {
		return ""
	}
	return m[1]
}

// Status is the API view of the connector state.
type Status struct {
	Installed         bool   `json:"installed"`
	Version           string `json:"version"`
	Pinned            string `json:"pinned_version"`
	TokenInstalled    bool   `json:"token_installed"`
	ServiceState      string `json:"service_state"` // active|inactive|failed|activating|deactivating|unknown
	AptServiceRunning bool   `json:"apt_service_running"`
}

// Status reports binary, token, and unit state. Missing pieces are reported,
// never treated as errors, so the UI can render each next step.
func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{Pinned: Version, ServiceState: "unknown"}
	if result, err := s.exec.Run(ctx, BinaryPath, "version"); err == nil && result != nil && result.ExitCode == 0 {
		status.Installed = true
		status.Version = parseVersion(result.Stdout)
	}
	if result, err := s.exec.RunSudo(ctx, "test", "-f", TokenEnvPath); err == nil && result != nil && result.ExitCode == 0 {
		status.TokenInstalled = true
	}
	status.ServiceState = activeState(s.sudoOut(ctx, "systemctl", "is-active", UnitName))
	status.AptServiceRunning = activeState(s.sudoOut(ctx, "systemctl", "is-active", "cloudflared")) == "active"
	return status, nil
}

// sudoOut runs a privileged command and returns trimmed stdout, treating any
// failure as empty output (callers interpret the state themselves).
func (s *Service) sudoOut(ctx context.Context, name string, args ...string) string {
	result, err := s.exec.RunSudo(ctx, name, args...)
	if err != nil || result == nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

func activeState(out string) string {
	switch out {
	case "active", "inactive", "failed", "activating", "deactivating":
		return out
	default:
		return "unknown"
	}
}

// EnsureInstalled reports a precise error when connect is requested before
// the binary install happened.
func (s *Service) EnsureInstalled(ctx context.Context) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return model.NewDomainError("CLOUDFLARED_MISSING",
			"cloudflared is not installed; install it first on this page", nil)
	}
	return nil
}

// Install downloads, verifies, and installs the pinned binary as a background
// task. Returns the task ID for polling. Also used to update when the
// installed version differs from the pin.
func (s *Service) Install(ctx context.Context) (string, error) {
	if s.tasks == nil {
		return "", model.NewDomainError("TASK_RUNNER_UNAVAILABLE", "task runner is not available", nil)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "cloudflared_install", Module: "tunnel", Detail: "installing cloudflared " + Version})
	return s.tasks.Run("Install cloudflared "+Version, "bash", "-c", installScript()), nil
}

// Connect writes the root-only token env file, installs the unit, and starts
// the service. Runs inside a task; log streams progress to the UI. Restart
// (not start) makes re-connecting with a new token idempotent.
func (s *Service) Connect(ctx context.Context, token string, log func(string)) error {
	if err := ValidateToken(token); err != nil {
		return err
	}
	if err := s.EnsureInstalled(ctx); err != nil {
		return err
	}
	token = strings.TrimSpace(token)

	log("Writing connector token to " + TokenEnvPath + "\n")
	if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", "/etc/cloudflared"); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "chmod", "0750", "/etc/cloudflared"); err != nil {
		return fmt.Errorf("mode config directory: %w", err)
	}
	if _, err := s.exec.RunSudoWithInput(ctx, "TUNNEL_TOKEN="+token+"\n", "tee", TokenEnvPath); err != nil {
		return fmt.Errorf("write token env file: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "chmod", "0600", TokenEnvPath); err != nil {
		return fmt.Errorf("mode token env file: %w", err)
	}

	log("Writing systemd unit " + unitPath + "\n")
	unit := RenderUnit()
	if _, err := s.exec.RunSudo(ctx, "bash", "-c",
		fmt.Sprintf("cat > %s << 'UNITEOF'\n%sUNITEOF", unitPath, unit)); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}

	log("Starting " + UnitName + "\n")
	if _, err := s.exec.RunSudo(ctx, "systemctl", "enable", UnitName); err != nil {
		return fmt.Errorf("enable tunnel service: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "restart", UnitName); err != nil {
		return fmt.Errorf("start tunnel service: %w", err)
	}
	log("Connector started. Check tunnel health in the Cloudflare dashboard.\n")
	return nil
}

// Disconnect stops the connector and removes the unit and token. The binary
// stays installed.
func (s *Service) Disconnect(ctx context.Context) error {
	_, _ = s.exec.RunSudo(ctx, "systemctl", "stop", UnitName)
	_, _ = s.exec.RunSudo(ctx, "systemctl", "disable", UnitName)
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", unitPath, TokenEnvPath)
	if _, err := s.exec.RunSudo(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

// Restart restarts the connector service.
func (s *Service) Restart(ctx context.Context) error {
	if _, err := s.exec.RunSudo(ctx, "systemctl", "restart", UnitName); err != nil {
		return fmt.Errorf("restart tunnel service: %w", err)
	}
	return nil
}

// Logs tails the connector's journald log.
func (s *Service) Logs(ctx context.Context, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	if lines > 1000 {
		lines = 1000
	}
	result, err := s.exec.RunSudo(ctx, "journalctl", "-u", UnitName, "-n", strconv.Itoa(lines), "--no-pager")
	if err != nil {
		return "", model.NewDomainError("TUNNEL_LOGS_UNAVAILABLE", "could not read tunnel logs", nil)
	}
	if result == nil {
		return "", nil
	}
	return result.Stdout, nil
}
