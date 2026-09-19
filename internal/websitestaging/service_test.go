package websitestaging

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	"github.com/mohammadirham37/jenderal_panel/internal/website"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestService(t *testing.T, db *sql.DB, exec *executor.MockExecutor) *Service {
	t.Helper()
	websiteSvc := website.NewService(db, exec, audit.NewService(db))
	svc := NewService(db, exec, websiteSvc, nil, taskrunner.New(), audit.NewService(db))
	svc.provisionPollInterval = 10 * time.Millisecond
	svc.provisionTimeout = 150 * time.Millisecond
	svc.taskPollInterval = 10 * time.Millisecond
	return svc
}

func insertSourceSite(t *testing.T, db *sql.DB, domain, phpVersion string) model.Website {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	w := model.Website{
		ID:           "01SRC" + domain,
		Domain:       domain,
		AppType:      "wordpress",
		PHPVersion:   phpVersion,
		DocumentRoot: "/home/" + domain + "/public",
		WebUser:      domain + "_u",
		Status:       "active",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	_, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Domain, w.AppType, w.PHPVersion, w.DocumentRoot, w.WebUser, w.Status, now, now,
	)
	if err != nil {
		t.Fatalf("insert source site: %v", err)
	}
	return w
}

func TestStartCloneCreatesTargetAndRecordsTimeout(t *testing.T) {
	db := setupDB(t)
	exec := &executor.MockExecutor{
		RunFunc:     func(ctx context.Context, name string, args ...string) (*executor.Result, error) { return &executor.Result{}, nil },
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) { return &executor.Result{}, nil },
	}
	svc := newTestService(t, db, exec)
	ctx := context.Background()

	src := insertSourceSite(t, db, "example.com", "8.2")

	clone, err := svc.StartClone(ctx, src.ID, "Staging.Example.com ", "01CALLER")
	if err != nil {
		t.Fatalf("start clone: %v", err)
	}
	if clone.Status != StatusWaitingProvision {
		t.Errorf("expected initial status waiting_provision, got %s", clone.Status)
	}
	if clone.TargetDomain != "staging.example.com" {
		t.Errorf("expected normalized staging domain, got %s", clone.TargetDomain)
	}

	// The staging site is created as a config-only static site owned by the
	// caller, with the same PHP version as the source.
	target, err := svc.websites.Get(ctx, clone.TargetWebsiteID)
	if err != nil {
		t.Fatalf("get target site: %v", err)
	}
	if target.SetupMode != website.SetupConfigOnly || target.AppType != "static" {
		t.Errorf("expected config-only static staging site, got %s/%s", target.SetupMode, target.AppType)
	}
	if target.PHPVersion != "8.2" {
		t.Errorf("expected source PHP version to carry over, got %s", target.PHPVersion)
	}
	if target.CreatedBy != "01CALLER" {
		t.Errorf("expected caller to own the staging site, got %q", target.CreatedBy)
	}

	// The target stays pending (no provisioner), so the waiter must give up
	// and mark the clone failed with a timeout error.
	deadline := time.Now().Add(3 * time.Second)
	for {
		clones, err := svc.ListClones(ctx, src.ID)
		if err != nil {
			t.Fatalf("list clones: %v", err)
		}
		if len(clones) == 1 && clones[0].Status == StatusFailed {
			if !strings.Contains(clones[0].Error, "timed out") {
				t.Errorf("expected timeout error, got %q", clones[0].Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("clone never reached failed status; last=%+v", clones)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStartCloneRejectsSameDomain(t *testing.T) {
	db := setupDB(t)
	exec := &executor.MockExecutor{
		RunFunc:     func(ctx context.Context, name string, args ...string) (*executor.Result, error) { return &executor.Result{}, nil },
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) { return &executor.Result{}, nil },
	}
	svc := newTestService(t, db, exec)

	src := insertSourceSite(t, db, "example.com", "8.2")

	if _, err := svc.StartClone(context.Background(), src.ID, "example.com", "01CALLER"); err == nil {
		t.Error("expected error when the staging domain equals the site domain")
	}
	if _, err := svc.StartClone(context.Background(), src.ID, "", "01CALLER"); err == nil {
		t.Error("expected error when the staging domain is empty")
	}
}

func TestWpConfigCommandsShape(t *testing.T) {
	target := model.Website{
		Domain:       "staging.example.com",
		DocumentRoot: "/home/staging_example_com/public",
	}
	cmds := wpConfigCommands(target, "stg_example_abc123", "stg_example_abcd", "secretPass1")

	if len(cmds) != 3 {
		t.Fatalf("expected 3 sed commands, got %d", len(cmds))
	}
	sed := cmds[0]
	if sed[0] != "sed" || sed[4] != config0ExpectName() || sed[len(sed)-1] != target.DocumentRoot+"/wp-config.php" {
		t.Errorf("unexpected wp-config sed command: %v", sed)
	}
	if !strings.Contains(sed[4], "stg_example_abc123") {
		t.Errorf("expected staging db name in sed expression: %s", sed[4])
	}
}

func config0ExpectName() string {
	return `s|'DB_NAME',\s*'[^']*'|'DB_NAME', 'stg_example_abc123'|`
}

func TestEnvConfigCommandsShape(t *testing.T) {
	target := model.Website{
		Domain:       "staging.example.com",
		DocumentRoot: "/home/staging_example_com/public",
	}
	cmds := envConfigCommands(target, "stg_example_abc123", "stg_example_abcd", "secretPass1")

	if len(cmds) != 1 {
		t.Fatalf("expected 1 sed command, got %d", len(cmds))
	}
	sed := cmds[0]
	joined := strings.Join(sed, " ")
	for _, want := range []string{"APP_URL=https://staging.example.com", "DB_DATABASE=stg_example_abc123", "DB_USERNAME=stg_example_abcd", "DB_PASSWORD=secretPass1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in sed expressions: %s", want, joined)
		}
	}
}

func TestWpSearchReplaceCommandsRunAsTargetUser(t *testing.T) {
	src := model.Website{Domain: "example.com", WebUser: "example_com_u"}
	target := model.Website{
		Domain:       "staging.example.com",
		WebUser:      "staging_example_c",
		PHPVersion:   "8.2",
		DocumentRoot: "/home/staging_example_c/public",
	}
	cmds := wpSearchReplaceCommands(src, target)

	// mkdir, cp, chown then two search-replace runs and a cache flush.
	if len(cmds) != 6 {
		t.Fatalf("expected 6 commands, got %d", len(cmds))
	}
	sr := cmds[3]
	if sr[0] != "-u" || sr[1] != target.WebUser || sr[2] != "--" {
		t.Errorf("expected wp to run as the staging user, got %v", sr[:3])
	}
	if !strings.Contains(sr[4], "staging_example_c/.wp-cli/wp-cli.phar") {
		t.Errorf("expected the copied phar to be used, got %s", sr[4])
	}
	joined := strings.Join(sr, " ")
	for _, want := range []string{"search-replace", "http://example.com", "http://staging.example.com"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in wp invocation: %s", want, joined)
		}
	}
}

func TestSanitizeIdentifierAndHelpers(t *testing.T) {
	if got := sanitizeIdentifier("staging.my-site.co.id"); got != "staging" {
		t.Errorf("sanitizeIdentifier = %q", got)
	}
	if got := sanitizeIdentifier("my_site.example.com"); got != "my_site" {
		t.Errorf("sanitizeIdentifier = %q", got)
	}
	if got := truncate("abcdefghij", 4); got != "abcd" {
		t.Errorf("truncate = %q", got)
	}
	if got := unquote(`"db"`); got != `db` {
		t.Errorf("unquote = %q", got)
	}
	if got := unquote(`'secret'`); got != `secret` {
		t.Errorf("unquote = %q", got)
	}
	if got := unquote("plain"); got != "plain" {
		t.Errorf("unquote = %q", got)
	}
	if got := uncomment("value # comment"); got != "value" {
		t.Errorf("uncomment = %q", got)
	}
	tok, err := randomToken(24)
	if err != nil || len(tok) != 24 {
		t.Errorf("randomToken = %q, err %v", tok, err)
	}
}
