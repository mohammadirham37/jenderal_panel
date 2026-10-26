package trafficguard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type memoryManagedFiles struct{ values map[string]string }

func (f *memoryManagedFiles) Read(_ context.Context, path string) (string, bool, error) {
	v, ok := f.values[path]
	return v, ok, nil
}
func (f *memoryManagedFiles) Write(_ context.Context, path, value string) error {
	f.values[path] = value
	return nil
}
func (f *memoryManagedFiles) Remove(_ context.Context, path string) error {
	delete(f.values, path)
	return nil
}

func TestApplyWebsiteRollsBackSnippetWhenNginxReloadFails(t *testing.T) {
	files := &memoryManagedFiles{values: map[string]string{"/etc/nginx/jenderal/security/sites/01SITE.conf": "previous"}}
	reloads := 0
	exec := &executor.MockExecutor{
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "systemctl" && len(args) > 0 && args[0] == "reload" {
				reloads++
				if reloads == 2 {
					return &executor.Result{ExitCode: 1}, nil
				}
			}
			return &executor.Result{}, nil
		},
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return nil, errors.New("unexpected command")
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			return nil, errors.New("unexpected command")
		},
	}
	m := NewNginxManager(exec, files, nil)
	m.now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }
	p := DefaultProfile("01SITE", m.now())
	if err := m.ApplyWebsite(context.Background(), model.Website{ID: "01SITE"}, p, false); err == nil {
		t.Fatal("expected reload error")
	}
	if got := files.values["/etc/nginx/jenderal/security/sites/01SITE.conf"]; got != "previous" {
		t.Fatalf("snippet=%q", got)
	}
}

func TestCustomProfileUsesBoundedDedicatedRateZone(t *testing.T) {
	p := DefaultProfile("01SITE", time.Now())
	p.Mode, p.RequestsPerSecond, p.Burst, p.Connections = "custom", 37, 55, 42
	got := renderTrafficSnippet(p, "")
	for _, want := range []string{"zone=jenderal_custom_01SITE", "burst=55", "jenderal_connections 42", "limit_req_status 429"} {
		if !strings.Contains(got, want) {
			t.Fatalf("snippet missing %q:\n%s", want, got)
		}
	}
}

// Applying a Traffic Guard profile must not erase the WAF/DoS directives
// the website's include already carries.
func TestApplyWebsiteKeepsWAFProtection(t *testing.T) {
	include := "/etc/nginx/jenderal/security/sites/01SITE.conf"
	files := &memoryManagedFiles{values: map[string]string{
		include: "# Managed by Jenderal Panel - per-site protection\nmodsecurity on;\nlimit_req_status 403;\nlimit_req zone=jenderal_dos burst=20 nodelay;\n",
	}}
	exec := &executor.MockExecutor{
		RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
	}
	m := NewNginxManager(exec, files, nil)
	m.now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }
	snippet := renderTrafficSnippet(DefaultProfile("01SITE", m.now()), "")
	if err := m.applySiteFragment(context.Background(), "01SITE", snippet); err != nil {
		t.Fatal(err)
	}
	got := files.values[include]
	for _, want := range []string{"modsecurity on;", "zone=jenderal_dos"} {
		if !strings.Contains(got, want) {
			t.Errorf("Traffic Guard erased %q:\n%s", want, got)
		}
	}
}
