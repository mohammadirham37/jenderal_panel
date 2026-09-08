package website

import (
	"context"
	"testing"
	"time"
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
