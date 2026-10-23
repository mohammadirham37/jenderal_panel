package backup

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Website archives are tenant data. Extracting them as root lets archive
// entries land outside the home (through a planted directory symlink) and
// restores setuid bits, so extraction runs as the site account instead.
func TestRestoreWebsiteExtractsAsTheSiteAccount(t *testing.T) {
	var calls [][]string
	db := setupTestDB(t)
	svc := NewService(db, &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			return &executor.Result{}, nil
		},
	}, nil, "/tmp/test-backups")

	if err := svc.restoreWebsite(context.Background(), model.Backup{
		Target: "web_shop", Path: "/var/lib/jenderal/backups/shop.tar.gz",
	}); err != nil {
		t.Fatalf("restoreWebsite: %v", err)
	}

	var extract string
	for _, call := range calls {
		line := strings.Join(call, " ")
		if strings.Contains(line, "tar") {
			extract = line
		}
		if call[0] == "chown" {
			t.Errorf("a root chown over the restored home can be redirected: %s", line)
		}
	}
	if extract == "" {
		t.Fatal("no extraction was issued")
	}
	if !strings.HasPrefix(extract, "-u web_shop -- ") {
		t.Errorf("extraction runs as root: %s", extract)
	}
	for _, flag := range []string{"--no-same-owner", "--no-same-permissions", "--no-overwrite-dir"} {
		if !strings.Contains(extract, flag) {
			t.Errorf("extraction is missing %s: %s", flag, extract)
		}
	}
}
