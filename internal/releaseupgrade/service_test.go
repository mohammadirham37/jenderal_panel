package releaseupgrade

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// fakeServer is a healthy noble server: everything passes unless a test
// changes a field.
type fakeServer struct {
	mu            sync.Mutex
	codename      string
	pendingLines  string
	rebootPending bool
	dpkgAudit     string
	dfOutput      string
	sources       string
	mysqlNative   string
	mysqlMissing  bool
	state         string
	unitActive    bool
	bootID        string
	currentBootID string
	sudoCalls     [][]string
	inputs        map[string]string
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		codename: "noble",
		dfOutput: "    Avail\n20000000000\n900000000\n",
		sources:  "",
		inputs:   map[string]string{},
	}
}

func (f *fakeServer) executor() *executor.MockExecutor {
	ok := func(out string) (*executor.Result, error) { return &executor.Result{Stdout: out}, nil }
	fail := func() (*executor.Result, error) { return &executor.Result{ExitCode: 1}, nil }
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			joined := name + " " + strings.Join(args, " ")
			switch {
			case joined == "/bin/cat /etc/os-release":
				return ok("VERSION_CODENAME=" + f.codename + "\n")
			case joined == "/usr/bin/test -x /usr/bin/do-release-upgrade":
				return ok("")
			case joined == "/usr/bin/test -e /var/run/reboot-required":
				if f.rebootPending {
					return ok("")
				}
				return fail()
			case joined == "/usr/bin/test -x /usr/sbin/mysqld":
				if f.mysqlMissing {
					return fail()
				}
				return ok("")
			case name == "df":
				return ok(f.dfOutput)
			case joined == "/bin/cat /proc/sys/kernel/random/boot_id":
				return ok(f.currentBootID + "\n")
			}
			return fail()
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.sudoCalls = append(f.sudoCalls, append([]string{name}, args...))
			joined := name + " " + strings.Join(args, " ")
			switch {
			case name == "apt-get":
				return ok(f.pendingLines)
			case name == "dpkg":
				return ok(f.dpkgAudit)
			case name == "fuser":
				return fail()
			case name == "/bin/bash" && args[1] == sourcesDumpScript:
				return ok(f.sources)
			case name == "mysql":
				return ok(f.mysqlNative + "\n")
			case joined == "cat "+StateDir+"/state":
				if f.state == "" {
					return fail()
				}
				return ok(f.state + "\n")
			case joined == "cat "+StateDir+"/boot_id":
				if f.bootID == "" {
					return fail()
				}
				return ok(f.bootID + "\n")
			case joined == "systemctl is-active "+UnitName:
				if f.unitActive {
					return ok("active\n")
				}
				return &executor.Result{Stdout: "inactive\n", ExitCode: 3}, nil
			}
			return ok("")
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if name == "tee" {
				f.inputs[args[0]] = input
			}
			return ok("")
		},
	}
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(s))
	zw.Close()
	return buf.Bytes()
}

// repoServer answers the meta-release, the sury Packages index and
// Release-file probes. Paths in missing answer 404.
func repoServer(t *testing.T, missing ...string) *httptest.Server {
	t.Helper()
	packages := gzipped(t, "Package: php8.3-fpm\nVersion: 8.3.36\n\nPackage: php8.4-fpm\n\nPackage: php8.3-cli\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, m := range missing {
			if strings.Contains(r.URL.Path, m) {
				http.NotFound(w, r)
				return
			}
		}
		switch {
		case r.URL.Path == "/meta-release-lts":
			w.Write([]byte("Dist: noble\nVersion: 24.04.3 LTS\nSupported: 1\n\nDist: resolute\nName: Resolute Raccoon\nVersion: 26.04.1 LTS\nSupported: 1\n"))
		case r.URL.Path == "/sury/Packages.gz":
			w.Write(packages)
		case strings.HasSuffix(r.URL.Path, "/InRelease"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setupDB(t *testing.T, versions ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE websites (id TEXT PRIMARY KEY, php_version TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i, v := range versions {
		if _, err := db.Exec(`INSERT INTO websites (id, php_version) VALUES (?, ?)`, string(rune('a'+i)), v); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func newTestService(t *testing.T, f *fakeServer, db *sql.DB, srv *httptest.Server) *Service {
	t.Helper()
	s := NewService(f.executor(), db, nil)
	s.http = srv.Client()
	s.metaReleaseURL = srv.URL + "/meta-release-lts"
	s.suryPackagesURL = srv.URL + "/sury/Packages.gz"
	return s
}

func checkByID(p *Preflight, id string) Check {
	for _, c := range p.Checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestPreflightHealthyServerIsNotBlocked(t *testing.T) {
	srv := repoServer(t)
	f := newFakeServer()
	f.sources = "==> /etc/apt/sources.list.d/sury-php.list\ndeb [signed-by=/k.gpg] " + srv.URL + "/sury-repo/ noble main\n"
	f.mysqlNative = "0"
	s := newTestService(t, f, setupDB(t, "8.3", "8.3", ""), srv)

	p, err := s.Preflight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked {
		t.Fatalf("healthy server blocked: %+v", p.Checks)
	}
	for _, id := range []string{"release", "available", "pending_updates", "reboot_required", "dpkg", "disk", "sources", "php", "mysql_native_password"} {
		if c := checkByID(p, id); c.Status != CheckOK {
			t.Errorf("%s = %+v", id, c)
		}
	}
	if strings.Join(p.phpVersions, ",") != "8.3" {
		t.Errorf("php versions = %v", p.phpVersions)
	}
}

func TestPreflightBlocks(t *testing.T) {
	cases := []struct {
		name  string
		check string
		setup func(f *fakeServer)
	}{
		{"pending updates", "pending_updates", func(f *fakeServer) {
			f.pendingLines = "Inst libc6 [2.39-0ubuntu8.5] (2.39-0ubuntu8.6 Ubuntu:24.04/noble-updates)\nConf libc6\n"
		}},
		{"reboot pending", "reboot_required", func(f *fakeServer) { f.rebootPending = true }},
		{"dpkg broken", "dpkg", func(f *fakeServer) { f.dpkgAudit = "The following packages are only half configured" }},
		{"root disk", "disk", func(f *fakeServer) { f.dfOutput = "Avail\n1000000000\n900000000\n" }},
		{"boot disk", "disk", func(f *fakeServer) { f.dfOutput = "Avail\n20000000000\n100000000\n" }},
		{"already running", "panel_task", func(f *fakeServer) { f.state = StateRunning; f.unitActive = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := repoServer(t)
			f := newFakeServer()
			f.mysqlMissing = true
			tc.setup(f)
			p, err := newTestService(t, f, setupDB(t), srv).Preflight(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !p.Blocked || checkByID(p, tc.check).Status != CheckBlock {
				t.Fatalf("%s not blocked: %+v", tc.check, p.Checks)
			}
		})
	}
}

func TestPreflightOnlyOffersNoble(t *testing.T) {
	srv := repoServer(t)
	for _, codename := range []string{"jammy", "resolute"} {
		f := newFakeServer()
		f.codename = codename
		p, _ := newTestService(t, f, setupDB(t), srv).Preflight(context.Background())
		if !p.Blocked || len(p.Checks) != 1 || p.Checks[0].ID != "release" {
			t.Errorf("%s: %+v", codename, p.Checks)
		}
	}
}

func TestPreflightNotOfferedYetBlocks(t *testing.T) {
	srv := repoServer(t, "/meta-release-lts")
	f := newFakeServer()
	f.mysqlMissing = true
	p, _ := newTestService(t, f, setupDB(t), srv).Preflight(context.Background())
	if checkByID(p, "available").Status != CheckBlock {
		t.Fatalf("available = %+v", checkByID(p, "available"))
	}
}

func TestPreflightPHPVersionMissingForResoluteBlocks(t *testing.T) {
	srv := repoServer(t)
	f := newFakeServer()
	f.mysqlMissing = true
	p, _ := newTestService(t, f, setupDB(t, "8.3", "7.4"), srv).Preflight(context.Background())
	c := checkByID(p, "php")
	if c.Status != CheckBlock || !strings.Contains(c.Message, "7.4") {
		t.Fatalf("php = %+v", c)
	}
}

// A panel repository without resolute blocks; any other source without it
// only warns (the upgrader disables it and keeps its packages).
func TestPreflightSources(t *testing.T) {
	srv := repoServer(t, "/dists/resolute-pgdg/", "/thirdparty/")
	f := newFakeServer()
	f.mysqlMissing = true
	// The test server's host stands in for every repository; classification
	// uses hosts, so rewrite them per case.
	thirdParty := "==> /etc/apt/sources.list.d/x.list\ndeb " + srv.URL + "/thirdparty noble main\n"
	f.sources = thirdParty
	p, _ := newTestService(t, f, setupDB(t), srv).Preflight(context.Background())
	if c := checkByID(p, "sources"); c.Status != CheckWarn || !strings.Contains(c.Message, "/thirdparty") {
		t.Fatalf("third-party source = %+v", c)
	}
	if c := checkByID(p, "foreign_sources"); c.Status != CheckWarn {
		t.Fatalf("foreign_sources = %+v", c)
	}
	if p.Blocked {
		t.Fatalf("a third-party source must not block: %+v", p.Checks)
	}

	s := newTestService(t, f, setupDB(t), srv)
	s.http = &http.Client{Transport: rewriteHost{to: srv.URL, base: srv.Client().Transport}}
	f.sources = "==> /etc/apt/sources.list.d/pgdg.sources\nTypes: deb\nURIs: https://apt.postgresql.org/pub/repos/apt\nSuites: noble-pgdg\nComponents: main\n"
	p, _ = s.Preflight(context.Background())
	if c := checkByID(p, "sources"); c.Status != CheckBlock || !strings.Contains(c.Message, "resolute-pgdg") {
		t.Fatalf("pgdg without resolute = %+v", c)
	}
}

// rewriteHost sends every request to the test server, keeping the path.
type rewriteHost struct {
	to   string
	base http.RoundTripper
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	target, _ := http.NewRequestWithContext(req.Context(), req.Method, r.to+u.Path, nil)
	return r.base.RoundTrip(target)
}

func TestPreflightMySQLNativePasswordWarns(t *testing.T) {
	srv := repoServer(t)
	f := newFakeServer()
	f.mysqlNative = "2"
	p, _ := newTestService(t, f, setupDB(t), srv).Preflight(context.Background())
	if c := checkByID(p, "mysql_native_password"); c.Status != CheckWarn || !p.mysqlNative {
		t.Fatalf("mysql = %+v native=%v", c, p.mysqlNative)
	}
	if p.Blocked {
		t.Fatal("mysql_native_password must only warn")
	}
}

func TestStartValidatesConfirmation(t *testing.T) {
	srv := repoServer(t)
	f := newFakeServer()
	f.mysqlMissing = true
	s := newTestService(t, f, setupDB(t), srv)
	for _, req := range []StartRequest{
		{SnapshotConfirmed: false, Confirm: "26.04"},
		{SnapshotConfirmed: true, Confirm: "26.04.1"},
		{SnapshotConfirmed: true, Confirm: ""},
	} {
		err := s.Start(context.Background(), req)
		var de *model.DomainError
		if !errors.As(err, &de) || de.Code != "VALIDATION_ERROR" {
			t.Errorf("Start(%+v) = %v, want validation error", req, err)
		}
	}
	if len(f.inputs) != 0 {
		t.Error("nothing may be written before the confirmation is valid")
	}
}

func TestStartRefusesWhenBlocked(t *testing.T) {
	srv := repoServer(t)
	f := newFakeServer()
	f.mysqlMissing = true
	f.rebootPending = true
	err := newTestService(t, f, setupDB(t), srv).Start(context.Background(), StartRequest{SnapshotConfirmed: true, Confirm: "26.04"})
	if err == nil || !strings.Contains(err.Error(), "reboot") {
		t.Fatalf("Start = %v", err)
	}
	for _, call := range f.sudoCalls {
		if call[0] == "systemd-run" {
			t.Fatal("a blocked upgrade must not start")
		}
	}
}

func TestStartLaunchesTheUnitWithInputs(t *testing.T) {
	srv := repoServer(t)
	f := newFakeServer()
	f.mysqlNative = "1"
	s := newTestService(t, f, setupDB(t, "8.4", "8.3"), srv)
	if err := s.Start(context.Background(), StartRequest{SnapshotConfirmed: true, Confirm: " 26.04 "}); err != nil {
		t.Fatal(err)
	}
	if f.inputs[StateDir+"/php-versions"] != "8.3\n8.4\n" {
		t.Errorf("php-versions = %q", f.inputs[StateDir+"/php-versions"])
	}
	if f.inputs[StateDir+"/mysql-native"] != "1\n" {
		t.Errorf("mysql-native = %q", f.inputs[StateDir+"/mysql-native"])
	}
	var run []string
	for _, call := range f.sudoCalls {
		if call[0] == "systemd-run" {
			run = call
		}
	}
	if len(run) == 0 || run[1] != "--unit="+UnitName || run[len(run)-1] != UpgradeScript() || run[len(run)-3] != "/bin/bash" {
		t.Fatalf("systemd-run call = %v", run[:min(len(run), 6)])
	}
}

func TestStatusMapsAVanishedUnitToFailed(t *testing.T) {
	f := newFakeServer()
	f.state = StateRunning
	f.unitActive = false
	s := NewService(f.executor(), nil, nil)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateFailed || st.Running {
		t.Fatalf("status = %+v", st)
	}

	f.unitActive = true
	st, _ = s.Status(context.Background())
	if st.State != StateRunning || !st.Running {
		t.Fatalf("status = %+v", st)
	}

	f.state = ""
	f.unitActive = false
	st, _ = s.Status(context.Background())
	if st.State != StateIdle {
		t.Fatalf("status = %+v", st)
	}
}

func TestRepairOnlyAfterFailure(t *testing.T) {
	f := newFakeServer()
	s := NewService(f.executor(), nil, nil)
	var got [][]string
	s.runTask = func(name string, commands [][]string) string {
		got = commands
		return "task-1"
	}
	f.state = StateRebootRequired
	if _, err := s.Repair(context.Background()); err == nil {
		t.Fatal("repair must be refused unless the upgrade failed")
	}
	f.state = StateFailed
	id, err := s.Repair(context.Background())
	if err != nil || id != "task-1" {
		t.Fatalf("Repair = %q, %v", id, err)
	}
	joined := strings.Join(got[0], " ") + " | " + strings.Join(got[1], " ")
	for _, want := range []string{"dpkg --configure -a", "apt-get -f install -y", "--force-confold"} {
		if !strings.Contains(joined, want) {
			t.Errorf("repair commands missing %q: %s", want, joined)
		}
	}
}

func TestParseMetaRelease(t *testing.T) {
	content := "Dist: noble\nVersion: 24.04.3 LTS\nSupported: 1\n\nDist: resolute\nVersion: 26.04.1 LTS\nSupported: 0\n"
	if v, ok := parseMetaRelease(content, "resolute"); v != "26.04.1 LTS" || ok {
		t.Errorf("got %q %v", v, ok)
	}
	if v, _ := parseMetaRelease(content, "questing"); v != "" {
		t.Errorf("unknown dist = %q", v)
	}
}

func TestStatusClearsRebootRequiredAfterReboot(t *testing.T) {
	f := newFakeServer()
	f.state = StateRebootRequired
	f.bootID = "boot-a"
	f.currentBootID = "boot-a"
	s := NewService(f.executor(), nil, nil)
	if st, _ := s.Status(context.Background()); st.State != StateRebootRequired {
		t.Fatalf("before reboot: %+v", st)
	}
	f.currentBootID = "boot-b"
	if st, _ := s.Status(context.Background()); st.State != StateIdle {
		t.Fatalf("after reboot: %+v", st)
	}
}

// Every check ID the preflight can report needs a label in the UI
// dictionary (web/src/lib/i18n/domains/relupg.ts).
func TestEveryCheckHasAUILabel(t *testing.T) {
	dict, err := os.ReadFile("../../web/src/lib/i18n/domains/relupg.ts")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("preflight.go")
	if err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`p\.add\("([a-z_]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(ids) < 10 {
		t.Fatalf("found only %d check calls", len(ids))
	}
	for _, m := range ids {
		key := "'relupg.check." + m[1] + "'"
		if strings.Count(string(dict), key) != 2 {
			t.Errorf("%s must be in both the en and id dictionaries", key)
		}
	}
}
