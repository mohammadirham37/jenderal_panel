package website

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
}

func appUnitName(websiteID string) string {
	return appUnitPrefix + websiteID + ".service"
}

// allocateAppPort picks the lowest free app port: not used by another site
// and not currently listening on the server.
func (s *Service) allocateAppPort(ctx context.Context) (int, error) {
	used := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT app_port FROM websites WHERE app_port > 0`)
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

	result, err := s.exec.RunSudo(ctx, "ss", "-ltnH")
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
	}, nil
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
	} else if !strings.ContainsAny(exec, "/") {
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
	unitContent, err := buildAppUnit(w, s.appWorkDir(ctx, w))
	if err != nil {
		return AppServiceStatus{}, err
	}
	unitPath := "/etc/systemd/system/" + appUnitName(w.ID)
	if _, err := s.exec.RunSudoWithInput(ctx, unitContent, "tee", unitPath); err != nil {
		return AppServiceStatus{}, fmt.Errorf("write unit file: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "daemon-reload"); err != nil {
		return AppServiceStatus{}, fmt.Errorf("daemon-reload: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "enable", appUnitName(w.ID)); err != nil {
		return AppServiceStatus{}, fmt.Errorf("enable unit: %w", err)
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
	if strings.TrimSpace(w.AppBuildCommand) == "" && w.AppType != "python" {
		return "", model.NewValidationError("no build command configured")
	}
	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
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
func (s *Service) appWorkDir(ctx context.Context, w model.Website) string {
	if w.AppRuntime != "go" && w.AppRuntime != "go-binary" {
		return w.DocumentRoot
	}
	if root, _ := siteops.AppProjectRoot(ctx, s.exec, "/home/"+w.WebUser, w.DocumentRoot); root != "" {
		return root
	}
	return w.DocumentRoot
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

// sortedAppPorts is a tiny helper for deterministic tests.
func sortedAppPorts(m map[int]bool) []int {
	ports := make([]int, 0, len(m))
	for p := range m {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}
