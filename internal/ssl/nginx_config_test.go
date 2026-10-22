package ssl

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestLaravelProfileSurvivesCertificateActivation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-laravel-profile", "laravel.example.com")
	if _, err := db.Exec(`UPDATE websites SET app_type = 'laravel', framework = 'laravel', framework_version = '13', document_root = '/home/web_laravel_example_com/app/public' WHERE id = 'ws-laravel-profile'`); err != nil {
		t.Fatal(err)
	}

	installed := map[string]string{}
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			if name == "install" && len(args) == 4 {
				content, err := os.ReadFile(args[2])
				if err != nil {
					t.Fatal(err)
				}
				installed[args[3]] = string(content)
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, nil, "/etc/jenderal/ssl")
	svc.ipv6Available = func() bool { return false }
	site, err := svc.loadSiteForDomain(context.Background(), "ws-laravel-profile", "laravel.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.activateCertificate(context.Background(), activationRequest{Site: site, CertificatePEM: []byte("cert"), PrivateKeyPEM: []byte("key")}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/etc/nginx/sites-available/laravel.example.com", "/etc/nginx/sites-available/laravel.example.com.ssl"} {
		config := installed[path]
		if !strings.Contains(config, "root /home/web_laravel_example_com/app/public;") || !strings.Contains(config, "location ~ ^/index\\.php(/|$)") {
			t.Errorf("Laravel routing was not preserved in %s:\n%s", path, config)
		}
	}
}

func TestActivateCertificateRendersIPv4AndRedirect(t *testing.T) {
	installed := make(map[string]string)
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			if name == "install" && len(args) == 4 {
				content, err := os.ReadFile(args[2])
				if err != nil {
					t.Fatalf("read temporary install source: %v", err)
				}
				installed[args[3]] = string(content)
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	svc := NewService(nil, mock, nil, nil, "/etc/jenderal/ssl")
	svc.ipv6Available = func() bool { return false }
	site := siteRecord{
		WebsiteID:     "ws-1",
		PrimaryDomain: "example.com",
		Domain:        "www.example.com",
		DocumentRoot:  "/home/web_example/public",
		AppType:       "static",
		LogDir:        "/home/web_example/logs",
		Aliases:       []string{"www.example.com"},
		ForceHTTPS:    true,
	}
	err := svc.activateCertificate(context.Background(), activationRequest{
		Site:            site,
		CertificatePEM:  []byte("certificate"),
		PrivateKeyPEM:   []byte("private-key"),
		RedirectDomains: []string{"www.example.com"},
	})
	if err != nil {
		t.Fatalf("activateCertificate() error = %v", err)
	}

	httpConfig := installed["/etc/nginx/sites-available/example.com"]
	tlsConfig := installed["/etc/nginx/sites-available/www.example.com.ssl"]
	if !strings.Contains(httpConfig, "return 301 https://$host$request_uri;") {
		t.Fatalf("HTTP config has no HTTPS redirect:\n%s", httpConfig)
	}
	if strings.Contains(httpConfig+tlsConfig, "listen [::]") {
		t.Fatalf("IPv4-only config contains IPv6 listener:\n%s\n%s", httpConfig, tlsConfig)
	}
	if !strings.Contains(tlsConfig, "server_name www.example.com;") {
		t.Fatalf("TLS config has wrong domain:\n%s", tlsConfig)
	}
	if got := installed["/etc/jenderal/ssl/www.example.com/key.pem"]; got != "private-key" {
		t.Fatalf("installed key = %q", got)
	}
}

func TestActivationRollbackWhenNginxTestFails(t *testing.T) {
	var nginxTests int
	var restored bool
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "test":
				return &executor.Result{ExitCode: 0}, nil
			case "nginx":
				nginxTests++
				if nginxTests == 1 {
					return &executor.Result{ExitCode: 1, Stderr: "candidate invalid"}, nil
				}
			case "cp":
				if len(args) == 3 && args[0] == "-a" && strings.Contains(args[1], "jenderal_ssl_backup_") {
					restored = true
				}
			}
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}

	svc := NewService(nil, mock, nil, nil, "/etc/jenderal/ssl")
	svc.ipv6Available = func() bool { return false }
	err := svc.activateCertificate(context.Background(), activationRequest{
		Site: siteRecord{
			WebsiteID: "ws-1", PrimaryDomain: "example.com", Domain: "example.com",
			DocumentRoot: "/home/web_example/public", AppType: "static", LogDir: "/home/web_example/logs",
		},
		CertificatePEM:  []byte("new-certificate"),
		PrivateKeyPEM:   []byte("new-private-key"),
		RedirectDomains: []string{"example.com"},
	})
	if err == nil || !strings.Contains(err.Error(), "candidate invalid") {
		t.Fatalf("activateCertificate() error = %v, want nginx failure", err)
	}
	if !restored {
		t.Fatal("existing files were not restored")
	}
	if nginxTests != 2 {
		t.Fatalf("nginx test calls = %d, want candidate and restored validation", nginxTests)
	}
}

func TestActivationRollbackSurvivesCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var restored bool
	var nginxTests int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(callCtx context.Context, name string, args ...string) (*executor.Result, error) {
			if callCtx.Err() != nil {
				return nil, callCtx.Err()
			}
			switch name {
			case "test":
				return &executor.Result{ExitCode: 0}, nil
			case "nginx":
				nginxTests++
				if nginxTests == 1 {
					cancel()
					return &executor.Result{ExitCode: 1, Stderr: "request canceled"}, nil
				}
			case "cp":
				if len(args) == 3 && args[0] == "-a" && strings.Contains(args[1], "jenderal_ssl_backup_") {
					restored = true
				}
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(nil, mock, nil, nil, "/etc/jenderal/ssl")
	svc.ipv6Available = func() bool { return false }
	err := svc.activateCertificate(ctx, activationRequest{
		Site:           siteRecord{WebsiteID: "ws", PrimaryDomain: "example.com", Domain: "example.com", DocumentRoot: "/srv/example", AppType: "static", LogDir: "/var/log/example"},
		CertificatePEM: []byte("cert"), PrivateKeyPEM: []byte("key"), RedirectDomains: []string{"example.com"},
	})
	if err == nil {
		t.Fatal("activateCertificate() error = nil")
	}
	if !restored || nginxTests != 2 {
		t.Fatalf("rollback restored = %v, nginx tests = %d; want true and 2", restored, nginxTests)
	}
}

func TestRemoveCertificateConfigRollsBackWhenNginxTestFails(t *testing.T) {
	var nginxTests int
	var restoredPaths []string
	var linkedSuspendedHTTP bool
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "test":
				return &executor.Result{ExitCode: 0}, nil
			case "nginx":
				nginxTests++
				if nginxTests == 1 {
					return &executor.Result{ExitCode: 1, Stderr: "removal invalid"}, nil
				}
			case "cp":
				if len(args) == 3 && args[0] == "-a" && strings.Contains(args[1], "jenderal_ssl_backup_") {
					restoredPaths = append(restoredPaths, args[2])
				}
			case "ln":
				if len(args) == 3 && args[2] == "/etc/nginx/sites-enabled/example.com.suspended" {
					linkedSuspendedHTTP = true
				}
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	svc := NewService(nil, mock, nil, nil, "/etc/jenderal/ssl")
	svc.ipv6Available = func() bool { return false }
	err := svc.removeCertificateConfig(context.Background(), siteRecord{
		WebsiteID: "ws-1", PrimaryDomain: "example.com", Domain: "www.example.com",
		DocumentRoot: "/home/web_example/public", AppType: "static", LogDir: "/home/web_example/logs",
		Aliases: []string{"www.example.com"}, Status: "suspended",
	}, []string{"example.com"})
	if err == nil || !strings.Contains(err.Error(), "removal invalid") {
		t.Fatalf("removeCertificateConfig() error = %v, want nginx failure", err)
	}
	if len(restoredPaths) != 7 {
		t.Fatalf("restored paths = %v, want certificate, HTTP, and TLS paths", restoredPaths)
	}
	if nginxTests != 2 {
		t.Fatalf("nginx test calls = %d, want candidate and restored validation", nginxTests)
	}
	if !linkedSuspendedHTTP {
		t.Fatal("suspended website HTTP config was re-enabled during certificate removal")
	}
}

// renderingMock records every file installed through `install` and lets
// probes for certificate material succeed.
func renderingMock(t *testing.T, installed map[string]string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "install" && len(args) == 4 {
				content, err := os.ReadFile(args[2])
				if err != nil {
					t.Fatal(err)
				}
				installed[args[3]] = string(content)
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func TestHTTPSVhostsKeepTheWebsiteProfileAndUpstreams(t *testing.T) {
	cases := []struct {
		name, setup string
		want        []string
	}{
		{
			name:  "laravel octane",
			setup: `UPDATE websites SET app_type='laravel', framework='laravel', framework_version='12', nginx_profile='laravel-octane', octane_port=8105 WHERE id='ws'`,
			want:  []string{"proxy_pass http://127.0.0.1:8105;"},
		},
		{
			name:  "reverse proxy",
			setup: `UPDATE websites SET app_type='reverse-proxy', nginx_profile='reverse-proxy', proxy_scheme='http', proxy_host='127.0.0.1', proxy_port=3000 WHERE id='ws'`,
			want:  []string{"proxy_pass http://127.0.0.1:3000"},
		},
		{
			name:  "go app",
			setup: `UPDATE websites SET app_type='go', framework='go', nginx_profile='app-proxy', app_port=8200 WHERE id='ws'`,
			want:  []string{"proxy_pass http://127.0.0.1:8200;", "try_files $uri @app;"},
		},
		{
			name:  "manual codeigniter template",
			setup: `UPDATE websites SET app_type='php', nginx_profile='codeigniter4' WHERE id='ws'`,
			want:  []string{"try_files $uri $uri/ /index.php?$query_string;"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()
			insertTestWebsite(t, db, "ws", "site.example.com")
			if _, err := db.Exec(tc.setup); err != nil {
				t.Fatal(err)
			}
			installed := map[string]string{}
			svc := NewService(db, renderingMock(t, installed), nil, nil, "/etc/jenderal/ssl")
			svc.ipv6Available = func() bool { return false }
			ctx := context.Background()
			site, err := svc.loadSiteForDomain(ctx, "ws", "site.example.com")
			if err != nil {
				t.Fatal(err)
			}

			// Certificate activation writes both vhosts.
			if err := svc.activateCertificate(ctx, activationRequest{Site: site, CertificatePEM: []byte("c"), PrivateKeyPEM: []byte("k")}); err != nil {
				t.Fatal(err)
			}
			// The redirect sync rewrites the HTTP vhost on its own.
			if err := svc.syncHTTPConfig(ctx, site, nil); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/etc/nginx/sites-available/site.example.com", "/etc/nginx/sites-available/site.example.com.ssl"} {
				config := installed[path]
				for _, want := range tc.want {
					if !strings.Contains(config, want) {
						t.Errorf("%s missing %q:\n%s", path, want, config)
					}
				}
				if !strings.Contains(config, "include /etc/nginx/jenderal/security/sites/ws.conf;") {
					t.Errorf("%s lost the website security include:\n%s", path, config)
				}
			}

			// Re-rendering the TLS vhost (document root / PHP changes) too.
			delete(installed, "/etc/nginx/sites-available/site.example.com.ssl")
			written := false
			if err := svc.rewriteTLSVhost(ctx, site, "site.example.com", &written); err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(installed["/etc/nginx/sites-available/site.example.com.ssl"], want) {
					t.Errorf("rewritten TLS vhost missing %q", want)
				}
			}
		})
	}
}

func TestVhostBaseSkipsMissingSecuritySnippet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws", "site.example.com")
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" && strings.HasPrefix(args[len(args)-1], "/etc/nginx/jenderal/security/sites/") {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, nil, "/etc/jenderal/ssl")
	svc.ipv6Available = func() bool { return false }
	site, err := svc.loadSiteForDomain(context.Background(), "ws", "site.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.vhostBase(context.Background(), site).SecurityInclude; got != "" {
		t.Errorf("SecurityInclude = %q, want empty when the snippet does not exist", got)
	}
}
