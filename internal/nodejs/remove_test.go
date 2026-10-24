package nodejs

import (
	"context"
	"strings"
	"testing"
)

func TestRemoveRuntimeClearsAPHPSitesNode(t *testing.T) {
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	insertTestWebsite(t, db, "site-php", "php.example.com", "web_php", "/home/web_php/public")
	if _, err := db.Exec(`UPDATE websites SET app_type = 'laravel' WHERE id = 'site-php'`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, newMockExec(), nil)
	var removed []string
	fake := installedRuntime("24")
	fake.remove = func(_ context.Context, user string) (int64, error) {
		removed = append(removed, user)
		return 300 << 20, nil
	}
	svc.runtime = fake

	var logs []string
	if err := svc.RemoveRuntime(context.Background(), "site-php", func(line string) { logs = append(logs, line) }); err != nil {
		t.Fatalf("RemoveRuntime: %v", err)
	}
	if strings.Join(removed, ",") != "web_php" {
		t.Errorf("removed = %v", removed)
	}
	var version string
	_ = db.QueryRow(`SELECT node_version FROM websites WHERE id = 'site-php'`).Scan(&version)
	if version != "" {
		t.Errorf("node_version = %q, want cleared", version)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "freed 300 MB") {
		t.Errorf("logs = %v", logs)
	}
}

// A Node website's own service and Node apps run on the runtime; removing
// it would take them down.
func TestRemoveRuntimeRefusesNodeWebsitesAndSitesWithApps(t *testing.T) {
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	insertTestWebsite(t, db, "site-node", "node.example.com", "web_node", "/home/web_node/public")
	if _, err := db.Exec(`UPDATE websites SET app_type = 'node' WHERE id = 'site-node'`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, newMockExec(), nil)
	called := false
	fake := installedRuntime("24")
	fake.remove = func(context.Context, string) (int64, error) { called = true; return 0, nil }
	svc.runtime = fake

	if err := svc.RemoveRuntime(context.Background(), "site-node", nil); err == nil {
		t.Error("removing a Node website's runtime must be refused")
	}
	if called {
		t.Error("runtime removed despite the refusal")
	}
}
