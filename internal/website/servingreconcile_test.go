package website

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// A build run with umask 027 (or under a default ACL whose other entry is
// ---) leaves public/build 0750/0640: nginx cannot read it. The reconciler
// must notice and re-apply serving access; a clean tree costs one find.
func TestReconcileServingAccessRepairsUnreadableBuildOutput(t *testing.T) {
	for _, tc := range []struct {
		name       string
		unreadable string
		wantRepair bool
	}{
		{"umask 027 build output", "/home/web_example_com/public/build/assets/app.css\n", true},
		{"servable tree", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()
			insertTestWebsite(t, db, "ws-1", "example.com", "laravel", "8.4", "active")
			insertTestWebsite(t, db, "ws-2", "pending.com", "php", "8.4", "pending")

			var finds []string
			repaired := false
			mock := &executor.MockExecutor{
				RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
					return &executor.Result{ExitCode: 0}, nil
				},
				RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
					call := name + " " + strings.Join(args, " ")
					switch {
					case name == "find":
						finds = append(finds, call)
						return &executor.Result{Stdout: tc.unreadable}, nil
					case strings.HasPrefix(call, "chmod -R o+rX -- /home/web_example_com/public"):
						repaired = true
					}
					return &executor.Result{ExitCode: 0}, nil
				},
			}
			NewProvisioner(db, mock, nil).ReconcileServingAccess(context.Background())

			if len(finds) != 1 {
				t.Fatalf("finds = %v, want one for the active site", finds)
			}
			want := "find /home/web_example_com/public -xdev ( ( -type d ! -perm -o=rx ) -o ( -type f ! -perm -o=r ) ) -print -quit"
			if finds[0] != want {
				t.Errorf("find = %q\nwant  %q", finds[0], want)
			}
			if repaired != tc.wantRepair {
				t.Errorf("repaired = %v, want %v", repaired, tc.wantRepair)
			}
		})
	}
}

func TestUnservablePathIgnoresRootsOutsideTheSiteHome(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		},
	}
	w := websiteRow{WebUser: "web_example_com", Domain: "example.com", DocumentRoot: "/var/www/html"}
	if got := NewProvisioner(nil, mock, nil).unservablePath(context.Background(), w); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestPanelCommandsKeepTheDocumentRootServable(t *testing.T) {
	w := model.Website{WebUser: "web_example_com", DocumentRoot: "/home/web_example_com/app/public", AppType: "laravel"}
	got := keepDocumentRootServable(w, "cd /home/web_example_com/app && npm run build")
	want := "{ cd /home/web_example_com/app && npm run build; }; rc=$?; chmod -R o+rX -- '/home/web_example_com/app/public' 2>/dev/null; exit $rc"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	w.AppType = "go"
	if got := keepDocumentRootServable(w, "go build"); got != "go build" {
		t.Errorf("go sites have no static document root: %q", got)
	}
	w.AppType = "php"
	w.DocumentRoot = "/var/www/shared"
	if got := keepDocumentRootServable(w, "x"); got != "x" {
		t.Errorf("roots outside the site home are left alone: %q", got)
	}
}

func TestLaravelProfileServesBuildAssetsStatically(t *testing.T) {
	out, err := RenderVhost(VhostData{
		Domain: "example.com", DocumentRoot: "/home/web_example_com/app/public",
		LogDir: "/home/web_example_com/logs", PHPVersion: "8.4", AppType: "laravel",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "location ^~ /build/ {\n        try_files $uri =404;\n    }") {
		t.Errorf("laravel vhost must not route /build/ assets to index.php:\n%s", out)
	}
}
