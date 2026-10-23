package nodejs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
)

func TestAnnotateSecurityFlagsOutdatedAndEOLRuntimes(t *testing.T) {
	cases := []struct {
		selected, installed string
		vulnerable, eol     bool
		baseline            string
	}{
		{"22", "v22.20.0", true, false, "22.23.4"},
		{"22", "v22.23.4", false, false, "22.23.4"},
		{"24", "v24.9.0", true, false, "24.22.1"},
		{"24", "v24.30.0", false, false, "24.22.1"},
		{"20", "v20.19.5", false, true, ""},
	}
	for _, tc := range cases {
		r := RuntimeInfo{SelectedVersion: tc.selected, Installed: true, InstalledVersion: tc.installed}
		annotateSecurity(&r)
		if r.Vulnerable != tc.vulnerable || r.EndOfLife != tc.eol || r.SecurityBaseline != tc.baseline {
			t.Errorf("%s/%s => vulnerable=%v eol=%v baseline=%q", tc.selected, tc.installed, r.Vulnerable, r.EndOfLife, r.SecurityBaseline)
		}
		if r.Vulnerable && r.AdvisoryURL == "" {
			t.Errorf("%s: vulnerable runtime needs the advisory link", tc.installed)
		}
	}
	missing := RuntimeInfo{SelectedVersion: "22"}
	annotateSecurity(&missing)
	if missing.Vulnerable {
		t.Error("a runtime that is not installed is not reported vulnerable")
	}
}

func TestUpdateRuntimeReinstallsMajorAndRestartsWebsiteApp(t *testing.T) {
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	insertTestWebsite(t, db, "site-a", "a.example.com", "web_a", "/home/web_a/public")
	var sudo []string
	mock := newMockExec()
	base := mock.RunSudoFunc
	mock.RunSudoFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		sudo = append(sudo, name+" "+strings.Join(args, " "))
		return base(ctx, name, args...)
	}
	svc := NewService(db, mock, nil)
	var installed []string
	svc.runtime = fakeRuntime{
		detect: func(_ context.Context, _ string, v string) (noderuntime.Status, error) {
			return noderuntime.Status{Installed: true, NodeVersion: "v" + v + ".22.1", NPMVersion: "11.0.0", NVMState: noderuntime.NVMStateReady}, nil
		},
		install: func(_ context.Context, user, v string, _ func(string)) error {
			installed = append(installed, user+"@"+v)
			return nil
		},
		installPanel: func(context.Context, func(string)) error { return nil },
	}
	if err := svc.UpdateRuntime(context.Background(), "site-a", func(string) {}); err != nil {
		t.Fatalf("UpdateRuntime: %v", err)
	}
	if strings.Join(installed, ",") != "web_a@24" {
		t.Errorf("must reinstall the selected major to pull the latest patch, got %v", installed)
	}
	restarted := false
	for _, c := range sudo {
		if c == "systemctl try-restart jenderal-app-site-a.service" {
			restarted = true
		}
	}
	if !restarted {
		t.Errorf("website app unit not restarted on the new Node; sudo calls: %v", sudo)
	}
}

func TestUpdateAllRuntimesSkipsSitesWithoutNodeAndReportsFailures(t *testing.T) {
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	insertTestWebsite(t, db, "site-a", "a.example.com", "web_a", "/home/web_a/public")
	insertTestWebsite(t, db, "site-b", "b.example.com", "web_b", "/home/web_b/public")
	insertTestWebsite(t, db, "site-none", "none.example.com", "web_none", "/home/web_none/public")
	if _, err := db.Exec(`UPDATE websites SET node_version = '' WHERE id = 'site-none'`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, newMockExec(), nil)
	var installed []string
	svc.runtime = fakeRuntime{
		detect: func(_ context.Context, _ string, v string) (noderuntime.Status, error) {
			return noderuntime.Status{Installed: true, NodeVersion: "v" + v + ".22.1", NPMVersion: "11.0.0", NVMState: noderuntime.NVMStateReady}, nil
		},
		install: func(_ context.Context, user, v string, _ func(string)) error {
			installed = append(installed, user)
			if user == "web_b" {
				return context.DeadlineExceeded
			}
			return nil
		},
		installPanel: func(context.Context, func(string)) error { return nil },
	}
	err := svc.UpdateAllRuntimes(context.Background(), func(string) {})
	if strings.Join(installed, ",") != "web_a,web_b" {
		t.Errorf("updated users = %v, want web_a,web_b (site without Node skipped)", installed)
	}
	if err == nil || !strings.Contains(err.Error(), "b.example.com") {
		t.Errorf("err = %v, want the failed site named", err)
	}
}

func TestChangeRuntimeRefreshesWebsiteAppUnit(t *testing.T) {
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	insertTestWebsite(t, db, "site-a", "a.example.com", "web_a", "/home/web_a/public")
	svc := NewService(db, newMockExec(), nil)
	svc.runtime = installedRuntime("22")
	var refreshed []string
	svc.SetWebsiteAppRefresher(func(_ context.Context, websiteID string) error {
		var version string
		if err := db.QueryRow(`SELECT node_version FROM websites WHERE id = ?`, websiteID).Scan(&version); err != nil {
			return err
		}
		refreshed = append(refreshed, websiteID+"@"+version)
		return nil
	})
	if err := svc.ChangeRuntime(context.Background(), "site-a", "22", func(string) {}); err != nil {
		t.Fatalf("ChangeRuntime: %v", err)
	}
	// The refresher must run after the new version is committed so the
	// rewritten unit pins NODE_VERSION=22, not the old 24.
	if strings.Join(refreshed, ",") != "site-a@22" {
		t.Errorf("website app refresh = %v, want site-a@22", refreshed)
	}
}

// Updating used to leave the replaced patch installed next to the new one;
// a successful update now removes it, and a failed cleanup does not fail
// the update.
func TestUpdateRuntimePrunesReplacedVersions(t *testing.T) {
	for _, pruneErr := range []error{nil, errors.New("disk busy")} {
		db := setupTestDB(t)
		db.SetMaxOpenConns(1)
		insertTestWebsite(t, db, "site-a", "a.example.com", "web_a", "/home/web_a/public")
		svc := NewService(db, newMockExec(), nil)
		var pruned []string
		svc.runtime = fakeRuntime{
			detect: func(_ context.Context, _ string, v string) (noderuntime.Status, error) {
				return noderuntime.Status{Installed: true, NodeVersion: "v" + v + ".22.1", NPMVersion: "11.0.0", NVMState: noderuntime.NVMStateReady}, nil
			},
			install:      func(context.Context, string, string, func(string)) error { return nil },
			installPanel: func(context.Context, func(string)) error { return nil },
			prune: func(_ context.Context, user, v string, _ func(string)) (noderuntime.PruneResult, error) {
				pruned = append(pruned, user+"@"+v)
				return noderuntime.PruneResult{Kept: "v24.22.1"}, pruneErr
			},
		}
		if err := svc.UpdateRuntime(context.Background(), "site-a", func(string) {}); err != nil {
			t.Fatalf("UpdateRuntime (prune error %v): %v", pruneErr, err)
		}
		if strings.Join(pruned, ",") != "web_a@24" {
			t.Errorf("pruned = %v, want the website's selected major", pruned)
		}
	}
}
