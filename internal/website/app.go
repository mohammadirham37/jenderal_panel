package website

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/goruntime"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
	"github.com/mohammadirham37/jenderal_panel/internal/siteops"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// App service (long-running Node.js process) support: config, systemd unit,
// lifecycle actions, and build tasks. Ports live in 8200–8299, separate from
// the Octane range.

const (
	appPortMin = 8200
	appPortMax = 8299

	appUnitPrefix = "jenderal-app-"
)

// WpStyle status view for the app service.
type AppServiceStatus struct {
	Runtime      string `json:"runtime"`
	Port         int    `json:"port"`
	StartCommand string `json:"start_command"`
	BuildCommand string `json:"build_command"`
	Unit         string `json:"unit"`
	Active       bool   `json:"active"`
	NodeVersion  string `json:"node_version"`
	// AutoBuild (Go sites) runs build + restart after every git deploy;
	// when false the operator builds from the Deployment tab instead.
	AutoBuild bool `json:"auto_build"`
}

func appUnitName(websiteID string) string {
	return appUnitPrefix + websiteID + ".service"
}

// allocateAppPort picks the lowest free app port: not used by another site
// and not currently listening on the server. Package-level so the
// provisioner shares the exact same allocation logic as SaveAppService.
func allocateAppPort(ctx context.Context, db *sql.DB, exec executor.CommandExecutor) (int, error) {
	used := map[int]bool{}
	rows, err := db.QueryContext(ctx, `SELECT app_port FROM websites WHERE app_port > 0`)
	if err != nil {
		return 0, fmt.Errorf("list used app ports: %w", err)
	}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return 0, err
		}
		used[p] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	result, err := exec.RunSudo(ctx, "ss", "-ltnH")
	if err == nil {
		for _, line := range strings.Split(result.Stdout, "\n") {
			// Format: ... peers — local address is field 4 ("*:8123").
			fields := strings.Fields(line)
			for _, f := range fields {
				if i := strings.LastIndexByte(f, ':'); i >= 0 {
					if p, cerr := strconv.Atoi(f[i+1:]); cerr == nil {
						used[p] = true
					}
				}
			}
		}
	}

	for p := appPortMin; p <= appPortMax; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free app ports in range %d-%d", appPortMin, appPortMax)
}

func (s *Service) allocateAppPort(ctx context.Context) (int, error) {
	return allocateAppPort(ctx, s.db, s.exec)
}

// GetAppService returns the app service configuration and unit state.
func (s *Service) GetAppService(ctx context.Context, websiteID string) (AppServiceStatus, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return AppServiceStatus{}, err
	}
	if w.AppType != "node" && w.AppType != "go" && w.AppType != "python" && w.AppType != "deno" && w.AppType != "bun" {
		return AppServiceStatus{}, model.NewValidationError("the app service is only available for node, go, python, deno, and bun sites")
	}

	unit := appUnitName(w.ID)
	active := false
	if result, err := s.exec.RunSudo(ctx, "systemctl", "is-active", unit); err == nil {
		active = strings.TrimSpace(result.Stdout) == "active"
	}

	runtime := w.AppRuntime
	if runtime == "" {
		if w.AppType == "go" {
			runtime = "go"
		} else {
			runtime = "node"
		}
	}

	return AppServiceStatus{
		Runtime:      runtime,
		Port:         w.AppPort,
		StartCommand: w.AppStartCommand,
		BuildCommand: w.AppBuildCommand,
		Unit:         unit,
		Active:       active,
		NodeVersion:  w.NodeVersion,
		AutoBuild:    s.appAutoBuild(ctx, w.ID),
	}, nil
}

// appAutoBuild reports whether git deploys build and restart the app; on by
// default and for rows the column cannot be read for.
func (s *Service) appAutoBuild(ctx context.Context, websiteID string) bool {
	var enabled int
	if err := s.db.QueryRowContext(ctx, `SELECT app_auto_build FROM websites WHERE id = ?`, websiteID).Scan(&enabled); err != nil {
		return true
	}
	return enabled != 0
}

// SetAppAutoBuild turns the post-deploy build + restart of a Go site on or
// off. It never touches the running service.
func (s *Service) SetAppAutoBuild(ctx context.Context, websiteID string, enabled bool) error {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}
	if w.AppType != "go" {
		return model.NewValidationError("auto build only applies to Go websites")
	}
	value := 0
	if enabled {
		value = 1
	}
	_, err = s.db.ExecContext(ctx, `UPDATE websites SET app_auto_build = ?, updated_at = ? WHERE id = ?`,
		value, time.Now().UTC().Format(time.RFC3339), websiteID)
	if err != nil {
		return fmt.Errorf("save auto build: %w", err)
	}
	return nil
}

// buildAppUnit renders the systemd unit for an app service, dispatching on
// the site's runtime. dir is the unit's working directory: the document root
// for node/python, the repository root for go/go-binary.
func buildAppUnit(w model.Website, dir string) (string, error) {
	switch w.AppRuntime {
	case "", "node":
		return buildNodeAppUnit(w, dir)
	case "go", "go-binary":
		return buildBinaryAppUnit(w, dir)
	case "python":
		return buildPythonAppUnit(w, dir)
	case "deno", "bun":
		return buildBinaryAppUnit(w, dir)
	default:
		return "", model.NewValidationError("unsupported app runtime: " + w.AppRuntime)
	}
}

// buildNodeAppUnit runs the start command through nvm-exec with the site's
// pinned Node version.
func buildNodeAppUnit(w model.Website, docroot string) (string, error) {
	startParts := strings.Fields(w.AppStartCommand)
	if len(startParts) == 0 {
		return "", model.NewValidationError("start command is required")
	}
	execArgs, err := noderuntime.ExecArgs(w.WebUser, w.NodeVersion, startParts[0], startParts[1:]...)
	if err != nil {
		return "", err
	}
	// RunSudo argv = ["-u", user, "--", rest...]; the unit runs as User=
	// already, so drop the leading "-u <user> --".
	execArgv := execArgs[3:]
	for _, part := range execArgv {
		if strings.ContainsAny(part, " \t") {
			return "", model.NewValidationError("start command arguments must not contain spaces or tabs: " + part)
		}
	}

	return `[Unit]
Description=Jenderal App ` + w.Domain + `
After=network.target

[Service]
User=` + w.WebUser + `
WorkingDirectory=` + docroot + `
ExecStart=` + strings.Join(execArgv, " ") + `
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, nil
}

// buildPythonAppUnit runs the start command with the site's virtualenv on
// PATH (created by the build task), so venv tools resolve without absolute
// paths. The start command runs under exec through bash.
func buildPythonAppUnit(w model.Website, docroot string) (string, error) {
	start := strings.TrimSpace(w.AppStartCommand)
	if start == "" {
		return "", model.NewValidationError("start command is required")
	}
	if strings.ContainsAny(start, "\r\n") {
		return "", model.NewValidationError("start command must be a single line")
	}
	home := "/home/" + w.WebUser
	venvBin := home + "/venv/bin"

	return `[Unit]
Description=Jenderal App ` + w.Domain + `
After=network.target

[Service]
User=` + w.WebUser + `
WorkingDirectory=` + docroot + `
Environment="PATH=` + venvBin + `:/usr/local/bin:/usr/bin:/bin"
ExecStart=/bin/bash -c 'exec ` + start + `'
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, nil
}

// buildBinaryAppUnit runs a self-contained executable (Go binary, deno
// entrypoint, etc.). Relative paths resolve against the working directory:
// for go/go-binary that is the repository root (not the document root), so
// the built binary and .env live outside the nginx-served tree.
func buildBinaryAppUnit(w model.Website, projectRoot string) (string, error) {
	start := strings.TrimSpace(w.AppStartCommand)
	if start == "" {
		return "", model.NewValidationError("start command is required")
	}
	exec := start
	if strings.HasPrefix(exec, "./") {
		exec = projectRoot + "/" + strings.TrimPrefix(exec, "./")
	} else if !strings.ContainsAny(strings.Fields(exec)[0], "/") {
		// A bare command runs from the working directory.
		exec = projectRoot + "/" + exec
	}
	envLines := ""
	if (w.AppRuntime == "go" || w.AppRuntime == "go-binary") && w.AppPort > 0 {
		// The panel allocates the loopback port; the app must read it from
		// the environment instead of hardcoding it.
		envLines = "Environment=PORT=" + strconv.Itoa(w.AppPort) + "\n" +
			"EnvironmentFile=-" + projectRoot + "/.env\n"
	}

	if envLines != "" {
		// systemd lets EnvironmentFile= override Environment=, so a PORT in
		// the app's .env would win and the app would listen where nginx and
		// the deploy health check never look. env(1) on the command line has
		// the last word; it execs the binary, so MainPID stays the app.
		exec = "/usr/bin/env PORT=" + strconv.Itoa(w.AppPort) + " " + exec
	}

	return `[Unit]
Description=Jenderal App ` + w.Domain + `
After=network.target

[Service]
User=` + w.WebUser + `
WorkingDirectory=` + projectRoot + `
` + envLines + `ExecStart=` + exec + `
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, nil
}

// SaveAppService validates and stores the app configuration, (re)writes the
// systemd unit, regenerates the nginx vhost (port), and restarts the service
// when it was running.
func (s *Service) SaveAppService(ctx context.Context, websiteID string, startCommand, buildCommand string) (AppServiceStatus, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return AppServiceStatus{}, err
	}
	if w.AppType != "node" && w.AppType != "go" && w.AppType != "python" && w.AppType != "deno" && w.AppType != "bun" {
		return AppServiceStatus{}, model.NewValidationError("the app service is only available for node, go, python, deno, and bun sites")
	}

	startCommand = strings.TrimSpace(startCommand)
	buildCommand = strings.TrimSpace(buildCommand)
	if w.AppRuntime == "" {
		// Legacy rows predate app_runtime; go sites were go-build all along.
		if w.AppType == "go" {
			w.AppRuntime = "go"
		} else {
			w.AppRuntime = "node"
		}
	}
	if w.AppRuntime == "node" && w.NodeVersion != "" {
		if err := noderuntime.ValidateVersion(w.NodeVersion); err != nil {
			return AppServiceStatus{}, fmt.Errorf("node runtime: %w", err)
		}
	}
	if _, err := buildAppStartArgv(w.AppRuntime, w.WebUser, w.DocumentRoot, startCommand); err != nil {
		return AppServiceStatus{}, err
	}

	port := w.AppPort
	wasActive := false
	if w.AppPort <= 0 {
		var allocErr error
		port, allocErr = s.allocateAppPort(ctx)
		if allocErr != nil {
			return AppServiceStatus{}, allocErr
		}
	}
	if result, err := s.exec.RunSudo(ctx, "systemctl", "is-active", appUnitName(w.ID)); err == nil {
		wasActive = strings.TrimSpace(result.Stdout) == "active"
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE websites SET app_port = ?, app_start_command = ?, app_build_command = ?, updated_at = ? WHERE id = ?`,
		port, startCommand, buildCommand, time.Now().UTC().Format(time.RFC3339), websiteID,
	); err != nil {
		return AppServiceStatus{}, fmt.Errorf("save app config: %w", err)
	}

	w.AppPort = port
	w.AppStartCommand = startCommand
	w.AppBuildCommand = buildCommand

	// Go apps run from the repository root, not the document root.
	if err := writeAppUnitFiles(ctx, s.exec, w); err != nil {
		return AppServiceStatus{}, err
	}

	// The port is part of the nginx vhost; regenerate when it is new or
	// changed. w.AppPort still holds the old value here.
	if w.AppPort != port {
		if err := s.regenerateConfig(ctx, w, ""); err != nil {
			return AppServiceStatus{}, fmt.Errorf("regenerate vhost: %w", err)
		}
	}

	if wasActive {
		if err := s.AppAction(ctx, w.ID, "restart"); err != nil {
			return AppServiceStatus{}, err
		}
	}

	return s.GetAppService(ctx, websiteID)
}

func (s *Service) AppAction(ctx context.Context, websiteID, action string) error {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}
	unit := appUnitName(w.ID)
	switch action {
	case "start", "stop", "restart":
		result, err := s.exec.RunSudo(ctx, "systemctl", action, unit)
		if err != nil {
			return fmt.Errorf("%s app service: %w", action, err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("%s app service failed: %s", action, strings.TrimSpace(result.Stderr))
		}
	default:
		return model.NewValidationError("unknown app action: " + action)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx, `UPDATE websites SET updated_at = ? WHERE id = ?`, now, websiteID)
	return nil
}

// RunAppBuildTask runs the build command (if configured) as a background
// task and restarts the app service afterwards.
func (s *Service) RunAppBuildTask(ctx context.Context, websiteID string) (string, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return "", err
	}
	if w.AppType != "node" && w.AppType != "go" && w.AppType != "python" && w.AppType != "deno" && w.AppType != "bun" {
		return "", model.NewValidationError("the app service is only available for node, go, python, deno, and bun sites")
	}
	if w.AppType != "go" && strings.TrimSpace(w.AppBuildCommand) == "" && w.AppType != "python" {
		return "", model.NewValidationError("no build command configured")
	}
	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}
	if w.AppType == "go" {
		// Manual Build & Restart runs the same pipeline as a git deploy:
		// build, executable check, unit refresh, restart, health check.
		return tr.RunFuncWithOptions(
			taskrunner.Options{Name: "Build & restart — " + w.Domain, Module: "website", Timeout: 30 * time.Minute},
			func(taskCtx context.Context, write func(string)) error {
				return s.deployGoApp(taskCtx, websiteID, write)
			}), nil
	}

	return tr.RunFuncWithOptions(
		taskrunner.Options{Name: "App build — " + w.Domain, Module: "website", Timeout: 30 * time.Minute},
		func(taskCtx context.Context, write func(string)) error {
			if w.AppRuntime == "go" || w.AppRuntime == "go-binary" {
				if err := s.runGoBuild(taskCtx, w, s.appWorkDir(taskCtx, w), write); err != nil {
					return err
				}
			} else {
				args, buildErr := buildAppBuildArgv(w.AppRuntime, w.WebUser, w.DocumentRoot, w.AppBuildCommand)
				if buildErr != nil {
					return buildErr
				}
				write("Running build: " + w.AppBuildCommand)
				result, err := s.exec.RunSudo(taskCtx, args[0], args[1:]...)
				if result != nil && result.Stdout != "" {
					write(result.Stdout)
				}
				if err != nil {
					return fmt.Errorf("build: %w", err)
				}
				if result.ExitCode != 0 {
					return fmt.Errorf("build failed with exit status %d", result.ExitCode)
				}
			}
			if err := s.AppAction(taskCtx, websiteID, "restart"); err != nil {
				return err
			}
			write("App service restarted.")
			return nil
		}), nil
}

// appWorkDir returns the directory the app's unit and build run from: the
// document root for most runtimes, the repository root for go/go-binary.
func appWorkDirFor(ctx context.Context, exec executor.CommandExecutor, w model.Website) string {
	if w.AppRuntime != "go" && w.AppRuntime != "go-binary" {
		return w.DocumentRoot
	}
	if root, _ := siteops.AppProjectRoot(ctx, exec, "/home/"+w.WebUser, w.DocumentRoot); root != "" {
		return root
	}
	return w.DocumentRoot
}

func (s *Service) appWorkDir(ctx context.Context, w model.Website) string {
	return appWorkDirFor(ctx, s.exec, w)
}

// writeAppUnitFiles renders the app unit and installs it enabled but not
// started — starting is the deploy hook's job once a binary exists.
func writeAppUnitFiles(ctx context.Context, exec executor.CommandExecutor, w model.Website) error {
	unitContent, err := buildAppUnit(w, appWorkDirFor(ctx, exec, w))
	if err != nil {
		return err
	}
	unitPath := "/etc/systemd/system/" + appUnitName(w.ID)
	if _, err := exec.RunSudoWithInput(ctx, unitContent, "tee", unitPath); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	if _, err := exec.RunSudo(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if _, err := exec.RunSudo(ctx, "systemctl", "enable", appUnitName(w.ID)); err != nil {
		return fmt.Errorf("enable unit: %w", err)
	}
	return nil
}

// ensureGoToolchain probes the server-wide Go toolchain and installs the
// pinned release inline when it is missing, streaming progress into write.
func (s *Service) ensureGoToolchain(ctx context.Context, write func(string)) error {
	gr := goruntime.NewService(s.exec, nil)
	if status, err := gr.Status(ctx); err == nil && status.Installed {
		return nil
	}
	write("Installing Go toolchain " + goruntime.Version + " ...")
	return gr.InstallSync(ctx, write)
}

// runGoBuild validates the project shape, ensures the toolchain, and runs
// the site's build command as the site user from the repository root.
func (s *Service) runGoBuild(ctx context.Context, w model.Website, root string, write func(string)) error {
	res, err := s.exec.RunSudo(ctx, "test", "-f", root+"/go.mod")
	if err != nil || res == nil || res.ExitCode != 0 {
		return model.NewValidationError("no go.mod found in the repository root; is this a Go project?")
	}
	if err := s.ensureGoToolchain(ctx, write); err != nil {
		return err
	}
	if strings.TrimSpace(w.AppBuildCommand) == "" {
		return model.NewValidationError("no build command configured")
	}
	write("Running build: " + w.AppBuildCommand)
	args, argvErr := buildAppBuildArgv(w.AppRuntime, w.WebUser, root, w.AppBuildCommand)
	if argvErr != nil {
		return argvErr
	}
	result, err := s.exec.RunSudo(ctx, args[0], args[1:]...)
	if result != nil && result.Stdout != "" {
		write(result.Stdout)
	}
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = fmt.Sprintf("exit status %d", result.ExitCode)
		}
		return fmt.Errorf("build failed: %s", detail)
	}
	return nil
}

// waitAppHealthy polls the app's loopback port until it accepts TCP
// connections (30 × 1s), so a deployment is only marked successful when
// the restarted binary actually came up.
func (s *Service) waitAppHealthy(ctx context.Context, port int) error {
	if port <= 0 {
		return model.NewDomainError("APP_HEALTH_CHECK_FAILED", "no app port allocated", nil)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return model.NewDomainError("APP_HEALTH_CHECK_FAILED",
				fmt.Sprintf("the app is not listening on %s; deployment aborted while waiting", addr), nil)
		case <-time.After(time.Second):
		}
	}
	return model.NewDomainError("APP_HEALTH_CHECK_FAILED",
		fmt.Sprintf("the app is not listening on %s after 30s; check the app unit logs", addr), nil)
}

// appExecPath resolves the executable of a start command the same way the
// unit does: "./x" and bare names live in the project root; arguments are
// dropped.
func appExecPath(start, projectRoot string) string {
	fields := strings.Fields(start)
	if len(fields) == 0 {
		return ""
	}
	exe := fields[0]
	if strings.HasPrefix(exe, "./") {
		return projectRoot + "/" + strings.TrimPrefix(exe, "./")
	}
	if !strings.Contains(exe, "/") {
		return projectRoot + "/" + exe
	}
	return exe
}

// checkAppExecutable verifies, before the unit is restarted, that the start
// command points at something systemd can execute. The common trap is a
// repository with an "app/" directory: `go build -o app .` then writes the
// binary inside that directory, and exec'ing the directory fails with
// "Permission denied" (status 126) in an endless restart loop.
func (s *Service) checkAppExecutable(ctx context.Context, w model.Website, projectRoot string, write func(string)) error {
	start := strings.TrimSpace(w.AppStartCommand)
	path := appExecPath(start, projectRoot)
	if path == "" {
		return model.NewValidationError("start command is required")
	}
	res, err := s.exec.RunSudo(ctx, "stat", "-c", "%F", path)
	if err != nil || res == nil || res.ExitCode != 0 {
		return model.NewValidationError(fmt.Sprintf(
			"the start command %q points to %s, which does not exist; check the build output (-o) and the start command on the App tab",
			start, path))
	}
	switch kind := strings.TrimSpace(res.Stdout); kind {
	case "regular file", "regular empty file":
	case "directory":
		return model.NewValidationError(fmt.Sprintf(
			"%s is a directory in your repository, so the build put the binary inside it instead of creating the file; "+
				"on the App tab set the build command to `go build -o server .` and the start command to `./server`",
			path))
	default:
		return model.NewValidationError(fmt.Sprintf("%s is a %s, not an executable file", path, kind))
	}
	if res, err := s.exec.RunSudo(ctx, "findmnt", "-no", "OPTIONS", "-T", path); err == nil && res != nil {
		for _, opt := range strings.Split(strings.TrimSpace(res.Stdout), ",") {
			if opt == "noexec" {
				return model.NewValidationError(fmt.Sprintf(
					"the filesystem holding %s is mounted noexec, so binaries there cannot run; remount it without noexec", path))
			}
		}
	}
	if res, err := s.exec.RunSudo(ctx, "test", "-x", path); err == nil && res != nil && res.ExitCode != 0 {
		if res, err := s.exec.RunSudo(ctx, "chmod", "+x", path); err != nil || res == nil || res.ExitCode != 0 {
			return model.NewValidationError(path + " is not executable and could not be made executable")
		}
		write("made " + path + " executable")
	}
	return nil
}

// appStartupReport explains why an app unit did not come up on its port.
type appStartupReport struct {
	ActiveState string
	SubState    string
	MainPID     int
	Restarts    int
	// Listening lists the TCP addresses the unit's main process listens on.
	Listening []string
	Logs      []string
}

// appStartupReport collects the unit state, the ports its process actually
// listens on, and its last journal lines.
func (s *Service) appStartupReport(ctx context.Context, unit string) appStartupReport {
	var report appStartupReport
	if res, err := s.exec.RunSudo(ctx, "systemctl", "show", "--property=ActiveState,SubState,MainPID,NRestarts", unit); err == nil && res != nil {
		for _, line := range strings.Split(res.Stdout, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			switch key {
			case "ActiveState":
				report.ActiveState = value
			case "SubState":
				report.SubState = value
			case "MainPID":
				report.MainPID, _ = strconv.Atoi(value)
			case "NRestarts":
				report.Restarts, _ = strconv.Atoi(value)
			}
		}
	}
	if report.MainPID > 0 {
		if res, err := s.exec.RunSudo(ctx, "ss", "-ltnpH"); err == nil && res != nil {
			marker := "pid=" + strconv.Itoa(report.MainPID) + ","
			for _, line := range strings.Split(res.Stdout, "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 4 && strings.Contains(line, marker) {
					report.Listening = append(report.Listening, fields[3])
				}
			}
		}
	}
	report.Logs = s.octaneUnitLogs(ctx, unit, 20)
	return report
}

// explain turns the report into an actionable deploy error message.
func (r appStartupReport) explain(port int) string {
	want := fmt.Sprintf("127.0.0.1:%d", port)
	if len(r.Listening) > 0 {
		return fmt.Sprintf("the app is listening on %s instead of %s; make it listen on the port from the PORT environment variable (the panel sets PORT=%d)",
			strings.Join(r.Listening, ", "), want, port)
	}
	if r.ActiveState != "active" || r.Restarts > 0 {
		for _, line := range r.Logs {
			if strings.Contains(line, "Permission denied") {
				return "the app cannot be executed (Permission denied); the start command must point to the built binary file — " +
					"if your repository has a directory with the same name, build to another name such as `go build -o server .` and start `./server`"
			}
		}
		return fmt.Sprintf("the app exits right after starting (state %s/%s, restarted %d times); see the app log above",
			r.ActiveState, r.SubState, r.Restarts)
	}
	return fmt.Sprintf("the app is running but not listening on %s after 30s; make it listen on the PORT environment variable (the panel sets PORT=%d)", want, port)
}

// sortedAppPorts is a tiny helper for deterministic tests.
func sortedAppPorts(m map[int]bool) []int {
	ports := make([]int, 0, len(m))
	for p := range m {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}
