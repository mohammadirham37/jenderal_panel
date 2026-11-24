package depaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// fakeOSV serves querybatch and vulns. vulnsFor maps "name@version" to the
// vuln ids affecting it; records maps id to its JSON record.
type fakeOSV struct {
	mu        sync.Mutex
	vulnsFor  map[string][]string
	records   map[string]string
	modified  map[string]string
	batches   int
	batchSize []int
	fetched   map[string]int
	pageOnce  map[string]bool // first answer for this package carries a next_page_token
	fail      bool
}

func (f *fakeOSV) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		switch {
		case r.URL.Path == "/v1/querybatch":
			var body struct{ Queries []osvQuery }
			json.NewDecoder(r.Body).Decode(&body)
			f.batches++
			f.batchSize = append(f.batchSize, len(body.Queries))
			var results []map[string]any
			for _, q := range body.Queries {
				key := q.Package.Name + "@" + q.Version
				var vulns []map[string]string
				ids := f.vulnsFor[key]
				res := map[string]any{}
				if f.pageOnce[key] && q.PageToken == "" {
					ids = ids[:1]
					res["next_page_token"] = "p2"
				} else if q.PageToken == "p2" {
					ids = ids[1:]
				}
				for _, id := range ids {
					vulns = append(vulns, map[string]string{"id": id, "modified": f.modified[id]})
				}
				res["vulns"] = vulns
				results = append(results, res)
			}
			json.NewEncoder(w).Encode(map[string]any{"results": results})
		case strings.HasPrefix(r.URL.Path, "/v1/vulns/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
			f.fetched[id]++
			fmt.Fprint(w, f.records[id])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newFakeOSV() *fakeOSV {
	return &fakeOSV{vulnsFor: map[string][]string{}, records: map[string]string{}, modified: map[string]string{}, fetched: map[string]int{}, pageOnce: map[string]bool{}}
}

func TestOSVQueryBatchesAndPaginates(t *testing.T) {
	f := newFakeOSV()
	f.vulnsFor["axios@1.6.0"] = []string{"GHSA-1", "GHSA-2"}
	f.pageOnce["axios@1.6.0"] = true
	srv := f.server(t)
	c := newOSVClient(testDB(t))
	c.baseURL = srv.URL

	var pkgs []pkgKey
	for i := range 1499 {
		pkgs = append(pkgs, pkgKey{EcosystemNPM, fmt.Sprintf("pkg%d", i), "1.0.0"})
	}
	pkgs = append(pkgs, pkgKey{EcosystemNPM, "axios", "1.6.0"})
	got, err := c.query(context.Background(), pkgs)
	if err != nil {
		t.Fatal(err)
	}
	refs := got[pkgKey{EcosystemNPM, "axios", "1.6.0"}]
	if len(refs) != 2 || refs[0].ID != "GHSA-1" || refs[1].ID != "GHSA-2" {
		t.Errorf("axios refs = %+v", refs)
	}
	// 1000 + 500, then one follow-up page for axios.
	if f.batches != 3 || f.batchSize[0] != 1000 || f.batchSize[1] != 500 || f.batchSize[2] != 1 {
		t.Errorf("batches = %d sizes = %v", f.batches, f.batchSize)
	}
}

func TestOSVVulnUsesCacheUntilModified(t *testing.T) {
	f := newFakeOSV()
	f.records["GHSA-1"] = `{"id":"GHSA-1","modified":"2026-01-01T00:00:00Z","summary":"first"}`
	srv := f.server(t)
	c := newOSVClient(testDB(t))
	c.baseURL = srv.URL
	ctx := context.Background()

	ref := vulnRef{ID: "GHSA-1", Modified: "2026-01-01T00:00:00Z"}
	for range 2 {
		v, err := c.vuln(ctx, ref)
		if err != nil || v.Summary != "first" {
			t.Fatalf("vuln = %+v, %v", v, err)
		}
	}
	if f.fetched["GHSA-1"] != 1 {
		t.Errorf("fetched %d times, want 1 (cached)", f.fetched["GHSA-1"])
	}
	f.records["GHSA-1"] = `{"id":"GHSA-1","modified":"2026-02-01T00:00:00Z","summary":"second"}`
	v, err := c.vuln(ctx, vulnRef{ID: "GHSA-1", Modified: "2026-02-01T00:00:00Z"})
	if err != nil || v.Summary != "second" || f.fetched["GHSA-1"] != 2 {
		t.Errorf("modified record not refetched: %+v %v fetched=%d", v, err, f.fetched["GHSA-1"])
	}
}

func TestOSVFailureIsAnError(t *testing.T) {
	f := newFakeOSV()
	f.fail = true
	srv := f.server(t)
	c := newOSVClient(testDB(t))
	c.baseURL = srv.URL
	if _, err := c.query(context.Background(), []pkgKey{{EcosystemNPM, "a", "1.0.0"}}); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("query error = %v", err)
	}
}

func TestVulnSeverity(t *testing.T) {
	var v Vuln
	json.Unmarshal([]byte(`{"database_specific":{"severity":"MODERATE"},"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]}`), &v)
	if v.severity() != "medium" {
		t.Errorf("GitHub label must win: %s", v.severity())
	}
	v = Vuln{}
	json.Unmarshal([]byte(`{"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H"}]}`), &v)
	if v.severity() != "high" {
		t.Errorf("CVSS fallback = %s", v.severity())
	}
	if (&Vuln{}).severity() != "unknown" {
		t.Error("no data must be unknown")
	}
}

func TestVulnFixedFor(t *testing.T) {
	var v Vuln
	json.Unmarshal([]byte(`{"id":"GHSA-x","affected":[
	  {"package":{"ecosystem":"Packagist","name":"laravel/framework"},"ranges":[{"type":"ECOSYSTEM","events":[
	    {"introduced":"0"},{"fixed":"1.9.5"},{"introduced":"2.0.0"},{"fixed":"2.3.1"}]}]},
	  {"package":{"ecosystem":"npm","name":"laravel/framework"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"9.9.9"}]}]}
	],"references":[{"type":"WEB","url":"https://example.com"},{"type":"ADVISORY","url":"https://github.com/advisories/GHSA-x"}],
	"aliases":["CVE-2026-1234","OTHER-1"]}`), &v)
	if got := v.fixedFor(EcosystemPackagist, "laravel/framework", "2.3.0"); got != "2.3.1" {
		t.Errorf("fixed for 2.3.0 = %q", got)
	}
	if got := v.fixedFor(EcosystemPackagist, "laravel/framework", "1.2.0"); got != "1.9.5" {
		t.Errorf("fixed for 1.2.0 = %q", got)
	}
	if got := v.fixedFor(EcosystemPackagist, "laravel/framework", "3.0.0"); got != "" {
		t.Errorf("fixed for 3.0.0 = %q, want none", got)
	}
	if v.advisoryURL() != "https://github.com/advisories/GHSA-x" {
		t.Errorf("url = %s", v.advisoryURL())
	}
	if strings.Join(v.cveAliases(), ",") != "CVE-2026-1234" {
		t.Errorf("aliases = %v", v.cveAliases())
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.10", -1},
		{"1.10", "1.9.9", 1},
		{"1.0.0-beta.2", "1.0.0", -1},
		{"1.0.0-beta.2", "1.0.0-beta.10", -1},
		{"2.0", "2.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
