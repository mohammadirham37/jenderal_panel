package website

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestWordPressVhostIncludesHardeningGlob(t *testing.T) {
	out, err := RenderVhost(VhostData{
		Domain: "example.com", DocumentRoot: "/home/web_example_com/public", LogDir: "/home/web_example_com/logs",
		PHPVersion: "8.2", AppType: "wordpress", Profile: "wordpress",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "include /etc/nginx/jenderal/wordpress/example.com.conf*;") {
		t.Fatalf("wordpress vhost lacks the hardening include:\n%s", out)
	}
	// The include must precede the generic PHP location so its deny rules win.
	if strings.Index(out, "jenderal/wordpress/") > strings.Index(out, `location ~ \.php$`) {
		t.Fatal("hardening include must come before the PHP location")
	}
}

func TestRenderWpHardeningOnlyEnabledRules(t *testing.T) {
	empty := renderWpHardening(WpHardening{})
	if strings.Contains(empty, "location") || strings.Contains(empty, "add_header") {
		t.Fatalf("no toggles must render no rules:\n%s", empty)
	}

	all := renderWpHardening(WpHardening{BlockUploadPHP: true, BlockIncludesPHP: true, BlockSensitive: true,
		BlockAuthorScan: true, SecurityHeaders: true})
	for _, want := range []string{
		`^/wp-content/uploads/.*\.(php|phtml|phar)$`,
		`^/wp-includes/[^/]+\.php$`,
		`wp-config\.php`,
		`author=\d+`,
		`^/wp-admin/`,
		"X-Content-Type-Options nosniff",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("rules missing %q:\n%s", want, all)
		}
	}
}

type wpHardeningExec struct {
	calls       []string
	nginxTestOK bool
}

func (e *wpHardeningExec) mock() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			call := strings.Join(append([]string{name}, args...), " ")
			e.calls = append(e.calls, call)
			switch {
			case name == "cat":
				return &executor.Result{ExitCode: 1}, nil
			case name == "nginx":
				if e.nginxTestOK {
					return &executor.Result{ExitCode: 0}, nil
				}
				return &executor.Result{ExitCode: 1, Stderr: "bad rule"}, nil
			case strings.Contains(call, "config path"):
				return &executor.Result{Stdout: "/home/web_wp_example_com/public/wp-config.php\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func (e *wpHardeningExec) called(fragment string) bool {
	for _, c := range e.calls {
		if strings.Contains(c, fragment) {
			return true
		}
	}
	return false
}

func setupWpHardeningDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE website_wp_hardening (
		website_id TEXT PRIMARY KEY REFERENCES websites(id) ON DELETE CASCADE,
		block_upload_php INTEGER NOT NULL DEFAULT 0, block_includes_php INTEGER NOT NULL DEFAULT 0,
		block_sensitive INTEGER NOT NULL DEFAULT 0, block_author_scan INTEGER NOT NULL DEFAULT 0,
		security_headers INTEGER NOT NULL DEFAULT 0, disallow_file_edit INTEGER NOT NULL DEFAULT 0,
		force_ssl_admin INTEGER NOT NULL DEFAULT 0, disable_debug INTEGER NOT NULL DEFAULT 0,
		protect_wp_config INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, created_at, updated_at)
		VALUES ('wp1', 'wp.example.com', 'wordpress', '8.2', '/home/web_wp_example_com/public', 'web_wp_example_com', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestApplyWpHardeningWritesRulesConstantsAndMode(t *testing.T) {
	db := setupWpHardeningDB(t)
	rec := &wpHardeningExec{nginxTestOK: true}
	svc := NewService(db, rec.mock(), nil)
	svc.ipv6Available = func() bool { return false }

	want := WpHardening{BlockUploadPHP: true, DisallowFileEdit: true, DisableDebug: true, ProtectWpConfig: true}
	if _, err := svc.ApplyWpHardening(context.Background(), "wp1", want); err != nil {
		t.Fatalf("ApplyWpHardening: %v", err)
	}
	for _, fragment := range []string{
		"install -D -m 0644 -o root -g root",
		"/etc/nginx/jenderal/wordpress/wp.example.com.conf",
		"config set DISALLOW_FILE_EDIT true --raw --type=constant",
		"config set FORCE_SSL_ADMIN false --raw --type=constant",
		"config set WP_DEBUG false --raw --type=constant",
		"-u web_wp_example_com -- chmod 0600 -- /home/web_wp_example_com/public/wp-config.php",
	} {
		if !rec.called(fragment) {
			t.Errorf("missing command containing %q; calls:\n%s", fragment, strings.Join(rec.calls, "\n"))
		}
	}

	got, err := svc.GetWpHardening(context.Background(), "wp1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.BlockUploadPHP || !got.DisallowFileEdit || !got.ProtectWpConfig || got.SecurityHeaders || got.UpdatedAt == "" {
		t.Fatalf("stored hardening = %+v", got)
	}
}

func TestApplyWpHardeningForceSSLAdminNeedsCertificate(t *testing.T) {
	db := setupWpHardeningDB(t)
	rec := &wpHardeningExec{nginxTestOK: true}
	svc := NewService(db, rec.mock(), nil)

	_, err := svc.ApplyWpHardening(context.Background(), "wp1", WpHardening{ForceSSLAdmin: true})
	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("err = %v, want validation error", err)
	}
	if rec.called("install -D") {
		t.Fatal("nothing must be written when validation fails")
	}
}

func TestApplyWpHardeningRollsBackInvalidNginx(t *testing.T) {
	db := setupWpHardeningDB(t)
	rec := &wpHardeningExec{nginxTestOK: false}
	svc := NewService(db, rec.mock(), nil)
	svc.ipv6Available = func() bool { return false }

	if _, err := svc.ApplyWpHardening(context.Background(), "wp1", WpHardening{SecurityHeaders: true}); err == nil {
		t.Fatal("expected nginx validation error")
	}
	if !rec.called("rm -f /etc/nginx/jenderal/wordpress/wp.example.com.conf") {
		t.Fatalf("rules file not rolled back; calls:\n%s", strings.Join(rec.calls, "\n"))
	}
	got, _ := svc.loadWpHardening(context.Background(), "wp1")
	if got.SecurityHeaders {
		t.Fatal("failed apply must not be stored")
	}
}

func TestPlainPHPVhostIncludesHardeningGlob(t *testing.T) {
	out, err := RenderVhost(VhostData{
		Domain: "example.com", DocumentRoot: "/home/web_example_com/public", LogDir: "/home/web_example_com/logs",
		PHPVersion: "8.2", AppType: "php",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "include /etc/nginx/jenderal/wordpress/example.com.conf*;") {
		t.Fatalf("plain PHP vhost lacks the hardening include:\n%s", out)
	}
}

func TestDetectWordPressOnPlainPHPSiteInstallsWpCli(t *testing.T) {
	db := setupWpHardeningDB(t)
	if _, err := db.Exec(`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, created_at, updated_at)
		VALUES ('php1', 'php.example.com', 'php', '8.2', '/home/web_php_example_com/public', 'web_php_example_com', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		       ('php2', 'plain.example.com', 'php', '8.2', '/home/web_plain_example_com/public', 'web_plain_example_com', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	var calls []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			call := strings.Join(append([]string{name}, args...), " ")
			calls = append(calls, call)
			switch call {
			case "test -f /home/web_php_example_com/public/wp-load.php":
				return &executor.Result{ExitCode: 0}, nil
			case "test -f /home/web_php_example_com/.wp-cli/wp-cli.phar":
				return &executor.Result{ExitCode: 1}, nil
			}
			if name == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil)

	if ok, err := svc.DetectWordPress(context.Background(), "php1"); err != nil || !ok {
		t.Fatalf("DetectWordPress(php1) = %v, %v; want true", ok, err)
	}
	if ok, err := svc.DetectWordPress(context.Background(), "php2"); err != nil || ok {
		t.Fatalf("DetectWordPress(php2) = %v, %v; want false", ok, err)
	}

	if _, err := svc.WpStatus(context.Background(), "php1"); err != nil {
		t.Fatalf("WpStatus: %v", err)
	}
	want := "-u web_php_example_com -- /usr/bin/curl -fsSL -o /home/web_php_example_com/.wp-cli/wp-cli.phar " + wpCliURL
	found := false
	for _, c := range calls {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("wp-cli not downloaded as the site user; calls:\n%s", strings.Join(calls, "\n"))
	}
	if _, err := svc.WpStatus(context.Background(), "php2"); err == nil {
		t.Fatal("toolkit must stay unavailable for PHP sites without WordPress")
	}
}
