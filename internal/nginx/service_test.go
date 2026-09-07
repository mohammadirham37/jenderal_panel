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
