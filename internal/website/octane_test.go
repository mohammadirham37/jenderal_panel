package website

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestRenderOctaneCaddyfileIsLoopbackOnly(t *testing.T) {
	content := RenderOctaneCaddyfile("/home/web_example_com/app", 8100, 4)

	for _, want := range []string{
		"admin localhost:8200",
		"http://127.0.0.1:8100 {",
		"file /home/web_example_com/app/public/frankenphp-worker.php",
		"num 4",
		"root * /home/web_example_com/app/public",
		"try_files {path} frankenphp-worker.php",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("Caddyfile missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "0.0.0.0") || strings.Contains(content, ":80") {
		t.Errorf("Caddyfile must never bind a public interface:\n%s", content)
	}
}

func TestRenderOctaneUnitBindsLoopbackWithOctaneWorker(t *testing.T) {
	unit := RenderOctaneUnit("example.com", "web_example_com", "/home/web_example_com/app",
		"/etc/jenderal/octane/w1/Caddyfile", "8.3", 8105, 4)

	if !strings.Contains(unit, "User=web_example_com") {
		t.Errorf("unit must run as the website user:\n%s", unit)
	}
	if !strings.Contains(unit, "WorkingDirectory=/home/web_example_com/app") {
		t.Errorf("unit must run in the project root:\n%s", unit)
	}
	want := "ExecStart=/usr/bin/php8.3 artisan octane:start --server=frankenphp --host=127.0.0.1 --port=8105 --admin-port=8205 --workers=4 --max-requests=500 --no-interaction --caddyfile=/etc/jenderal/octane/w1/Caddyfile"
	if !strings.Contains(unit, want) {
		t.Errorf("ExecStart missing:\n%s\nwant:\n%s", unit, want)
	}
	if !strings.Contains(unit, "Restart=always") {
		t.Errorf("unit must restart on failure:\n%s", unit)
	}
}

func TestLaravelOctaneVhostRendersProxyWithUniqueWebsocketMaps(t *testing.T) {
	data := VhostData{
		Domain:       "example.com",
		DocumentRoot: "/home/web_example_com/app/public",
		LogDir:       "/home/web_example_com/logs",
		AppType:      "laravel",
		Profile:      "laravel-octane",
		PHPVersion:   "8.3",
		OctanePort:   8100,
	}

	httpVhost, err := RenderVhost(data)
	if err != nil {
		t.Fatalf("RenderVhost: %v", err)
	}
	for _, want := range []string{
		"map $http_upgrade $jenderal_ws_example_com_ws {",
		"proxy_pass http://127.0.0.1:8100;",
		"try_files $uri $uri/ @octane;",
		"proxy_set_header Upgrade $http_upgrade;",
		"location ^~ /.well-known/acme-challenge/",
	} {
		if !strings.Contains(httpVhost, want) {
			t.Errorf("HTTP vhost missing %q:\n%s", want, httpVhost)
		}
	}
	// Static PHP handling must be gone: requests go to the proxy.
	if strings.Contains(httpVhost, "fastcgi_pass") {
		t.Errorf("octane vhost must not keep fastcgi_pass:\n%s", httpVhost)
	}

	tlsData := data
	tlsData.Aliases = "www.example.com"
	tlsVhost, err := RenderTLSVhost(TLSVhostData{
		VhostData:       tlsData,
		TLSDomain:       "example.com",
		CertificatePath: "/etc/jenderal/ssl/example.com/fullchain.pem",
		PrivateKeyPath:  "/etc/jenderal/ssl/example.com/privkey.pem",
	})
	if err != nil {
		t.Fatalf("RenderTLSVhost: %v", err)
	}
	if !strings.Contains(tlsVhost, "$jenderal_ws_example_com_wss") {
		t.Errorf("TLS vhost must use its own websocket map name:\n%s", tlsVhost)
	}
	// Both files load in the same http context; identical map names break nginx.
	if strings.Contains(tlsVhost, "$jenderal_ws_example_com_ws ") {
		t.Error("TLS and HTTP vhosts must not share the websocket map variable")
	}
}

func TestResolveLaravelOctaneProfile(t *testing.T) {
	profile, err := ResolveProfile(CreateRequest{
		Template: "laravel-octane", FrameworkVersion: "12", PHPVersion: "8.2",
		FrontendStack: "blade", ProjectVariant: "empty", SetupMode: SetupAutomatic,
	})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if profile.Template != "laravel-octane" || profile.NginxProfile != "laravel-octane" {
		t.Errorf("profile = %s/%s, want laravel-octane", profile.Template, profile.NginxProfile)
	}
	if profile.Framework != "laravel" || profile.RelativeDocumentRoot != "app/public" {
		t.Errorf("framework/docroot = %s/%s", profile.Framework, profile.RelativeDocumentRoot)
	}
	if !profile.RequiresComposer {
		t.Error("automatic octane installs require composer")
	}

	if _, err := ResolveProfile(CreateRequest{Template: "laravel-octane", FrameworkVersion: "9", PHPVersion: "8.1", SetupMode: SetupConfigOnly}); err == nil {
		t.Error("Octane should refuse Laravel 9 (needs 10+)")
	}
}

func TestLaravelOctaneOptionsAreListed(t *testing.T) {
	options := websiteProfileOptions()
	found := 0
	for _, option := range options {
		if option.Template == "laravel-octane" && option.Enabled {
			found++
		}
	}
	if found == 0 {
		t.Error("no enabled laravel-octane options in the profile catalog")
	}
}

func TestLaravelOctaneInstallationPlanInstallsOctane(t *testing.T) {
	w := websiteRow{
		ID: "01HQ", Domain: "example.com", AppType: "laravel", PHPVersion: "8.3",
		DocumentRoot: "/home/web_example_com/app/public", WebUser: "web_example_com",
		Framework: "laravel", FrameworkVersion: "12", FrontendStack: "blade",
		ProjectVariant: "empty", SetupMode: SetupAutomatic, NginxProfile: "laravel-octane",
	}
	steps, err := installationPlan(w)
	if err != nil {
		t.Fatalf("installationPlan: %v", err)
	}
	var joined []string
	for _, step := range steps {
		joined = append(joined, step.Name+" :: "+strings.Join(step.Args, " "))
	}
	plan := strings.Join(joined, "\n")
	if !strings.Contains(plan, "require laravel/octane") {
		t.Errorf("plan must require laravel/octane:\n%s", plan)
	}
	if !strings.Contains(plan, "octane:install --server=frankenphp") {
		t.Errorf("plan must run octane:install for frankenphp:\n%s", plan)
	}
}

func TestAllocateOctanePortSkipsUsedAndListeningPorts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO websites (id, domain, app_type, document_root, web_user, status, ssl_enabled, created_at, updated_at, octane_port)
		VALUES ('w1', 'a.com', 'php', '/home/x/public', 'web_x', 'active', 0, '2026-01-01', '2026-01-01', 8100),
		       ('w2', 'b.com', 'php', '/home/y/public', 'web_y', 'active', 0, '2026-01-01', '2026-01-01', 8101)`); err != nil {
		t.Fatal(err)
	}

	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "ss" {
			out := "State  Recv-Q Send-Q Local Address:Port Peer Address:Port\nLISTEN 0      128        127.0.0.1:8102       0.0.0.0:*\n"
			return &executor.Result{ExitCode: 0, Stdout: out}, nil
		}
		return &executor.Result{ExitCode: 0}, nil
	}}

	svc := NewService(db, mock, nil)
	port, err := svc.allocateOctanePort(context.Background())
	if err != nil {
		t.Fatalf("allocateOctanePort: %v", err)
	}
	if port != 8103 {
		t.Errorf("allocated port = %d, want 8103 (8100/8101 stored, 8102 listening)", port)
	}
}

func TestCommandPresetsIncludeOctaneReloadOnlyWhenEnabled(t *testing.T) {
	base := model.Website{Framework: "laravel", AppType: "laravel"}
	if presets := commandPresetsFor(base); presetsContain(presets, "php artisan octane:reload") {
		t.Error("octane:reload must not be offered when Octane is disabled")
	}

	octane := base
	octane.OctaneEnabled = true
	if !presetsContain(commandPresetsFor(octane), "php artisan octane:reload") {
		t.Error("octane:reload preset missing for Octane sites")
	}
}

func presetsContain(presets []CommandPreset, command string) bool {
	for _, preset := range presets {
		if preset.Command == command {
			return true
		}
	}
	return false
}

func TestOctaneUnitStateAndLogsParsing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "systemctl" && args[0] == "show" {
				return &executor.Result{ExitCode: 0, Stdout: "failed\nauto-restart\n"}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "journalctl" {
				return &executor.Result{ExitCode: 0, Stdout: "line-one\nline-two\nline-three\n"}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
	}
	svc := NewService(db, mock, nil)

	state, sub := svc.octaneUnitState(context.Background(), "jenderal-octane-x.service")
	if state != "failed" || sub != "auto-restart" {
		t.Fatalf("state = %q/%q, want failed/auto-restart", state, sub)
	}

	logs := svc.octaneUnitLogs(context.Background(), "jenderal-octane-x.service", 15)
	if len(logs) != 3 || logs[0] != "line-one" || logs[2] != "line-three" {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestOctanePrepareScriptRegistersProvider(t *testing.T) {
	script := octanePrepareScript(model.Website{WebUser: "web_x", PHPVersion: "8.3"}, "/home/web_x/app")
	if !strings.Contains(script, "package:discover") {
		t.Fatal("prepare script must run package:discover — without it artisan does not know the octane namespace")
	}
	if !strings.Contains(script, "--no-scripts") {
		t.Fatal("composer require should keep --no-scripts (discovery is run explicitly)")
	}
	if !strings.Contains(script, "octane:install") || strings.Contains(script, "systemctl start") {
		t.Fatal("prepare script must run octane:install and must not start the unit")
	}
}

func TestStartOctaneTaskPreparesStartsAndVerifies(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	var prepared bool
	var started bool
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "systemctl" && args[0] == "is-active" {
				if !prepared || !started {
					t.Fatalf("is-active probed before prepare(%v)/start(%v)", prepared, started)
				}
				return &executor.Result{ExitCode: 0}, nil
			}
			// Default success: Create() probes the PHP binary on the way in.
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "bash" {
				prepared = true
				return &executor.Result{ExitCode: 0, Stdout: "ok"}, nil
			}
			if name == "systemctl" && args[0] == "start" {
				started = true
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil)
	svc.SetTaskRunner(taskrunner.New())

	created, err := svc.Create(context.Background(), CreateRequest{
		Domain: "octane.example.com", Template: "laravel", PHPVersion: "8.3", SetupMode: "config-only",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := db.Exec(`UPDATE websites SET octane_enabled = 1, octane_port = 8101 WHERE id = ?`, created.ID); err != nil {
		t.Fatal(err)
	}

	taskID, err := svc.StartOctaneTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("StartOctaneTask() error = %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		task, ok := svc.taskRunner().Get(taskID)
		if !ok {
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if task.Status == "completed" {
			break
		}
		if task.Status == "failed" {
			t.Fatalf("start task failed: %s\noutput: %s", task.Error, task.Output)
		}
		if time.Now().After(deadline) {
			t.Fatal("start task did not finish in time")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestProbeOctaneServedBy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "Caddy")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	port := server.URL[len("http://127.0.0.1:"):]
	portNum := 0
	fmt.Sscanf(port, "%d", &portNum)
	if got := probeOctaneServedBy(context.Background(), portNum, "octane.example.com"); got != "Caddy" {
		t.Fatalf("served by = %q, want Caddy", got)
	}
	if got := probeOctaneServedBy(context.Background(), 1, "octane.example.com"); got != "" {
		t.Fatalf("served by for dead port = %q, want empty", got)
	}
}
