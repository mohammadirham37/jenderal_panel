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

func TestRepeatedBucketCommitMergesTopEvidence(t *testing.T) {
	r := testRepo(t)
	now := time.Now().UTC().Truncate(time.Minute)
	for _, b := range []MinuteBucket{
		{WebsiteID: "site", BucketAt: now, Requests: 2, TopIPs: map[string]int{"1.1.1.1": 2}, TopPaths: map[string]int{}, TopAgents: map[string]int{}},
		{WebsiteID: "site", BucketAt: now, Requests: 3, TopIPs: map[string]int{"1.1.1.1": 1, "2.2.2.2": 2}, TopPaths: map[string]int{}, TopAgents: map[string]int{}},
	} {
		if err := r.CommitBucket(context.Background(), b, LogCursor{WebsiteID: "site", Inode: 1, Offset: int64(b.Requests), UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.Bucket(context.Background(), "site", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 5 || got.TopIPs["1.1.1.1"] != 3 || got.TopIPs["2.2.2.2"] != 2 {
		t.Fatalf("bucket=%#v", got)
	}
}

// Buckets committed once must accumulate into the monthly bandwidth table
// exactly once, split by the bucket's UTC month, regardless of traffic
// bucket retention.
func TestCommitBucketsAccumulatesMonthlyBandwidth(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	late := time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)
	early := late.Add(-time.Hour) // still 2026-09 in UTC

	if err := r.CommitBuckets(ctx, []MinuteBucket{
		{WebsiteID: "site", BucketAt: late, Requests: 4, Bytes: 400, TopIPs: map[string]int{}, TopPaths: map[string]int{}, TopAgents: map[string]int{}},
		{WebsiteID: "site", BucketAt: early, Requests: 2, Bytes: 200, TopIPs: map[string]int{}, TopPaths: map[string]int{}, TopAgents: map[string]int{}},
	}, LogCursor{WebsiteID: "site", Inode: 1, Offset: 10, UpdatedAt: late}); err != nil {
		t.Fatal(err)
	}
	// Re-committing the same month must add, not replace.
	if err := r.CommitBuckets(ctx, []MinuteBucket{
		{WebsiteID: "site", BucketAt: late, Requests: 1, Bytes: 100, TopIPs: map[string]int{}, TopPaths: map[string]int{}, TopAgents: map[string]int{}},
	}, LogCursor{WebsiteID: "site", Inode: 1, Offset: 20, UpdatedAt: late}); err != nil {
		t.Fatal(err)
	}

	var sepBytes, sepRequests int
	if err := r.db.QueryRow(`SELECT bytes, requests FROM website_bandwidth_monthly WHERE website_id='site' AND month='2026-09'`).Scan(&sepBytes, &sepRequests); err != nil {
		t.Fatal(err)
	}
	if sepBytes != 200 || sepRequests != 2 {
		t.Fatalf("september = (%d bytes, %d requests), want (200, 2)", sepBytes, sepRequests)
	}
	var octBytes, octRequests int
	if err := r.db.QueryRow(`SELECT bytes, requests FROM website_bandwidth_monthly WHERE website_id='site' AND month='2026-10'`).Scan(&octBytes, &octRequests); err != nil {
		t.Fatal(err)
	}
	if octBytes != 500 || octRequests != 5 {
		t.Fatalf("october = (%d bytes, %d requests), want (500, 5)", octBytes, octRequests)
	}
}
