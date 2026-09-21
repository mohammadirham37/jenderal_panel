# Cloudflare Tunnel Connector — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Panel meng-install dan menjalankan connector Cloudflare Tunnel (`cloudflared`) via token paste sehingga server tanpa IP publik tetap bisa melayani website dan panelnya.

**Architecture:** Modul baru `internal/cloudflared/` (service + handler, tanpa DB). Installer binary ter-pin mengikuti pola `internal/frankenphp`; unit systemd `jenderal-cloudflared.service` ditulis dengan pola heredoc `internal/website/octane.go`; token connector hidup di env file root-only `/etc/cloudflared/jenderal-cloudflared.env`, tidak pernah masuk DB/API response. Operasi lama (install, connect) lewat `taskrunner`; operasi cepat (disconnect, restart, logs) sinkron.

**Tech Stack:** Go (chi, taskrunner, executor), systemd unit, SvelteKit 5 runes + Tailwind v4 tokens, i18n domain baru `cft.*`.

**Spec:** `docs/superpowers/specs/2026-09-21-cloudflare-tunnel-design.md`

## Global Constraints

- ALL system commands via `internal/executor` (`RunSudo` untuk yang privileged); tidak pernah `sh -c` dengan input user. Token user hanya masuk lewat `RunSudoWithInput(ctx, input, "tee", path)` — literal stdin, bukan interpolasi shell.
- Flag cloudflared **terverifikasi terhadap binary 2026.9.1**: `cloudflared tunnel --no-autoupdate run`, env `TUNNEL_TOKEN` dihormati, `--token-file` tersedia sebagai fallback. Jangan mengubah flag tanpa verifikasi ulang.
- Long ops → `taskrunner` (return task ID, frontend poll `/api/v1/tasks/{id}`); frontend `bind:taskId` + `TaskProgress` + `storageKey`.
- Response API via `httputil.JSON` / `httputil.DecodeJSON` / `httputil.HandleError`; error domain via `model.NewDomainError` / `model.NewValidationError`.
- RBAC: `tunnel.view` + `tunnel.manage` (server-wide → tidak masuk `userRolePermissions`).
- i18n: setiap string user-facing lewat `translate($language, key)`; keys `cft.*` wajib ada di en DAN id (TypeScript meng-enforce); `nav.cloudflared` di `domains/core.ts`.
- Tailwind: pakai token tema (`gray-*`, `blue-*`, `red-*`, `green-*`) — bukan raw colors.
- Tidak ada migration DB; tidak menjalankan panel saat test (statis saja: `go test ./...`, `npm run check`, `npm test`, `npm run build`).
- Backend target Linux; module path `github.com/mohammadirham37/jenderal_panel`.

## File Structure

- Create `internal/cloudflared/service.go` — konstanta pin, `ValidateToken`, `RenderUnit`, `installScript`, `parseVersion`, `Status`, `Install`, `Connect`, `Disconnect`, `Restart`, `Logs`.
- Create `internal/cloudflared/service_test.go` — test pure + MockExecutor.
- Create `internal/cloudflared/handler.go` — 6 endpoint tipis + audit log.
- Create `internal/cloudflared/handler_test.go` — validation-path test.
- Modify `internal/api/router.go` — field Dependencies, handler, 6 routes.
- Modify `cmd/jenderal/main.go` — konstruksi service + wiring.
- Modify `internal/auth/rbac.go` — seed 2 permission.
- Create `web/src/lib/i18n/domains/tunnel.ts` — dict `cft.*` en+id.
- Modify `web/src/lib/i18n/index.ts` — register domain `tunnel`.
- Modify `web/src/lib/i18n/domains/core.ts` — key `nav.cloudflared` (en+id).
- Modify `web/src/routes/+layout.svelte` — nav item group infrastructure + ikon `cloud`.
- Create `web/src/routes/cloudflared/+page.svelte` — halaman status/koneksi/log/panduan.

---

### Task 1: Service — pure core (pin, validasi token, render unit, script install)

**Files:**
- Create: `internal/cloudflared/service.go`
- Test: `internal/cloudflared/service_test.go`

**Interfaces:**
- Consumes: `internal/executor`, `internal/audit`, `internal/taskrunner`, `internal/model` (sama seperti `internal/frankenphp`).
- Produces (dipakai task berikutnya): `Version`, `BinaryPath`, `TokenEnvPath`, `UnitName`, `ValidateToken(token string) error`, `RenderUnit() string`, `parseVersion(string) string`, `installScript() string`, `type Service` + `NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service` + `(s *Service) SetTaskRunner(tr *taskrunner.Runner)`, `type tokenPayload` (unexported).

- [ ] **Step 1: Tulis test gagal untuk pure core**

Buat `internal/cloudflared/service_test.go`:

```go
package cloudflared

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func validToken() string {
	b, _ := json.Marshal(tokenPayload{A: "acc", T: "tunnel-id", S: "secret"})
	return base64.StdEncoding.EncodeToString(b)
}

func TestValidateTokenAcceptsConnectorToken(t *testing.T) {
	if err := ValidateToken(validToken()); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateTokenAcceptsUnpaddedBase64(t *testing.T) {
	token := strings.TrimRight(validToken(), "=")
	if err := ValidateToken(token); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateTokenRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty":          "   ",
		"not base64":     "!!! not base64 !!!",
		"not json":       base64.StdEncoding.EncodeToString([]byte("hello world")),
		"missing secret": base64.StdEncoding.EncodeToString([]byte(`{"a":"acc"}`)),
	}
	for name, token := range cases {
		if err := ValidateToken(token); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestRenderUnitUsesEnvFileAndPinnedBinary(t *testing.T) {
	unit := RenderUnit()
	for _, want := range []string{
		"EnvironmentFile=" + TokenEnvPath,
		"ExecStart=" + BinaryPath + " tunnel --no-autoupdate run",
		"Restart=always",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestInstallScriptPinsChecksumsPerArch(t *testing.T) {
	script := installScript()
	if !strings.Contains(script, "cloudflared/releases/download/"+Version+"/cloudflared-linux-") {
		t.Fatalf("script missing pinned release URL:\n%s", script)
	}
	for arch, sum := range checksums {
		if !strings.Contains(script, arch) || !strings.Contains(script, sum) {
			t.Fatalf("script missing arch %s or checksum:\n%s", arch, script)
		}
	}
	if !strings.Contains(script, "unsupported architecture") {
		t.Fatalf("script missing unsupported arch guard:\n%s", script)
	}
}

func TestParseVersionReadsCloudflaredOutput(t *testing.T) {
	out := "cloudflared version 2026.9.1 (built 2026-09-11-13:35 UTC)\n"
	if got := parseVersion(out); got != Version {
		t.Fatalf("got %q want %q", got, Version)
	}
	if got := parseVersion("no version here"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
```

- [ ] **Step 2: Jalankan test, pastikan gagal compile**

Run: `go test ./internal/cloudflared/ -v`
Expected: FAIL — `undefined: ValidateToken` (package belum ada isinya).

- [ ] **Step 3: Tulis implementasi pure core**

Buat `internal/cloudflared/service.go`:

```go
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
```

Catatan: import `strconv` dipakai Task 2 (Logs) — kalau compiler komplain "imported and not used" di akhir Task 1, pindahkan import `strconv` ke Task 2 (boleh menambahkan method di Task 2 dengan importnya). `context` dipakai mulai Task 2; bila Task 1 hanya berisi pure core, tunda import `context`/`strconv` sampai Task 2 menggabungkan file yang sama.

- [ ] **Step 4: Jalankan test, pastikan pass**

Run: `go test ./internal/cloudflared/ -v`
Expected: PASS (6 test).

- [ ] **Step 5: Commit**

```bash
git add internal/cloudflared/service.go internal/cloudflared/service_test.go
git commit -m "feat(tunnel): pinned cloudflared core with token validation"
```

---

### Task 2: Service — executor methods (Status, Install, Connect, Disconnect, Restart, Logs)

**Files:**
- Modify: `internal/cloudflared/service.go` (tambah method di bawah pure core)
- Test: `internal/cloudflared/service_test.go` (tambah test MockExecutor)

**Interfaces:**
- Consumes: semua dari Task 1; `executor.MockExecutor` (fungsi `RunFunc`/`RunSudoFunc`/`RunSudoWithInputFunc`, `executor.Result{Stdout, Stderr, ExitCode, Duration}`).
- Produces (dipakai handler): `type Status struct {Installed bool; Version, Pinned string; TokenInstalled bool; ServiceState string; AptServiceRunning bool}` dengan json tags `installed, version, pinned_version, token_installed, service_state, apt_service_running`; `(s *Service) Status(ctx) (Status, error)`; `(s *Service) Install(ctx) (string, error)`; `(s *Service) Connect(ctx context.Context, token string, log func(string)) error`; `(s *Service) Disconnect(ctx) error`; `(s *Service) Restart(ctx) error`; `(s *Service) Logs(ctx, lines int) (string, error)`; `(s *Service) EnsureInstalled(ctx) error`.

- [ ] **Step 1: Tulis test gagal untuk method executor**

Tambahkan ke `internal/cloudflared/service_test.go`:

```go
func TestStatusReadsBinaryTokenAndService(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "cloudflared version " + Version + "\n"}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch {
			case name == "systemctl" && args[len(args)-1] == UnitName:
				return &executor.Result{ExitCode: 0, Stdout: "active\n"}, nil
			case name == "systemctl":
				return &executor.Result{ExitCode: 3, Stdout: "inactive\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	s := NewService(mock, nil)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Installed || st.Version != Version || !st.TokenInstalled {
		t.Fatalf("bad status: %+v", st)
	}
	if st.ServiceState != "active" {
		t.Fatalf("service state %q, want active", st.ServiceState)
	}
	if st.AptServiceRunning {
		t.Fatal("apt cloudflared service should be inactive")
	}
}

func TestStatusReportsMissingBinary(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 127}, errors.New("exec: not found")
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 4, Stdout: "inactive\n"}, nil
		},
	}
	s := NewService(mock, nil)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("missing binary must not error: %v", err)
	}
	if st.Installed || st.TokenInstalled || st.ServiceState != "inactive" {
		t.Fatalf("bad status: %+v", st)
	}
}

func TestConnectWritesTokenUnitAndRestarts(t *testing.T) {
	var teeInput, heredoc string
	var systemctlCmds [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "cloudflared version " + Version + "\n"}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch {
			case name == "systemctl":
				systemctlCmds = append(systemctlCmds, args)
			case name == "bash":
				heredoc = args[1]
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			if name == "tee" {
				teeInput = input
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	s := NewService(mock, nil)
	if err := s.Connect(context.Background(), validToken(), func(string) {}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(teeInput, "TUNNEL_TOKEN=") {
		t.Fatalf("env file missing token: %q", teeInput)
	}
	if !strings.Contains(heredoc, "ExecStart="+BinaryPath+" tunnel --no-autoupdate run") {
		t.Fatalf("unit not written via heredoc: %q", heredoc)
	}
	restarted := false
	for _, args := range systemctlCmds {
		if len(args) >= 2 && args[0] == "restart" && args[1] == UnitName {
			restarted = true
		}
	}
	if !restarted {
		t.Fatalf("service not restarted: %v", systemctlCmds)
	}
}

func TestConnectRejectsInvalidTokenBeforeRunningCommands(t *testing.T) {
	called := false
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			called = true
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			called = true
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			called = true
			return &executor.Result{}, nil
		},
	}
	s := NewService(mock, nil)
	if err := s.Connect(context.Background(), "garbage", func(string) {}); err == nil {
		t.Fatal("expected validation error")
	}
	if called {
		t.Fatal("no command may run for an invalid token")
	}
}

func TestDisconnectStopsAndRemovesAssets(t *testing.T) {
	var cmds [][]string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			cmds = append(cmds, append([]string{name}, args...))
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	s := NewService(mock, nil)
	if err := s.Disconnect(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := fmt.Sprint(cmds)
	for _, want := range []string{"stop", "disable", UnitName, TokenEnvPath, "daemon-reload"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("disconnect missing %s: %v", want, cmds)
		}
	}
}

func TestLogsTailsJournalctlWithCap(t *testing.T) {
	var args []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, argsc ...string) (*executor.Result, error) {
			args = append([]string{name}, argsc...)
			return &executor.Result{ExitCode: 0, Stdout: "log line\n"}, nil
		},
	}
	s := NewService(mock, nil)
	out, err := s.Logs(context.Background(), 50)
	if err != nil || out != "log line\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := fmt.Sprint(args); !strings.Contains(got, UnitName) || !strings.Contains(got, "50") || !strings.Contains(got, "--no-pager") {
		t.Fatalf("bad journalctl args: %v", args)
	}
	if _, err := s.Logs(context.Background(), 99999); err != nil {
		t.Fatalf("oversized lines must be capped, not error: %v", err)
	}
}
```

Tambahkan `"context"`, `"errors"`, `"fmt"` ke import test file.

- [ ] **Step 2: Jalankan test, pastikan gagal**

Run: `go test ./internal/cloudflared/ -v`
Expected: FAIL — `s.Status undefined` dst.

- [ ] **Step 3: Implementasikan method executor**

Tambahkan di `internal/cloudflared/service.go` (lengkapi import `context`, `strconv`):

```go
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
// failure as empty output (callers interpret state themselves).
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
```

- [ ] **Step 4: Jalankan test, pastikan semua pass**

Run: `go test ./internal/cloudflared/ -v`
Expected: PASS (semua test, `-race` juga).

- [ ] **Step 5: Commit**

```bash
git add internal/cloudflared/service.go internal/cloudflared/service_test.go
git commit -m "feat(tunnel): connector status, connect, disconnect, logs"
```

---

### Task 3: Handler HTTP + test

**Files:**
- Create: `internal/cloudflared/handler.go`
- Test: `internal/cloudflared/handler_test.go`

**Interfaces:**
- Consumes: `Service` methods dari Task 2; `httputil.JSON/DecodeJSON/HandleError`; `taskrunner.Runner.RunFuncWithOptions(taskrunner.Options{Name, Module, Timeout}, func(ctx, log) error) string` (pola `trafficguard/handler.go:54`); `audit.Service.Log`.
- Produces (dipakai router): `type Handler` + `NewHandler(s *Service, t *taskrunner.Runner, a *audit.Service) *Handler` dengan method `Status`, `Install`, `Connect`, `Disconnect`, `Restart`, `Logs` (semua `http.ResponseWriter, *http.Request`).

- [ ] **Step 1: Tulis test gagal**

Buat `internal/cloudflared/handler_test.go`:

```go
package cloudflared

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectRejectsInvalidToken(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"token":"garbage"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.Connect(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
```

- [ ] **Step 2: Jalankan test, pastikan gagal**

Run: `go test ./internal/cloudflared/ -run TestConnectRejectsInvalidToken -v`
Expected: FAIL — `h.Connect undefined` (handler belum ada).

- [ ] **Step 3: Implementasikan handler**

Buat `internal/cloudflared/handler.go`:

```go
package cloudflared

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler exposes the Cloudflare Tunnel connector over HTTP.
type Handler struct {
	service *Service
	tasks   *taskrunner.Runner
	audit   *audit.Service
}

// NewHandler creates a new cloudflared Handler.
func NewHandler(s *Service, t *taskrunner.Runner, a *audit.Service) *Handler {
	return &Handler{service: s, tasks: t, audit: a}
}

// Status reports connector state.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.service.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, st)
}

// Install downloads and installs the pinned binary (also used for updates).
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Install(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "cloudflared_install", "cloudflared", "accepted install; task:"+task)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": task})
}

// Connect wires the pasted connector token into the systemd service.
func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := ValidateToken(req.Token); err != nil {
		httputil.HandleError(w, err)
		return
	}
	task := h.tasks.RunFuncWithOptions(
		taskrunner.Options{Name: "Connect Cloudflare Tunnel", Module: "tunnel", Timeout: 2 * time.Minute},
		func(ctx context.Context, log func(string)) error {
			return h.service.Connect(ctx, req.Token, log)
		},
	)
	h.log(r, "cloudflared_connect", "cloudflared", "accepted connect; task:"+task)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": task})
}

// Disconnect stops the connector and removes token + unit.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Disconnect(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "cloudflared_disconnect", "cloudflared", "tunnel disconnected")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// Restart restarts the connector service.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Restart(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "cloudflared_restart", "cloudflared", "tunnel service restarted")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}

// Logs tails the connector's journald log.
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	lines, err := strconv.Atoi(r.URL.Query().Get("lines"))
	if err != nil || lines <= 0 {
		lines = 200
	}
	output, err := h.service.Logs(r.Context(), lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"lines": lines, "output": output})
}

func (h *Handler) log(r *http.Request, action, target, detail string) {
	if h.audit == nil {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{UserID: u.ID, Action: action, Module: "tunnel", Target: target, Detail: detail, IP: r.RemoteAddr})
}
```

- [ ] **Step 4: Jalankan test, pastikan pass**

Run: `go test ./internal/cloudflared/ -v -race`
Expected: PASS semua.

- [ ] **Step 5: Commit**

```bash
git add internal/cloudflared/handler.go internal/cloudflared/handler_test.go
git commit -m "feat(tunnel): HTTP handler for connector endpoints"
```

---

### Task 4: Wiring — router, main.go, RBAC

**Files:**
- Modify: `internal/api/router.go` (Dependencies struct ~line 52-100; handler konstruksi ~line 124-152; routes setelah blok FrankenPHP ~line 250-262)
- Modify: `cmd/jenderal/main.go` (service ~line 301; `SetTaskRunner` ~line 327; `api.Dependencies{...}` ~line 380-415)
- Modify: `internal/auth/rbac.go` (permissions slice ~line 60-119)

**Interfaces:**
- Consumes: `cloudflared.NewService`, `NewHandler` dari Task 1-3.
- Produces: routes `/api/v1/cloudflared*` berpermisi `tunnel.view`/`tunnel.manage`; permissions ter-seed.

- [ ] **Step 1: Seed permission RBAC**

Di `internal/auth/rbac.go`, tambahkan ke slice `permissions` (setelah baris `{"services.manage", "services"},`):

```go
		{"tunnel.view", "tunnel"},
		{"tunnel.manage", "tunnel"},
```

Jangan menambah apa pun di `userRolePermissions` (resource server-wide, admin-only).

- [ ] **Step 2: Daftarkan handler dan routes di router.go**

Di `internal/api/router.go`:

1. Import: `"github.com/mohammadirham37/jenderal_panel/internal/cloudflared"`.
2. Field baru di struct `Dependencies` (dekat `FrankenphpSvc *frankenphp.Service`):

```go
	CloudflaredSvc  *cloudflared.Service
```

3. Konstruksi handler di awal `NewRouter` (dekat `frankenphpHandler := ...`):

```go
	cloudflaredHandler := cloudflared.NewHandler(deps.CloudflaredSvc, deps.Tasks, deps.AuditSvc)
```

4. Routes setelah blok FrankenPHP/runtimes (sebelum komentar `// Panel domain`):

```go
			// Cloudflare Tunnel connector (server-wide, admin only)
			r.With(auth.RequirePermission(deps.RBAC, "tunnel.view")).
				Get("/cloudflared", cloudflaredHandler.Status)
			r.With(auth.RequirePermission(deps.RBAC, "tunnel.manage")).
				Post("/cloudflared/install", cloudflaredHandler.Install)
			r.With(auth.RequirePermission(deps.RBAC, "tunnel.manage")).
				Post("/cloudflared/connect", cloudflaredHandler.Connect)
			r.With(auth.RequirePermission(deps.RBAC, "tunnel.manage")).
				Post("/cloudflared/disconnect", cloudflaredHandler.Disconnect)
			r.With(auth.RequirePermission(deps.RBAC, "tunnel.manage")).
				Post("/cloudflared/restart", cloudflaredHandler.Restart)
			r.With(auth.RequirePermission(deps.RBAC, "tunnel.view")).
				Get("/cloudflared/logs", cloudflaredHandler.Logs)
```

- [ ] **Step 3: Wiring main.go**

Di `cmd/jenderal/main.go`:

1. Import: `"github.com/mohammadirham37/jenderal_panel/internal/cloudflared"`.
2. Setelah `frankenphpSvc := frankenphp.NewService(exec, auditSvc)` (~line 301):

```go
	cloudflaredSvc := cloudflared.NewService(exec, auditSvc)
```

3. Setelah `frankenphpSvc.SetTaskRunner(tasks)` (~line 327):

```go
	cloudflaredSvc.SetTaskRunner(tasks)
```

4. Di literal `api.Dependencies{...}` (dekat `FrankenphpSvc: frankenphpSvc,`):

```go
		CloudflaredSvc:  cloudflaredSvc,
```

- [ ] **Step 4: Verifikasi build + vet + seluruh test suite**

Run: `go build ./... && go vet ./... && go test ./internal/cloudflared/ ./internal/auth/ -v -race`
Expected: build OK, vet bersih, test PASS. (RBAC seed count berubah 63 → 65; kalau ada test yang mem-pin jumlah permission dan gagal, update angkanya sesuai pesan test.)

- [ ] **Step 5: Commit**

```bash
git add internal/api/router.go cmd/jenderal/main.go internal/auth/rbac.go
git commit -m "feat(tunnel): wire cloudflared module into router, main, RBAC"
```

---

### Task 5: Frontend i18n + navigasi

**Files:**
- Create: `web/src/lib/i18n/domains/tunnel.ts`
- Modify: `web/src/lib/i18n/index.ts` (import + spread `tunnel` di en dan id)
- Modify: `web/src/lib/i18n/domains/core.ts` (key `nav.cloudflared` di en dan id)
- Modify: `web/src/routes/+layout.svelte` (nav item + ikon `cloud`)

**Interfaces:**
- Consumes: format domain dict (`export const dict = { en, id }`, lihat `domains/disk.ts`).
- Produces: keys `cft.*` dan `nav.cloudflared` untuk Task 6.

- [ ] **Step 1: Buat domain dict `tunnel.ts`**

Buat `web/src/lib/i18n/domains/tunnel.ts`:

```ts
// Domain dictionary: see ../index.ts for how these merge into the global lookup.
// Fill both `en` and `id` — TypeScript enforces that `id` has exactly the keys of `en`.
// Owns key prefixes: `cft.` (Cloudflare Tunnel page).

const en = {
	'cft.title': 'Cloudflare Tunnel',
	'cft.subtitle': 'Expose this server without a public IP through an outbound-only Cloudflare Tunnel',
	'cft.loadFailed': 'Failed to load tunnel status',
	'cft.status.title': 'Connector Status',
	'cft.status.binary': 'cloudflared binary',
	'cft.status.notInstalled': 'Not installed',
	'cft.status.version': 'Installed version',
	'cft.status.pinned': 'Pinned version',
	'cft.status.updateAvailable': 'Update available',
	'cft.status.service': 'Tunnel service',
	'cft.state.active': 'Connected (service active)',
	'cft.state.inactive': 'Stopped',
	'cft.state.failed': 'Failed — check logs',
	'cft.state.activating': 'Starting…',
	'cft.state.deactivating': 'Stopping…',
	'cft.state.unknown': 'Not set up',
	'cft.status.token': 'Connector token',
	'cft.status.tokenSet': 'Saved',
	'cft.status.tokenMissing': 'Not set',
	'cft.conflict': 'A cloudflared service from the apt package is running. Two connectors for the same account can fight over the connection — consider stopping it on the Services page.',
	'cft.install.button': 'Install cloudflared {version}',
	'cft.install.task': 'Installing cloudflared…',
	'cft.token.title': 'Connect a Tunnel',
	'cft.token.desc': 'Paste the connector token from Cloudflare Zero Trust → Networks → Tunnels → your tunnel. The token is stored root-only on this server and never displayed again.',
	'cft.token.placeholder': 'eyJhIjoi…(paste connector token here)',
	'cft.token.show': 'Show',
	'cft.token.hide': 'Hide',
	'cft.token.required': 'Paste the connector token first',
	'cft.token.connect': 'Connect Tunnel',
	'cft.token.task': 'Connecting tunnel…',
	'cft.token.replace': 'Connecting again replaces the saved token and restarts the service.',
	'cft.disconnect': 'Disconnect',
	'cft.disconnectConfirm': 'Stop the tunnel service and remove the saved token?',
	'cft.restart': 'Restart',
	'cft.logs.title': 'Tunnel Logs',
	'cft.logs.refresh': 'Refresh logs',
	'cft.logs.empty': 'No logs yet.',
	'cft.guide.title': 'How to set it up',
	'cft.guide.step1': 'In the Cloudflare dashboard open Zero Trust → Networks → Tunnels and create a tunnel (Cloudflared connector).',
	'cft.guide.step2': 'Copy the connector token it offers and paste it above, then press Connect Tunnel.',
	'cft.guide.step3': 'Back in Cloudflare, add a Public Hostname for each site: subdomain + your domain, service HTTP://localhost:80 for websites served by nginx.',
	'cft.guide.step4': 'To reach this panel itself without a public IP, add another Public Hostname pointing to HTTPS://localhost:8443 and enable “No TLS Verify” in the additional settings.',
	'cft.guide.note': 'The tunnel connects outbound only — no port forwarding or public IP needed.'
};

const id: Record<keyof typeof en, string> = {
	'cft.title': 'Cloudflare Tunnel',
	'cft.subtitle': 'Ekspos server ini tanpa IP publik lewat Cloudflare Tunnel yang hanya memakai koneksi keluar',
	'cft.loadFailed': 'Gagal memuat status tunnel',
	'cft.status.title': 'Status Connector',
	'cft.status.binary': 'Binary cloudflared',
	'cft.status.notInstalled': 'Belum terinstall',
	'cft.status.version': 'Versi terinstall',
	'cft.status.pinned': 'Versi pin',
	'cft.status.updateAvailable': 'Update tersedia',
	'cft.status.service': 'Service tunnel',
	'cft.state.active': 'Terhubung (service aktif)',
	'cft.state.inactive': 'Berhenti',
	'cft.state.failed': 'Gagal — cek log',
	'cft.state.activating': 'Memulai…',
	'cft.state.deactivating': 'Menghentikan…',
	'cft.state.unknown': 'Belum disiapkan',
	'cft.status.token': 'Token connector',
	'cft.status.tokenSet': 'Tersimpan',
	'cft.status.tokenMissing': 'Belum diatur',
	'cft.conflict': 'Ada service cloudflared dari paket apt yang sedang berjalan. Dua connector untuk akun yang sama bisa saling rebutan koneksi — pertimbangkan menghentikannya di halaman Services.',
	'cft.install.button': 'Install cloudflared {version}',
	'cft.install.task': 'Menginstall cloudflared…',
	'cft.token.title': 'Hubungkan Tunnel',
	'cft.token.desc': 'Tempel token connector dari Cloudflare Zero Trust → Networks → Tunnels → tunnel Anda. Token disimpan root-only di server ini dan tidak akan ditampilkan lagi.',
	'cft.token.placeholder': 'eyJhIjoi…(tempel token connector di sini)',
	'cft.token.show': 'Lihat',
	'cft.token.hide': 'Sembunyikan',
	'cft.token.required': 'Tempel token connector dulu',
	'cft.token.connect': 'Hubungkan Tunnel',
	'cft.token.task': 'Menghubungkan tunnel…',
	'cft.token.replace': 'Menghubungkan lagi akan mengganti token tersimpan dan me-restart service.',
	'cft.disconnect': 'Putuskan',
	'cft.disconnectConfirm': 'Hentikan service tunnel dan hapus token tersimpan?',
	'cft.restart': 'Restart',
	'cft.logs.title': 'Log Tunnel',
	'cft.logs.refresh': 'Segarkan log',
	'cft.logs.empty': 'Belum ada log.',
	'cft.guide.title': 'Cara mengatur',
	'cft.guide.step1': 'Di dashboard Cloudflare buka Zero Trust → Networks → Tunnels dan buat tunnel (connector Cloudflared).',
	'cft.guide.step2': 'Salin token connector yang ditawarkan, tempel di atas, lalu tekan Hubungkan Tunnel.',
	'cft.guide.step3': 'Kembali di Cloudflare, tambahkan Public Hostname untuk setiap situs: subdomain + domain Anda, service HTTP://localhost:80 untuk website yang dilayani nginx.',
	'cft.guide.step4': 'Untuk mengakses panel ini sendiri tanpa IP publik, tambahkan Public Hostname lain yang mengarah ke HTTPS://localhost:8443 dan aktifkan “No TLS Verify” di pengaturan tambahan.',
	'cft.guide.note': 'Tunnel hanya memakai koneksi keluar — tidak perlu port forwarding atau IP publik.'
};

export const dict = { en, id };
```

Catatan: kalau format domain lain memakai `Record<string, string>` untuk `id` (lihat file disk.ts bagian bawah), ikuti format persis file existing — yang penting `export const dict = { en, id }` dan key id == key en.

- [ ] **Step 2: Register domain di `index.ts`**

Di `web/src/lib/i18n/index.ts`: import `{ dict as tunnel } from './domains/tunnel';` dan spread `...tunnel.en` / `...tunnel.id` di masing-masing blok (setelah `...disk`).

- [ ] **Step 3: Tambah `nav.cloudflared` di `core.ts`**

Di `web/src/lib/i18n/domains/core.ts`, blok en (dekat `'nav.disk'`): `'nav.cloudflared': 'Cloudflare Tunnel',` — blok id: `'nav.cloudflared': 'Cloudflare Tunnel',`.

- [ ] **Step 4: Nav item + ikon cloud di layout**

Di `web/src/routes/+layout.svelte`, group `nav.group.infrastructure`, tambahkan setelah item `/services`:

```ts
				{ href: '/cloudflared', permission: 'tunnel.view', labelKey: 'nav.cloudflared', icon: 'cloud' },
```

Dan di if-chain ikon (sebelum `{:else if item.icon === 'lock'}`):

```svelte
								{:else if item.icon === 'cloud'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M2.25 15a4.5 4.5 0 004.5 4.5H18a3.75 3.75 0 001.332-7.257 3 3 0 00-3.758-3.848 5.25 5.25 0 00-10.233 2.33A4.502 4.502 0 002.25 15z" />
```

- [ ] **Step 5: Verifikasi svelte-check**

Run: `cd web && npm run check`
Expected: 0 errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/i18n/domains/tunnel.ts web/src/lib/i18n/index.ts web/src/lib/i18n/domains/core.ts web/src/routes/+layout.svelte
git commit -m "feat(tunnel): i18n domain and navigation for cloudflare tunnel"
```

---

### Task 6: Halaman `/cloudflared`

**Files:**
- Create: `web/src/routes/cloudflared/+page.svelte`

**Interfaces:**
- Consumes: `api.get<T>/post<T>` dari `$lib/api` (unwraps `{data}`); `translate($language, key)` dari `$lib/stores/language`; `TaskProgress` props `{taskId = $bindable(''), storageKey?, onComplete?, onMissing?}`; endpoint dari Task 4; keys `cft.*` dari Task 5.
- Produces: halaman `/cloudflared`.

- [ ] **Step 1: Buat halaman**

Buat `web/src/routes/cloudflared/+page.svelte`:

```svelte
<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { language, translate } from '$lib/stores/language';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	interface TunnelStatus {
		installed: boolean;
		version: string;
		pinned_version: string;
		token_installed: boolean;
		service_state: string;
		apt_service_running: boolean;
	}

	let status = $state<TunnelStatus | null>(null);
	let loading = $state(true);
	let error = $state('');

	let token = $state('');
	let showToken = $state(false);
	let connecting = $state(false);
	let connectError = $state('');
	let installTaskId = $state('');
	let connectTaskId = $state('');

	let logs = $state('');
	let logsLoading = $state(false);
	let actionError = $state('');

	const t = (key: string) => translate($language, key);

	async function load() {
		error = '';
		try {
			status = await api.get<TunnelStatus>('/cloudflared');
		} catch (e) {
			error = e instanceof Error ? e.message : t('cft.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function loadLogs() {
		logsLoading = true;
		try {
			const res = await api.get<{ lines: number; output: string }>('/cloudflared/logs?lines=200');
			logs = res.output ?? '';
		} catch {
			logs = '';
		} finally {
			logsLoading = false;
		}
	}

	async function install() {
		actionError = '';
		try {
			const res = await api.post<{ task_id: string }>('/cloudflared/install', {});
			installTaskId = res.task_id;
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	async function connect() {
		connectError = '';
		actionError = '';
		if (!token.trim()) {
			connectError = t('cft.token.required');
			return;
		}
		connecting = true;
		try {
			const res = await api.post<{ task_id: string }>('/cloudflared/connect', { token: token.trim() });
			connectTaskId = res.task_id;
			token = '';
		} catch (e) {
			connectError = e instanceof Error ? e.message : t('cft.loadFailed');
		} finally {
			connecting = false;
		}
	}

	async function disconnect() {
		if (!confirm(t('cft.disconnectConfirm'))) return;
		actionError = '';
		try {
			await api.post('/cloudflared/disconnect', {});
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	async function restart() {
		actionError = '';
		try {
			await api.post('/cloudflared/restart', {});
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	function stateBadge(state: string): string {
		if (state === 'active') return 'bg-green-500/15 text-green-300 border-green-500/30';
		if (state === 'failed') return 'bg-red-500/15 text-red-300 border-red-500/30';
		return 'bg-gray-500/15 text-gray-300 border-gray-500/30';
	}

	onMount(() => {
		load();
		loadLogs();
	});
</script>

<div class="mx-auto max-w-4xl px-4 py-8 space-y-6">
	<header>
		<h1 class="text-2xl font-semibold text-gray-100">{t('cft.title')}</h1>
		<p class="mt-1 text-sm text-gray-400">{t('cft.subtitle')}</p>
	</header>

	{#if error}
		<div class="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{error}</div>
	{/if}
	{#if actionError}
		<div class="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{actionError}</div>
	{/if}

	{#if loading}
		<div class="text-sm text-gray-400">{t('common.loading')}</div>
	{:else if status}
		{#if status.apt_service_running}
			<div class="rounded-lg border border-yellow-500/30 bg-yellow-500/10 px-4 py-3 text-sm text-yellow-200">
				{t('cft.conflict')}
			</div>
		{/if}

		<!-- Status -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<h2 class="text-base font-medium text-gray-100">{t('cft.status.title')}</h2>
			<dl class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
				<div>
					<dt class="text-xs uppercase tracking-wide text-gray-500">{t('cft.status.binary')}</dt>
					<dd class="mt-1 text-sm text-gray-200">
						{#if status.installed}
							{t('cft.status.version')}: {status.version || '?'}
							{#if status.version !== status.pinned_version}
								<span class="ml-2 rounded border border-yellow-500/30 bg-yellow-500/10 px-1.5 py-0.5 text-xs text-yellow-200">
									{t('cft.status.updateAvailable')} ({status.pinned_version})
								</span>
							{/if}
						{:else}
							{t('cft.status.notInstalled')} · {t('cft.status.pinned')}: {status.pinned_version}
						{/if}
					</dd>
				</div>
				<div>
					<dt class="text-xs uppercase tracking-wide text-gray-500">{t('cft.status.service')}</dt>
					<dd class="mt-1">
						<span class="rounded border px-2 py-0.5 text-sm {stateBadge(status.service_state)}">
							{t('cft.state.' + status.service_state)}
						</span>
					</dd>
				</div>
				<div>
					<dt class="text-xs uppercase tracking-wide text-gray-500">{t('cft.status.token')}</dt>
					<dd class="mt-1 text-sm text-gray-200">{status.token_installed ? t('cft.status.tokenSet') : t('cft.status.tokenMissing')}</dd>
				</div>
			</dl>
			<div class="mt-5 flex flex-wrap gap-2">
				{#if !status.installed || status.version !== status.pinned_version}
					<button class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50" disabled={!!installTaskId} onclick={install}>
						{t('cft.install.button').replace('{version}', status.pinned_version)}
					</button>
				{/if}
				{#if status.token_installed}
					<button class="rounded-lg border border-gray-700 px-4 py-2 text-sm text-gray-200 hover:bg-white/5" onclick={restart}>
						{t('cft.restart')}
					</button>
					<button class="rounded-lg border border-red-500/40 px-4 py-2 text-sm text-red-300 hover:bg-red-500/10" onclick={disconnect}>
						{t('cft.disconnect')}
					</button>
				{/if}
			</div>
			{#if installTaskId}
				<div class="mt-4">
					<TaskProgress bind:taskId={installTaskId} storageKey="jenderal_cft_install" onComplete={() => { load(); loadLogs(); }} />
				</div>
			{/if}
		</section>

		<!-- Connect -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<h2 class="text-base font-medium text-gray-100">{t('cft.token.title')}</h2>
			<p class="mt-1 text-sm text-gray-400">{t('cft.token.desc')}</p>
			{#if status.token_installed}
				<p class="mt-2 text-xs text-yellow-200/80">{t('cft.token.replace')}</p>
			{/if}
			<div class="mt-4 flex gap-2">
				<input
					type={showToken ? 'text' : 'password'}
					bind:value={token}
					placeholder={t('cft.token.placeholder')}
					autocomplete="off"
					spellcheck="false"
					class="w-full rounded-lg border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none"
				/>
				<button class="shrink-0 rounded-lg border border-gray-700 px-3 py-2 text-sm text-gray-300 hover:bg-white/5" onclick={() => (showToken = !showToken)}>
					{showToken ? t('cft.token.hide') : t('cft.token.show')}
				</button>
			</div>
			{#if connectError}
				<p class="mt-2 text-sm text-red-300">{connectError}</p>
			{/if}
			<button class="mt-4 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50" disabled={connecting} onclick={connect}>
				{t('cft.token.connect')}
			</button>
			{#if connectTaskId}
				<div class="mt-4">
					<TaskProgress bind:taskId={connectTaskId} storageKey="jenderal_cft_connect" onComplete={() => { load(); loadLogs(); }} />
				</div>
			{/if}
		</section>

		<!-- Guide -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<h2 class="text-base font-medium text-gray-100">{t('cft.guide.title')}</h2>
			<ol class="mt-3 list-decimal space-y-2 pl-5 text-sm text-gray-300">
				<li>{t('cft.guide.step1')}</li>
				<li>{t('cft.guide.step2')}</li>
				<li>{t('cft.guide.step3')}</li>
				<li>{t('cft.guide.step4')}</li>
			</ol>
			<p class="mt-3 text-xs text-gray-500">{t('cft.guide.note')}</p>
		</section>

		<!-- Logs -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<div class="flex items-center justify-between">
				<h2 class="text-base font-medium text-gray-100">{t('cft.logs.title')}</h2>
				<button class="rounded-lg border border-gray-700 px-3 py-1.5 text-sm text-gray-300 hover:bg-white/5" onclick={loadLogs}>
					{t('cft.logs.refresh')}
				</button>
			</div>
			<pre class="mt-3 max-h-96 overflow-auto rounded-lg bg-gray-950 p-3 text-xs leading-relaxed text-gray-300">{logs || t('cft.logs.empty')}</pre>
		</section>
	{/if}
</div>
```

Catatan implementasi: key `common.loading` sudah ada di domain `common` — bila ternyata tidak ada, ganti dengan `t('cft.loadFailed')` atau tambahkan key `cft.loading` di kedua bahasa. Untuk `{t('cft.install.button').replace('{version}', ...)}` — bila pola existing lebih menyukai interpolasi manual, ganti dengan dua key (`cft.install.button` + versi di elemen terpisah).

- [ ] **Step 2: Verifikasi check + test + build frontend**

Run: `cd web && npm run check && npm test && npm run build`
Expected: check 0 errors; test pass; build sukses.

- [ ] **Step 3: Commit**

```bash
git add web/src/routes/cloudflared/+page.svelte
git commit -m "feat(tunnel): cloudflare tunnel page with status, connect, logs"
```

---

### Task 7: Validasi akhir + push ke main

**Files:** tidak ada file baru; verifikasi menyeluruh.

- [ ] **Step 1: Backend penuh**

Run: `go vet ./... && go test ./... -race`
Expected: vet bersih, semua test PASS.

- [ ] **Step 2: Frontend penuh**

Run: `cd web && npm run check && npm test && npm run build`
Expected: 0 errors, test pass, build sukses.

- [ ] **Step 3: Lint (kalau golangci-lint tersedia)**

Run: `make lint`
Expected: bersih. (Bila tool tidak terinstall di mesin dev, catat dan lanjut — vet sudah jalan di Step 1.)

- [ ] **Step 4: Build binary penuh (embed frontend)**

Run: `make build`
Expected: binary sukses dibangun dengan `web/build/` ter-embed.

- [ ] **Step 5: Push ke main (diminta user)**

Run:
```bash
git push origin main
```
Expected: push sukses. Setelah itu deployment dilakukan user via tombol **Update** di panel (jangan build manual di server).

---

## Self-Review

1. **Spec coverage:** installer pinned+checksum (T1), token env root-only + unit systemd + hardening minimal (T1/T2), Status+konflik apt (T2), 6 endpoint+RBAC (T3/T4), halaman+nav+i18n+guide (T5/T6), tanpa migration ✓, YAGNI list tidak diimplementasikan ✓.
2. **Placeholder scan:** tidak ada TBD/TODO; semua step berisi kode lengkap.
3. **Type consistency:** `Status` json tags sama di service (T2) dan interface TS (T6); `NewHandler(s, t, a)` konsisten router (T4); `Connect(ctx, token, log)` konsisten handler (T3) dan test (T2); storageKey `jenderal_cft_install`/`jenderal_cft_connect` hanya di T6.
