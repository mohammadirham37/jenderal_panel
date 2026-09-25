package website

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestParseNginxUserDirective(t *testing.T) {
	cases := []struct {
		name, conf, want string
	}{
		{"ubuntu default", "user www-data;\nworker_processes auto;\n", "www-data"},
		{"custom build", "# user www-data;\nuser nginx nginx;\n", "nginx"},
		{"indented", "events {\n}\n  user www-data;\n", "www-data"},
		{"missing falls back", "worker_processes auto;\n", defaultNginxUser},
		{"comment only falls back", "#user nobody;\n", defaultNginxUser},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseNginxUserDirective(tc.conf); got != tc.want {
				t.Fatalf("parseNginxUserDirective(%q) = %q, want %q", tc.conf, got, tc.want)
			}
		})
	}
}

// nginxACLEntries renders the recorded sudo commands for assertions.
func nginxACLEntries(calls [][]string) []string {
	entries := make([]string, 0, len(calls))
	for _, call := range calls {
		entries = append(entries, strings.Join(call, " "))
	}
	return entries
}

func TestGrantNginxACLsLaravelLayout(t *testing.T) {
	var calls [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil // setfacl --version present
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	w := websiteRow{
		WebUser:      "web_example_com",
		Domain:       "example.com",
		DocumentRoot: "/home/web_example_com/app/public",
	}
	if err := NewProvisioner(nil, mock, nil).grantNginxACLs(context.Background(), w); err != nil {
		t.Fatal(err)
	}

	entries := strings.Join(nginxACLEntries(calls), "\n")
	for _, want := range []string{
		// Traverse only on the boundaries and storage parents.
		"setfacl -m u:www-data:x -- /home/web_example_com",
		"setfacl -m u:www-data:x -- /home/web_example_com/app",
		"setfacl -m u:www-data:x -- /home/web_example_com/app/storage",
		"setfacl -m u:www-data:x -- /home/web_example_com/app/storage/app",
		// Read on the document root and public storage, current and default.
		"setfacl -R -m u:www-data:rX -- /home/web_example_com/app/public",
		"setfacl -R -d -m u:www-data:rX -- /home/web_example_com/app/public",
		"setfacl -R -m u:www-data:rX -- /home/web_example_com/app/storage/app/public",
		"setfacl -R -d -m u:www-data:rX -- /home/web_example_com/app/storage/app/public",
	} {
		if !strings.Contains(entries, want) {
			t.Fatalf("missing ACL grant %q in:\n%s", want, entries)
		}
	}
	// The project root and .env must stay unreadable for the worker account:
	// no read grants may target the boundaries themselves.
	for _, entry := range nginxACLEntries(calls) {
		if strings.Contains(entry, "rX") && strings.HasPrefix(strings.TrimPrefix(entry, "setfacl "), "-R") {
			target := entry[strings.LastIndex(entry, "-- ")+3:]
			if target == "/home/web_example_com" || target == "/home/web_example_com/app" ||
				target == "/home/web_example_com/app/storage" || target == "/home/web_example_com/app/storage/app" {
				t.Fatalf("worker account got read access to private boundary %s", target)
			}
		}
	}
}

func TestGrantNginxACLsUsesConfiguredWorkerUser(t *testing.T) {
	var calls [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			if name == "cat" && len(args) == 1 && args[0] == "/etc/nginx/nginx.conf" {
				return &executor.Result{Stdout: "user nginx;\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	w := websiteRow{
		WebUser:      "web_example_com",
		Domain:       "example.com",
		DocumentRoot: "/home/web_example_com/public",
	}
	if err := NewProvisioner(nil, mock, nil).grantNginxACLs(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	entries := strings.Join(nginxACLEntries(calls), "\n")
	if !strings.Contains(entries, "setfacl -R -m u:nginx:rX -- /home/web_example_com/public") {
		t.Fatalf("ACL grants did not use the nginx.conf worker account:\n%s", entries)
	}
	if strings.Contains(entries, "www-data") {
		t.Fatalf("fallback account used despite nginx.conf user directive:\n%s", entries)
	}
}

func TestGrantNginxACLsCustomDocumentRootUntouched(t *testing.T) {
	var calls [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	w := websiteRow{
		WebUser:      "web_example_com",
		Domain:       "example.com",
		DocumentRoot: "/home/web_example_com/web/current",
	}
	if err := NewProvisioner(nil, mock, nil).grantNginxACLs(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	for _, entry := range nginxACLEntries(calls) {
		if strings.HasPrefix(entry, "setfacl -m") || strings.HasPrefix(entry, "setfacl -R") || strings.HasPrefix(entry, "chmod") {
			t.Fatalf("custom document root was modified: %s", entry)
		}
	}
}

func TestGrantNginxACLsChmodFallbackWithoutSetfacl(t *testing.T) {
	var calls [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// setfacl missing.
			return &executor.Result{ExitCode: 1}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	w := websiteRow{
		WebUser:      "web_example_com",
		Domain:       "example.com",
		DocumentRoot: "/home/web_example_com/app/public",
	}
	if err := NewProvisioner(nil, mock, nil).grantNginxACLs(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	entries := strings.Join(nginxACLEntries(calls), "\n")
	for _, want := range []string{
		"chmod o+x -- /home/web_example_com",
		"chmod o+x -- /home/web_example_com/app",
		"chmod o+x -- /home/web_example_com/app/storage",
		"chmod -R o+rX -- /home/web_example_com/app/public",
		"chmod -R o+rX -- /home/web_example_com/app/storage/app/public",
	} {
		if !strings.Contains(entries, want) {
			t.Fatalf("missing chmod fallback %q in:\n%s", want, entries)
		}
	}
	if strings.Contains(entries, "setfacl -m") {
		t.Fatalf("setfacl used although unavailable:\n%s", entries)
	}
}
