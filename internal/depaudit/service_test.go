package depaudit

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"
)

func addSite(t *testing.T, db *sql.DB, id, domain, webUser, docroot string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO websites (id, domain, document_root, web_user, created_at, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`, id, domain, docroot, webUser); err != nil {
		t.Fatal(err)
	}
}

type recordingSink struct {
	mu    sync.Mutex
	calls [][]Finding
}

func (r *recordingSink) SyncDependencyFindings(_ context.Context, f []Finding, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, f)
	return nil
}

const laravelLock = `{"packages":[{"name":"laravel/framework","version":"v11.9.2"},{"name":"guzzlehttp/guzzle","version":"7.8.1"}],
"packages-dev":[{"name":"phpunit/phpunit","version":"10.5.20"}]}`

func newTestService(t *testing.T) (*Service, *fakeOSV, *recordingSink) {
	t.Helper()
	db := testDB(t)
	addSite(t, db, "site-a", "shop.example.com", "web_shop", "/home/web_shop/app/public")
	addSite(t, db, "site-b", "blog.example.com", "web_blog", "/home/web_blog")
	exec := fakeFS(map[string]string{
		"/home/web_shop/app/composer.lock":     laravelLock,
		"/home/web_shop/app/package-lock.json": `{"lockfileVersion":3,"packages":{"node_modules/axios":{"version":"1.6.0"}}}`,
		"/home/web_blog/composer.lock":         `{"packages":[{"name":"guzzlehttp/guzzle","version":"7.8.1"}]}`,
	}, nil)
	f := newFakeOSV()
	f.vulnsFor["laravel/framework@11.9.2"] = []string{"GHSA-lara"}
	f.vulnsFor["guzzlehttp/guzzle@7.8.1"] = []string{"GHSA-guzz"}
	f.vulnsFor["phpunit/phpunit@10.5.20"] = []string{"GHSA-unit"}
	f.records["GHSA-lara"] = `{"id":"GHSA-lara","summary":"Laravel bug","aliases":["CVE-2026-1"],"database_specific":{"severity":"HIGH"},
	  "affected":[{"package":{"ecosystem":"Packagist","name":"laravel/framework"},"ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"11.0.0"},{"fixed":"11.9.3"}]}]}]}`
	f.records["GHSA-guzz"] = `{"id":"GHSA-guzz","summary":"Guzzle bug","database_specific":{"severity":"CRITICAL"}}`
	f.records["GHSA-unit"] = `{"id":"GHSA-unit","summary":"PHPUnit bug","database_specific":{"severity":"LOW"}}`
	srv := f.server(t)
	s := NewService(db, exec)
	s.osv.baseURL = srv.URL
	sink := &recordingSink{}
	s.SetEventSink(sink)
	return s, f, sink
}

func TestScanStoresFindingsPerSite(t *testing.T) {
	s, f, sink := newTestService(t)
	if _, err := s.StartScan(TriggerManual); err != nil {
		t.Fatal(err)
	}
	s.wait()
	r, err := s.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Scan == nil || r.Scan.Status != StatusCompleted || r.Scan.Sites != 2 || r.Scan.Packages != 4 || r.Scan.Findings != 4 {
		t.Fatalf("scan = %+v", r.Scan)
	}
	var got []string
	for _, fd := range r.Findings {
		s := fd.Severity + " " + fd.Domain + " " + fd.Package + " " + fd.VulnID
		if fd.Dev {
			s += " dev"
		}
		got = append(got, s)
	}
	want := []string{
		"critical blog.example.com guzzlehttp/guzzle GHSA-guzz",
		"critical shop.example.com guzzlehttp/guzzle GHSA-guzz",
		"high shop.example.com laravel/framework GHSA-lara",
		"low shop.example.com phpunit/phpunit GHSA-unit dev",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s", strings.Join(got, "\n"))
	}
	lara := r.Findings[2]
	if lara.FixedVersion != "11.9.3" || strings.Join(lara.Aliases, ",") != "CVE-2026-1" || lara.Lockfile != "composer.lock" ||
		!strings.HasPrefix(lara.URL, "https://osv.dev/vulnerability/GHSA-lara") {
		t.Errorf("laravel finding = %+v", lara)
	}
	// Guzzle is used by two sites but its advisory is fetched once.
	if f.fetched["GHSA-guzz"] != 1 {
		t.Errorf("GHSA-guzz fetched %d times", f.fetched["GHSA-guzz"])
	}
	if len(sink.calls) != 1 || len(sink.calls[0]) != 4 {
		t.Errorf("sink calls = %d", len(sink.calls))
	}
}

func TestFailedScanKeepsThePreviousFindings(t *testing.T) {
	s, f, sink := newTestService(t)
	s.StartScan(TriggerManual)
	s.wait()
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	s.StartScan(TriggerManual)
	s.wait()
	r, err := s.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Scan.Status != StatusFailed || !strings.Contains(r.Scan.Error, "502") {
		t.Errorf("latest scan = %+v", r.Scan)
	}
	if r.Completed == nil || len(r.Findings) != 4 {
		t.Errorf("previous findings lost: completed=%+v findings=%d", r.Completed, len(r.Findings))
	}
	if len(sink.calls) != 1 {
		t.Error("a failed scan must not resolve events")
	}
}

func TestNewScanReplacesTheOldOne(t *testing.T) {
	s, _, _ := newTestService(t)
	s.StartScan(TriggerManual)
	s.wait()
	s.StartScan(TriggerManual)
	s.wait()
	var scans, findings int
	s.db.QueryRow(`SELECT COUNT(*) FROM dependency_scans`).Scan(&scans)
	s.db.QueryRow(`SELECT COUNT(*) FROM dependency_findings`).Scan(&findings)
	if scans != 1 || findings != 4 {
		t.Errorf("scans=%d findings=%d, want 1 and 4", scans, findings)
	}
}

func TestOnlyOneScanRuns(t *testing.T) {
	s, f, _ := newTestService(t)
	f.mu.Lock() // hold the OSV server so the first scan stays running
	if _, err := s.StartScan(TriggerManual); err != nil {
		t.Fatal(err)
	}
	_, err := s.StartScan(TriggerManual)
	f.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("second scan = %v", err)
	}
	s.wait()
}

func TestScanIfDue(t *testing.T) {
	s, _, _ := newTestService(t)
	now := time.Now().UTC()
	s.ScanIfDue(context.Background(), now)
	s.wait()
	var scans int
	s.db.QueryRow(`SELECT COUNT(*) FROM dependency_scans WHERE trigger = ?`, TriggerSchedule).Scan(&scans)
	if scans != 1 {
		t.Fatalf("first due scan not started: %d", scans)
	}
	s.ScanIfDue(context.Background(), now.Add(time.Hour))
	s.wait()
	var ids []string
	rows, _ := s.db.Query(`SELECT id FROM dependency_scans`)
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	r, _ := s.Latest(context.Background())
	s.ScanIfDue(context.Background(), now.Add(25*time.Hour))
	s.wait()
	r2, _ := s.Latest(context.Background())
	if len(ids) != 1 || r.Scan.ID == r2.Scan.ID {
		t.Errorf("scan within 24h ran again or none after 24h: ids=%v first=%s later=%s", ids, r.Scan.ID, r2.Scan.ID)
	}
}

func TestStaleRunningScanIsMarkedFailed(t *testing.T) {
	db := testDB(t)
	db.Exec(`INSERT INTO dependency_scans (id, trigger, status, started_at) VALUES ('x', 'manual', 'running', '2026-01-01T00:00:00Z')`)
	s := NewService(db, nil)
	r, _ := s.Latest(context.Background())
	if r.Scan.Status != StatusFailed {
		t.Errorf("stale scan = %+v", r.Scan)
	}
}
