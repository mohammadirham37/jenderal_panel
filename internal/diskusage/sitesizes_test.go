package diskusage

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func sizesService(t *testing.T, calls *int32) *Service {
	t.Helper()
	db, _ := insightDB(t)
	for _, row := range [][]string{
		{"w1", "shop.test", "web_shop", "u1"},
		{"w2", "blog.test", "web_blog", "u2"},
	} {
		if _, err := db.Exec(`INSERT INTO websites (id, domain, document_root, web_user, created_by, created_at, updated_at)
			VALUES (?, ?, '/home/'||?||'/public', ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			row[0], row[1], row[2], row[2], row[3]); err != nil {
			t.Fatal(err)
		}
	}
	return NewService(db, &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			atomic.AddInt32(calls, 1)
			var out strings.Builder
			for _, arg := range args {
				switch arg {
				case "/home/web_shop":
					out.WriteString("2048\t/home/web_shop\n")
				case "/home/web_blog":
					out.WriteString("4096\t/home/web_blog\n")
				}
			}
			return &executor.Result{Stdout: out.String()}, nil
		},
	})
}

func TestWebsiteSizesScopesToOwnerAndCaches(t *testing.T) {
	var calls int32
	svc := sizesService(t, &calls)
	ctx := context.Background()

	all, err := svc.WebsiteSizes(ctx, "admin", true, false)
	if err != nil || len(all) != 2 {
		t.Fatalf("admin sizes = %+v, %v", all, err)
	}
	own, err := svc.WebsiteSizes(ctx, "u1", false, false)
	if err != nil || len(own) != 1 || own[0].ID != "w1" || own[0].Bytes != 2048 {
		t.Fatalf("owner sizes = %+v, %v", own, err)
	}
	if calls != 1 {
		t.Errorf("du ran %d times, want 1 (second call must use the cache)", calls)
	}
	if _, err := svc.WebsiteSizes(ctx, "u1", false, true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("refresh did not re-measure: du ran %d times", calls)
	}
}
