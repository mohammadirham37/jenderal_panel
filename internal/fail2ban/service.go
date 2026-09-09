package fail2ban

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/security"
)

type Service struct {
	exec   executor.CommandExecutor
	files  ManagedFiles
	db     *sql.DB
	events *security.EventService
	now    func() time.Time
}

func NewService(exec executor.CommandExecutor, files ManagedFiles, db *sql.DB, events *security.EventService) *Service {
	if files == nil {
		files = newSudoManagedFiles(exec)
	}
	return &Service{exec: exec, files: files, db: db, events: events, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Check(ctx context.Context) security.ComponentStatus {
	status, err := s.Status(ctx)
	if err != nil {
		return security.ComponentStatus{Name: s.Name(), State: "error", Message: err.Error(), CheckedAt: s.now()}
	}
	return security.ComponentStatus{
		Name: s.Name(), State: status.State, Version: status.Version, Message: status.Message,
		Installed: status.Installed, Enabled: status.Enabled, Healthy: status.Healthy, CheckedAt: status.CheckedAt,
	}
}

func (s *Service) Name() string { return "fail2ban" }

func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{State: "not_installed", CheckedAt: s.now(), Jails: []Jail{}}
	versionResult, err := s.exec.Run(ctx, "fail2ban-client", "--version")
	if err != nil || versionResult.ExitCode != 0 {
		status.Message = "Fail2ban is not installed."
		return status, nil
	}
	status.Installed = true
	status.Version = parseVersion(versionResult.Stdout + "\n" + versionResult.Stderr)

	serviceResult, err := s.exec.RunSudo(ctx, "systemctl", "show", "fail2ban", "--property=ActiveState,SubState,UnitFileState")
	if err != nil {
		return Status{}, fmt.Errorf("inspect fail2ban service: %w", err)
	}
	if serviceResult.ExitCode != 0 {
		status.State = "error"
		status.Message = commandFailure("inspect fail2ban service", serviceResult).Error()
		return status, nil
	}
	properties := parseSystemctlProperties(serviceResult.Stdout)
	status.Running = properties["ActiveState"] == "active" && properties["SubState"] == "running"
	status.Enabled = properties["UnitFileState"] == "enabled"
	status.State = properties["ActiveState"]
	if !status.Running {
		status.Message = "Fail2ban is installed but not running."
		return status, nil
	}

	global, err := s.exec.RunSudo(ctx, "fail2ban-client", "status")
	if err != nil {
		return Status{}, fmt.Errorf("read fail2ban status: %w", err)
	}
	if global.ExitCode != 0 {
		status.State = "error"
		status.Message = commandFailure("read fail2ban status", global).Error()
		return status, nil
	}
	for _, jailName := range ParseActiveJails(global.Stdout) {
		result, err := s.exec.RunSudo(ctx, "fail2ban-client", "status", jailName)
		if err != nil {
			return Status{}, fmt.Errorf("read fail2ban jail %s: %w", jailName, err)
		}
		if result.ExitCode != 0 {
			return Status{}, commandFailure("read fail2ban jail "+jailName, result)
		}
		jail, err := ParseJailStatus(jailName, result.Stdout)
		if err != nil {
			return Status{}, err
		}
		jail.FilterAvailable = s.filterAvailable(ctx, jailName)
		jail.SourceAvailable = s.sourceAvailable(ctx, jailName)
		status.Jails = append(status.Jails, jail)
	}
	if sshResult, sshErr := s.exec.RunSudo(ctx, "sshd", "-T"); sshErr == nil && sshResult.ExitCode == 0 {
		status.SSHPort = parseSSHPort(sshResult.Stdout)
	}
	status.Healthy = true
	status.State = "running"
	status.Message = "Fail2ban is running."
	return status, nil
}

func (s *Service) Install(ctx context.Context) error {
	return s.InstallWithProgress(ctx, nil)
}

func (s *Service) InstallWithProgress(ctx context.Context, progress func(string)) error {
	reportProgress(progress, "=== package install ===")
	result, err := s.exec.RunSudo(ctx, "apt-get", "install", "-y",
		"-o", "DPkg::Lock::Timeout=120", "-o", "Acquire::Retries=2",
		"-o", "Acquire::http::Timeout=30", "-o", "Acquire::https::Timeout=30", "fail2ban")
	if err != nil {
		return fmt.Errorf("install fail2ban package: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("install fail2ban package", result)
	}
	reportProgress(progress, "=== enable service ===")
	result, err = s.exec.RunSudo(ctx, "systemctl", "enable", "--now", "fail2ban")
	if err != nil {
		return fmt.Errorf("enable fail2ban service: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("enable fail2ban service", result)
	}
	return nil
}

func (s *Service) Apply(ctx context.Context, settings Settings) error {
	return s.ApplyWithProgress(ctx, settings, nil)
}

func (s *Service) ApplyWithProgress(ctx context.Context, settings Settings, progress func(string)) error {
	candidate, err := Render(settings)
	if err != nil {
		return err
	}
	_, requestedJails, _ := validateSettings(settings)
	for _, jail := range requestedJails {
		if !s.filterAvailable(ctx, jail) {
			return model.NewValidationError("required fail2ban filter is unavailable: " + knownJails[jail])
		}
		if !s.sourceAvailable(ctx, jail) {
			return model.NewValidationError("required log or journal source is unavailable for jail: " + jail)
		}
	}

	reportProgress(progress, "=== validation ===")
	validationRoot, err := s.createValidationRoot(ctx, candidate)
	if err != nil {
		return err
	}
	defer s.removeValidationRoot(validationRoot)
	result, err := s.exec.RunSudo(ctx, "fail2ban-client", "-t", "-c", validationRoot)
	if err != nil {
		return fmt.Errorf("validate fail2ban configuration: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("validate fail2ban configuration", result)
	}

	previous, existed, err := s.files.Read(ctx, managedConfigPath)
	if err != nil {
		return fmt.Errorf("read previous fail2ban configuration: %w", err)
	}
	reportProgress(progress, "=== promotion ===")
	if err := s.files.WriteAtomic(ctx, managedConfigPath, candidate); err != nil {
		return fmt.Errorf("install fail2ban configuration: %w", err)
	}
	if err := s.reloadAndConfirm(ctx, requestedJails, progress); err != nil {
		reportProgress(progress, "=== rollback ===")
		if rollbackErr := s.restoreConfig(ctx, previous, existed); rollbackErr != nil {
			return fmt.Errorf("%v; rollback failed: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (s *Service) Start(ctx context.Context) error   { return s.serviceAction(ctx, "start") }
func (s *Service) Stop(ctx context.Context) error    { return s.serviceAction(ctx, "stop") }
func (s *Service) Restart(ctx context.Context) error { return s.serviceAction(ctx, "restart") }

func (s *Service) Bans(ctx context.Context) ([]Ban, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	bans := make([]Ban, 0)
	manual, err := s.loadActiveManualBans(ctx)
	if err != nil {
		return nil, err
	}
	for _, jail := range status.Jails {
		for _, address := range jail.BannedIPs {
			ban := Ban{Jail: jail.Name, IP: address, Reason: "fail2ban", Manual: false}
			if stored, ok := manual[jail.Name+"\x00"+address]; ok {
				ban = stored
			}
			bans = append(bans, ban)
		}
	}
	return bans, nil
}

func (s *Service) Ban(ctx context.Context, jailName, address string, duration time.Duration) (Ban, error) {
	if _, allowed := knownJails[jailName]; !allowed {
		return Ban{}, model.NewValidationError("unsupported fail2ban jail")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return Ban{}, model.NewValidationError("invalid IP address")
	}
	if duration < time.Minute {
		return Ban{}, model.NewValidationError("manual bans must be temporary and at least 60 seconds")
	}
	if err := s.ensureActiveJail(ctx, jailName); err != nil {
		return Ban{}, err
	}
	bantime, err := s.jailBanTime(ctx, jailName)
	if err != nil {
		return Ban{}, err
	}
	if duration > time.Duration(bantime)*time.Second {
		return Ban{}, model.NewValidationError(fmt.Sprintf("duration must not exceed jail bantime of %d seconds", bantime))
	}
	result, err := s.exec.RunSudo(ctx, "fail2ban-client", "set", jailName, "banip", ip.String())
	if err != nil {
		return Ban{}, fmt.Errorf("ban IP in fail2ban: %w", err)
	}
	if result.ExitCode != 0 {
		return Ban{}, commandFailure("ban IP in fail2ban", result)
	}
	now := s.now()
	requestedExpiry := now.Add(duration)
	actualExpiry := now.Add(time.Duration(bantime) * time.Second)
	ban := Ban{ID: ulid.Make().String(), Jail: jailName, IP: ip.String(), Reason: "manual", StartedAt: now, ExpiresAt: &actualExpiry, Manual: true}
	if s.db != nil {
		_, err = s.db.ExecContext(ctx, `INSERT INTO security_manual_bans
			(id, component, jail, address, requested_expiry, actual_expiry, status, created_at, updated_at)
			VALUES (?, 'fail2ban', ?, ?, ?, ?, 'active', ?, ?)`, ban.ID, jailName, ip.String(),
			requestedExpiry.Format(time.RFC3339Nano), actualExpiry.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			_, _ = s.exec.RunSudo(context.Background(), "fail2ban-client", "set", jailName, "unbanip", ip.String())
			return Ban{}, fmt.Errorf("persist manual fail2ban ban: %w", err)
		}
	}
	return ban, nil
}

func (s *Service) ensureActiveJail(ctx context.Context, jail string) error {
	result, err := s.exec.RunSudo(ctx, "fail2ban-client", "status")
	if err != nil {
		return fmt.Errorf("discover active fail2ban jails: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("discover active fail2ban jails", result)
	}
	for _, active := range ParseActiveJails(result.Stdout) {
		if active == jail {
			return nil
		}
	}
	return model.NewValidationError("fail2ban jail is not active: " + jail)
}

func (s *Service) Unban(ctx context.Context, jailName, address string) error {
	if _, allowed := knownJails[jailName]; !allowed {
		return model.NewValidationError("unsupported fail2ban jail")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return model.NewValidationError("invalid IP address")
	}
	result, err := s.exec.RunSudo(ctx, "fail2ban-client", "set", jailName, "unbanip", ip.String())
	if err != nil {
		return fmt.Errorf("unban IP in fail2ban: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("unban IP in fail2ban", result)
	}
	if s.db != nil {
		_, err = s.db.ExecContext(ctx, `UPDATE security_manual_bans SET status = 'released', updated_at = ?
			WHERE component = 'fail2ban' AND jail = ? AND address = ? AND status = 'active'`,
			s.now().Format(time.RFC3339Nano), jailName, ip.String())
		if err != nil {
			return fmt.Errorf("release manual fail2ban ban: %w", err)
		}
	}
	if s.events != nil {
		_ = s.events.ResolveFingerprint(ctx, "fail2ban:"+jailName+":"+ip.String(), s.now())
	}
	return nil
}

func (s *Service) ReconcileExpiredBans(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT jail, address FROM security_manual_bans
		WHERE component = 'fail2ban' AND status = 'active' AND requested_expiry <= ?`, s.now().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("list expired manual fail2ban bans: %w", err)
	}
	type expiredBan struct{ jail, address string }
	var expired []expiredBan
	for rows.Next() {
		var ban expiredBan
		if err := rows.Scan(&ban.jail, &ban.address); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, ban)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, ban := range expired {
		if err := s.Unban(ctx, ban.jail, ban.address); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) StartReconciler(ctx context.Context) {
	go func() {
		_ = s.ReconcileExpiredBans(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.ReconcileExpiredBans(ctx)
			}
		}
	}()
}

func (s *Service) filterAvailable(ctx context.Context, jail string) bool {
	filter, allowed := knownJails[jail]
	if !allowed {
		return false
	}
	result, err := s.exec.RunSudo(ctx, "test", "-f", "/etc/fail2ban/filter.d/"+filter+".conf")
	return err == nil && result.ExitCode == 0
}

func (s *Service) sourceAvailable(ctx context.Context, jail string) bool {
	if jail == "sshd" {
		if result, err := s.exec.RunSudo(ctx, "test", "-r", "/var/log/auth.log"); err == nil && result.ExitCode == 0 {
			return true
		}
		result, err := s.exec.RunSudo(ctx, "journalctl", "--quiet", "--no-pager", "-n", "1", "-u", "ssh.service")
		return err == nil && result.ExitCode == 0
	}
	logPath := "/var/log/nginx/access.log"
	if jail == "nginx-http-auth" {
		logPath = "/var/log/nginx/error.log"
	}
	result, err := s.exec.RunSudo(ctx, "test", "-r", logPath)
	return err == nil && result.ExitCode == 0
}

func (s *Service) createValidationRoot(ctx context.Context, candidate string) (string, error) {
	result, err := s.exec.RunSudo(ctx, "mktemp", "-d", "/tmp/jenderal-fail2ban.XXXXXX")
	if err != nil {
		return "", fmt.Errorf("create fail2ban validation directory: %w", err)
	}
	if result.ExitCode != 0 {
		return "", commandFailure("create fail2ban validation directory", result)
	}
	root := strings.TrimSpace(result.Stdout)
	if !strings.HasPrefix(filepath.Clean(root), "/tmp/jenderal-fail2ban.") {
		return "", errors.New("create fail2ban validation directory: unexpected path")
	}
	for _, command := range [][]string{{"cp", "-a", "/etc/fail2ban/.", root}, {"mkdir", "-p", filepath.Join(root, "jail.d")}} {
		result, err = s.exec.RunSudo(ctx, command[0], command[1:]...)
		if err != nil {
			s.removeValidationRoot(root)
			return "", fmt.Errorf("prepare fail2ban validation directory: %w", err)
		}
		if result.ExitCode != 0 {
			s.removeValidationRoot(root)
			return "", commandFailure("prepare fail2ban validation directory", result)
		}
	}
	result, err = s.exec.RunSudoWithInput(ctx, candidate, "tee", "--", filepath.Join(root, "jail.d", "jenderal-panel.local"))
	if err != nil {
		s.removeValidationRoot(root)
		return "", fmt.Errorf("write candidate fail2ban configuration: %w", err)
	}
	if result.ExitCode != 0 {
		s.removeValidationRoot(root)
		return "", commandFailure("write candidate fail2ban configuration", result)
	}
	return root, nil
}

func (s *Service) removeValidationRoot(root string) {
	if strings.HasPrefix(filepath.Clean(root), "/tmp/jenderal-fail2ban.") {
		_, _ = s.exec.RunSudo(context.Background(), "rm", "-rf", "--", root)
	}
}

func (s *Service) reloadAndConfirm(ctx context.Context, requested []string, progress func(string)) error {
	reportProgress(progress, "=== reload ===")
	result, err := s.exec.RunSudo(ctx, "systemctl", "reload", "fail2ban")
	if err != nil {
		return fmt.Errorf("reload fail2ban: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("reload fail2ban", result)
	}
	reportProgress(progress, "=== health confirmation ===")
	result, err = s.exec.RunSudo(ctx, "fail2ban-client", "status")
	if err != nil {
		return fmt.Errorf("confirm fail2ban jails: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("confirm fail2ban jails", result)
	}
	active := ParseActiveJails(result.Stdout)
	activeSet := make(map[string]struct{}, len(active))
	for _, jail := range active {
		activeSet[jail] = struct{}{}
	}
	for _, jail := range requested {
		if _, ok := activeSet[jail]; !ok {
			return fmt.Errorf("confirm fail2ban jails: %s is not active", jail)
		}
	}
	return nil
}

func reportProgress(progress func(string), message string) {
	if progress != nil {
		progress(message)
	}
}

func (s *Service) restoreConfig(ctx context.Context, previous string, existed bool) error {
	var err error
	if existed {
		err = s.files.WriteAtomic(ctx, managedConfigPath, previous)
	} else {
		err = s.files.Remove(ctx, managedConfigPath)
	}
	if err != nil {
		return err
	}
	result, reloadErr := s.exec.RunSudo(ctx, "systemctl", "reload", "fail2ban")
	if reloadErr != nil {
		return reloadErr
	}
	if result.ExitCode != 0 {
		return commandFailure("reload restored fail2ban configuration", result)
	}
	return nil
}

func (s *Service) serviceAction(ctx context.Context, action string) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", action, "fail2ban")
	if err != nil {
		return fmt.Errorf("%s fail2ban: %w", action, err)
	}
	if result.ExitCode != 0 {
		return commandFailure(action+" fail2ban", result)
	}
	return nil
}

func (s *Service) jailBanTime(ctx context.Context, jail string) (int, error) {
	result, err := s.exec.RunSudo(ctx, "fail2ban-client", "get", jail, "bantime")
	if err != nil {
		return 0, fmt.Errorf("read fail2ban jail bantime: %w", err)
	}
	if result.ExitCode != 0 {
		return 0, commandFailure("read fail2ban jail bantime", result)
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if err != nil || seconds < 60 || seconds > 604800 {
		return 0, fmt.Errorf("read fail2ban jail bantime: invalid value %q", strings.TrimSpace(result.Stdout))
	}
	return seconds, nil
}

func (s *Service) loadActiveManualBans(ctx context.Context) (map[string]Ban, error) {
	bans := make(map[string]Ban)
	if s.db == nil {
		return bans, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, jail, address, actual_expiry, created_at
		FROM security_manual_bans WHERE component = 'fail2ban' AND status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("list manual fail2ban bans: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ban Ban
		var expiry sql.NullString
		var started string
		if err := rows.Scan(&ban.ID, &ban.Jail, &ban.IP, &expiry, &started); err != nil {
			return nil, err
		}
		ban.Manual = true
		ban.Reason = "manual"
		ban.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		if expiry.Valid {
			parsed, parseErr := time.Parse(time.RFC3339Nano, expiry.String)
			if parseErr == nil {
				ban.ExpiresAt = &parsed
			}
		}
		bans[ban.Jail+"\x00"+ban.IP] = ban
	}
	return bans, rows.Err()
}

func parseVersion(output string) string {
	value := strings.TrimSpace(output)
	value = strings.TrimPrefix(value, "Fail2Ban")
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	return value
}

func sortedKnownJails() []string {
	jails := make([]string, 0, len(knownJails))
	for jail := range knownJails {
		jails = append(jails, jail)
	}
	sort.Strings(jails)
	return jails
}
