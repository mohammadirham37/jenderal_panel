package ssl

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

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
