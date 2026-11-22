package releaseupgrade

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Service runs and reports the Ubuntu release upgrade.
type Service struct {
	exec executor.CommandExecutor
	db   *sql.DB
	http *http.Client

	metaReleaseURL  string
	suryPackagesURL string
	// runTask starts a background task; tests replace it.
	runTask func(name string, commands [][]string) string

	mu sync.Mutex
}

// NewService creates the release upgrade service.
func NewService(exec executor.CommandExecutor, db *sql.DB, tasks *taskrunner.Runner) *Service {
	arch := runtime.GOARCH
	if arch != "arm64" {
		arch = "amd64"
	}
	s := &Service{
		exec:            exec,
		db:              db,
		http:            &http.Client{Timeout: 15 * time.Second},
		metaReleaseURL:  "https://changelogs.ubuntu.com/meta-release-lts",
		suryPackagesURL: "https://packages.sury.org/php/dists/" + ToCodename + "/main/binary-" + arch + "/Packages.gz",
	}
	if tasks != nil {
		s.runTask = tasks.RunMultiple
	}
	return s
}

// Status is the state of the current or last upgrade.
type Status struct {
	State    string   `json:"state"`
	Running  bool     `json:"running"`
	Codename string   `json:"codename"`
	Log      string   `json:"log"`
	Warnings []string `json:"warnings"`
}

// Status reads the state file, the unit and the log tail. A "running" state
// whose unit is gone (the machine rebooted or the script was killed) is
// reported as failed.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	st := &Status{State: StateIdle, Warnings: []string{}}
	if res, err := s.exec.RunSudo(ctx, "cat", StateDir+"/state"); err == nil && res != nil && res.ExitCode == 0 {
		if v := strings.TrimSpace(res.Stdout); v != "" {
			st.State = v
		}
	}
	if res, err := s.exec.RunSudo(ctx, "systemctl", "is-active", UnitName); err == nil && res != nil {
		switch strings.TrimSpace(res.Stdout) {
		case "active", "activating", "reloading":
			st.Running = true
		}
	}
	if st.State == StateRunning && !st.Running {
		st.State = StateFailed
	}
	// The upgrade recorded the boot it finished in; once the server has
	// booted again the reboot is done and there is nothing left to show.
	if st.State == StateRebootRequired && s.rebootedSinceUpgrade(ctx) {
		st.State = StateIdle
	}
	if st.State != StateIdle {
		if res, err := s.exec.RunSudo(ctx, "tail", "-n", "400", LogPath); err == nil && res != nil && res.ExitCode == 0 {
			st.Log = res.Stdout
		}
		if res, err := s.exec.RunSudo(ctx, "cat", StateDir+"/warnings"); err == nil && res != nil && res.ExitCode == 0 {
			for _, line := range strings.Split(res.Stdout, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					st.Warnings = append(st.Warnings, line)
				}
			}
		}
	}
	st.Codename = s.codename(ctx)
	return st, nil
}

// rebootedSinceUpgrade compares the boot ID recorded when the upgrade
// finished with the current one.
func (s *Service) rebootedSinceUpgrade(ctx context.Context) bool {
	res, err := s.exec.RunSudo(ctx, "cat", StateDir+"/boot_id")
	if err != nil || res == nil || res.ExitCode != 0 {
		return false
	}
	recorded := strings.TrimSpace(res.Stdout)
	cur, err := s.exec.Run(ctx, "/bin/cat", "/proc/sys/kernel/random/boot_id")
	if err != nil || cur == nil || cur.ExitCode != 0 {
		return false
	}
	current := strings.TrimSpace(cur.Stdout)
	return recorded != "" && current != "" && recorded != current
}

// StartRequest is the admin's confirmation.
type StartRequest struct {
	SnapshotConfirmed bool   `json:"snapshot_confirmed"`
	Confirm           string `json:"confirm"`
}

// Start re-runs the preflight and launches the upgrade in its own systemd
// unit. It returns once the unit has started.
func (s *Service) Start(ctx context.Context, req StartRequest) error {
	if !req.SnapshotConfirmed {
		return model.NewValidationError("confirm that a snapshot or full backup of the server exists")
	}
	if strings.TrimSpace(req.Confirm) != ToVersion {
		return model.NewValidationError("type " + ToVersion + " to confirm the upgrade")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.Preflight(ctx)
	if err != nil {
		return err
	}
	if p.Blocked {
		var reasons []string
		for _, c := range p.Checks {
			if c.Status == CheckBlock {
				reasons = append(reasons, c.Message)
			}
		}
		return model.NewValidationError("the upgrade cannot start: " + strings.Join(reasons, " "))
	}

	if err := s.sudo(ctx, "install", "-d", "-m", "0700", StateDir); err != nil {
		return err
	}
	mysqlNative := "0\n"
	if p.mysqlNative {
		mysqlNative = "1\n"
	}
	inputs := map[string]string{
		StateDir + "/php-versions": strings.Join(p.phpVersions, "\n") + "\n",
		StateDir + "/mysql-native": mysqlNative,
		StateDir + "/state":        StateRunning + "\n",
	}
	for path, content := range inputs {
		res, err := s.exec.RunSudoWithInput(ctx, content, "tee", path)
		if err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if res == nil || res.ExitCode != 0 {
			return fmt.Errorf("write %s failed", path)
		}
	}
	// A unit left from an earlier failed run would block the name.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reset-failed", UnitName)
	if err := s.sudo(ctx, "systemd-run", "--unit="+UnitName, "--collect",
		"--property=Type=exec",
		"/bin/bash", "-c", UpgradeScript()); err != nil {
		_, _ = s.exec.RunSudoWithInput(ctx, StateFailed+"\n", "tee", StateDir+"/state")
		return err
	}
	return nil
}

// Repair finishes interrupted package operations after a failed upgrade.
// It returns the task ID.
func (s *Service) Repair(ctx context.Context) (string, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return "", err
	}
	if st.Running || st.State != StateFailed {
		return "", model.NewValidationError("repair is only available after a failed upgrade")
	}
	if s.runTask == nil {
		return "", fmt.Errorf("task runner unavailable")
	}
	apt := []string{"env", "DEBIAN_FRONTEND=noninteractive", "UCF_FORCE_CONFFOLD=1"}
	return s.runTask("Repair packages after the Ubuntu upgrade", [][]string{
		append(apt, "dpkg", "--configure", "-a", "--force-confdef", "--force-confold"),
		append(apt, "apt-get", "-f", "install", "-y",
			"-o", "DPkg::Lock::Timeout=120",
			"-o", "Dpkg::Options::=--force-confdef",
			"-o", "Dpkg::Options::=--force-confold"),
	}), nil
}

func (s *Service) sudo(ctx context.Context, name string, args ...string) error {
	res, err := s.exec.RunSudo(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if res == nil || res.ExitCode != 0 {
		detail := ""
		if res != nil {
			detail = strings.TrimSpace(res.Stderr)
		}
		return fmt.Errorf("%s failed: %s", name, detail)
	}
	return nil
}
