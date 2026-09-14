package nginx

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// mockResult creates an executor.Result with the given stdout, stderr, and exit code.
func mockResult(stdout, stderr string, exitCode int) *executor.Result {
	return &executor.Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Duration: time.Millisecond,
	}
}

func TestStatus_Installed(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// nginx -v outputs version to stderr
			return mockResult("", "nginx version: nginx/1.24.0\n", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "nginx":
				// nginx -t
				return mockResult("", "nginx: configuration file /etc/nginx/nginx.conf test is successful\n", 0), nil
			case "systemctl":
				output := strings.Join([]string{
					"ActiveState=active",
					"SubState=running",
					"MainPID=1234",
					"UnitFileState=enabled",
				}, "\n") + "\n"
				return mockResult(output, "", 0), nil
			default:
				return mockResult("", "", 0), nil
			}
		},
	}

	svc := NewService(mock, nil)
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.Installed {
		t.Error("expected Installed to be true")
	}
	if status.Version != "1.24.0" {
		t.Errorf("expected version '1.24.0', got %q", status.Version)
	}
	if !status.Running {
		t.Error("expected Running to be true")
	}
	if !status.Enabled {
		t.Error("expected Enabled to be true")
	}
	if status.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", status.PID)
	}
	if !status.ConfigOK {
		t.Error("expected ConfigOK to be true")
	}
}

func TestStatus_NotInstalled(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// nginx not found, exit code 127
			return mockResult("", "bash: nginx: command not found\n", 127), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
	}

	svc := NewService(mock, nil)
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Installed {
		t.Error("expected Installed to be false")
	}
	if status.Running {
		t.Error("expected Running to be false")
	}
	if status.Version != "" {
		t.Errorf("expected empty version, got %q", status.Version)
	}
}

func TestInstallReplacesDefaultWelcomePage(t *testing.T) {
	written := make(map[string]string)
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoWithInputFunc: func(_ context.Context, input, name string, args ...string) (*executor.Result, error) {
			if name == "tee" && len(args) == 2 && args[0] == "--" {
				written[args[1]] = input
			}
			return mockResult("", "", 0), nil
		},
	}

	if err := NewService(mock, nil).Install(context.Background()); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	welcomeHTML := written["/var/www/html/index.nginx-debian.html"]
	if !strings.Contains(welcomeHTML, "Jenderal-Panel") {
		t.Fatalf("welcome page is missing branding or Tailwind:\n%s", welcomeHTML)
	}
	if strings.Contains(welcomeHTML, "cdn.tailwindcss.com") || !strings.Contains(welcomeHTML, "jenderal-landing.css") {
		t.Fatalf("welcome page must use the local stylesheet:\n%s", welcomeHTML)
	}
	if css := written["/var/www/html/jenderal-landing.css"]; !strings.Contains(css, "tailwindcss") {
		t.Fatalf("compiled Tailwind stylesheet was not installed: %q", css)
	}
}

func TestTestConfig_Valid(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("",
				"nginx: the configuration file /etc/nginx/nginx.conf syntax is ok\nnginx: configuration file /etc/nginx/nginx.conf test is successful\n",
				0), nil
		},
	}

	svc := NewService(mock, nil)
	valid, output, err := svc.TestConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected config to be valid")
	}
	if !strings.Contains(output, "successful") {
		t.Errorf("expected output to contain 'successful', got %q", output)
	}
}

func TestTestConfig_Invalid(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("",
				"nginx: [emerg] unknown directive \"invalid\" in /etc/nginx/nginx.conf:1\nnginx: configuration file /etc/nginx/nginx.conf test failed\n",
				1), nil
		},
	}

	svc := NewService(mock, nil)
	valid, output, err := svc.TestConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected config to be invalid")
	}
	if !strings.Contains(output, "failed") {
		t.Errorf("expected output to contain 'failed', got %q", output)
	}
}

func TestListSites(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// Distinguish between the two ls calls by the path argument.
			if len(args) > 0 && strings.Contains(args[0], "sites-available") {
				return mockResult("default\nexample.com\ntest.com\n", "", 0), nil
			}
			if len(args) > 0 && strings.Contains(args[0], "sites-enabled") {
				return mockResult("default\nexample.com\n", "", 0), nil
			}
			return mockResult("", "", 0), nil
		},
	}

	svc := NewService(mock, nil)
	sites, err := svc.ListSites(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sites) != 3 {
		t.Fatalf("expected 3 sites, got %d", len(sites))
	}

	// Build a map for easier assertion.
	siteMap := make(map[string]bool)
	for _, s := range sites {
		siteMap[s.Name] = s.Enabled
	}

	if !siteMap["default"] {
		t.Error("expected 'default' to be enabled")
	}
	if !siteMap["example.com"] {
		t.Error("expected 'example.com' to be enabled")
	}
	if siteMap["test.com"] {
		t.Error("expected 'test.com' to be disabled")
	}
}

func TestFixPortConflictKillsOrphanedNginx(t *testing.T) {
	portsHeld := true
	var kills []string
	started := false
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "ss":
				if !portsHeld {
					return mockResult("", "", 0), nil
				}
				out := "LISTEN 0    511          0.0.0.0:80        0.0.0.0:*    users:((\"nginx\",pid=1200,fd=6))\n" +
					"LISTEN 0    511             [::]:80           [::]:*    users:((\"nginx\",pid=1200,fd=7))\n" +
					"LISTEN 0    128          0.0.0.0:8080      0.0.0.0:*    users:((\"java\",pid=99,fd=50))\n"
				return mockResult(out, "", 0), nil
			case "kill":
				kills = append(kills, args...)
				portsHeld = false
				return mockResult("", "", 0), nil
			case "systemctl":
				if len(args) == 2 && args[0] == "start" {
					started = true
				}
				return mockResult("", "", 0), nil
			default:
				return mockResult("", "", 0), nil
			}
		},
	}

	report, err := NewService(mock, nil).FixPortConflict(context.Background())
	if err != nil {
		t.Fatalf("FixPortConflict() error = %v", err)
	}
	if report.Outcome != "fixed" {
		t.Errorf("outcome = %q, want fixed", report.Outcome)
	}
	if len(report.Killed) != 1 || report.Killed[0] != 1200 {
		t.Errorf("killed = %v, want [1200]", report.Killed)
	}
	foundTERM := false
	for i, a := range kills {
		if a == "-TERM" && i+1 < len(kills) && kills[i+1] == "1200" {
			foundTERM = true
		}
	}
	if !foundTERM {
		t.Errorf("expected a graceful kill -TERM 1200, got %v", kills)
	}
	if !started {
		t.Error("nginx must be started after clearing the port holders")
	}
	if len(report.Holders) != 0 {
		t.Errorf("holders = %v, want empty", report.Holders)
	}
}

func TestFixPortConflictStopsAndDisablesApache(t *testing.T) {
	apacheRunning := true
	var systemctlCalls [][2]string
	started := false
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "ss":
				if !apacheRunning {
					return mockResult("", "", 0), nil
				}
				out := "LISTEN 0    511          0.0.0.0:80        0.0.0.0:*    users:((\"apache2\",pid=77,fd=4))\n" +
					"LISTEN 0    511             [::]:80           [::]:*    users:((\"apache2\",pid=77,fd=5))\n"
				return mockResult(out, "", 0), nil
			case "systemctl":
				if len(args) == 2 {
					systemctlCalls = append(systemctlCalls, [2]string{args[0], args[1]})
					if args[0] == "stop" && args[1] == "apache2" {
						apacheRunning = false
					}
					if args[0] == "start" && args[1] == "nginx" {
						started = true
					}
				}
				return mockResult("", "", 0), nil
			case "kill":
				t.Errorf("apache must be stopped via systemd, not killed: kill %v", args)
				return mockResult("", "", 0), nil
			default:
				return mockResult("", "", 0), nil
			}
		},
	}

	report, err := NewService(mock, nil).FixPortConflict(context.Background())
	if err != nil {
		t.Fatalf("FixPortConflict() error = %v", err)
	}
	if report.Outcome != "fixed" {
		t.Errorf("outcome = %q, want fixed", report.Outcome)
	}
	if len(report.Stopped) != 1 || report.Stopped[0] != "apache2" {
		t.Errorf("stopped = %v, want [apache2]", report.Stopped)
	}
	if len(report.Killed) != 0 {
		t.Errorf("killed = %v, want empty", report.Killed)
	}
	for _, want := range [][2]string{{"stop", "apache2"}, {"disable", "apache2"}} {
		found := false
		for _, call := range systemctlCalls {
			if call == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing systemctl %s %s", want[0], want[1])
		}
	}
	if !started {
		t.Error("nginx must be started after freeing the ports")
	}
}

func TestFixPortConflictReportsForeignProcessWithoutKilling(t *testing.T) {
	out := "LISTEN 0    511          0.0.0.0:80        0.0.0.0:*    users:((\"caddy\",pid=77,fd=4))\n"
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "kill" {
				t.Errorf("must not kill a foreign process, got kill %v", args)
			}
			if name == "systemctl" && args[0] != "start" {
				t.Errorf("must not stop a foreign process via systemd, got systemctl %v", args)
			}
			if name == "ss" {
				return mockResult(out, "", 0), nil
			}
			return mockResult("", "", 0), nil
		},
	}

	report, err := NewService(mock, nil).FixPortConflict(context.Background())
	if err != nil {
		t.Fatalf("FixPortConflict() error = %v", err)
	}
	if report.Outcome != "foreign_process" {
		t.Errorf("outcome = %q, want foreign_process", report.Outcome)
	}
	if len(report.Killed) != 0 {
		t.Errorf("killed = %v, want empty", report.Killed)
	}
	if len(report.Stopped) != 0 {
		t.Errorf("stopped = %v, want empty", report.Stopped)
	}
	if len(report.Holders) != 1 || report.Holders[0].Name != "caddy" || report.Holders[0].PID != 77 || report.Holders[0].Port != 80 {
		t.Errorf("holders = %v, want caddy pid 77 on port 80", report.Holders)
	}
}

func TestFixPortConflictWithoutConflictStartsNginx(t *testing.T) {
	started := false
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "kill" {
				t.Errorf("nothing holds the ports; must not kill, got kill %v", args)
			}
			if name == "ss" {
				return mockResult("", "", 0), nil
			}
			if name == "systemctl" && len(args) == 2 && args[0] == "start" {
				started = true
			}
			return mockResult("", "", 0), nil
		},
	}

	report, err := NewService(mock, nil).FixPortConflict(context.Background())
	if err != nil {
		t.Fatalf("FixPortConflict() error = %v", err)
	}
	if report.Outcome != "no_conflict" {
		t.Errorf("outcome = %q, want no_conflict", report.Outcome)
	}
	if !started {
		t.Error("nginx must still be started when the ports are free")
	}
}

func TestWebPortHoldersParsesSSOutput(t *testing.T) {
	out := "LISTEN 0    511          0.0.0.0:80        0.0.0.0:*    users:((\"nginx\",pid=1200,fd=6))\n" +
		"LISTEN 0    511       [::]:443           [::]:*    users:((\"nginx\",pid=1200,fd=7),(\"nginx\",pid=1201,fd=7))\n" +
		"LISTEN 0    4096     127.0.0.1:8443        0.0.0.0:*    users:((\"jenderal\",pid=500,fd=3))\n" +
		"LISTEN 0    128       0.0.0.0:22          0.0.0.0:*\n"
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult(out, "", 0), nil
		},
	}
	holders, err := NewService(mock, nil).webPortHolders(context.Background())
	if err != nil {
		t.Fatalf("webPortHolders() error = %v", err)
	}
	want := []PortHolder{
		{PID: 1200, Name: "nginx", Port: 80},
		{PID: 1200, Name: "nginx", Port: 443},
		{PID: 1201, Name: "nginx", Port: 443},
	}
	if len(holders) != len(want) {
		t.Fatalf("holders = %v, want %v", holders, want)
	}
	for i, h := range holders {
		if h != want[i] {
			t.Errorf("holders[%d] = %+v, want %+v", i, h, want[i])
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"nginx version: nginx/1.24.0\n", "1.24.0"},
		{"nginx version: nginx/1.18.0 (Ubuntu)\n", "1.18.0"},
		{"some garbage", ""},
		{"", ""},
	}

	for _, tc := range tests {
		got := parseVersion(tc.input)
		if got != tc.expected {
			t.Errorf("parseVersion(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestValidateSiteName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"default", false},
		{"example.com", false},
		{"my-site_01", false},
		{"", true},
		{"../etc/passwd", true},
		{"foo/bar", true},
		{"..", true},
	}

	for _, tc := range tests {
		err := validateSiteName(tc.name)
		if tc.wantErr && err == nil {
			t.Errorf("validateSiteName(%q): expected error, got nil", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("validateSiteName(%q): unexpected error: %v", tc.name, err)
		}
	}
}

func TestSplitLines(t *testing.T) {
	input := "foo\nbar\n  baz  \n\n"
	lines := splitLines(input)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "foo" || lines[1] != "bar" || lines[2] != "baz" {
		t.Errorf("unexpected lines: %v", lines)
	}
}
