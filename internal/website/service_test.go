package website

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestOptionsReportsInstalledRuntimesAndDependencies(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		switch name {
		case "test":
			path := args[len(args)-1]
			if path == "/etc/php/8.2" || path == "/etc/php/8.4" {
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		case "composer":
			return &executor.Result{ExitCode: 0, Stdout: "Composer version 2.10.3 2026-04-20"}, nil
		case "node":
			return &executor.Result{ExitCode: 0, Stdout: "v20.19.0\n"}, nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return nil, nil
		}
	}}

	options, err := NewService(db, mock, nil).Options(context.Background())
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}
	var installed []string
	for _, option := range options.PHPVersions {
		if option.Installed {
			installed = append(installed, option.Version)
		}
	}
	if strings.Join(installed, ",") != "8.2,8.4" {
		t.Fatalf("installed PHP versions = %v, want [8.2 8.4]", installed)
	}
	if len(options.Dependencies) != 2 || options.Dependencies[0].Version != "2.10.3" || options.Dependencies[1].Version != "20.19.0" {
		t.Fatalf("dependencies = %#v", options.Dependencies)
	}
	if len(options.Profiles) == 0 {
		t.Fatal("profile catalog is empty")
	}
}

func TestCreateRejectsMissingPHPBeforeInsert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 1}, nil
	}}

	_, err := NewService(db, mock, nil).Create(context.Background(), CreateRequest{
		Domain: "missing-php.example.com", Template: "laravel", FrameworkVersion: "12", PHPVersion: "8.2",
	})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Create() error = %v, want missing PHP validation", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM websites`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("website rows = %d, want 0", count)
	}
}

func TestCreatePersistsDerivedProfileAndCanonicalRoot(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 0}, nil
	}}
	svc := NewService(db, mock, nil)
	created, err := svc.Create(context.Background(), CreateRequest{
		Domain: "ci.example.com", Template: "codeigniter4", PHPVersion: "8.3", SetupMode: "config-only",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Framework != "codeigniter" || created.FrameworkVersion != "4" || created.DocumentRoot != "/home/web_ci_example_com/app/public" {
		t.Fatalf("created website = %#v", created)
	}
	loaded, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Framework != created.Framework || loaded.SetupMode != "config-only" {
		t.Fatalf("loaded profile = %#v", loaded)
	}
}

func TestListCompletesWithSingleSQLiteConnection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	insertTestWebsite(t, db, "ws-001", "example.com", "php", "8.3", "active")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	websites, err := NewService(db, nil, nil).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(websites) != 1 {
		t.Fatalf("List() returned %d websites, want 1", len(websites))
	}
	if websites[0].Domain != "example.com" {
		t.Errorf("website domain = %q, want %q", websites[0].Domain, "example.com")
	}
	if len(websites[0].Domains) != 1 {
		t.Fatalf("website has %d domains, want 1", len(websites[0].Domains))
	}
	if websites[0].Domains[0].Name != "example.com" {
		t.Errorf("related domain = %q, want %q", websites[0].Domains[0].Name, "example.com")
	}
}

func TestAddDomainPreservesActiveSSLRedirects(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-ssl", "example.com", "static", "", "active")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES ('cert-primary', 'ws-ssl', 'example.com', 'letsencrypt', 'active', 1, ?, ?)`, now, now,
	); err != nil {
		t.Fatal(err)
	}

	var rendered string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "cp" && len(args) == 2 {
				content, err := os.ReadFile(args[0])
				if err != nil {
					t.Fatal(err)
				}
				rendered = string(content)
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil)
	svc.ipv6Available = func() bool { return false }
	if err := svc.AddDomain(context.Background(), "ws-ssl", "www.example.com", "alias"); err != nil {
		t.Fatalf("AddDomain() error = %v", err)
	}
	for _, expected := range []string{"server_name example.com;", "return 301 https://$host$request_uri;", "server_name www.example.com;"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("regenerated config missing %q:\n%s", expected, rendered)
		}
	}
}

func TestRemoveDomainRejectsDomainWithCertificate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-remove-ssl", "example.com", "static", "", "active")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO domains (id, website_id, name, type, created_at) VALUES ('alias-ssl', 'ws-remove-ssl', 'www.example.com', 'alias', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES ('cert-alias', 'ws-remove-ssl', 'www.example.com', 'custom', 'active', 0, ?, ?)`, now, now,
	); err != nil {
		t.Fatal(err)
	}

	svc := NewService(db, &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}, nil)
	if err := svc.RemoveDomain(context.Background(), "ws-remove-ssl", "alias-ssl"); err == nil {
		t.Fatal("RemoveDomain() removed a domain that still has an SSL certificate")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE id = 'alias-ssl'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("domain count = %d, want 1", count)
	}
}

func TestDeleteRemovesPerDomainSSLArtifacts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-delete-ssl", "example.com", "static", "", "active")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO domains (id, website_id, name, type, created_at) VALUES ('alias-delete', 'ws-delete-ssl', 'www.example.com', 'alias', ?)`, now); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, domain string }{{"cert-primary-delete", "example.com"}, {"cert-alias-delete", "www.example.com"}} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-delete-ssl', ?, 'custom', 'active', 0, ?, ?)`, item.id, item.domain, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}

	removed := make(map[string]bool)
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "rm" && len(args) >= 2 {
				removed[args[len(args)-1]] = true
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	if err := NewService(db, mock, nil).Delete(context.Background(), "ws-delete-ssl", false); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	for _, domain := range []string{"example.com", "www.example.com"} {
		for _, path := range []string{
			"/etc/nginx/sites-available/" + domain + ".ssl",
			"/etc/nginx/sites-enabled/" + domain + ".ssl",
			"/etc/nginx/sites-enabled/" + domain + ".ssl.suspended",
			"/etc/jenderal/ssl/" + domain,
		} {
			if !removed[path] {
				t.Errorf("SSL artifact was not removed: %s", path)
			}
		}
	}
	if !removed["/etc/nginx/sites-enabled/example.com.suspended"] {
		t.Error("suspended primary HTTP symlink was not removed")
	}
}

func TestDeleteIgnoresUnregisteredHistoricalCertificateDomain(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-delete-unsafe", "example.com", "static", "", "active")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES ('cert-unsafe', 'ws-delete-unsafe', '../../tmp/owned', 'custom', 'active', 0, ?, ?)`, now, now,
	); err != nil {
		t.Fatal(err)
	}

	var removed []string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "rm" {
				removed = append(removed, args[len(args)-1])
			}
			if name == "id" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	if err := NewService(db, mock, nil).Delete(context.Background(), "ws-delete-unsafe", false); err == nil {
		t.Fatal("Delete() error = nil, want unregistered certificate rejection")
	}
	for _, path := range removed {
		if strings.Contains(path, "tmp/owned") {
			t.Fatalf("Delete() used unsafe historical certificate path %q", path)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM websites WHERE id = 'ws-delete-unsafe'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("website rows = %d, want retained recovery metadata", count)
	}
}

func TestDeleteRetainsDatabaseStateWhenSystemCleanupFails(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-delete-fail", "example.com", "static", "", "active")
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "rm" && args[len(args)-1] == "/etc/nginx/sites-available/example.com" {
				return &executor.Result{ExitCode: 1, Stderr: "permission denied"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	if err := NewService(db, mock, nil).Delete(context.Background(), "ws-delete-fail", false); err == nil {
		t.Fatal("Delete() error = nil, want cleanup failure")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM websites WHERE id = 'ws-delete-fail'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("website rows = %d, want retained metadata", count)
	}
}

func TestDeleteRejectsStoredSystemUserOutsideWebsiteOwnership(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-delete-root", "example.com", "static", "", "active")
	if _, err := db.Exec(`UPDATE websites SET web_user = 'root' WHERE id = 'ws-delete-root'`); err != nil {
		t.Fatal(err)
	}
	var sudoCalls int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudoCalls++
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	if err := NewService(db, mock, nil).Delete(context.Background(), "ws-delete-root", true); err == nil {
		t.Fatal("Delete() error = nil, want foreign system user rejection")
	}
	if sudoCalls != 0 {
		t.Fatalf("system calls = %d, want 0", sudoCalls)
	}
}

func TestSuspendAndEnableTogglePerDomainTLSVhosts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-suspend-ssl", "example.com", "static", "", "active")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range []struct{ id, domain string }{{"cert-suspend-primary", "example.com"}, {"cert-suspend-alias", "www.example.com"}} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-suspend-ssl', ?, 'custom', 'active', 0, ?, ?)`, item.id, item.domain, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}

	var moves [][2]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "mv" && len(args) == 2 {
				moves = append(moves, [2]string{args[0], args[1]})
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil)
	if err := svc.Suspend(context.Background(), "ws-suspend-ssl"); err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	if err := svc.Enable(context.Background(), "ws-suspend-ssl"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	want := map[[2]string]bool{}
	for _, path := range []string{
		"/etc/nginx/sites-enabled/example.com",
		"/etc/nginx/sites-enabled/example.com.ssl",
		"/etc/nginx/sites-enabled/www.example.com.ssl",
	} {
		want[[2]string{path, path + ".suspended"}] = true
		want[[2]string{path + ".suspended", path}] = true
	}
	for move := range want {
		if !containsMove(moves, move) {
			t.Errorf("missing move %q -> %q; moves = %v", move[0], move[1], moves)
		}
	}
}

func TestSuspendMovesDuplicateCertificateDomainOnce(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-suspend-duplicate", "example.com", "static", "", "active")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"cert-duplicate-a", "cert-duplicate-b"} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-suspend-duplicate', 'example.com', 'custom', 'active', 0, ?, ?)`, id, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}

	var tlsMoves int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "mv" && len(args) == 2 && args[0] == "/etc/nginx/sites-enabled/example.com.ssl" {
				tlsMoves++
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	if err := NewService(db, mock, nil).Suspend(context.Background(), "ws-suspend-duplicate"); err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	if tlsMoves != 1 {
		t.Fatalf("TLS config moves = %d, want 1", tlsMoves)
	}
}

func containsMove(moves [][2]string, want [2]string) bool {
	for _, move := range moves {
		if move == want {
			return true
		}
	}
	return false
}
