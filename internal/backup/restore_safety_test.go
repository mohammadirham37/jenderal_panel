package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

// A backup records the target by name. If that name is re-used by another
// tenant later, restoring an old backup would write over their data, so the
// target is authorized again at restore time.
func TestRestoreRechecksTargetOwnership(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, &executor.MockExecutor{}, nil, "/tmp/test-backups")
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// The website "shop.example.com" now belongs to someone else.
	if _, err := db.Exec(`INSERT INTO websites (id, domain, document_root, web_user, created_at, updated_at, created_by)
		VALUES ('w1', 'shop.example.com', '/home/web_shop/public', 'web_shop', ?, ?, 'other-user')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO backups (id, type, target, path, size_bytes, status, created_at, created_by)
		VALUES ('b1', 'website', 'shop.example.com', '/var/lib/jenderal/backups/b1.tar.gz', 10, 'completed', ?, 'first-user')`, now); err != nil {
		t.Fatal(err)
	}

	// No task runner is wired here, so authorized callers fail later with a
	// different error; only the authorization outcome is asserted.
	_, err := svc.RequestRestore(ctx, Caller{UserID: "first-user"}, "b1", "")
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("err = %v, want forbidden for a target the caller no longer owns", err)
	}
	// The website's current owner does not own that backup either.
	if _, err := svc.RequestRestore(ctx, Caller{UserID: "other-user"}, "b1", ""); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("err = %v, want forbidden for a backup the caller did not create", err)
	}
	if _, err := svc.RequestRestore(ctx, Caller{UserID: "root", Admin: true}, "b1", ""); errors.Is(err, model.ErrForbidden) {
		t.Error("an admin must pass authorization")
	}

	// Owning both the backup and its current target still works.
	if _, err := db.Exec(`INSERT INTO websites (id, domain, document_root, web_user, created_at, updated_at, created_by)
		VALUES ('w2', 'mine.example.com', '/home/web_mine/public', 'web_mine', ?, ?, 'first-user')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO backups (id, type, target, path, size_bytes, status, created_at, created_by)
		VALUES ('b2', 'website', 'mine.example.com', '/var/lib/jenderal/backups/b2.tar.gz', 10, 'completed', ?, 'first-user')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequestRestore(ctx, Caller{UserID: "first-user"}, "b2", ""); errors.Is(err, model.ErrForbidden) {
		t.Error("the owner of both the backup and its target must pass authorization")
	}
}
