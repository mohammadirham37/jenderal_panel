package trafficguard

import (
	"context"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"testing"
	"time"
)

func testRepo(t *testing.T) *Repository {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return NewRepository(db)
}
func TestCursorAndBucketCommitAtomically(t *testing.T) {
	r := testRepo(t)
	now := time.Now().UTC().Truncate(time.Minute)
	b := MinuteBucket{WebsiteID: "site", BucketAt: now, Requests: 2, TopIPs: map[string]int{"1.1.1.1": 2}, TopPaths: map[string]int{}, TopAgents: map[string]int{}}
	if err := r.CommitBucket(context.Background(), b, LogCursor{WebsiteID: "site", Inode: 2, Offset: 99, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	c, err := r.Cursor(context.Background(), "site")
	if err != nil || c.Offset != 99 {
		t.Fatalf("cursor=%#v err=%v", c, err)
	}
}

func TestEnsureProfilePreservesObservationStart(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	first := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	if err := r.EnsureProfile(ctx, "site", first); err != nil {
		t.Fatal(err)
	}
	if err := r.EnsureProfile(ctx, "site", first.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, err := r.Profile(ctx, "site", first.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !p.ObserveStartedAt.Equal(first) {
		t.Fatalf("observation start changed: %s", p.ObserveStartedAt)
	}
}
