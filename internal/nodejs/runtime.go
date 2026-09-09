package nodejs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
)

type runtimeManager interface {
	Detect(context.Context, string, string) (noderuntime.Status, error)
	Install(context.Context, string, string, func(string)) error
	InstallPanel(context.Context, func(string)) error
}

type RuntimeInfo struct {
	WebsiteID        string `json:"website_id"`
	Domain           string `json:"domain"`
	WebUser          string `json:"web_user"`
	SelectedVersion  string `json:"selected_version"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version"`
	NPMVersion       string `json:"npm_version"`
	NVMVersion       string `json:"nvm_version"`
	NVMState         string `json:"nvm_state"`
	ErrorMessage     string `json:"error_message,omitempty"`
}

type GlobalStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Package   string `json:"package"`
}

type websiteRuntimeRow struct {
	ID           string
	Domain       string
	WebUser      string
	DocumentRoot string
	NodeVersion  string
}

func (s *Service) loadWebsiteRuntime(ctx context.Context, websiteID string) (websiteRuntimeRow, error) {
	var website websiteRuntimeRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id, domain, web_user, document_root, node_version FROM websites WHERE id = ?`, websiteID,
	).Scan(&website.ID, &website.Domain, &website.WebUser, &website.DocumentRoot, &website.NodeVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return websiteRuntimeRow{}, model.ErrNotFound
	}
	if err != nil {
		return websiteRuntimeRow{}, fmt.Errorf("load website runtime: %w", err)
	}
	return website, nil
}

func (s *Service) ListRuntimes(ctx context.Context) ([]RuntimeInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, domain, web_user, node_version FROM websites ORDER BY domain`)
	if err != nil {
		return nil, fmt.Errorf("list website runtimes: %w", err)
	}
	runtimes := make([]RuntimeInfo, 0)
	for rows.Next() {
		var runtime RuntimeInfo
		if err := rows.Scan(&runtime.WebsiteID, &runtime.Domain, &runtime.WebUser, &runtime.SelectedVersion); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan website runtime: %w", err)
		}
		runtimes = append(runtimes, runtime)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read website runtimes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close website runtimes: %w", err)
	}

	for index := range runtimes {
		runtime := &runtimes[index]
		if runtime.SelectedVersion == "" {
			runtime.NVMState = "not_selected"
			continue
		}
		status, err := s.runtime.Detect(ctx, runtime.WebUser, runtime.SelectedVersion)
		if err != nil {
			runtime.NVMState = "error"
			runtime.ErrorMessage = err.Error()
			continue
		}
		runtime.Installed = status.Installed
		runtime.InstalledVersion = status.NodeVersion
		runtime.NPMVersion = status.NPMVersion
		runtime.NVMVersion = status.NVMVersion
		runtime.NVMState = status.NVMState
	}
	return runtimes, nil
}

type unitSnapshot struct {
	app     model.NodeApp
	path    string
	content string
	exists  bool
	active  bool
	enabled bool
}

type defaultSnapshot struct {
	value  string
	exists bool
}

func (s *Service) ChangeRuntime(ctx context.Context, websiteID, version string, log func(string)) error {
	if err := s.validateVersion(version); err != nil {
		return err
	}
	s.runtimeGate.RLock()
	defer s.runtimeGate.RUnlock()
	unlock := s.mutations.Lock(websiteID)
	defer unlock()

	website, err := s.loadWebsiteRuntime(ctx, websiteID)
	if err != nil {
		return err
	}
	apps, err := s.ListByWebsite(ctx, websiteID)
	if err != nil {
		return err
	}
	snapshots, err := s.snapshotUnits(ctx, apps)
	if err != nil {
		return err
	}
	previousDefault, err := s.readDefaultAlias(ctx, website.WebUser)
	if err != nil {
		return err
	}

	if log != nil {
		log(fmt.Sprintf("Installing Node.js %s for %s", version, website.Domain))
	}
	if err := s.runtime.Install(ctx, website.WebUser, version, log); err != nil {
		return err
	}
	rollback := func(cause error) error {
		rollbackErr := s.rollbackActivation(ctx, website, version, previousDefault, snapshots, log)
		if rollbackErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", cause, rollbackErr)
		}
		return cause
	}
	status, err := s.runtime.Detect(ctx, website.WebUser, version)
	if err != nil {
		return rollback(fmt.Errorf("verify installed runtime: %w", err))
	}
	if !status.Installed {
		return rollback(model.NewValidationError("Node.js " + version + " installation did not pass verification; retry from /nodejs"))
	}

	for index := range snapshots {
		updated := snapshots[index].app
		updated.NodeVersion = version
		unit, err := s.buildSystemdUnit(updated, website.WebUser, website.DocumentRoot)
		if err != nil {
			return rollback(err)
		}
		if err := s.writeFileViaSudo(ctx, snapshots[index].path, unit); err != nil {
			return rollback(fmt.Errorf("regenerate %s: %w", serviceName(updated.ID), err))
		}
	}
	if len(snapshots) > 0 {
		if err := s.runSudoOK(ctx, "systemctl", "daemon-reload"); err != nil {
			return rollback(fmt.Errorf("reload systemd: %w", err))
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rollback(fmt.Errorf("begin runtime activation: %w", err))
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `UPDATE websites SET node_version = ?, updated_at = ? WHERE id = ?`, version, now, websiteID); err != nil {
		return rollback(fmt.Errorf("persist website runtime: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodejs_apps SET node_version = ?, updated_at = ? WHERE website_id = ?`, version, now, websiteID); err != nil {
		return rollback(fmt.Errorf("persist application runtime: %w", err))
	}
	for _, snapshot := range snapshots {
		if !snapshot.active {
			continue
		}
		if log != nil {
			log("Restarting " + serviceName(snapshot.app.ID))
		}
		if err := s.runSudoOK(ctx, "systemctl", "restart", serviceName(snapshot.app.ID)); err != nil {
			_ = tx.Rollback()
			return rollback(fmt.Errorf("restart %s: %w", serviceName(snapshot.app.ID), err))
		}
	}
	if err := tx.Commit(); err != nil {
		return rollback(fmt.Errorf("commit runtime activation: %w", err))
	}
	if log != nil {
		log(fmt.Sprintf("Activated Node.js %s for %s", version, website.Domain))
	}
	return nil
}

func (s *Service) snapshotUnits(ctx context.Context, apps []model.NodeApp) ([]unitSnapshot, error) {
	snapshots := make([]unitSnapshot, 0, len(apps))
	for _, app := range apps {
		if err := validateNodeAppID(app.ID); err != nil {
			return nil, err
		}
		path := filepath.Join("/etc/systemd/system", serviceName(app.ID))
		content, exists, err := s.readSystemFile(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", serviceName(app.ID), err)
		}
		active, err := s.systemdState(ctx, "is-active", serviceName(app.ID))
		if err != nil {
			return nil, err
		}
		enabled, err := s.systemdState(ctx, "is-enabled", serviceName(app.ID))
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, unitSnapshot{app: app, path: path, content: content, exists: exists, active: active, enabled: enabled})
	}
	return snapshots, nil
}

func (s *Service) readSystemFile(ctx context.Context, path string) (string, bool, error) {
	result, err := s.exec.RunSudo(ctx, "cat", path)
	if err != nil {
		return "", false, err
	}
	if result == nil {
		return "", false, errors.New("executor returned no result")
	}
	if result.ExitCode != 0 {
		return "", false, nil
	}
	return result.Stdout, true, nil
}

func (s *Service) systemdState(ctx context.Context, action, service string) (bool, error) {
	result, err := s.exec.RunSudo(ctx, "systemctl", action, "--quiet", service)
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", action, service, err)
	}
	if result == nil {
		return false, fmt.Errorf("%s %s: executor returned no result", action, service)
	}
	return result.ExitCode == 0, nil
}

func (s *Service) readDefaultAlias(ctx context.Context, user string) (defaultSnapshot, error) {
	home, err := noderuntime.Home(user)
	if err != nil {
		return defaultSnapshot{}, err
	}
	result, err := s.exec.RunSudo(ctx, "-u", user, "--", "/usr/bin/cat", home+"/.nvm/alias/default")
	if err != nil {
		return defaultSnapshot{}, fmt.Errorf("read Node.js default alias: %w", err)
	}
	if result == nil {
		return defaultSnapshot{}, errors.New("read Node.js default alias: executor returned no result")
	}
	value := strings.TrimSpace(result.Stdout)
	return defaultSnapshot{value: value, exists: result.ExitCode == 0 && value != ""}, nil
}

func (s *Service) restoreDefaultAlias(ctx context.Context, user, activeVersion string, previous defaultSnapshot) error {
	script := `. "$NVM_DIR/nvm.sh"; nvm unalias default >/dev/null`
	args := []string{"-c", script, "--"}
	if previous.exists {
		script = `. "$NVM_DIR/nvm.sh"; nvm alias default "$1" >/dev/null`
		args = []string{"-c", script, "--", previous.value}
	}
	execArgs, err := noderuntime.ExecArgs(user, activeVersion, "/bin/bash", args...)
	if err != nil {
		return err
	}
	result, err := s.exec.RunSudo(ctx, "-u", execArgs...)
	if err != nil {
		return err
	}
	if result == nil || result.ExitCode != 0 {
		return errors.New("restore Node.js default alias failed")
	}
	return nil
}

func (s *Service) rollbackActivation(ctx context.Context, website websiteRuntimeRow, activeVersion string, previousDefault defaultSnapshot, snapshots []unitSnapshot, log func(string)) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if log != nil {
		log("Activation failed; restoring prior service units and runtime metadata")
	}
	var failures []string
	for _, snapshot := range snapshots {
		if snapshot.exists {
			if err := s.writeFileViaSudo(rollbackCtx, snapshot.path, snapshot.content); err != nil {
				failures = append(failures, err.Error())
			}
		} else if err := s.runSudoOK(rollbackCtx, "rm", "-f", snapshot.path); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(snapshots) > 0 {
		if err := s.runSudoOK(rollbackCtx, "systemctl", "daemon-reload"); err != nil {
			failures = append(failures, err.Error())
		}
	}
	for _, snapshot := range snapshots {
		name := serviceName(snapshot.app.ID)
		action := "stop"
		if snapshot.active {
			action = "restart"
		}
		if err := s.runSudoOK(rollbackCtx, "systemctl", action, name); err != nil {
			failures = append(failures, err.Error())
		}
		enableAction := "disable"
		if snapshot.enabled {
			enableAction = "enable"
		}
		if err := s.runSudoOK(rollbackCtx, "systemctl", enableAction, name); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if err := s.restoreDefaultAlias(rollbackCtx, website.WebUser, activeVersion, previousDefault); err != nil {
		failures = append(failures, err.Error())
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func serviceName(appID string) string {
	return "jenderal-node-" + appID + ".service"
}

func (s *Service) GlobalStatus(ctx context.Context) (GlobalStatus, error) {
	result, err := s.exec.Run(ctx, "/usr/bin/dpkg-query", "-W", "-f=${Status}\t${Version}\n", "nodejs")
	if err != nil {
		return GlobalStatus{}, fmt.Errorf("detect global nodejs package: %w", err)
	}
	if result == nil {
		return GlobalStatus{}, errors.New("detect global nodejs package: executor returned no result")
	}
	if result.ExitCode != 0 {
		return GlobalStatus{}, nil
	}
	line := strings.TrimSpace(result.Stdout)
	const prefix = "install ok installed\t"
	if !strings.HasPrefix(line, prefix) {
		return GlobalStatus{}, nil
	}
	return GlobalStatus{Installed: true, Version: strings.TrimPrefix(line, prefix), Package: "nodejs"}, nil
}

func (s *Service) RemoveGlobal(ctx context.Context, log func(string)) error {
	s.runtimeGate.Lock()
	defer s.runtimeGate.Unlock()
	status, err := s.GlobalStatus(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return model.NewValidationError("the apt nodejs package is not installed")
	}
	dependencies, err := s.legacyGlobalDependencies(ctx)
	if err != nil {
		return err
	}
	if len(dependencies) > 0 {
		return model.NewValidationError("global Node.js is still used by panel-managed apps: " + strings.Join(dependencies, ", ") + "; install the website runtime and change/reinstall those services first")
	}
	if log != nil {
		log("Preparing the panel build runtime before removing apt nodejs")
	}
	if err := s.runtime.InstallPanel(ctx, log); err != nil {
		return fmt.Errorf("prepare panel runtime: %w", err)
	}
	dependencies, err = s.legacyGlobalDependencies(ctx)
	if err != nil {
		return err
	}
	if len(dependencies) > 0 {
		return model.NewValidationError("global Node.js became required by panel-managed apps: " + strings.Join(dependencies, ", ") + "; removal was canceled")
	}
	if log != nil {
		log("Removing apt package nodejs (dependencies are preserved; autoremove is not used)")
	}
	result, err := s.exec.RunSudo(ctx, "apt-get", "remove", "-y", "-o", "DPkg::Lock::Timeout=120", "nodejs")
	if err != nil {
		return fmt.Errorf("remove global nodejs package: %w", err)
	}
	if result == nil || result.ExitCode != 0 {
		detail := "command failed"
		if result != nil && strings.TrimSpace(result.Stderr) != "" {
			detail = strings.TrimSpace(result.Stderr)
		}
		return fmt.Errorf("remove global nodejs package: %s", detail)
	}
	if log != nil {
		log("Removed apt package nodejs")
	}
	return nil
}

func (s *Service) legacyGlobalDependencies(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, w.web_user, w.node_version
		FROM nodejs_apps a JOIN websites w ON w.id = a.website_id ORDER BY a.id`)
	if err != nil {
		return nil, fmt.Errorf("list Node.js application dependencies: %w", err)
	}
	type dependency struct{ id, user, version string }
	var apps []dependency
	for rows.Next() {
		var app dependency
		if err := rows.Scan(&app.id, &app.user, &app.version); err != nil {
			rows.Close()
			return nil, err
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var legacy []string
	for _, app := range apps {
		if app.version == "" {
			legacy = append(legacy, app.id)
			continue
		}
		status, err := s.runtime.Detect(ctx, app.user, app.version)
		if err != nil || !status.Installed {
			legacy = append(legacy, app.id)
			continue
		}
		home, err := noderuntime.Home(app.user)
		if err != nil {
			legacy = append(legacy, app.id)
			continue
		}
		unit, exists, err := s.readSystemFile(ctx, filepath.Join("/etc/systemd/system", serviceName(app.id)))
		if err != nil {
			return nil, err
		}
		if !exists || !strings.Contains(unit, "ExecStart="+home+"/.nvm/nvm-exec ") || !strings.Contains(unit, `Environment="NODE_VERSION=`+app.version+`"`) {
			legacy = append(legacy, app.id)
		}
	}
	return legacy, nil
}

func (s *Service) runSudoOK(ctx context.Context, name string, args ...string) error {
	result, err := s.exec.RunSudo(ctx, name, args...)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("executor returned no result")
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = fmt.Sprintf("exit status %d", result.ExitCode)
		}
		return errors.New(detail)
	}
	return nil
}

var _ runtimeManager = (*noderuntime.Service)(nil)
