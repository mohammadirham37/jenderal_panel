package website

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

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
