package website

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// An uploaded archive must not be staged in the shared /tmp, where other
// local accounts (other tenants' shells) could read it.
func TestUploadArchiveStagesInsideTheWebsiteHome(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	var staged string
	var sudo []string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudo = append(sudo, name+" "+strings.Join(args, " "))
			return &executor.Result{}, nil
		},
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			staged = name + " " + strings.Join(args, " ")
			_, _ = io.Copy(io.Discard, stdin)
			return 0, nil
		},
	}
	svc := NewService(db, mock, nil)
	svc.SetTaskRunner(taskrunner.New())
	if _, err := db.Exec(`INSERT INTO websites (id, domain, app_type, document_root, web_user, status, created_at, updated_at)
		VALUES ('w1', 'shop.test', 'php', '/home/web_shop/public', 'web_shop', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UploadArchive(context.Background(), "w1", strings.NewReader("archive"), "site.zip"); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}
	if !strings.HasPrefix(staged, "-u web_shop -- ") || !strings.Contains(staged, "/home/web_shop/.jenderal-upload-") {
		t.Errorf("the archive must be staged in the website home as its account: %q", staged)
	}
	if strings.Contains(staged, "/tmp/") {
		t.Errorf("the archive is staged in the shared /tmp: %q", staged)
	}
	chmodded := false
	for _, call := range sudo {
		if strings.HasPrefix(call, "-u web_shop -- chmod 600 /home/web_shop/.jenderal-upload-") {
			chmodded = true
		}
	}
	if !chmodded {
		t.Errorf("the staged archive must be private to its owner: %v", sudo)
	}
}
