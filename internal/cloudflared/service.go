// Package cloudflared installs and supervises the Cloudflare Tunnel connector
// (cloudflared) so a server without a public IP can serve its websites and
// the panel itself through an outbound-only tunnel. The tunnel is remotely
// managed: the administrator creates it in the Cloudflare Zero Trust
// dashboard and pastes the connector token into the panel.
package cloudflared

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
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
