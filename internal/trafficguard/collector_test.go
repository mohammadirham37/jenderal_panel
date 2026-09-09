package trafficguard

import (
	"context"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestCollectReleasesWebsiteRowsBeforeWritingWithSingleDBConnection(t *testing.T) {
	repo := testRepo(t)
	repo.db.SetMaxOpenConns(1)
	now := time.Date(2026, 9, 9, 15, 0, 0, 0, time.UTC)
	if _, err := repo.db.Exec(`INSERT INTO websites
		(id, domain, document_root, web_user, status, created_at, updated_at)
		VALUES ('site-1', 'example.com', '/home/web_example_com/public', 'web_example_com', 'active', ?, ?)`,
		now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	commands := &executor.MockExecutor{
		RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1}, nil
		},
	}
	collector := NewCollector(repo.db, repo, commands, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := collector.Collect(ctx, now); err != nil {
		t.Fatalf("Collect() error = %v; website rows may still hold the only database connection", err)
	}

	var profiles int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM traffic_guard_profiles WHERE website_id = 'site-1'`).Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if profiles != 1 {
		t.Fatalf("traffic guard profiles = %d, want 1", profiles)
	}
}
