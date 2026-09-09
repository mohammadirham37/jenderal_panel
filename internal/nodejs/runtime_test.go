package nodejs

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
)

type fakeRuntime struct {
	detect       func(context.Context, string, string) (noderuntime.Status, error)
	install      func(context.Context, string, string, func(string)) error
	installPanel func(context.Context, func(string)) error
}

func (f fakeRuntime) Detect(ctx context.Context, user, version string) (noderuntime.Status, error) {
	return f.detect(ctx, user, version)
}
func (f fakeRuntime) Install(ctx context.Context, user, version string, log func(string)) error {
	return f.install(ctx, user, version, log)
}
func (f fakeRuntime) InstallPanel(ctx context.Context, log func(string)) error {
	return f.installPanel(ctx, log)
}

func installedRuntime(version string) fakeRuntime {
	return fakeRuntime{
		detect: func(_ context.Context, _ string, requested string) (noderuntime.Status, error) {
			return noderuntime.Status{Installed: true, NodeVersion: "v" + requested + ".2.1", NPMVersion: "11.1.0", NVMVersion: "v0.40.7", NVMState: noderuntime.NVMStateReady}, nil
		},
		install:      func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error { return nil },
	}
}

func TestListRuntimesDistinguishesSelectedAndDetectedVersions(t *testing.T) {
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	insertTestWebsite(t, db, "site-runtime", "runtime.example.com", "web_runtime", "/home/web_runtime/public")
	insertTestWebsite(t, db, "site-none", "none.example.com", "web_none", "/home/web_none/public")
	insertTestWebsite(t, db, "site-broken", "broken.example.com", "web_broken", "/home/web_broken/public")
	if _, err := db.Exec(`UPDATE websites SET node_version = '' WHERE id = 'site-none'`); err != nil {
		t.Fatal(err)
	}
	detectCalls := 0
	svc := NewService(db, newMockExec(), nil)
	svc.runtime = fakeRuntime{
		detect: func(_ context.Context, user, version string) (noderuntime.Status, error) {
			detectCalls++
			if user == "web_broken" {
				return noderuntime.Status{}, errors.New("website user does not exist")
			}
			if user != "web_runtime" || version != "24" {
				t.Fatalf("Detect(%q,%q)", user, version)
			}
			return noderuntime.Status{Installed: true, NodeVersion: "v24.7.0", NPMVersion: "11.5.1", NVMVersion: "v0.40.7", NVMState: noderuntime.NVMStateReady}, nil
		},
		install:      func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error { return nil },
	}
	got, err := svc.ListRuntimes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || detectCalls != 2 {
		t.Fatalf("runtimes=%#v detectCalls=%d", got, detectCalls)
	}
	byID := make(map[string]RuntimeInfo)
	for _, runtime := range got {
		byID[runtime.WebsiteID] = runtime
	}
	if runtime := byID["site-runtime"]; runtime.SelectedVersion != "24" || runtime.InstalledVersion != "v24.7.0" || runtime.NPMVersion != "11.5.1" || !runtime.Installed {
		t.Fatalf("installed runtime = %#v", runtime)
	}
	if runtime := byID["site-none"]; runtime.SelectedVersion != "" || runtime.NVMState != "not_selected" || runtime.Installed {
		t.Fatalf("empty runtime = %#v", runtime)
	}
	if runtime := byID["site-broken"]; runtime.NVMState != "error" || !strings.Contains(runtime.ErrorMessage, "website user") {
		t.Fatalf("broken runtime = %#v", runtime)
	}
}

func TestCreateAppInheritsInstalledWebsiteRuntimeAndWritesNVMUnit(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-app", "app.example.com", "web_app", "/home/web_app/public")
	var unit string
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "cp" && len(args) == 2 && strings.HasSuffix(args[1], ".service") {
			content, err := os.ReadFile(args[0])
			if err != nil {
				t.Fatal(err)
			}
			unit = string(content)
		}
		return mockResult("", "", 0), nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = installedRuntime("24")
	app, err := svc.CreateApp(context.Background(), CreateAppRequest{WebsiteID: "site-app", PackageMgr: "npm", StartCmd: "server.js", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if app.NodeVersion != "24" {
		t.Fatalf("NodeVersion=%q, want inherited 24", app.NodeVersion)
	}
	for _, required := range []string{
		`Environment="NVM_DIR=/home/web_app/.nvm"`, `Environment="NODE_VERSION=24"`,
		`Environment="PATH=/usr/local/bin:/usr/bin:/bin"`, `ExecStart=/home/web_app/.nvm/nvm-exec node server.js`,
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("unit missing %q:\n%s", required, unit)
		}
	}
	if strings.Contains(unit, "/usr/bin/node") {
		t.Fatalf("unit retained global Node path:\n%s", unit)
	}
}

func TestCreateAppRejectsMissingRuntimeAndReservedOrInjectedEnvironment(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-validation", "validation.example.com", "web_validation", "/home/web_validation/public")
	svc := NewService(db, newMockExec(), nil)
	svc.runtime = fakeRuntime{
		detect: func(context.Context, string, string) (noderuntime.Status, error) {
			return noderuntime.Status{NVMState: noderuntime.NVMStateReady}, nil
		},
		install:      func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error { return nil },
	}
	_, err := svc.CreateApp(context.Background(), CreateAppRequest{WebsiteID: "site-validation", StartCmd: "server.js", Port: 3000})
	if err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("missing runtime error=%v", err)
	}

	svc.runtime = installedRuntime("24")
	for _, req := range []CreateAppRequest{
		{WebsiteID: "site-validation", StartCmd: "server.js", Port: 3000, EnvVars: `{"PATH":"/tmp"}`},
		{WebsiteID: "site-validation", StartCmd: "server.js\nExecStart=/bin/id", Port: 3000},
		{WebsiteID: "site-validation", StartCmd: "server.js", Port: 3000, EnvVars: `{"GOOD":"ok\nEnvironment=BAD=yes"}`},
	} {
		if _, err := svc.CreateApp(context.Background(), req); err == nil {
			t.Fatalf("CreateApp accepted injected/reserved request %#v", req)
		}
	}
}

func TestStartLegacyAppDoesNotInstallOrRewriteWhenRuntimeMissing(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-legacy", "legacy.example.com", "web_legacy", "/home/web_legacy/public")
	now := "2026-09-09T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO nodejs_apps
		(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
		VALUES ('legacy-app', 'site-legacy', '20', 'npm', 'server.js', 3000, 'stopped', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	installCalls, systemctlCalls := 0, 0
	mock := newMockExec()
	mock.RunSudoFunc = func(context.Context, string, ...string) (*executor.Result, error) {
		systemctlCalls++
		return mockResult("", "", 0), nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = fakeRuntime{
		detect: func(context.Context, string, string) (noderuntime.Status, error) {
			return noderuntime.Status{NVMState: noderuntime.NVMStateMissing}, nil
		},
		install: func(context.Context, string, string, func(string)) error {
			installCalls++
			return nil
		},
		installPanel: func(context.Context, func(string)) error { return nil },
	}
	err := svc.Start(context.Background(), "legacy-app")
	if err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("Start() error=%v", err)
	}
	if installCalls != 0 || systemctlCalls != 0 {
		t.Fatalf("installCalls=%d systemctlCalls=%d", installCalls, systemctlCalls)
	}
}

func TestChangeRuntimeRollsBackMetadataAndUnitOnRestartFailure(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-change", "change.example.com", "web_change", "/home/web_change/public")
	now := "2026-09-09T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO nodejs_apps
		(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
		VALUES ('change-app', 'site-change', '24', 'npm', 'server.js', 3000, 'running', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	const oldUnit = "[Service]\nExecStart=/usr/bin/node server.js\n"
	var writes []string
	restartCalls := 0
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		switch {
		case name == "cat":
			return mockResult(oldUnit, "", 0), nil
		case name == "systemctl" && len(args) >= 2 && args[0] == "is-active":
			return mockResult("", "", 0), nil
		case name == "systemctl" && len(args) >= 2 && args[0] == "is-enabled":
			return mockResult("", "", 0), nil
		case name == "cp" && len(args) == 2 && strings.HasSuffix(args[1], ".service"):
			content, err := os.ReadFile(args[0])
			if err != nil {
				t.Fatal(err)
			}
			writes = append(writes, string(content))
			return mockResult("", "", 0), nil
		case strings.Contains(joined, "systemctl restart jenderal-node-change-app.service"):
			restartCalls++
			if restartCalls == 1 {
				return mockResult("", "restart failed", 1), nil
			}
			return mockResult("", "", 0), nil
		default:
			return mockResult("", "", 0), nil
		}
	}
	runtime := installedRuntime("22")
	runtime.detect = func(_ context.Context, _ string, version string) (noderuntime.Status, error) {
		return noderuntime.Status{Installed: true, NodeVersion: "v" + version + ".1.0", NPMVersion: "10", NVMState: noderuntime.NVMStateReady}, nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = runtime
	err := svc.ChangeRuntime(context.Background(), "site-change", "22", nil)
	if err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("ChangeRuntime() error=%v", err)
	}
	var websiteVersion, appVersion string
	if err := db.QueryRow(`SELECT node_version FROM websites WHERE id = 'site-change'`).Scan(&websiteVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT node_version FROM nodejs_apps WHERE id = 'change-app'`).Scan(&appVersion); err != nil {
		t.Fatal(err)
	}
	if websiteVersion != "24" || appVersion != "24" {
		t.Fatalf("metadata website=%q app=%q, want rollback to 24", websiteVersion, appVersion)
	}
	if len(writes) < 2 || writes[len(writes)-1] != oldUnit {
		t.Fatalf("unit writes=%q, want exact legacy unit restored", writes)
	}
	if restartCalls != 2 {
		t.Fatalf("restartCalls=%d, want failed activation and successful rollback restart", restartCalls)
	}
}

func TestChangeRuntimeRestoresPreviousDefaultWhenPostInstallVerificationFails(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-verify", "verify.example.com", "web_verify", "/home/web_verify/public")
	aliasReads := 0
	var restored string
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "-u" {
			aliasReads++
			return mockResult("24\n", "", 0), nil
		}
		return mockResult("", "", 0), nil
	}
	mock.RunSudoWithInputFunc = func(_ context.Context, input, name string, args ...string) (*executor.Result, error) {
		if name == "-u" {
			restored = input
		}
		return mockResult("", "", 0), nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = fakeRuntime{
		detect: func(context.Context, string, string) (noderuntime.Status, error) {
			return noderuntime.Status{}, errors.New("verification failed")
		},
		install:      func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error { return nil },
	}
	err := svc.ChangeRuntime(context.Background(), "site-verify", "22", nil)
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("ChangeRuntime() error=%v", err)
	}
	if aliasReads != 1 || restored != "24\n" {
		t.Fatalf("aliasReads=%d restored=%q", aliasReads, restored)
	}
	var stored string
	if err := db.QueryRow(`SELECT node_version FROM websites WHERE id = 'site-verify'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "24" {
		t.Fatalf("stored version=%q, want 24", stored)
	}
}

func TestChangeRuntimePreservesExistingEmptyDefaultAlias(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-empty-alias", "empty-alias.example.com", "web_empty_alias", "/home/web_empty_alias/public")
	var restoredInputs []string
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "-u" {
			return mockResult("", "", 0), nil
		}
		return mockResult("", "", 0), nil
	}
	mock.RunSudoWithInputFunc = func(_ context.Context, input, name string, args ...string) (*executor.Result, error) {
		restoredInputs = append(restoredInputs, input)
		return mockResult("", "", 0), nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = fakeRuntime{
		detect: func(context.Context, string, string) (noderuntime.Status, error) {
			return noderuntime.Status{}, errors.New("verification failed")
		},
		install:      func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error { return nil },
	}

	err := svc.ChangeRuntime(context.Background(), "site-empty-alias", "22", nil)
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("ChangeRuntime() error=%v", err)
	}
	if len(restoredInputs) != 1 || restoredInputs[0] != "" {
		t.Fatalf("restored alias inputs=%q, want one exact empty write", restoredInputs)
	}
}

func TestReadDefaultAliasRecognizesProvenFirstInstallAbsence(t *testing.T) {
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name != "-u" {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		if args[2] == "/usr/bin/cat" {
			return mockResult("", "No such file", 1), nil
		}
		flag, path := args[3], args[4]
		switch {
		case flag == "-d" && strings.HasSuffix(path, "/.nvm"):
			return mockResult("", "", 0), nil
		case flag == "-x" && strings.HasSuffix(path, "/.nvm"):
			return mockResult("", "", 0), nil
		default:
			return mockResult("", "", 1), nil
		}
	}
	svc := NewService(setupTestDB(t), mock, nil)

	snapshot, err := svc.readDefaultAlias(context.Background(), "web_first_install")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.exists || snapshot.content != "" {
		t.Fatalf("snapshot=%#v, want proven absent alias", snapshot)
	}
}

func TestChangeRuntimeFailsBeforeInstallWhenSnapshotCannotBeRead(t *testing.T) {
	tests := []struct {
		name    string
		withApp bool
		mock    func(string, ...string) (*executor.Result, error)
	}{
		{
			name:    "existing unit unreadable",
			withApp: true,
			mock: func(name string, args ...string) (*executor.Result, error) {
				if name == "cat" {
					return mockResult("", "permission denied", 1), nil
				}
				if name == "test" && len(args) >= 2 && args[0] == "-e" {
					return mockResult("", "", 0), nil
				}
				return mockResult("", "", 0), nil
			},
		},
		{
			name: "default alias parent unreadable",
			mock: func(name string, args ...string) (*executor.Result, error) {
				if name == "-u" && len(args) >= 4 && args[2] == "/usr/bin/cat" {
					return mockResult("", "permission denied", 1), nil
				}
				if name == "-u" && len(args) >= 5 && args[2] == "/usr/bin/test" {
					if args[3] == "-d" && strings.HasSuffix(args[4], "/.nvm/alias") {
						return mockResult("", "", 0), nil
					}
					if args[3] == "-x" && strings.HasSuffix(args[4], "/.nvm/alias") {
						return mockResult("", "", 1), nil
					}
					return mockResult("", "", 1), nil
				}
				return mockResult("", "", 0), nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			insertTestWebsite(t, db, "site-snapshot", "snapshot.example.com", "web_snapshot", "/home/web_snapshot/public")
			if tt.withApp {
				now := "2026-09-09T00:00:00Z"
				if _, err := db.Exec(`INSERT INTO nodejs_apps
					(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
					VALUES ('snapshot-app', 'site-snapshot', '24', 'npm', 'server.js', 3000, 'stopped', ?, ?)`, now, now); err != nil {
					t.Fatal(err)
				}
			}
			installCalls := 0
			mock := newMockExec()
			mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
				return tt.mock(name, args...)
			}
			runtime := installedRuntime("22")
			runtime.install = func(context.Context, string, string, func(string)) error {
				installCalls++
				return nil
			}
			svc := NewService(db, mock, nil)
			svc.runtime = runtime

			if err := svc.ChangeRuntime(context.Background(), "site-snapshot", "22", nil); err == nil {
				t.Fatal("ChangeRuntime() error=nil, want snapshot failure")
			}
			if installCalls != 0 {
				t.Fatalf("runtime install calls=%d, want no mutation", installCalls)
			}
		})
	}
}

func TestChangeRuntimeFailsBeforeInstallOnUnexpectedSystemdProbeExit(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-probe", "probe.example.com", "web_probe", "/home/web_probe/public")
	now := "2026-09-09T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO nodejs_apps
		(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
		VALUES ('probe-app', 'site-probe', '24', 'npm', 'server.js', 3000, 'stopped', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	installCalls := 0
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "cat" {
			return mockResult("[Service]\nExecStart=/usr/bin/node server.js\n", "", 0), nil
		}
		if name == "systemctl" && args[0] == "is-active" {
			return mockResult("", "probe failed", 2), nil
		}
		return mockResult("", "", 0), nil
	}
	runtime := installedRuntime("22")
	runtime.install = func(context.Context, string, string, func(string)) error {
		installCalls++
		return nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = runtime

	if err := svc.ChangeRuntime(context.Background(), "site-probe", "22", nil); err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("ChangeRuntime() error=%v", err)
	}
	if installCalls != 0 {
		t.Fatalf("runtime install calls=%d, want no mutation", installCalls)
	}
}

func TestUnitUsesWebsiteRuntimeRequiresActiveExactDirectives(t *testing.T) {
	home := "/home/web_strict"
	valid := "[Unit]\nDescription=Node\n[Service]\nEnvironment=\"NVM_DIR=/home/web_strict/.nvm\"\nEnvironment=\"NODE_VERSION=24\"\nEnvironment=\"PATH=/usr/local/bin:/usr/bin:/bin\"\nExecStart=/home/web_strict/.nvm/nvm-exec node server.js\n"
	if !unitUsesWebsiteRuntime(valid, home, "24", "node") {
		t.Fatal("panel-generated unit was not recognized")
	}
	for _, command := range []string{"yarn", "pnpm"} {
		unit := strings.Replace(valid, "nvm-exec node ", "nvm-exec "+command+" ", 1)
		if !unitUsesWebsiteRuntime(unit, home, "24", command) {
			t.Fatalf("panel-generated %s unit was not recognized", command)
		}
	}
	misleading := []string{
		"[Service]\n# Environment=\"NODE_VERSION=24\"\n# ExecStart=/home/web_strict/.nvm/nvm-exec node server.js\nEnvironment=\"NODE_VERSION=20\"\nExecStart=/usr/bin/node server.js\n",
		"[Service]\nEnvironment=\"NODE_VERSION=24\"\nExecStart=/usr/bin/node server.js ExecStart=/home/web_strict/.nvm/nvm-exec node\n",
		"[Service]\nEnvironment=\"NODE_VERSION=24\"\nEnvironment=\"NODE_VERSION=20\"\nExecStart=/home/web_strict/.nvm/nvm-exec node server.js\n",
		"[Service]\nEnvironment=\"NVM_DIR=/home/web_strict/.nvm\"\nEnvironment=\"NODE_VERSION=24\"\nEnvironment=\"PATH=/usr/local/bin:/usr/bin:/bin\"\nExecStart=/home/web_strict/.nvm/nvm-exec /usr/bin/node server.js\n",
		"[Service]\nEnvironment=\"NODE_VERSION=24\"\nExecStart=/home/web_strict/.nvm/nvm-exec node server.js\\\n --ambiguous\n",
	}
	for _, unit := range misleading {
		if unitUsesWebsiteRuntime(unit, home, "24", "node") {
			t.Fatalf("misleading unit accepted:\n%s", unit)
		}
	}
}

func TestTenantRuntimeCommandRejectsGlobalPackageManagerFallback(t *testing.T) {
	mock := newMockExec()
	resolved := "/usr/bin/yarn\n"
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		return mockResult(resolved, "", 0), nil
	}
	svc := NewService(setupTestDB(t), mock, nil)
	if svc.tenantRuntimeCommand(context.Background(), "web_strict", "24", "/home/web_strict", "yarn") {
		t.Fatal("global yarn fallback was accepted as tenant-owned")
	}
	resolved = "/home/web_strict/.nvm/versions/node/v24.9.0/bin/yarn\n"
	if !svc.tenantRuntimeCommand(context.Background(), "web_strict", "24", "/home/web_strict", "yarn") {
		t.Fatal("tenant NVM yarn was rejected")
	}
}

func TestLegacyGlobalDependenciesExplainsTenantPackageManagerRequirement(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-yarn", "yarn.example.com", "web_yarn", "/home/web_yarn/public")
	now := "2026-09-09T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO nodejs_apps
		(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
		VALUES ('yarn-app', 'site-yarn', '24', 'yarn', 'start', 3000, 'stopped', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "cat" {
			return mockResult("[Service]\nEnvironment=\"NVM_DIR=/home/web_yarn/.nvm\"\nEnvironment=\"NODE_VERSION=24\"\nEnvironment=\"PATH=/usr/local/bin:/usr/bin:/bin\"\nExecStart=/home/web_yarn/.nvm/nvm-exec yarn start\n", "", 0), nil
		}
		return mockResult("/usr/bin/yarn\n", "", 0), nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = installedRuntime("24")

	dependencies, err := svc.legacyGlobalDependencies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 1 || !strings.Contains(dependencies[0], "install yarn") || !strings.Contains(dependencies[0], "NVM runtime") {
		t.Fatalf("dependencies=%v, want actionable tenant yarn requirement", dependencies)
	}
}

func TestChangeRuntimeActivatesVerifiedVersionAndRestartsOnlyActiveApps(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-success", "success.example.com", "web_success", "/home/web_success/public")
	now := "2026-09-09T00:00:00Z"
	for _, app := range []struct{ id, status string }{{"active-app", "running"}, {"stopped-app", "stopped"}} {
		if _, err := db.Exec(`INSERT INTO nodejs_apps
			(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
			VALUES (?, 'site-success', '24', 'npm', 'server.js', 3000, ?, ?, ?)`, app.id, app.status, now, now); err != nil {
			t.Fatal(err)
		}
	}
	var restarted []string
	var units []string
	mock := newMockExec()
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		switch {
		case name == "-u":
			return mockResult("24\n", "", 0), nil
		case name == "cat":
			return mockResult("[Service]\nExecStart=/usr/bin/node server.js\n", "", 0), nil
		case name == "systemctl" && args[0] == "is-active":
			if strings.Contains(args[len(args)-1], "active-app") {
				return mockResult("", "", 0), nil
			}
			return mockResult("", "", 3), nil
		case name == "systemctl" && args[0] == "is-enabled":
			return mockResult("", "", 0), nil
		case name == "systemctl" && args[0] == "restart":
			restarted = append(restarted, args[1])
			return mockResult("", "", 0), nil
		case name == "cp" && strings.HasSuffix(args[1], ".service"):
			content, err := os.ReadFile(args[0])
			if err != nil {
				t.Fatal(err)
			}
			units = append(units, string(content))
		}
		return mockResult("", "", 0), nil
	}
	installVersion := ""
	runtime := installedRuntime("22")
	runtime.install = func(_ context.Context, user, version string, log func(string)) error {
		if user != "web_success" {
			t.Fatalf("install user=%q", user)
		}
		installVersion = version
		return nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = runtime
	if err := svc.ChangeRuntime(context.Background(), "site-success", "22", nil); err != nil {
		t.Fatal(err)
	}
	if installVersion != "22" || len(restarted) != 1 || restarted[0] != serviceName("active-app") {
		t.Fatalf("installVersion=%q restarted=%v", installVersion, restarted)
	}
	if len(units) != 2 {
		t.Fatalf("unit writes=%d, want 2", len(units))
	}
	for _, unit := range units {
		if !strings.Contains(unit, `Environment="NODE_VERSION=22"`) || !strings.Contains(unit, "/home/web_success/.nvm/nvm-exec node server.js") {
			t.Fatalf("unit did not activate Node 22:\n%s", unit)
		}
	}
	var websiteVersion string
	if err := db.QueryRow(`SELECT node_version FROM websites WHERE id = 'site-success'`).Scan(&websiteVersion); err != nil {
		t.Fatal(err)
	}
	if websiteVersion != "22" {
		t.Fatalf("website version=%q", websiteVersion)
	}
	var migrated int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nodejs_apps WHERE website_id = 'site-success' AND node_version = '22'`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != 2 {
		t.Fatalf("migrated apps=%d, want 2", migrated)
	}
}

func TestGlobalRemovalRequiresMigratedAppsAndPanelRuntime(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-global", "global.example.com", "web_global", "/home/web_global/public")
	now := "2026-09-09T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO nodejs_apps
		(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
		VALUES ('global-app', 'site-global', '24', 'npm', 'server.js', 3000, 'running', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	mock := newMockExec()
	mock.RunFunc = func(context.Context, string, ...string) (*executor.Result, error) {
		return mockResult("install ok installed\t20.19.0-1nodesource1\n", "", 0), nil
	}
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "cat" {
			return mockResult("[Service]\nExecStart=/usr/bin/node server.js\n", "", 0), nil
		}
		return mockResult("", "", 0), nil
	}
	panelCalls := 0
	svc := NewService(db, mock, nil)
	svc.runtime = fakeRuntime{
		detect: func(context.Context, string, string) (noderuntime.Status, error) {
			return noderuntime.Status{Installed: true, NodeVersion: "v24.1.0", NPMVersion: "11", NVMState: noderuntime.NVMStateReady}, nil
		},
		install: func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error {
			panelCalls++
			return errors.New("panel runtime failed")
		},
	}
	if err := svc.RemoveGlobal(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "global-app") {
		t.Fatalf("legacy app error=%v", err)
	}
	if panelCalls != 0 {
		t.Fatalf("panel runtime prepared before legacy dependency check")
	}

	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "cat" {
			return mockResult("[Service]\nEnvironment=\"NVM_DIR=/home/web_global/.nvm\"\nEnvironment=\"NODE_VERSION=24\"\nEnvironment=\"PATH=/usr/local/bin:/usr/bin:/bin\"\nExecStart=/home/web_global/.nvm/nvm-exec node server.js\n", "", 0), nil
		}
		return mockResult("", "", 0), nil
	}
	if err := svc.RemoveGlobal(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "panel runtime") {
		t.Fatalf("panel bootstrap error=%v", err)
	}
	if panelCalls != 1 {
		t.Fatalf("panelCalls=%d, want 1", panelCalls)
	}
}

func TestGlobalRemovalCannotRaceRuntimeActivationRollback(t *testing.T) {
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	insertTestWebsite(t, db, "site-overlap", "overlap.example.com", "web_overlap", "/home/web_overlap/public")
	now := "2026-09-09T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO nodejs_apps
		(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
		VALUES ('overlap-app', 'site-overlap', '24', 'npm', 'server.js', 3000, 'running', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	const legacyUnit = "[Service]\nExecStart=/usr/bin/node server.js\n"
	var unitMu sync.Mutex
	currentUnit := legacyUnit
	activationBlocked := make(chan struct{})
	allowFailure := make(chan struct{})
	aptCalled := make(chan struct{}, 1)
	daemonReloadCalls := 0
	mock := newMockExec()
	mock.RunFunc = func(context.Context, string, ...string) (*executor.Result, error) {
		return mockResult("install ok installed\t20.19.0-1\n", "", 0), nil
	}
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		switch {
		case name == "cat":
			unitMu.Lock()
			defer unitMu.Unlock()
			return mockResult(currentUnit, "", 0), nil
		case name == "systemctl" && (args[0] == "is-active" || args[0] == "is-enabled"):
			return mockResult("", "", 0), nil
		case name == "systemctl" && args[0] == "daemon-reload":
			daemonReloadCalls++
			if daemonReloadCalls == 1 {
				close(activationBlocked)
				<-allowFailure
				return mockResult("", "activation failed", 1), nil
			}
			return mockResult("", "", 0), nil
		case name == "cp" && strings.HasSuffix(args[1], ".service"):
			content, err := os.ReadFile(args[0])
			if err != nil {
				t.Fatal(err)
			}
			unitMu.Lock()
			currentUnit = string(content)
			unitMu.Unlock()
			return mockResult("", "", 0), nil
		case name == "apt-get":
			aptCalled <- struct{}{}
			return mockResult("", "", 0), nil
		default:
			return mockResult("", "", 0), nil
		}
	}
	svc := NewService(db, mock, nil)
	svc.runtime = installedRuntime("22")
	changeDone := make(chan error, 1)
	go func() { changeDone <- svc.ChangeRuntime(context.Background(), "site-overlap", "24", nil) }()
	<-activationBlocked
	removeDone := make(chan error, 1)
	go func() { removeDone <- svc.RemoveGlobal(context.Background(), nil) }()
	select {
	case err := <-removeDone:
		t.Fatalf("global removal escaped in-progress activation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(allowFailure)
	if err := <-changeDone; err == nil {
		t.Fatal("ChangeRuntime() error=nil, want activation failure")
	}
	if err := <-removeDone; err == nil || !strings.Contains(err.Error(), "overlap-app") {
		t.Fatalf("RemoveGlobal() error=%v, want restored legacy dependency", err)
	}
	select {
	case <-aptCalled:
		t.Fatal("apt nodejs was removed during activation rollback")
	default:
	}
}

func TestGlobalRemovalRechecksDependenciesAfterPanelBootstrap(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-recheck", "recheck.example.com", "web_recheck", "/home/web_recheck/public")
	now := "2026-09-09T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO nodejs_apps
		(id, website_id, node_version, package_mgr, start_cmd, port, status, created_at, updated_at)
		VALUES ('recheck-app', 'site-recheck', '24', 'npm', 'server.js', 3000, 'running', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	unit := "[Service]\nEnvironment=\"NVM_DIR=/home/web_recheck/.nvm\"\nEnvironment=\"NODE_VERSION=24\"\nEnvironment=\"PATH=/usr/local/bin:/usr/bin:/bin\"\nExecStart=/home/web_recheck/.nvm/nvm-exec node server.js\n"
	aptCalls := 0
	mock := newMockExec()
	mock.RunFunc = func(context.Context, string, ...string) (*executor.Result, error) {
		return mockResult("install ok installed\t20.19.0-1\n", "", 0), nil
	}
	mock.RunSudoFunc = func(_ context.Context, name string, _ ...string) (*executor.Result, error) {
		if name == "cat" {
			return mockResult(unit, "", 0), nil
		}
		if name == "apt-get" {
			aptCalls++
		}
		return mockResult("", "", 0), nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = fakeRuntime{
		detect: func(context.Context, string, string) (noderuntime.Status, error) {
			return noderuntime.Status{Installed: true, NodeVersion: "v24.1.0", NPMVersion: "11", NVMState: noderuntime.NVMStateReady}, nil
		},
		install: func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error {
			unit = "[Service]\nExecStart=/usr/bin/node server.js\n"
			return nil
		},
	}
	if err := svc.RemoveGlobal(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "recheck-app") {
		t.Fatalf("RemoveGlobal() error=%v", err)
	}
	if aptCalls != 0 {
		t.Fatalf("aptCalls=%d, want recheck to cancel removal", aptCalls)
	}
}

var _ runtimeManager = fakeRuntime{}
