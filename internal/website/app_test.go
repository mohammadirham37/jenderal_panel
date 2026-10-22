package website

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestBuildAppUnit(t *testing.T) {
	w := model.Website{
		ID: "site-1", Domain: "app.example.com", AppType: "node",
		PHPVersion: "8.2", NodeVersion: "22",
		DocumentRoot: "/home/web_example_com/public", WebUser: "web_example_com",
		AppStartCommand: "npm run start", AppPort: 8200,
	}
	unit, err := buildAppUnit(w, w.DocumentRoot)
	if err != nil {
		t.Fatalf("buildAppUnit: %v", err)
	}
	for _, want := range []string{
		"User=web_example_com",
		"WorkingDirectory=/home/web_example_com/public",
		"NODE_VERSION=22",
		"nvm-exec npm run start",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestBuildAppUnitRejectsEmptyCommand(t *testing.T) {
	w := model.Website{WebUser: "web_example_com", NodeVersion: "22"}
	if _, err := buildAppUnit(w, "/home/web_example_com/public"); err == nil {
		t.Fatal("empty start command must be rejected")
	}
}

func TestAppProxyNginxProfile(t *testing.T) {
	data := VhostData{Domain: "app.example.com", Profile: "app-proxy", AppPort: 8200}
	d := directivesFor(data)

	if !strings.Contains(d.Header, "$jenderal_app_app_example_com_ws") {
		t.Errorf("ws map variable missing: %q", d.Header)
	}
	if !strings.Contains(d.Location, "proxy_pass http://127.0.0.1:8200;") {
		t.Errorf("proxy target wrong: %q", d.Location)
	}
	// Static files first, everything else to the app. "$uri/" must not be
	// tried: "/" would match the (index-less) document root directory and
	// nginx would answer 403 instead of proxying.
	if !strings.Contains(d.Location, "try_files $uri @app;") || strings.Contains(d.Location, "$uri/") {
		t.Errorf("app-proxy try_files must be \"$uri @app\": %q", d.Location)
	}
	if !strings.Contains(d.Location, "Upgrade $http_upgrade") {
		t.Errorf("websocket upgrade header missing: %q", d.Location)
	}
}

// The HTTP and TLS vhosts of one app site load in the same nginx http
// context; an identically named upgrade map in both files is a config error
// that stops nginx from starting at all.
func TestAppProxyVhostHTTPAndTLSUseDistinctWebsocketMaps(t *testing.T) {
	data := VhostData{Domain: "app.example.com", Profile: "app-proxy", AppPort: 8200}

	httpVhost, err := RenderVhost(data)
	if err != nil {
		t.Fatalf("RenderVhost: %v", err)
	}
	if !strings.Contains(httpVhost, "$jenderal_app_app_example_com_ws {") {
		t.Errorf("HTTP vhost must use the _ws map name:\n%s", httpVhost)
	}

	tlsVhost, err := RenderTLSVhost(TLSVhostData{
		VhostData:       data,
		TLSDomain:       "app.example.com",
		CertificatePath: "/etc/jenderal/ssl/app.example.com/fullchain.pem",
		PrivateKeyPath:  "/etc/jenderal/ssl/app.example.com/privkey.pem",
	})
	if err != nil {
		t.Fatalf("RenderTLSVhost: %v", err)
	}
	if !strings.Contains(tlsVhost, "$jenderal_app_app_example_com_wss {") {
		t.Errorf("TLS vhost must use its own _wss map name:\n%s", tlsVhost)
	}
	if strings.Contains(tlsVhost, "$jenderal_app_app_example_com_ws ") {
		t.Error("TLS and HTTP vhosts must not share the app upgrade map variable")
	}
}

func TestBuildPythonAppUnit(t *testing.T) {
	w := model.Website{
		ID: "site-2", Domain: "api.example.com", AppType: "python",
		PHPVersion: "8.2", DocumentRoot: "/home/web_example_com/public",
		WebUser: "web_example_com", AppStartCommand: "gunicorn -w 2 app:app",
	}
	unit, err := buildPythonAppUnit(w, w.DocumentRoot)
	if err != nil {
		t.Fatalf("buildPythonAppUnit: %v", err)
	}
	for _, want := range []string{
		`Environment="PATH=/home/web_example_com/venv/bin:/usr/local/bin:/usr/bin:/bin"`,
		"WorkingDirectory=/home/web_example_com/public",
		"exec gunicorn -w 2 app:app",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("python unit missing %q:\n%s", want, unit)
		}
	}
}

func TestBuildAppBuildArgvPython(t *testing.T) {
	args, err := buildAppBuildArgv("python", "web_example_com", "/home/web_example_com/public", "")
	if err != nil {
		t.Fatalf("buildAppBuildArgv: %v", err)
	}
	if len(args) < 6 || args[0] != "-u" || args[1] != "web_example_com" || args[2] != "--" {
		t.Fatalf("python argv prefix = %#v, want -u web_example_com -- ...", args)
	}
	script := args[5]
	for _, want := range []string{
		"python3 -m venv '/home/web_example_com/venv'",
		"pip install --no-input -r requirements.txt",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("python build script missing %q: %s", want, script)
		}
	}

	// A user build command runs after dependency installation.
	args, err = buildAppBuildArgv("python", "web_example_com", "/home/web_example_com/public", "python manage.py collectstatic --noinput")
	if err != nil {
		t.Fatalf("buildAppBuildArgv with build: %v", err)
	}
	if !strings.Contains(args[5], "python manage.py collectstatic --noinput") {
		t.Errorf("user build command missing: %s", args[5])
	}
}

func TestBuildGoAppUnit_PortEnvEnvFileAndWorkingDir(t *testing.T) {
	unit, err := buildBinaryAppUnit(model.Website{
		AppRuntime: "go", AppPort: 8210, AppStartCommand: "./app", WebUser: "u", Domain: "go.example.com",
	}, "/home/u/app")
	if err != nil {
		t.Fatalf("buildBinaryAppUnit error = %v", err)
	}
	for _, want := range []string{
		"WorkingDirectory=/home/u/app\n",
		"Environment=PORT=8210\n",
		"EnvironmentFile=-/home/u/app/.env\n",
		// EnvironmentFile overrides Environment=, so a PORT in .env would win;
		// passing it on the command line keeps the panel-allocated port.
		"ExecStart=/usr/bin/env PORT=8210 /home/u/app/app\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q\nunit:\n%s", want, unit)
		}
	}
}

func TestBuildGoAppUnit_NoPortOmitsEnvironment(t *testing.T) {
	unit, err := buildBinaryAppUnit(model.Website{
		AppRuntime: "go", AppPort: 0, AppStartCommand: "./app", WebUser: "u", Domain: "go.example.com",
	}, "/home/u/app")
	if err != nil {
		t.Fatalf("buildBinaryAppUnit error = %v", err)
	}
	if strings.Contains(unit, "Environment=PORT=") || strings.Contains(unit, "EnvironmentFile=") {
		t.Errorf("unit should omit env lines without a port\nunit:\n%s", unit)
	}
}

func TestBuildGoBinaryAppUnit_PortAndEnvFile(t *testing.T) {
	unit, err := buildBinaryAppUnit(model.Website{
		AppRuntime: "go-binary", AppPort: 8211, AppStartCommand: "./app", WebUser: "u", Domain: "go.example.com",
	}, "/home/u/app")
	if err != nil {
		t.Fatalf("buildBinaryAppUnit error = %v", err)
	}
	for _, want := range []string{"Environment=PORT=8211\n", "EnvironmentFile=-/home/u/app/.env\n", "WorkingDirectory=/home/u/app\n"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q\nunit:\n%s", want, unit)
		}
	}
}

func TestBuildDenoAppUnit_Unchanged(t *testing.T) {
	unit, err := buildBinaryAppUnit(model.Website{
		AppRuntime: "deno", AppPort: 8212, AppStartCommand: "./server", WebUser: "u", Domain: "deno.example.com",
	}, "/home/u/public")
	if err != nil {
		t.Fatalf("buildBinaryAppUnit error = %v", err)
	}
	if strings.Contains(unit, "Environment=PORT=") || strings.Contains(unit, "EnvironmentFile=") {
		t.Errorf("deno unit must not gain go env lines\nunit:\n%s", unit)
	}
	if !strings.Contains(unit, "WorkingDirectory=/home/u/public\n") {
		t.Errorf("deno unit working dir changed\nunit:\n%s", unit)
	}
}

func TestGetAppService_GoSiteDefaultsRuntime(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1, Stdout: "inactive"}, nil
		},
	}
	svc := NewService(db, mock, nil)
	res, err := db.Exec(`INSERT INTO websites (id, domain, app_type, document_root, web_user, status, created_at, updated_at)
		VALUES ('w1', 'go.example.com', 'go', '/home/web_go_example_com/app/public', 'web_go_example_com', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	status, err := svc.GetAppService(context.Background(), "w1")
	if err != nil {
		t.Fatalf("GetAppService error = %v", err)
	}
	if status.Runtime != "go" {
		t.Fatalf("runtime = %q, want go (healed from empty)", status.Runtime)
	}
}

func TestBuildAppBuildArgvGo(t *testing.T) {
	argv, err := buildAppBuildArgv("go", "u", "/home/u/app", "go build -o app .")
	if err != nil {
		t.Fatalf("buildAppBuildArgv error = %v", err)
	}
	want := []string{"-u", "u", "--", "/bin/bash", "-c", "cd /home/u/app && PATH=/usr/local/go/bin:$PATH go build -o app ."}
	if len(argv) != len(want) {
		t.Fatalf("argv = %#v, want %#v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q (full: %#v)", i, argv[i], want[i], argv)
		}
	}
}

func TestRunGoBuild_MissingGoMod(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1}, nil
		},
	}
	svc := NewService(nil, mock, nil)
	w := model.Website{AppType: "go", AppRuntime: "go", WebUser: "u", AppBuildCommand: "go build -o app ."}
	err := svc.runGoBuild(context.Background(), w, "/home/u/app", func(string) {})
	if err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("err = %v, want go.mod validation error", err)
	}
}

func TestRunGoBuild_RunsBuildAsWebUser(t *testing.T) {
	var sudoCalls [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "go version go1.27.1 linux/amd64"}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudoCalls = append(sudoCalls, append([]string{name}, args...))
			if name == "test" {
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 0, Stdout: "built"}, nil
		},
	}
	svc := NewService(nil, mock, nil)
	w := model.Website{AppType: "go", AppRuntime: "go", WebUser: "u", AppBuildCommand: "go build -o app ."}
	var logged []string
	if err := svc.runGoBuild(context.Background(), w, "/home/u/app", func(l string) { logged = append(logged, l) }); err != nil {
		t.Fatalf("runGoBuild error = %v", err)
	}
	var buildCall []string
	for _, c := range sudoCalls {
		if c[0] == "-u" {
			buildCall = c
		}
	}
	if buildCall == nil {
		t.Fatalf("no sudo -u build call; calls = %#v", sudoCalls)
	}
	if buildCall[1] != "u" || buildCall[2] != "--" {
		t.Fatalf("build argv prefix = %#v, want -u u --", buildCall[:3])
	}
	if len(logged) == 0 {
		t.Error("build output not forwarded to write")
	}
}

func TestRunGoBuild_CompilerErrorFails(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "go version go1.27.1 linux/amd64"}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" {
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 1, Stderr: "cannot find package"}, nil
		},
	}
	svc := NewService(nil, mock, nil)
	w := model.Website{AppType: "go", AppRuntime: "go", WebUser: "u", AppBuildCommand: "go build -o app ."}
	err := svc.runGoBuild(context.Background(), w, "/home/u/app", func(string) {})
	if err == nil || !strings.Contains(err.Error(), "cannot find package") {
		t.Fatalf("err = %v, want compiler error surfaced", err)
	}
}

func TestEnsureGoToolchain_InstallsWhenMissing(t *testing.T) {
	installed := false
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "/usr/local/go/bin/go" {
				if installed {
					return &executor.Result{ExitCode: 0, Stdout: "go version go1.27.1 linux/amd64"}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "bash" && len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "1.27.1") {
				installed = true
				return &executor.Result{ExitCode: 0, Stdout: "go version go1.27.1 linux/amd64"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(nil, mock, nil)
	var logged []string
	if err := svc.ensureGoToolchain(context.Background(), func(l string) { logged = append(logged, l) }); err != nil {
		t.Fatalf("ensureGoToolchain error = %v", err)
	}
	if !installed {
		t.Fatal("install script was not executed")
	}
	if len(logged) == 0 {
		t.Error("install progress not written")
	}

	// Second call with the toolchain present must not reinstall.
	before := installed
	if err := svc.ensureGoToolchain(context.Background(), func(l string) { logged = append(logged, l) }); err != nil {
		t.Fatalf("second ensureGoToolchain error = %v", err)
	}
	_ = before
}

func TestWaitAppHealthy_ListeningPortSucceeds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	svc := NewService(nil, &executor.MockExecutor{}, nil)
	if err := svc.waitAppHealthy(context.Background(), port); err != nil {
		t.Fatalf("waitAppHealthy error = %v", err)
	}
}

func TestWaitAppHealthy_ClosedPortTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	svc := NewService(nil, &executor.MockExecutor{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err = svc.waitAppHealthy(ctx, port)
	if err == nil {
		t.Fatal("waitAppHealthy error = nil, want failure")
	}
	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "APP_HEALTH_CHECK_FAILED" {
		t.Fatalf("err = %v, want APP_HEALTH_CHECK_FAILED domain error", err)
	}
}

func TestAppStartupReportFindsWrongPortAndLogs(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "systemctl":
				return &executor.Result{Stdout: "ActiveState=active\nSubState=running\nMainPID=4242\nNRestarts=0\n"}, nil
			case "ss":
				return &executor.Result{Stdout: "LISTEN 0 4096 *:8080 *:* users:((\"app\",pid=4242,fd=3))\n" +
					"LISTEN 0 4096 127.0.0.1:5432 0.0.0.0:* users:((\"postgres\",pid=900,fd=5))\n" +
					"LISTEN 0 4096 [::]:9090 [::]:* users:((\"app\",pid=4242,fd=7))\n"}, nil
			case "journalctl":
				return &executor.Result{Stdout: "server starting on :8080\n"}, nil
			}
			return &executor.Result{}, nil
		},
	}
	svc := &Service{exec: mock}
	report := svc.appStartupReport(context.Background(), "jenderal-app-x.service")
	if report.MainPID != 4242 || strings.Join(report.Listening, ",") != "*:8080,[::]:9090" {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Logs) != 1 {
		t.Fatalf("logs = %v", report.Logs)
	}
	msg := report.explain(8200)
	for _, want := range []string{"*:8080", "127.0.0.1:8200", "PORT"} {
		if !strings.Contains(msg, want) {
			t.Errorf("explain missing %q: %s", want, msg)
		}
	}
}

func TestAppStartupReportExplainsCrashLoopAndSilentApp(t *testing.T) {
	crash := appStartupReport{ActiveState: "activating", SubState: "auto-restart", Restarts: 3}
	if msg := crash.explain(8200); !strings.Contains(msg, "exits") || !strings.Contains(msg, "3") {
		t.Errorf("crash loop explain = %s", msg)
	}
	silent := appStartupReport{ActiveState: "active", SubState: "running", MainPID: 10}
	if msg := silent.explain(8200); !strings.Contains(msg, "not listening on 127.0.0.1:8200") || !strings.Contains(msg, "PORT") {
		t.Errorf("silent explain = %s", msg)
	}
}

func execCheckMock(statType string, statExit, testXExit int, mountOpts string, calls *[][]string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			*calls = append(*calls, append([]string{name}, args...))
			switch name {
			case "stat":
				return &executor.Result{ExitCode: statExit, Stdout: statType + "\n"}, nil
			case "test":
				return &executor.Result{ExitCode: testXExit}, nil
			case "findmnt":
				return &executor.Result{Stdout: mountOpts + "\n"}, nil
			}
			return &executor.Result{}, nil
		},
	}
}

func TestCheckAppExecutable(t *testing.T) {
	w := model.Website{AppRuntime: "go", AppStartCommand: "./app --flag", AppBuildCommand: "go build -o app ."}
	root := "/home/u/app"

	cases := []struct {
		name, statType     string
		statExit, testExit int
		mount              string
		wantErr            []string
		wantChmod          bool
	}{
		{name: "ok", statType: "regular file", mount: "rw,relatime"},
		{name: "directory", statType: "directory", mount: "rw", wantErr: []string{"/home/u/app/app is a directory", "go build -o server .", "./server"}},
		{name: "missing", statExit: 1, mount: "rw", wantErr: []string{"does not exist", "/home/u/app/app"}},
		{name: "not executable", statType: "regular file", testExit: 1, mount: "rw", wantChmod: true},
		{name: "noexec", statType: "regular file", mount: "rw,nosuid,nodev,noexec", wantErr: []string{"noexec"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			svc := &Service{exec: execCheckMock(tc.statType, tc.statExit, tc.testExit, tc.mount, &calls)}
			var notes []string
			err := svc.checkAppExecutable(context.Background(), w, root, func(s string) { notes = append(notes, s) })
			if len(tc.wantErr) == 0 && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("error %v missing %q", err, want)
				}
			}
			chmod := false
			for _, c := range calls {
				if c[0] == "stat" && c[len(c)-1] != "/home/u/app/app" {
					t.Errorf("stat must check the executable without its arguments: %v", c)
				}
				if c[0] == "chmod" {
					chmod = true
				}
			}
			if chmod != tc.wantChmod {
				t.Errorf("chmod called = %v, want %v (calls %v)", chmod, tc.wantChmod, calls)
			}
		})
	}
}

func TestAppExecPath(t *testing.T) {
	for start, want := range map[string]string{
		"./app":              "/home/u/app/app",
		"./bin/server -v":    "/home/u/app/bin/server",
		"server":             "/home/u/app/server",
		"/usr/local/bin/foo": "/usr/local/bin/foo",
	} {
		if got := appExecPath(start, "/home/u/app"); got != want {
			t.Errorf("appExecPath(%q) = %q, want %q", start, got, want)
		}
	}
}

func TestAppStartupReportExplainsPermissionDenied(t *testing.T) {
	r := appStartupReport{ActiveState: "activating", SubState: "auto-restart", Restarts: 5,
		Logs: []string{"/usr/bin/env: '/home/u/app/app': Permission denied"}}
	if msg := r.explain(8200); !strings.Contains(msg, "Permission denied") || !strings.Contains(msg, "./server") {
		t.Errorf("explain = %s", msg)
	}
}

func TestRefreshNodeAppUnitRewritesUnitWithCurrentVersion(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	var unit string
	var restarted bool
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) { return &executor.Result{}, nil },
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "systemctl" && len(args) > 0 && args[0] == "try-restart" {
				restarted = true
			}
			if name == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			unit = input
			return &executor.Result{}, nil
		},
	}
	svc := NewService(db, mock, nil)
	if _, err := db.Exec(`INSERT INTO websites (id, domain, app_type, app_runtime, app_port, app_start_command, node_version,
		document_root, web_user, status, framework, created_at, updated_at)
		VALUES ('n1', 'node.example.com', 'node', 'node', 8300, 'server.js', '24', '/home/web_node_example_com/public',
		'web_node_example_com', 'active', 'node', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshNodeAppUnit(context.Background(), "n1"); err != nil {
		t.Fatalf("RefreshNodeAppUnit: %v", err)
	}
	if !strings.Contains(unit, "NODE_VERSION=24") || !restarted {
		t.Errorf("unit not rewritten with NODE_VERSION=24 / not restarted (restarted=%v):\n%s", restarted, unit)
	}

	// Non-node and unconfigured sites are a no-op.
	unit, restarted = "", false
	if _, err := db.Exec(`UPDATE websites SET app_start_command = '' WHERE id = 'n1'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshNodeAppUnit(context.Background(), "n1"); err != nil || unit != "" || restarted {
		t.Errorf("unconfigured node site must be a no-op: err=%v unit=%q restarted=%v", err, unit, restarted)
	}
}
