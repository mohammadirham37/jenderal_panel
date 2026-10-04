# Backup Remote Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Backups can live on remote storage (Google Drive native API, S3-compatible, rclone) with a disk-saving mode that removes the local file after a successful upload, and all backup operations (download, restore, delete, prune) become remote-aware.

**Architecture:** New pure-client package `internal/remotestorage` exposes a `Storage` interface (`Upload/Download/Delete/Test`) with three implementations; `internal/backup` keeps orchestration and depends only on the interface plus a settings-backed `ConfigStore`. The Backups page gains a Storage tab (moved from Settings) and per-row remote badges/actions.

**Tech Stack:** Go 1.x + Chi + SQLite (settings KV), Google Drive REST API v3 + OAuth 2.0 (copy-paste code flow), hand-rolled AWS SigV4, SvelteKit 5 runes + Tailwind v4 tokens.

**Spec:** `docs/superpowers/specs/2026-10-04-backup-remote-storage-design.md` — read it first; this plan argues from it.

## Global Constraints

- All system commands go through `executor.CommandExecutor`; privileged ones via `RunSudo*` — never `sh -c` with user input.
- Backend targets Linux; **do not** launch the app or test in a browser. Static gates only: `go test ./... -race`, `make lint`, `cd web && npm run check` (0 errors), `npm run build`, `npm test`.
- Every user-facing string goes through `translate($language, key)`; both `en` and `id` keys are required (TypeScript enforces parity in `web/src/lib/i18n/domains/*.ts`).
- Use Tailwind tokens remapped in `web/src/app.css` (`gray-*`, `blue-*`, …), never raw colors.
- HTTP via `httputil.JSON` / `httputil.HandleError` / `model.NewValidationError`; mutating requests are CSRF-protected by middleware.
- SQLite migrations are idempotent, one file per change, executed once (tracked in `schema_migrations`).
- Long operations return a task ID via `taskrunner.RunFuncWithOptions`; frontend uses `bind:taskId` + `storageKey` on `TaskProgress`.
- Never block main: background workers `go func()`.

---

### Task 1: `remote_only` column + model plumbing

**Files:**
- Create: `internal/database/migrations/043_backup_remote_only.sql`
- Modify: `internal/model/models.go` (Backup struct, ~line 377-392)
- Modify: `internal/backup/service.go` (`listWhere` ~763, `Get` ~786, `scanBackup` ~1023, `scanBackupRow` ~1041, `finishBackupScan` ~1058)
- Test: `internal/backup/service_test.go`

**Interfaces:**
- Produces: `model.Backup.RemoteOnly bool` with JSON tag `remote_only`; DB column `backups.remote_only` (INTEGER 0/1). All later tasks read/write this.

- [ ] **Step 1: Write the failing test**

Append to `internal/backup/service_test.go`:

```go
func TestRemoteOnlyRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO backups (id, type, path, status, kind, created_by, created_at, remote_path, remote_only)
		VALUES ('b-1', 'website', '/tmp/x.tar.gz', 'completed', 'manual', '', ?, 'gdrive://abc123', 1)`, now)
	if err != nil {
		t.Fatalf("insert backup: %v", err)
	}

	b, err := svc.Get(context.Background(), "b-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !b.RemoteOnly {
		t.Error("expected RemoteOnly true")
	}
	if b.RemotePath != "gdrive://abc123" {
		t.Errorf("RemotePath = %q", b.RemotePath)
	}

	list, err := svc.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v (%d rows)", err, len(list))
	}
	if !list[0].RemoteOnly {
		t.Error("list row lost remote_only")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run TestRemoteOnlyRoundTrip -v`
Expected: FAIL — "no such column: remote_only" (or scan error).

- [ ] **Step 3: Write the migration**

Create `internal/database/migrations/043_backup_remote_only.sql`:

```sql
-- Disk-saving mode: after a successful off-site upload the local file is
-- removed and the backup is marked remote-only (the remote copy is the only
-- remaining one; download/restore/delete must go through the remote backend).
ALTER TABLE backups ADD COLUMN remote_only INTEGER NOT NULL DEFAULT 0;
```

- [ ] **Step 4: Add the model field**

In `internal/model/models.go`, inside `type Backup struct`, after `RemotePath string`:

```go
	RemotePath string    `json:"remote_path"`
	RemoteOnly bool      `json:"remote_only"`
```

- [ ] **Step 5: Wire the column through the queries and scans**

In `internal/backup/service.go`:

1. `listWhere` and `Get` SELECT lists: add `remote_path, remote_only, created_at` (replace the trailing `remote_path, created_at`).

```go
		`SELECT id, type, target, storage, path, size_bytes, status, error_msg,
		        kind, created_by, task_id, started_at, finished_at, remote_path, remote_only, created_at
		 FROM backups`+where+` ORDER BY created_at DESC`, args...)
```

2. In `scanBackup` and `scanBackupRow` add `var remoteOnly int` to the var block and extend the Scan to end with `&remotePath, &remoteOnly, &createdStr,`.
3. `finishBackupScan`: add parameter `remoteOnly int` after `remotePath sql.NullString` and set `b.RemoteOnly = remoteOnly == 1` inside; update both call sites to pass it.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/backup/ -v`
Expected: PASS (new test + all existing).

- [ ] **Step 7: Commit**

```bash
git add internal/database/migrations/043_backup_remote_only.sql internal/model/models.go internal/backup/service.go internal/backup/service_test.go
git commit -m "feat(backup): add remote_only flag to backups"
```

---

### Task 2: `internal/remotestorage` — config store, interface, factory

**Files:**
- Create: `internal/remotestorage/storage.go`
- Create: `internal/remotestorage/config.go`
- Test: `internal/remotestorage/config_test.go`

**Interfaces:**
- Produces (used by every later task):

```go
package remotestorage

type Config struct { ... }        // see below
func (c Config) Enabled() bool
func ParseRemoteRef(ref string) (backend, name string, ok bool)
var ErrNotConfigured = errors.New("remote storage is not configured")
type Storage interface {
	Upload(ctx context.Context, localPath string, size int64, name string) (string, error)
	Download(ctx context.Context, name string, w io.Writer) error
	Delete(ctx context.Context, name string) error
	Test(ctx context.Context) (string, error)
}
func New(cfg Config, exec executor.CommandExecutor, httpClient *http.Client) (Storage, error)
type ConfigStore struct{ db *sql.DB }
func NewConfigStore(db *sql.DB) *ConfigStore
func (s *ConfigStore) Load(ctx context.Context) (Config, error)
func (s *ConfigStore) Save(ctx context.Context, cfg Config) error   // empty secrets preserve stored values
```

- [ ] **Step 1: Write the failing test**

Create `internal/remotestorage/config_test.go`:

```go
package remotestorage

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testStore(t *testing.T) *ConfigStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT)`); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	return NewConfigStore(db)
}

func TestConfigStoreRoundTripAndSecretPreservation(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	cfg := Config{Type: "s3", Endpoint: "https://s3.wasabisys.com", Bucket: "panel",
		Region: "ap-southeast-1", AccessKey: "AKID", SecretKey: "sekrit", Prefix: "vps1", UseTLS: true}
	if err := store.Save(ctx, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Save again with an empty secret: the stored value must survive.
	cfg2 := cfg
	cfg2.SecretKey = ""
	cfg2.Prefix = "vps2"
	if err := store.Save(ctx, cfg2); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	got, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.SecretKey != "sekrit" {
		t.Errorf("empty secret must preserve stored value, got %q", got.SecretKey)
	}
	if got.Prefix != "vps2" {
		t.Errorf("prefix = %q, want vps2", got.Prefix)
	}
	if got.Region != "ap-southeast-1" || !got.UseTLS {
		t.Errorf("region/tls lost: %+v", got)
	}
}

func TestConfigEnabled(t *testing.T) {
	s3 := Config{Type: "s3", Endpoint: "https://e", Bucket: "b", AccessKey: "a", SecretKey: "s"}
	if !s3.Enabled() {
		t.Error("complete s3 config must be enabled")
	}
	if (Config{Type: "s3", Endpoint: "https://e"}).Enabled() {
		t.Error("partial s3 config must not be enabled")
	}
	gd := Config{Type: "gdrive", GDriveClientID: "id", GDriveClientSecret: "sec", GDriveRefreshToken: "rt"}
	if !gd.Enabled() {
		t.Error("connected gdrive config must be enabled")
	}
	if (Config{Type: "gdrive", GDriveClientID: "id"}).Enabled() {
		t.Error("gdrive without refresh token must not be enabled")
	}
	if (Config{Type: "rclone", RcloneRemote: "gdrive"}).Enabled() != true {
		t.Error("rclone with remote must be enabled")
	}
	if (Config{Type: "s3"}).Enabled() {
		t.Error("empty config must not be enabled")
	}
}

func TestParseRemoteRef(t *testing.T) {
	if b, n, ok := ParseRemoteRef("gdrive://AbC123"); !ok || b != "gdrive" || n != "AbC123" {
		t.Errorf("gdrive ref: %q %q %v", b, n, ok)
	}
	if b, n, ok := ParseRemoteRef("https://s3.end.com/bucket/prefix/website/f.tar.gz"); !ok || b != "s3" || n != "prefix/website/f.tar.gz" {
		t.Errorf("s3 ref: %q %q %v", b, n, ok)
	}
	// Legacy rows stored by older versions without a scheme.
	if b, n, ok := ParseRemoteRef("s3.end.com/bucket/website/f.tar.gz"); !ok || b != "s3" || n != "website/f.tar.gz" {
		t.Errorf("legacy s3 ref: %q %q %v", b, n, ok)
	}
	if b, n, ok := ParseRemoteRef("gdrive:backups/f.tar.gz"); !ok || b != "rclone" || n != "gdrive:backups/f.tar.gz" {
		t.Errorf("rclone ref: %q %q %v", b, n, ok)
	}
	if _, _, ok := ParseRemoteRef(""); ok {
		t.Error("empty ref must not parse")
	}
}

func TestNewRejectsIncomplete(t *testing.T) {
	if _, err := New(Config{Type: "s3"}, nil, nil); err == nil {
		t.Error("incomplete s3 config must be rejected by New")
	}
	if _, err := New(Config{Type: "nope"}, nil, nil); err == nil {
		t.Error("unknown type must be rejected by New")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/remotestorage/ -v`
Expected: FAIL — package does not exist (build error).

- [ ] **Step 3: Write `storage.go`**

Create `internal/remotestorage/storage.go`:

```go
// Package remotestorage speaks to off-site backup destinations: S3-compatible
// object storage, Google Drive (native API), and rclone remotes. It is a pure
// storage client — orchestration (when to upload, what to record) lives in
// internal/backup.
package remotestorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// ErrNotConfigured is returned when no remote destination is set up.
var ErrNotConfigured = errors.New("remote storage is not configured")

// Storage is one configured remote destination. `name` is the object name
// relative to the destination root (e.g. "website/20260102_x.tar.gz");
// remote references returned by Upload are scheme-tagged so deletes and
// downloads keep working later:
//
//	S3:     https://<host>/<bucket>/<prefix>/<name>
//	Drive:  gdrive://<fileId>
//	rclone: <remote>:<path>/<name>
type Storage interface {
	Upload(ctx context.Context, localPath string, size int64, name string) (string, error)
	Download(ctx context.Context, name string, w io.Writer) error
	Delete(ctx context.Context, name string) error
	Test(ctx context.Context) (string, error)
}

// New builds a Storage for cfg. cfg must be complete for its type — callers
// check Enabled() first. httpClient may be nil (http.DefaultClient).
func New(cfg Config, exec executor.CommandExecutor, httpClient *http.Client) (Storage, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	switch cfg.Type {
	case "s3":
		if !cfg.Enabled() {
			return nil, fmt.Errorf("s3 config incomplete: endpoint, bucket, access key and secret key are required")
		}
		return &s3Storage{cfg: cfg, http: httpClient, exec: exec}, nil
	case "gdrive":
		if cfg.GDriveClientID == "" || cfg.GDriveClientSecret == "" {
			return nil, fmt.Errorf("gdrive config incomplete: client id and client secret are required")
		}
		return NewGDrive(cfg, exec, httpClient), nil
	case "rclone":
		if cfg.RcloneRemote == "" {
			return nil, fmt.Errorf("rclone config incomplete: remote name is required")
		}
		return &rcloneStorage{cfg: cfg, exec: exec}, nil
	case "":
		return nil, ErrNotConfigured
	default:
		return nil, fmt.Errorf("unsupported remote type: %s", cfg.Type)
	}
}

// ParseRemoteRef splits a stored remote reference into its backend type and
// object name. Legacy rows written before scheme tagging (a bare
// "host/bucket/key") are recognised as S3.
func ParseRemoteRef(ref string) (backend, name string, ok bool) {
	switch {
	case strings.HasPrefix(ref, "gdrive://"):
		id := strings.TrimPrefix(ref, "gdrive://")
		if id == "" {
			return "", "", false
		}
		return "gdrive", id, true
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		u, err := url.Parse(ref)
		if err != nil {
			return "", "", false
		}
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
		if len(parts) != 2 || parts[1] == "" {
			return "", "", false
		}
		return "s3", parts[1], true
	case strings.Contains(ref, ":"):
		return "rclone", ref, true
	default:
		parts := strings.SplitN(ref, "/", 3)
		if len(parts) == 3 && parts[2] != "" {
			return "s3", parts[2], true
		}
		return "", "", false
	}
}
```

- [ ] **Step 4: Write `config.go`**

Create `internal/remotestorage/config.go`:

```go
package remotestorage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Settings keys. S3 and rclone keys are unchanged from the previous
// implementation so existing installations keep working.
const (
	keyType        = "backup_remote_type"
	keyEndpoint    = "backup_remote_s3_endpoint"
	keyBucket      = "backup_remote_s3_bucket"
	keyRegion      = "backup_remote_s3_region"
	keyAccessKey   = "backup_remote_s3_access_key"
	keySecretKey   = "backup_remote_s3_secret_key"
	keyPrefix      = "backup_remote_s3_prefix"
	keyRcloneRemote = "backup_remote_rclone_remote"
	keyRclonePath  = "backup_remote_rclone_path"

	keyGDClientID     = "backup_remote_gdrive_client_id"
	keyGDClientSecret = "backup_remote_gdrive_client_secret"
	keyGDRefreshToken = "backup_remote_gdrive_refresh_token"
	keyGDFolderID     = "backup_remote_gdrive_folder_id"

	keyDeleteLocal = "backup_remote_delete_local"
)

// Config is the remote storage configuration.
type Config struct {
	Type string `json:"type"` // "" | "s3" | "gdrive" | "rclone"

	// S3
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Prefix    string `json:"prefix"`
	UseTLS    bool   `json:"use_tls"`

	// Google Drive
	GDriveClientID     string `json:"gdrive_client_id"`
	GDriveClientSecret string `json:"gdrive_client_secret"`
	GDriveRefreshToken string `json:"gdrive_refresh_token"`
	GDriveFolderID     string `json:"gdrive_folder_id"`

	// rclone
	RcloneRemote string `json:"rclone_remote"`
	RclonePath   string `json:"rclone_path"`

	// Behavior: remove the local file after a successful upload.
	DeleteLocalAfterUpload bool `json:"delete_local_after_upload"`
}

// Enabled reports whether cfg is complete enough to upload with.
func (c Config) Enabled() bool {
	switch c.Type {
	case "s3":
		return c.Endpoint != "" && c.Bucket != "" && c.AccessKey != "" && c.SecretKey != ""
	case "gdrive":
		return c.GDriveClientID != "" && c.GDriveClientSecret != "" && c.GDriveRefreshToken != ""
	case "rclone":
		return c.RcloneRemote != ""
	default:
		return false
	}
}

// ConfigStore loads and saves Config in the settings KV table.
type ConfigStore struct {
	db *sql.DB
}

// NewConfigStore creates a ConfigStore.
func NewConfigStore(db *sql.DB) *ConfigStore {
	return &ConfigStore{db: db}
}

// Load reads the configuration; missing keys leave zero values.
func (s *ConfigStore) Load(ctx context.Context) (Config, error) {
	cfg := Config{Region: "us-east-1", UseTLS: true}
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value FROM settings WHERE key LIKE 'backup_remote%'`)
	if err != nil {
		return cfg, fmt.Errorf("read remote config: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return cfg, fmt.Errorf("scan remote config: %w", err)
		}
		switch key {
		case keyType:
			cfg.Type = value
		case keyEndpoint:
			cfg.Endpoint = value
			if strings.HasPrefix(value, "http://") {
				cfg.UseTLS = false
			}
		case keyBucket:
			cfg.Bucket = value
		case keyRegion:
			if value != "" {
				cfg.Region = value
			}
		case keyAccessKey:
			cfg.AccessKey = value
		case keySecretKey:
			cfg.SecretKey = value
		case keyPrefix:
			cfg.Prefix = strings.Trim(value, "/")
		case keyGDClientID:
			cfg.GDriveClientID = value
		case keyGDClientSecret:
			cfg.GDriveClientSecret = value
		case keyGDRefreshToken:
			cfg.GDriveRefreshToken = value
		case keyGDFolderID:
			cfg.GDriveFolderID = value
		case keyRcloneRemote:
			cfg.RcloneRemote = value
		case keyRclonePath:
			cfg.RclonePath = strings.Trim(value, "/")
		case keyDeleteLocal:
			cfg.DeleteLocalAfterUpload = value == "1"
		}
	}
	return cfg, rows.Err()
}

// Save upserts the configuration. Empty secret fields (S3 secret key, Drive
// client secret) preserve the stored values so the API never needs to echo
// secrets back to the browser. The Drive refresh token is not touched here —
// it is written only by the OAuth exchange.
func (s *ConfigStore) Save(ctx context.Context, cfg Config) error {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.SecretKey == "" || cfg.GDriveClientSecret == "" {
		old, err := s.Load(ctx)
		if err != nil {
			return fmt.Errorf("read previous config: %w", err)
		}
		if cfg.SecretKey == "" {
			cfg.SecretKey = old.SecretKey
		}
		if cfg.GDriveClientSecret == "" {
			cfg.GDriveClientSecret = old.GDriveClientSecret
		}
	}

	pairs := map[string]string{
		keyType:          cfg.Type,
		keyEndpoint:      cfg.Endpoint,
		keyBucket:        cfg.Bucket,
		keyRegion:        cfg.Region,
		keyAccessKey:     cfg.AccessKey,
		keySecretKey:     cfg.SecretKey,
		keyPrefix:        strings.Trim(cfg.Prefix, "/"),
		keyGDClientID:    cfg.GDriveClientID,
		keyGDClientSecret: cfg.GDriveClientSecret,
		keyGDFolderID:    cfg.GDriveFolderID,
		keyRcloneRemote:  cfg.RcloneRemote,
		keyRclonePath:    strings.Trim(cfg.RclonePath, "/"),
		keyDeleteLocal:   boolString(cfg.DeleteLocalAfterUpload),
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range pairs {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			key, value, now,
		); err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
	}
	return nil
}

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
```

- [ ] **Step 5: Create stubs so the package compiles**

The test file references `s3Storage`, `GDriveStorage`/`NewGDrive`, and `rcloneStorage` — created in Tasks 3–5. For this task's gate only, create minimal stubs that make `New` compile; they are replaced by the real implementations next:

Create `internal/remotestorage/backends_stub.go`:

```go
package remotestorage

import (
	"context"
	"io"
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

type s3Storage struct {
	cfg  Config
	http interface{}
	exec executor.CommandExecutor
}

func (s *s3Storage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	return "", ErrNotConfigured
}
func (s *s3Storage) Download(ctx context.Context, name string, w io.Writer) error {
	return ErrNotConfigured
}
func (s *s3Storage) Delete(ctx context.Context, name string) error { return ErrNotConfigured }
func (s *s3Storage) Test(ctx context.Context) (string, error)      { return "", ErrNotConfigured }
```

(Do NOT stub gdrive/rclone yet — Task 2's `New` references them. Add minimal ones in the same file:)

```go
type GDriveStorage struct {
	cfg  Config
	exec executor.CommandExecutor
}

func NewGDrive(cfg Config, exec executor.CommandExecutor, hc *http.Client) *GDriveStorage {
	return &GDriveStorage{cfg: cfg, exec: exec}
}
func (g *GDriveStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	return "", ErrNotConfigured
}
func (g *GDriveStorage) Download(ctx context.Context, name string, w io.Writer) error {
	return ErrNotConfigured
}
func (g *GDriveStorage) Delete(ctx context.Context, name string) error { return ErrNotConfigured }
func (g *GDriveStorage) Test(ctx context.Context) (string, error)      { return "", ErrNotConfigured }

type rcloneStorage struct {
	cfg  Config
	exec executor.CommandExecutor
}

func (r *rcloneStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	return "", ErrNotConfigured
}
func (r *rcloneStorage) Download(ctx context.Context, name string, w io.Writer) error {
	return ErrNotConfigured
}
func (r *rcloneStorage) Delete(ctx context.Context, name string) error { return ErrNotConfigured }
func (r *rcloneStorage) Test(ctx context.Context) (string, error)      { return "", ErrNotConfigured }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/remotestorage/ -v`
Expected: PASS. Run `go build ./...` too.

- [ ] **Step 7: Commit**

```bash
git add internal/remotestorage/
git commit -m "feat(storage): remotestorage package with config store and storage interface"
```

---

### Task 3: S3 backend (SigV4 port + download/delete/test)

**Files:**
- Create: `internal/remotestorage/s3.go` (replaces the stub struct)
- Delete: `internal/remotestorage/backends_stub.go` (s3 part — keep gdrive/rclone stubs until their tasks; simplest: delete the s3Storage block from the stub file now, the rest when Tasks 4–5 land)
- Test: `internal/remotestorage/s3_test.go`

**Interfaces:**
- Consumes: `Config`, `Storage`, `executor.CommandExecutor` (from Task 2).
- Produces: `s3Storage` implementing `Storage`. Remote ref format: `https://<host>/<bucket>/<prefix>/<name>`.

- [ ] **Step 1: Write the failing test**

Create `internal/remotestorage/s3_test.go`:

```go
package remotestorage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func s3TestConfig(endpoint string) Config {
	return Config{Type: "s3", Endpoint: endpoint, Bucket: "panel-backups",
		Region: "ap-southeast-1", AccessKey: "AKIDEXAMPLE", SecretKey: "secret",
		Prefix: "vps-1", UseTLS: strings.HasPrefix(endpoint, "https")}
}

func fakeExec(catData string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoStreamFunc: func(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
			if name == "cat" {
				n, _ := io.WriteString(w, catData)
				return n, nil
			}
			return 0, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func TestS3UploadDownloadDeleteRoundTrip(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		io.Copy(&gotBody, r.Body)
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.WriteHeader(200)
			io.WriteString(w, "backup-bytes")
		case http.MethodDelete:
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	st, err := New(s3TestConfig(srv.URL), fakeExec("backup-bytes"), srv.Client())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ref, err := st.Upload(context.Background(), "/data/f.tar.gz", 12, "website/f.tar.gz")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if !strings.HasSuffix(ref, "/panel-backups/vps-1/website/f.tar.gz") {
		t.Errorf("ref = %q", ref)
	}
	if !strings.Contains(gotAuth, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/") {
		t.Errorf("upload not SigV4-signed: %q", gotAuth)
	}
	if gotBody.String() != "backup-bytes" {
		t.Errorf("uploaded body = %q", gotBody.String())
	}

	// Download by key parsed from the ref.
	backend, name, ok := ParseRemoteRef(ref)
	if !ok || backend != "s3" {
		t.Fatalf("ParseRemoteRef: %q %q %v", backend, name, ok)
	}
	var buf strings.Builder
	if err := st.Download(context.Background(), name, &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != "backup-bytes" {
		t.Errorf("downloaded = %q", buf.String())
	}

	if err := st.Delete(context.Background(), name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("delete method = %s", gotMethod)
	}
}

func TestS3TestListsBucket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "list-type=2") {
			w.WriteHeader(200)
			io.WriteString(w, `<ListBucketResult></ListBucketResult>`)
			return
		}
		w.WriteHeader(403)
	}))
	defer srv.Close()

	st, _ := New(s3TestConfig(srv.URL), fakeExec(""), srv.Client())
	info, err := st.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !strings.Contains(info, "panel-backups") {
		t.Errorf("Test info = %q", info)
	}
}

func TestS3UploadPropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, "AccessDenied")
	}))
	defer srv.Close()

	st, _ := New(s3TestConfig(srv.URL), fakeExec("x"), srv.Client())
	if _, err := st.Upload(context.Background(), "/x", 1, "x"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestS3SigningHelpers(t *testing.T) {
	cfg := s3TestConfig("https://s3.wasabisys.com")
	if got := uriEncodePath("website/example.com/file 1.tar.gz"); got != "website/example.com/file%201.tar.gz" {
		t.Errorf("uriEncodePath = %q", got)
	}
	if got := canonicalURI(cfg.s3ObjectURL("database/app db.sql")); got != "/panel-backups/vps-1/database/app%20db.sql" {
		t.Errorf("canonicalURI = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/remotestorage/ -run TestS3 -v`
Expected: FAIL (stub returns ErrNotConfigured).

- [ ] **Step 3: Implement `s3.go`**

Create `internal/remotestorage/s3.go` (remove the `s3Storage` stub block from `backends_stub.go`):

```go
package remotestorage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// S3 path-style addressing with AWS SigV4. UNSIGNED-PAYLOAD allows streamed
// uploads; GET/DELETE sign the empty-body hash. Works with every
// S3-compatible provider (AWS, Wasabi, R2, MinIO).

const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type s3Storage struct {
	cfg  Config
	http *http.Client
	exec executor.CommandExecutor
}

func (c Config) s3Host() string {
	host := strings.TrimPrefix(strings.TrimPrefix(c.Endpoint, "https://"), "http://")
	return strings.TrimSuffix(host, "/")
}

func (c Config) s3ObjectURL(objectKey string) string {
	scheme := "https"
	if !c.UseTLS {
		scheme = "http"
	}
	return scheme + "://" + c.s3Host() + "/" + c.Bucket + "/" + strings.TrimPrefix(c.Prefix, "/") + "/" + objectKey
}

func uriEncodePath(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '/' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func canonicalURI(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "/"
	}
	return uriEncodePath(u.Path)
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

const (
	amzDateFormat   = "20060102T150405Z"
	scopeDateFormat = "20060102"
)

// signS3Request returns the headers for an SigV4-authenticated request.
// payloadHash is "UNSIGNED-PAYLOAD" for streamed uploads or the empty-body
// hash for everything else.
func signS3Request(cfg Config, method, host, canonicalURI, canonicalQuery, payloadHash string, now time.Time) http.Header {
	amzDate := now.UTC().Format(amzDateFormat)
	dateStamp := now.UTC().Format(scopeDateFormat)

	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	key := hmacSHA256([]byte("AWS4"+cfg.SecretKey), []byte(dateStamp))
	key = hmacSHA256(key, []byte(cfg.Region))
	key = hmacSHA256(key, []byte("s3"))
	key = hmacSHA256(key, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(key, []byte(stringToSign)))

	header := http.Header{}
	header.Set("Host", host)
	header.Set("X-Amz-Date", amzDate)
	header.Set("X-Amz-Content-Sha256", payloadHash)
	header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cfg.AccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
	return header
}

// s3ObjectKey composes the full object key (prefix + name).
func (c Config) s3ObjectKey(name string) string {
	prefix := strings.TrimPrefix(c.Prefix, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// Upload streams the local file (opened as root — backup files are
// root-owned) into a SigV4-signed PUT. Large archives never buffer in memory.
func (s *s3Storage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	objectKey := s.cfg.s3ObjectKey(name)
	rawURL := s.cfg.s3ObjectURL(objectKey)
	host := s.cfg.s3Host()
	canonical := canonicalURI(rawURL)

	pr, pw := io.Pipe()
	go func() {
		_, err := s.exec.RunSudoStream(ctx, pw, "cat", localPath)
		_ = pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, pr)
	if err != nil {
		return "", fmt.Errorf("build upload request: %w", err)
	}
	for key, values := range signS3Request(s.cfg, http.MethodPut, host, canonical, "", "UNSIGNED-PAYLOAD", time.Now().UTC()) {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	req.ContentLength = size

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return rawURL, nil
}

func (s *s3Storage) do(ctx context.Context, method, rawURL, canonicalQuery string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for key, values := range signS3Request(s.cfg, method, s.cfg.s3Host(), canonicalURI(rawURL), canonicalQuery, emptyPayloadHash, time.Now().UTC()) {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return s.http.Do(req)
}

// Download streams the object into w.
func (s *s3Storage) Download(ctx context.Context, name string, w io.Writer) error {
	resp, err := s.do(ctx, http.MethodGet, s.cfg.s3ObjectURL(s.cfg.s3ObjectKey(name)), "")
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("download stream: %w", err)
	}
	return nil
}

// Delete removes the object.
func (s *s3Storage) Delete(ctx context.Context, name string) error {
	resp, err := s.do(ctx, http.MethodDelete, s.cfg.s3ObjectURL(s.cfg.s3ObjectKey(name)), "")
	if err != nil {
		return fmt.Errorf("delete request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Test lists one object to verify credentials and returns the bucket name.
func (s *s3Storage) Test(ctx context.Context) (string, error) {
	rawURL := s.cfg.s3ObjectURL("") + "?list-type=2&max-keys=1"
	resp, err := s.do(ctx, http.MethodGet, rawURL, "list-type=2&max-keys=1")
	if err != nil {
		return "", fmt.Errorf("test request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return "bucket " + s.cfg.Bucket + " (list ok)", nil
}
```

Note: `filepath` is not used in s3.go — do not import it. The file needs `executor` import for the `s3Storage.exec` field type: add `"github.com/mohammadirham37/jenderal_panel/internal/executor"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/remotestorage/ -v`
Expected: PASS (all S3 + config tests).

- [ ] **Step 5: Commit**

```bash
git add internal/remotestorage/
git commit -m "feat(storage): S3 backend with upload, download, delete, and connection test"
```

---

### Task 4: Google Drive backend (OAuth + resumable upload)

**Files:**
- Create: `internal/remotestorage/gdrive.go` (replaces the gdrive stub; the struct is exported as `GDriveStorage` because Task 9 uses it from the backup package)
- Modify: `internal/remotestorage/backends_stub.go` (remove the gdrive stub block)
- Test: `internal/remotestorage/gdrive_test.go`

**Interfaces:**
- Produces:

```go
func NewGDrive(cfg Config, exec executor.CommandExecutor, hc *http.Client) *GDriveStorage
func (g *GDriveStorage) ExchangeCode(ctx context.Context, code string) (refreshToken string, err error)
func (g *GDriveStorage) AuthorizeURL() string
func (g *GDriveStorage) EnsureFolder(ctx context.Context) (string, error)   // creates+returns folder id when cfg.GDriveFolderID is empty
```

Exported names are deliberate: Task 9's OAuth endpoint flow builds a `GDriveStorage` directly. Test hooks: `GDriveStorage` fields `tokenURL`, `apiBase`, `uploadBase`, `authURL` default to Google endpoints; tests override them.

- [ ] **Step 1: Write the failing test**

Create `internal/remotestorage/gdrive_test.go`:

```go
package remotestorage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func gdriveTestConfig() Config {
	return Config{Type: "gdrive", GDriveClientID: "cid", GDriveClientSecret: "csecret",
		GDriveRefreshToken: "rtok", GDriveFolderID: "folder-1"}
}

// newTestGDrive builds a GDriveStorage wired to a fake Google API server.
func newTestGDrive(t *testing.T, handler http.Handler) (*gdriveStorage, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	g := NewGDrive(gdriveTestConfig(), nil, srv.Client())
	g.tokenURL = srv.URL + "/token"
	g.apiBase = srv.URL + "/drive/v3"
	g.uploadBase = srv.URL + "/upload/drive/v3"
	g.authURL = srv.URL + "/auth"
	return g, srv
}

func tokenHandler(counter *int32) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(counter, 1)
		_ = r.ParseForm()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"access_token": "atok", "expires_in": 3600})
	}
}

func TestGDriveExchangeCode(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "atok", "expires_in": 3600, "refresh_token": "new-rtok",
		})
	}))
	defer srv.Close()

	g := NewGDrive(Config{Type: "gdrive", GDriveClientID: "cid", GDriveClientSecret: "csecret"}, nil, srv.Client())
	g.tokenURL = srv.URL + "/token"

	rt, err := g.ExchangeCode(context.Background(), "paste-d-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if rt != "new-rtok" {
		t.Errorf("refresh token = %q", rt)
	}
	if gotForm.Get("code") != "paste-d-code" || gotForm.Get("grant_type") != "authorization_code" ||
		gotForm.Get("redirect_uri") != loopbackRedirect || gotForm.Get("client_id") != "cid" {
		t.Errorf("token exchange form = %v", gotForm)
	}
}

func TestGDriveAuthorizeURLContainsScopeAndPrompt(t *testing.T) {
	g := NewGDrive(Config{Type: "gdrive", GDriveClientID: "cid"}, nil, nil)
	u := g.AuthorizeURL()
	if !strings.Contains(u, "client_id=cid") || !strings.Contains(u, "scope=drive.file") ||
		!strings.Contains(u, "access_type=offline") || !strings.Contains(u, "prompt=consent") {
		t.Errorf("authorize URL = %q", u)
	}
}

func TestGDriveUploadResumableAndDownloadDelete(t *testing.T) {
	var sessionPut int32
	var uploaded strings.Builder
	var dlServed, deleted int32

	handler := http.NewServeMux()
	var tokenCount int32
	handler.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) { tokenHandler(&tokenCount)(w, r) })
	handler.HandleFunc("/upload/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uploadType") == "resumable" {
			w.Header().Set("Location", "http://"+r.Host+"/session/1")
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(400)
	})
	handler.HandleFunc("/session/1", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sessionPut, 1)
		io.Copy(&uploaded, r.Body)
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"id": "file-9", "name": "f.tar.gz", "size": "12"})
	})
	handler.HandleFunc("/drive/v3/files/file-9", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			atomic.AddInt32(&dlServed, 1)
			io.WriteString(w, "backup-bytes")
			return
		}
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleted, 1)
			w.WriteHeader(204)
		}
	})

	g, _ := newTestGDrive(t, handler)

	ref, err := g.Upload(context.Background(), "/data/f.tar.gz", 12, "website/f.tar.gz")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if ref != "gdrive://file-9" {
		t.Errorf("ref = %q", ref)
	}
	if sessionPut != 1 || uploaded.String() != "backup-bytes" {
		t.Errorf("session PUT=%d body=%q", sessionPut, uploaded.String())
	}

	backend, name, ok := ParseRemoteRef(ref)
	if !ok || backend != "gdrive" || name != "file-9" {
		t.Fatalf("ParseRemoteRef: %v %q %v", backend, name, ok)
	}
	var buf strings.Builder
	if err := g.Download(context.Background(), name, &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != "backup-bytes" || dlServed != 1 {
		t.Errorf("downloaded %q dlServed=%d", buf.String(), dlServed)
	}
	if err := g.Delete(context.Background(), name); err != nil || deleted != 1 {
		t.Fatalf("Delete: %v deleted=%d", err, deleted)
	}
}

func TestGDriveTestReturnsEmail(t *testing.T) {
	handler := http.NewServeMux()
	var tokenCount int32
	handler.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) { tokenHandler(&tokenCount)(w, r) })
	handler.HandleFunc("/drive/v3/about", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"emailAddress": "me@gmail.com"}})
	})
	g, _ := newTestGDrive(t, handler)
	info, err := g.Test(context.Background())
	if err != nil || info != "me@gmail.com" {
		t.Fatalf("Test: %q %v", info, err)
	}
}

func TestGDriveEnsureFolderCreatesWhenEmpty(t *testing.T) {
	var created int32
	handler := http.NewServeMux()
	var tokenCount int32
	handler.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) { tokenHandler(&tokenCount)(w, r) })
	handler.HandleFunc("/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&created, 1)
			var meta map[string]any
			json.NewDecoder(r.Body).Decode(&meta)
			if meta["mimeType"] != "application/vnd.google-apps.folder" || meta["name"] != "Jenderal Panel Backups" {
				t.Errorf("folder metadata = %v", meta)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "folder-new"})
			return
		}
		w.WriteHeader(400)
	})
	g, _ := newTestGDrive(t, handler)
	g.cfg.GDriveFolderID = ""

	id, err := g.EnsureFolder(context.Background())
	if err != nil || id != "folder-new" || created != 1 {
		t.Fatalf("EnsureFolder: %q %v created=%d", id, err, created)
	}
	// With a folder already configured nothing is created.
	g.cfg.GDriveFolderID = "folder-1"
	if id, err := g.EnsureFolder(context.Background()); err != nil || id != "folder-1" {
		t.Fatalf("EnsureFolder existing: %q %v", id, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/remotestorage/ -run TestGDrive -v`
Expected: FAIL (stub).

- [ ] **Step 3: Implement `gdrive.go`**

Create `internal/remotestorage/gdrive.go` (remove the gdrive stub from `backends_stub.go`):

```go
package remotestorage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Google Drive via the Drive API v3 with the least-privileged drive.file
// scope: the panel can only access files it created itself. Authorization is
// the copy-paste code flow against an OAuth client of type "Desktop app"
// (loopback redirects are implicitly allowed, so no public domain is needed).

const (
	loopbackRedirect  = "http://localhost:1/"
	gdriveScope       = "https://www.googleapis.com/auth/drive.file"
	gdriveFolderName  = "Jenderal Panel Backups"
	gdriveFolderMIME  = "application/vnd.google-apps.folder"
)

type GDriveStorage struct {
	cfg  Config
	exec executor.CommandExecutor
	http *http.Client

	// Overridable for tests; default to Google endpoints.
	tokenURL   string
	apiBase    string
	uploadBase string
	authURL    string

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewGDrive(cfg Config, exec executor.CommandExecutor, hc *http.Client) *GDriveStorage {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &GDriveStorage{
		cfg: cfg, exec: exec, http: hc,
		tokenURL:   "https://oauth2.googleapis.com/token",
		apiBase:    "https://www.googleapis.com/drive/v3",
		uploadBase: "https://www.googleapis.com/upload/drive/v3",
		authURL:    "https://accounts.google.com/o/oauth2/v2/auth",
	}
}

// AuthorizeURL builds the Google consent URL the admin opens in a browser.
func (g *GDriveStorage) AuthorizeURL() string {
	q := url.Values{}
	q.Set("client_id", g.cfg.GDriveClientID)
	q.Set("redirect_uri", loopbackRedirect)
	q.Set("response_type", "code")
	q.Set("scope", gdriveScope)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	return g.authURL + "?" + q.Encode()
}

// ExchangeCode swaps a pasted authorization code for a refresh token.
func (g *GDriveStorage) ExchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", g.cfg.GDriveClientID)
	form.Set("client_secret", g.cfg.GDriveClientSecret)
	form.Set("redirect_uri", loopbackRedirect)
	form.Set("grant_type", "authorization_code")

	var out struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := g.postForm(ctx, g.tokenURL, form, &out); err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	if out.RefreshToken == "" {
		return "", fmt.Errorf("token exchange returned no refresh token (re-authorize with prompt=consent)")
	}
	return out.RefreshToken, nil
}

func (g *GDriveStorage) postForm(ctx context.Context, tokenEndpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

// accessToken returns a valid access token, refreshing it when missing or
// near expiry. One retry after a 401 is handled by callers via authedRequest.
func (g *GDriveStorage) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.accessToken != "" && time.Now().Before(g.tokenExpiry) {
		return g.accessToken, nil
	}
	if g.cfg.GDriveRefreshToken == "" {
		return "", fmt.Errorf("google drive is not connected yet")
	}
	form := url.Values{}
	form.Set("client_id", g.cfg.GDriveClientID)
	form.Set("client_secret", g.cfg.GDriveClientSecret)
	form.Set("refresh_token", g.cfg.GDriveRefreshToken)
	form.Set("grant_type", "refresh_token")

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := g.postForm(ctx, g.tokenURL, form, &out); err != nil {
		return "", fmt.Errorf("refresh access token: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("token refresh returned no access token")
	}
	g.accessToken = out.AccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	return g.accessToken, nil
}

// authedRequest performs an authenticated Google API request; on 401 the
// cached token is dropped and the request is retried once.
func (g *GDriveStorage) authedRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := g.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		var req *http.Request
		if body != nil {
			req, err = http.NewRequestWithContext(ctx, method, rawURL, body)
		} else {
			req, err = http.NewRequestWithContext(ctx, method, rawURL, nil)
		}
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := g.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			g.mu.Lock()
			g.accessToken = ""
			g.mu.Unlock()
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("google api unauthorized")
}

// EnsureFolder returns the configured folder id, creating (but not
// persisting) the default folder when none is configured.
func (g *GDriveStorage) EnsureFolder(ctx context.Context) (string, error) {
	if g.cfg.GDriveFolderID != "" {
		return g.cfg.GDriveFolderID, nil
	}
	meta := map[string]any{"name": gdriveFolderName, "mimeType": gdriveFolderMIME}
	body, _ := json.Marshal(meta)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.apiBase+"/files", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	token, err := g.accessToken(ctx)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("create folder: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("create folder: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("create folder: unexpected response %q", strings.TrimSpace(string(respBody)))
	}
	return out.ID, nil
}

// Upload creates a resumable session and streams the file into it. The file
// is read as root (sudo cat) because backup files are root-owned.
func (g *GDriveStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	folderID, err := g.EnsureFolder(ctx)
	if err != nil {
		return "", err
	}
	meta := map[string]any{"name": filepath.Base(name)}
	if folderID != "" {
		meta["parents"] = []string{folderID}
	}
	metaBody, _ := json.Marshal(meta)

	sessionURL := g.uploadBase + "/files?uploadType=resumable"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionURL, strings.NewReader(string(metaBody)))
	if err != nil {
		return "", err
	}
	token, err := g.accessToken(ctx)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", size))
	req.Header.Set("X-Upload-Content-Type", "application/gzip")

	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("start resumable session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("start resumable session: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("start resumable session: no Location header")
	}

	pr, pw := io.Pipe()
	go func() {
		_, err := g.exec.RunSudoStream(ctx, pw, "cat", localPath)
		_ = pw.CloseWithError(err)
	}()

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, location, pr)
	if err != nil {
		return "", err
	}
	putReq.ContentLength = size
	putResp, err := g.http.Do(putReq)
	if err != nil {
		return "", fmt.Errorf("upload bytes: %w", err)
	}
	defer putResp.Body.Close()
	putBody, _ := io.ReadAll(io.LimitReader(putResp.Body, 1<<16))
	if putResp.StatusCode/100 != 2 {
		return "", fmt.Errorf("upload bytes: google returned %d: %s", putResp.StatusCode, strings.TrimSpace(string(putBody)))
	}
	var file struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(putBody, &file); err != nil || file.ID == "" {
		return "", fmt.Errorf("upload bytes: unexpected response %q", strings.TrimSpace(string(putBody)))
	}
	return "gdrive://" + file.ID, nil
}

// Download streams the file (alt=media) into w.
func (g *GDriveStorage) Download(ctx context.Context, name string, w io.Writer) error {
	resp, err := g.authedRequest(ctx, http.MethodGet, g.apiBase+"/files/"+url.PathEscape(name)+"?alt=media", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("download stream: %w", err)
	}
	return nil
}

// Delete removes the file.
func (g *GDriveStorage) Delete(ctx context.Context, name string) error {
	resp, err := g.authedRequest(ctx, http.MethodDelete, g.apiBase+"/files/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("delete: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Test verifies the token works and returns the account email.
func (g *GDriveStorage) Test(ctx context.Context) (string, error) {
	resp, err := g.authedRequest(ctx, http.MethodGet, g.apiBase+"/about?fields=user", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("about: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		User struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("about: unexpected response %q", strings.TrimSpace(string(body)))
	}
	if out.User.EmailAddress == "" {
		return "", fmt.Errorf("about: no account email in response")
	}
	return out.User.EmailAddress, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/remotestorage/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/remotestorage/
git commit -m "feat(storage): native Google Drive backend with OAuth copy-paste flow"
```

---

### Task 5: rclone backend

**Files:**
- Create: `internal/remotestorage/rclone.go` (replaces rclone stub; delete `backends_stub.go` entirely now)
- Test: `internal/remotestorage/rclone_test.go`

**Interfaces:**
- Produces: `rcloneStorage` implementing `Storage`; remote ref `<remote>:<path>/<name>` (same as the previous `uploadRclone`).

- [ ] **Step 1: Write the failing test**

Create `internal/remotestorage/rclone_test.go`:

```go
package remotestorage

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestRcloneUploadDownloadDeleteTest(t *testing.T) {
	var calls []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if name == "rclone" && args[0] == "lsjson" {
				return &executor.Result{ExitCode: 0, Stdout: "[]"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoStreamFunc: func(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return w.WriteString("backup-bytes")
		},
	}

	st, err := New(Config{Type: "rclone", RcloneRemote: "gdrive", RclonePath: "backups"}, mock, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ref, err := st.Upload(context.Background(), "/tmp/f.tar.gz", 12, "website/f.tar.gz")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if ref != "gdrive:backups/website/f.tar.gz" {
		t.Errorf("ref = %q", ref)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rclone copyto /tmp/f.tar.gz gdrive:backups/website/f.tar.gz") {
		t.Errorf("calls = %v", calls)
	}

	var buf strings.Builder
	if err := st.Download(context.Background(), ref, &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != "backup-bytes" {
		t.Errorf("downloaded = %q", buf.String())
	}

	calls = nil
	if err := st.Delete(context.Background(), ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rclone deletefile gdrive:backups/website/f.tar.gz") {
		t.Errorf("calls = %v", calls)
	}

	calls = nil
	info, err := st.Test(context.Background())
	if err != nil || !strings.Contains(info, "gdrive:backups") {
		t.Fatalf("Test: %q %v", info, err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rclone lsjson") {
		t.Errorf("test calls = %v", calls)
	}
}

func TestRcloneUploadErrorSurfaces(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 3, Stderr: "directory not found"}, nil
		},
	}
	st, _ := New(Config{Type: "rclone", RcloneRemote: "gdrive"}, mock, nil)
	if _, err := st.Upload(context.Background(), "/x", 1, "x"); err == nil || !strings.Contains(err.Error(), "exit 3") {
		t.Fatalf("expected exit-3 error, got %v", err)
	}
}
```

(The test needs `"io"` imported.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/remotestorage/ -run TestRclone -v`
Expected: FAIL (stub).

- [ ] **Step 3: Implement `rclone.go`**

Create `internal/remotestorage/rclone.go`, and delete `internal/remotestorage/backends_stub.go`:

```go
package remotestorage

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// rcloneStorage drives a server-configured rclone remote (e.g. "gdrive:backups").
// rclone must be installed on the server; its config is root's, so every
// command runs through sudo.
type rcloneStorage struct {
	cfg  Config
	exec executor.CommandExecutor
}

func (r *rcloneStorage) dest(name string) string {
	path := strings.Trim(r.cfg.RclonePath, "/")
	if path == "" {
		return r.cfg.RcloneRemote + ":" + name
	}
	return r.cfg.RcloneRemote + ":" + path + "/" + name
}

func (r *rcloneStorage) run(ctx context.Context, args ...string) error {
	result, err := r.exec.RunSudo(ctx, "rclone", args...)
	if err != nil {
		return fmt.Errorf("rclone %s: %w", args[0], err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("rclone %s failed (exit %d): %s", args[0], result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Upload copies the local file to the remote with rclone copyto.
func (r *rcloneStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	dest := r.dest(name)
	if err := r.run(ctx, "copyto", localPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Download streams the remote object (rclone cat) into w.
func (r *rcloneStorage) Download(ctx context.Context, name string, w io.Writer) error {
	if _, err := r.exec.RunSudoStream(ctx, w, "rclone", "cat", r.dest(name)); err != nil {
		return fmt.Errorf("rclone cat: %w", err)
	}
	return nil
}

// Delete removes the remote object.
func (r *rcloneStorage) Delete(ctx context.Context, name string) error {
	return r.run(ctx, "deletefile", r.dest(name))
}

// Test verifies the remote is reachable.
func (r *rcloneStorage) Test(ctx context.Context) (string, error) {
	path := strings.Trim(r.cfg.RclonePath, "/")
	target := r.cfg.RcloneRemote + ":" + path
	if _, err := r.exec.RunSudo(ctx, "rclone", "lsjson", "--max-depth", "1", target); err != nil {
		return "", fmt.Errorf("rclone lsjson: %w", err)
	}
	return target + " (reachable)", nil
}
```

Note: `filepath` is unused — omit the import if the compiler flags it (Go errors on unused imports; adjust the import block to only what compiles).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/remotestorage/ -v && go build ./...`
Expected: PASS, build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/remotestorage/
git commit -m "feat(storage): rclone backend with download, delete, and test"
```

---

### Task 6: Backup service — upload via interface, disk-saving mode, retry upload

**Files:**
- Delete: `internal/backup/s3.go`, `internal/backup/s3_test.go`
- Create: `internal/backup/remote.go`
- Modify: `internal/backup/service.go` (Service struct ~46-60, `executeBackup` upload block ~236-250)
- Modify: `internal/backup/handler.go` (add RetryUpload handler)
- Test: `internal/backup/service_test.go`

**Interfaces:**
- Consumes: `remotestorage.{Config, ConfigStore, Storage, New, ParseRemoteRef}` from Tasks 2–5.
- Produces (used by Tasks 7–9):

```go
func (s *Service) SetRemoteStore(store *remotestorage.ConfigStore)
func (s *Service) remoteConfig(ctx context.Context) (remotestorage.Config, error)
func (s *Service) uploadAfterBackup(ctx context.Context, b model.Backup, write func(string)) error
func (s *Service) RetryUpload(ctx context.Context, caller Caller, id string) (string, error)
```

- [ ] **Step 1: Write the failing tests**

Append to `internal/backup/service_test.go` (extend the import block with `fmt`, `io`, `"github.com/mohammadirham37/jenderal_panel/internal/model"`, `"github.com/mohammadirham37/jenderal_panel/internal/remotestorage"` — `context`, `time`, `sql`, and `executor` are already imported):

```go
// fakeRemoteStore is a fixed-config ConfigStore stand-in used via a tiny
// interface; the real one is *remotestorage.ConfigStore.
```

The Service field is typed `*remotestorage.ConfigStore`. For tests, instead of a fake store, pre-seed the settings table directly (ConfigStore reads the `settings` table):

```go
func seedRemoteConfig(t *testing.T, db *sql.DB, typ string, deleteLocal bool) {
	t.Helper()
	dl := "0"
	if deleteLocal {
		dl = "1"
	}
	pairs := map[string]string{
		"backup_remote_type": typ, "backup_remote_s3_endpoint": "https://s3.test",
		"backup_remote_s3_bucket": "bkt", "backup_remote_s3_access_key": "ak",
		"backup_remote_s3_secret_key": "sk", "backup_remote_delete_local": dl,
	}
	for k, v := range pairs {
		if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, k, v, "now"); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
}

// fakeStorage records calls; injected through the service test seam
// setRemoteTestStorage (see remote.go).
type fakeStorage struct {
	uploadRef  string
	uploadErr  error
	uploaded   []string
	deleted    []string
	downloaded []string
}

func (f *fakeStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	f.uploaded = append(f.uploaded, name)
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	return f.uploadRef, nil
}
func (f *fakeStorage) Download(ctx context.Context, name string, w io.Writer) error {
	f.downloaded = append(f.downloaded, name)
	_, _ = w.Write([]byte("backup-bytes"))
	return nil
}
func (f *fakeStorage) Delete(ctx context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	return nil
}
func (f *fakeStorage) Test(ctx context.Context) (string, error) { return "fake ok", nil }
```

Tests drive `executeBackup` directly (it is unexported — same package tests can call it). Also add a test-only seam in `remote.go`:

```go
// storageOverride, when set, short-circuits remotestorage.New (tests only).
var storageOverride func(cfg remotestorage.Config) (remotestorage.Storage, error)
```

Tests:

```go
func TestExecuteBackupUploadsAndKeepsLocalByDefault(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	seedBackupTargets(t, db)
	seedRemoteConfig(t, db, "s3", false)

	fake := &fakeStorage{uploadRef: "https://s3.test/bkt/website/f.tar.gz"}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	mock := mockExecutor()
	mock.RunSudoFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 0}, nil
	}
	svc.exec = mock

	b := model.Backup{ID: "b-up", Type: "website", Target: "example.com", Path: "/tmp/test-backups/website/f.tar.gz",
		Status: "running", Kind: KindManual, CreatedAt: time.Now().UTC()}
	if _, err := db.Exec(`INSERT INTO backups (id, type, target, path, status, kind, created_by, created_at) VALUES (?,?,?,?,?,?, '', ?)`,
		b.ID, b.Type, b.Target, b.Path, b.Status, b.Kind, b.CreatedAt.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	// createBackup via public API is easier, but it spawns a task; drive
	// executeBackup directly on an existing row:
	if err := svc.executeBackup(context.Background(), b, func(string) {}); err != nil {
		t.Fatalf("executeBackup: %v", err)
	}

	got, _ := svc.Get(context.Background(), b.ID)
	if got.RemotePath != "https://s3.test/bkt/website/f.tar.gz" {
		t.Errorf("remote_path = %q", got.RemotePath)
	}
	if got.RemoteOnly {
		t.Error("disk-saving off: local file must be kept")
	}
	if len(fake.uploaded) != 1 {
		t.Errorf("uploads = %v", fake.uploaded)
	}
}

func TestExecuteBackupDiskSavingRemovesLocal(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", true)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	seedBackupTargets(t, db)

	fake := &fakeStorage{uploadRef: "https://s3.test/bkt/website/f.tar.gz"}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	b := model.Backup{ID: "b-ds", Type: "config", Path: "/tmp/test-backups/config/c.tar.gz",
		Status: "running", Kind: KindManual, CreatedAt: time.Now().UTC()}
	if _, err := db.Exec(`INSERT INTO backups (id, type, target, path, status, kind, created_by, created_at) VALUES (?,?,?,?,?,?, '', ?)`,
		b.ID, b.Type, "", b.Path, b.Status, b.Kind, b.CreatedAt.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := svc.executeBackup(context.Background(), b, func(string) {}); err != nil {
		t.Fatalf("executeBackup: %v", err)
	}

	got, _ := svc.Get(context.Background(), b.ID)
	if !got.RemoteOnly || got.RemotePath == "" {
		t.Errorf("expected remote-only, got remote_only=%v remote_path=%q", got.RemoteOnly, got.RemotePath)
	}
}

func TestExecuteBackupUploadFailureKeepsLocal(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", true)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	seedBackupTargets(t, db)

	fake := &fakeStorage{uploadErr: fmt.Errorf("503 slow down")}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	b := model.Backup{ID: "b-fail", Type: "config", Path: "/tmp/test-backups/config/c.tar.gz",
		Status: "running", Kind: KindManual, CreatedAt: time.Now().UTC()}
	if _, err := db.Exec(`INSERT INTO backups (id, type, target, path, status, kind, created_by, created_at) VALUES (?,?,?,?,?,?, '', ?)`,
		b.ID, b.Type, "", b.Path, b.Status, b.Kind, b.CreatedAt.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := svc.executeBackup(context.Background(), b, func(string) {}); err != nil {
		t.Fatalf("upload failure must not fail the backup: %v", err)
	}

	got, _ := svc.Get(context.Background(), b.ID)
	if got.Status != "completed" || got.RemoteOnly || got.RemotePath != "" {
		t.Errorf("expected completed local-only, got %+v", got)
	}
}

func TestSafetyBackupsAreNeverUploaded(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", true)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	seedBackupTargets(t, db)

	fake := &fakeStorage{uploadRef: "https://s3.test/bkt/x"}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	b := model.Backup{ID: "b-safety", Type: "config", Path: "/tmp/test-backups/config/s.tar.gz",
		Status: "running", Kind: KindSafety, CreatedAt: time.Now().UTC()}
	if _, err := db.Exec(`INSERT INTO backups (id, type, target, path, status, kind, created_by, created_at) VALUES (?,?,?,?,?,?, '', ?)`,
		b.ID, b.Type, "", b.Path, b.Status, b.Kind, b.CreatedAt.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	_ = svc.executeBackup(context.Background(), b, func(string) {})
	if len(fake.uploaded) != 0 {
		t.Errorf("safety backup uploaded: %v", fake.uploaded)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/backup/ -run 'TestExecuteBackupUploads|TestExecuteBackupDiskSaving|TestExecuteBackupUploadFailure|TestSafetyBackups' -v`
Expected: FAIL — `SetRemoteStore`/`storageOverride` do not exist yet; `s3.go` still present.

- [ ] **Step 3: Delete the old S3 code and create `remote.go`**

Delete `internal/backup/s3.go` and `internal/backup/s3_test.go` (their logic now lives in `internal/remotestorage`).

Create `internal/backup/remote.go`:

```go
package backup

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/remotestorage"
)

// Remote storage integration: configuration lives in the settings KV (via
// remotestorage.ConfigStore); the backup service decides when to upload and
// what to record.

// storageOverride short-circuits remotestorage.New in tests.
var storageOverride func(cfg remotestorage.Config) (remotestorage.Storage, error)

// SetRemoteStore attaches the settings-backed config store. Without it the
// panel has no remote storage.
func (s *Service) SetRemoteStore(store *remotestorage.ConfigStore) {
	s.remoteStore = store
}

func (s *Service) remoteConfig(ctx context.Context) (remotestorage.Config, error) {
	if s.remoteStore == nil {
		return remotestorage.Config{}, remotestorage.ErrNotConfigured
	}
	return s.remoteStore.Load(ctx)
}

// remoteStorage builds the Storage for the currently configured backend.
func (s *Service) remoteStorage(ctx context.Context) (remotestorage.Storage, remotestorage.Config, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return nil, cfg, err
	}
	if !cfg.Enabled() {
		return nil, cfg, remotestorage.ErrNotConfigured
	}
	if storageOverride != nil {
		st, err := storageOverride(cfg)
		return st, cfg, err
	}
	st, err := remotestorage.New(cfg, s.exec, s.httpClient)
	return st, cfg, err
}

// uploadAfterBackup uploads a completed local backup to the remote
// destination. Upload failure is returned; the caller decides whether it is
// fatal (retry task) or best-effort (regular backup run).
func (s *Service) uploadAfterBackup(ctx context.Context, b model.Backup, write func(string)) error {
	st, cfg, err := s.remoteStorage(ctx)
	if err != nil {
		if err == remotestorage.ErrNotConfigured {
			return nil
		}
		write("Remote storage unavailable: " + err.Error())
		return err
	}

	size := b.SizeBytes
	if size <= 0 {
		size = s.getFileSize(ctx, b.Path)
	}

	write("Uploading off-site copy…")
	objectName := b.Type + "/" + filepath.Base(b.Path)
	remotePath, upErr := st.Upload(ctx, b.Path, size, objectName)
	if upErr != nil {
		write("Off-site upload failed: " + upErr.Error())
		return upErr
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE backups SET remote_path = ? WHERE id = ?`, remotePath, b.ID); err != nil {
		return fmt.Errorf("record remote path: %w", err)
	}
	write("Off-site copy uploaded: " + remotePath)

	if cfg.DeleteLocalAfterUpload {
		if _, err := s.exec.RunSudo(ctx, "rm", "-f", b.Path); err != nil {
			write("Failed to remove the local copy: " + err.Error())
			return nil
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE backups SET remote_only = 1 WHERE id = ?`, b.ID); err != nil {
			return fmt.Errorf("mark remote-only: %w", err)
		}
		write("Local copy removed (disk-saving mode).")
	}
	return nil
}

// RetryUpload uploads an existing completed local backup that has no remote
// copy yet. Runs as a visible task; returns the task ID.
func (s *Service) RetryUpload(ctx context.Context, caller Caller, id string) (string, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !auth.CanManageResource(caller.Admin, caller.UserID, b.CreatedBy) {
		return "", model.ErrForbidden
	}
	if b.Status != "completed" {
		return "", model.NewValidationError("only completed backups can be uploaded")
	}
	if b.Kind == KindSafety {
		return "", model.NewValidationError("safety backups always stay local")
	}
	if b.RemotePath != "" {
		return "", model.NewValidationError("backup already has a remote copy")
	}
	if s.getFileSize(ctx, b.Path) <= 0 {
		return "", model.NewValidationError("local backup file no longer exists")
	}

	if s.tasks == nil {
		return "", fmt.Errorf("task runner not available")
	}
	taskID := s.tasks.RunFuncWithOptions(taskrunner.Options{
		Name:    "Upload backup to remote — " + b.Target,
		Module:  "backup",
		Timeout: 2 * time.Hour,
	}, func(taskCtx context.Context, write func(string)) error {
		if err := s.uploadAfterBackup(taskCtx, b, write); err != nil {
			return err
		}
		got, err := s.Get(taskCtx, b.ID)
		if err != nil {
			return err
		}
		if got.RemotePath == "" {
			return fmt.Errorf("upload did not complete")
		}
		return nil
	})
	return taskID, nil
}
```

Adjust `internal/backup/service.go`:

1. Service struct gains two fields:

```go
type Service struct {
	db         *sql.DB
	exec       executor.CommandExecutor
	audit      *audit.Service
	localDir   string
	tasks      *taskrunner.Runner
	remoteStore *remotestorage.ConfigStore
	httpClient *http.Client
}
```

(NewService leaves them zero; `SetRemoteStore` sets the store. `httpClient` nil means `http.DefaultClient` inside remotestorage.)

2. `executeBackup`: replace the old off-site block (lines ~236-250) with:

```go
	// Off-site copy: best effort — a failed upload never fails the backup,
	// the local file is the primary copy until disk-saving removes it.
	if b.Kind != KindSafety {
		_ = s.uploadAfterBackup(ctx, b, write)
	}
```

3. Add imports: `net/http` (for the field), `remotestorage`. `time` is already imported.

- [ ] **Step 4: Add the handler endpoint**

In `internal/backup/handler.go`:

```go
// RetryUpload handles POST /api/backups/{id}/upload — uploads an existing
// completed local backup to the configured remote storage (202 + task id).
func (h *Handler) RetryUpload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	taskID, err := h.svc.RetryUpload(r.Context(), callerFromContext(r), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "upload_backup", id, "remote upload started (task "+taskID+")")
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/backup/ ./internal/remotestorage/ -v`
Expected: PASS. Run `go build ./...` — clean.

- [ ] **Step 6: Commit**

```bash
git add -A internal/backup/ internal/remotestorage/
git commit -m "feat(backup): upload through remotestorage interface with disk-saving mode and retry upload"
```

---

### Task 7: Delete-everywhere (manual delete + retention prune)

**Files:**
- Modify: `internal/backup/service.go` (`DeleteBackup` ~734)
- Modify: `internal/backup/scheduler.go` (`pruneExpired` ~123)
- Modify: `internal/backup/handler.go` (`Delete` ~112)
- Test: `internal/backup/service_test.go`

**Interfaces:**
- Consumes: `s.remoteStorage(ctx)` (Task 6), `remotestorage.ParseRemoteRef`.
- Produces: `DeleteBackup(ctx, id) (warning string, err error)` — warning is non-empty when the remote copy could not be matched to the configured backend and was left in place.

- [ ] **Step 1: Write the failing tests**

Append to `internal/backup/service_test.go`:

```go
func seedRemoteBackup(t *testing.T, db *sql.DB, id, remotePath string, remoteOnly bool) {
	t.Helper()
	ro := 0
	if remoteOnly {
		ro = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO backups (id, type, path, status, kind, created_by, created_at, remote_path, remote_only)
		VALUES (?, 'website', '/tmp/x.tar.gz', 'completed', 'manual', '', ?, ?, ?)`, id, now, remotePath, ro); err != nil {
		t.Fatalf("seed remote backup: %v", err)
	}
}

func TestDeleteBackupRemovesRemoteCopy(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", false)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	fake := &fakeStorage{}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	seedRemoteBackup(t, db, "b-del", "https://s3.test/bkt/website/f.tar.gz", false)

	warning, err := svc.DeleteBackup(context.Background(), "b-del")
	if err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if warning != "" {
		t.Errorf("unexpected warning %q", warning)
	}
	if len(fake.deleted) != 1 {
		t.Errorf("remote deletes = %v", fake.deleted)
	}
	if _, err := svc.Get(context.Background(), "b-del"); err != model.ErrNotFound {
		t.Errorf("row must be gone, got %v", err)
	}
}

func TestDeleteBackupAbortsWhenRemoteDeleteFails(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", false)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	fake := &fakeStorage{deleteErr: fmt.Errorf("403 keystore unavailable")}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	seedRemoteBackup(t, db, "b-del2", "https://s3.test/bkt/website/f.tar.gz", false)

	if _, err := svc.DeleteBackup(context.Background(), "b-del2"); err == nil {
		t.Fatal("remote delete failure must abort the deletion")
	}
	if _, err := svc.Get(context.Background(), "b-del2"); err != nil {
		t.Error("row must survive a failed deletion")
	}
}

func TestDeleteBackupWarnsWhenBackendMismatch(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", false)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	seedRemoteBackup(t, db, "b-del3", "gdrive://orphan-1", false)

	warning, err := svc.DeleteBackup(context.Background(), "b-del3")
	if err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if warning == "" {
		t.Error("expected a warning that the remote copy was left in place")
	}
	if _, err := svc.Get(context.Background(), "b-del3"); err != model.ErrNotFound {
		t.Error("row must still be deleted on mismatch")
	}
}
```

`fakeStorage` gains `deleteErr error` and `Delete` returns it (update the Task 6 fake accordingly).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/backup/ -run TestDeleteBackup -v`
Expected: FAIL — DeleteBackup returns only `error` today.

- [ ] **Step 3: Implement delete-everywhere**

In `internal/backup/service.go`, replace `DeleteBackup`:

```go
// DeleteBackup removes a backup everywhere: the remote object (when the
// stored remote reference matches the configured backend), the local file,
// and the database row. A failed remote deletion aborts the whole operation
// so no row is lost while its remote copy survives. When the remote reference
// cannot be handled (no backend configured, or a different backend type), the
// local deletion proceeds and a warning is returned.
func (s *Service) DeleteBackup(ctx context.Context, id string) (string, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}

	warning := ""
	if b.RemotePath != "" {
		backend, name, ok := remotestorage.ParseRemoteRef(b.RemotePath)
		switch {
		case !ok:
			warning = "remote copy left in place (unrecognized reference): " + b.RemotePath
		default:
			cfg, cfgErr := s.remoteConfig(ctx)
			if cfgErr != nil || cfg.Type != backend || !cfg.Enabled() {
				warning = "remote copy left in place (backend not configured): " + b.RemotePath
			} else {
				st, _, err := s.remoteStorage(ctx)
				if err != nil {
					return "", fmt.Errorf("delete remote copy: %w", err)
				}
				if delErr := st.Delete(ctx, name); delErr != nil {
					return "", fmt.Errorf("delete remote copy: %w", delErr)
				}
			}
		}
	}

	if b.Path != "" {
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", b.Path)
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM backups WHERE id = ?`, id)
	if err != nil {
		return warning, fmt.Errorf("delete backup: %w", err)
	}
	return warning, nil
}
```

In `internal/backup/scheduler.go`, `pruneExpired`'s delete branch:

```go
			if warning, err := svc.DeleteBackup(ctx, b.ID); err != nil {
				// Kept for the next hourly tick to retry (remote deletion
				// failures abort the deletion so nothing is orphaned).
				log.Printf("backup scheduler: delete expired backup %s: %v", b.ID, err)
				continue
			} else if warning != "" {
				log.Printf("backup scheduler: pruned backup %s with warning: %s", b.ID, warning)
			}
			pruned++
```

In `internal/backup/handler.go` `Delete`:

```go
	warning, err := h.svc.DeleteBackup(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_backup", id, "deleted backup")
	if warning != "" {
		h.logAction(r, "delete_backup_warning", id, warning)
		httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok", "warning": warning})
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/backup/ -v`
Expected: PASS (including the scheduler tests — their DeleteBackup call sites now need the two-value form; fix any compile errors by accepting the warning).

- [ ] **Step 5: Commit**

```bash
git add internal/backup/
git commit -m "feat(backup): delete backups everywhere (local + remote) with abort-on-remote-failure"
```

---

### Task 8: Remote-only download + restore staging

**Files:**
- Modify: `internal/backup/service.go` (`StreamBackupFile` ~714, `restoreBackup` ~527, `Stats` ~806)
- Test: `internal/backup/service_test.go`

**Interfaces:**
- Consumes: `s.remoteStorage`, `remotestorage.ParseRemoteRef`.
- Produces: `StreamBackupFile` transparently streams from remote when `b.RemoteOnly`; `restoreBackup` stages a temp file first.

- [ ] **Step 1: Write the failing tests**

Append to `internal/backup/service_test.go`:

```go
func TestStreamBackupFileFromRemote(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", false)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	fake := &fakeStorage{}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	seedRemoteBackup(t, db, "b-ro", "https://s3.test/bkt/website/f.tar.gz", true)

	b, _ := svc.Get(context.Background(), "b-ro")
	var buf strings.Builder
	if err := svc.StreamBackupFile(context.Background(), b, &buf); err != nil {
		t.Fatalf("StreamBackupFile: %v", err)
	}
	if buf.String() != "backup-bytes" {
		t.Errorf("streamed = %q", buf.String())
	}
	if len(fake.downloaded) != 1 {
		t.Errorf("downloads = %v", fake.downloaded)
	}
}

func TestRestoreFromRemoteStagesTempFile(t *testing.T) {
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", false)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	seedBackupTargets(t, db)

	fake := &fakeStorage{}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	defer func() { storageOverride = nil }()

	seedRemoteBackup(t, db, "b-restore", "https://s3.test/bkt/website/f.tar.gz", true)

	// config restore path is the simplest (tar -xzf on the staged file);
	// use a website backup row but drive restoreWebsite through the staging
	// seam by calling restoreBackup for a config row instead:
	if _, err := db.Exec(`UPDATE backups SET type = 'config' WHERE id = 'b-restore'`); err != nil {
		t.Fatalf("set type: %v", err)
	}
	b, _ := svc.Get(context.Background(), "b-restore")

	var staged string
	mock := mockExecutor()
	mock.RunSudoFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "df" {
			// free-space check in stageRemoteBackup; report plenty.
			return &executor.Result{ExitCode: 0, Stdout: "AVAIL\n999999999999\n"}, nil
		}
		if name == "tar" {
			staged = args[2] // tar -xzf <path> -C /
		}
		return &executor.Result{ExitCode: 0}, nil
	}
	svc.exec = mock

	err := svc.restoreBackup(context.Background(), SystemCaller, b, "", func(string) {})
	if err != nil {
		t.Fatalf("restoreBackup: %v", err)
	}
	if staged == "" || staged == b.Path {
		t.Errorf("restore must run against a staged temp file, got %q", staged)
	}
	if len(fake.downloaded) != 1 {
		t.Errorf("downloads = %v", fake.downloaded)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/backup/ -run 'TestStreamBackupFileFromRemote|TestRestoreFromRemote' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `internal/backup/service.go`:

1. `StreamBackupFile`:

```go
// StreamBackupFile streams the backup archive to w without buffering it
// whole in memory. Remote-only backups stream straight from the remote
// backend; local files are read as root via sudo cat.
func (s *Service) StreamBackupFile(ctx context.Context, b model.Backup, w io.Writer) error {
	if b.RemoteOnly && b.RemotePath != "" {
		backend, name, ok := remotestorage.ParseRemoteRef(b.RemotePath)
		if !ok {
			return fmt.Errorf("unrecognized remote path: %s", b.RemotePath)
		}
		st, cfg, err := s.remoteStorage(ctx)
		if err != nil {
			return fmt.Errorf("remote storage unavailable: %w", err)
		}
		if cfg.Type != backend {
			return fmt.Errorf("backup lives on %s storage but %s is configured", backend, cfg.Type)
		}
		if err := st.Download(ctx, name, w); err != nil {
			return fmt.Errorf("stream remote backup: %w", err)
		}
		return nil
	}
	if _, err := s.exec.RunSudoStream(ctx, w, "cat", b.Path); err != nil {
		return fmt.Errorf("stream backup: %w", err)
	}
	return nil
}
```

2. `restoreBackup` — add staging at the top (before the safety backup, so the safety snapshot does not consume disk for a restore that cannot start):

```go
func (s *Service) restoreBackup(ctx context.Context, caller Caller, b model.Backup, component string, write func(string)) error {
	if b.RemoteOnly && b.RemotePath != "" {
		tmpPath, err := s.stageRemoteBackup(ctx, b, write)
		if err != nil {
			return err
		}
		defer os.Remove(tmpPath)
		b.Path = tmpPath
	}
	// … existing body unchanged …
```

3. New helpers (place near `restoreBackup`):

```go
// stageRemoteBackup downloads a remote-only backup into a temp file so the
// existing path-based restore logic can run; the caller removes the temp
// file when the restore finishes.
func (s *Service) stageRemoteBackup(ctx context.Context, b model.Backup, write func(string)) (string, error) {
	backend, name, ok := remotestorage.ParseRemoteRef(b.RemotePath)
	if !ok {
		return "", fmt.Errorf("unrecognized remote path: %s", b.RemotePath)
	}
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("remote storage unavailable: %w", err)
	}
	if cfg.Type != backend || !cfg.Enabled() {
		return "", model.NewValidationError("backup lives on " + backend + " storage, which is not currently configured")
	}
	st, _, err := s.remoteStorage(ctx)
	if err != nil {
		return "", err
	}

	if free := s.freeSpace(ctx, os.TempDir()); free > 0 && free < b.SizeBytes {
		return "", fmt.Errorf("not enough disk space to download the backup: %d bytes free, %d needed", free, b.SizeBytes)
	}

	tmp, err := os.CreateTemp("", "jenderal-remote-restore-")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	write("Downloading backup from remote storage…")
	if err := st.Download(ctx, name, tmp); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("download backup: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("save temp file: %w", err)
	}
	write("Backup downloaded; starting restore…")
	return tmpName, nil
}

// freeSpace returns available bytes on dir's filesystem (0 when unknown).
func (s *Service) freeSpace(ctx context.Context, dir string) int64 {
	result, err := s.exec.RunSudo(ctx, "df", "-B1", "--output=avail", dir)
	if err != nil || result.ExitCode != 0 {
		return 0
	}
	lines := strings.Fields(strings.TrimSpace(result.Stdout))
	if len(lines) >= 2 {
		v, _ := strconv.ParseInt(lines[len(lines)-1], 10, 64)
		return v
	}
	return 0
}
```

4. Refactor `Stats` to reuse the helper:

```go
	stats.DiskFree = s.freeSpace(ctx, s.localDir)
```

(replacing the inline df block).

5. `os` is already imported in service.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/backup/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/
git commit -m "feat(backup): remote-only download streaming and restore-from-remote staging"
```

---

### Task 9: Config/test/OAuth endpoints, router, wiring, RBAC

**Files:**
- Create: `internal/backup/remote_handler.go`
- Modify: `internal/backup/remote.go` (service-level config view + test + gdrive flow)
- Modify: `internal/api/router.go` (routes ~833-863)
- Modify: `cmd/jenderal/main.go` (~line 262)
- Modify: `internal/auth/rbac.go` (permissions list ~117, user role stays unchanged)
- Modify: `AGENTS.md` (stale permission count)

**Interfaces:**
- Consumes: everything from Tasks 2–6.
- Produces: endpoints `GET/PUT /api/v1/backup-remote/config`, `POST /api/v1/backup-remote/test`, `GET /api/v1/backup-remote/gdrive/authorize`, `POST /api/v1/backup-remote/gdrive/exchange`, permission `backups.remote`.

- [ ] **Step 1: Write the failing tests**

Create `internal/backup/remote_handler_test.go` (handler-level tests with `httptest`; reuse `setupTestDB` + `mockExecutor` from service_test.go):

```go
package backup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/remotestorage"
)

func remoteSvc(t *testing.T) (*Service, *fakeStorage) {
	t.Helper()
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", false)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	fake := &fakeStorage{}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	t.Cleanup(func() { storageOverride = nil })
	return svc, fake
}

func TestRemoteConfigViewMasksSecrets(t *testing.T) {
	svc, _ := remoteSvc(t)
	view, err := svc.RemoteConfigView(context.Background())
	if err != nil {
		t.Fatalf("RemoteConfigView: %v", err)
	}
	out, _ := json.Marshal(view)
	if strings.Contains(string(out), "sk") && strings.Contains(string(out), `"secret_key":"sk"`) {
		t.Errorf("view leaks secret: %s", out)
	}
	if !view.SecretSet {
		t.Error("SecretSet must be true")
	}
	if view.Type != "s3" || view.Bucket != "bkt" {
		t.Errorf("view = %+v", view)
	}
}

func TestSaveRemoteConfigPreservesSecret(t *testing.T) {
	svc, _ := remoteSvc(t)
	err := svc.SaveRemoteConfig(context.Background(), RemoteConfigRequest{
		Type: "s3", Endpoint: "https://s3.new", Bucket: "bkt2", Region: "us-east-1", AccessKey: "ak2",
	})
	if err != nil {
		t.Fatalf("SaveRemoteConfig: %v", err)
	}
	cfg, _ := svc.remoteConfig(context.Background())
	if cfg.SecretKey != "sk" {
		t.Errorf("secret must survive a save without it, got %q", cfg.SecretKey)
	}
	if cfg.Bucket != "bkt2" {
		t.Errorf("bucket = %q", cfg.Bucket)
	}
}

func TestTestRemoteConnection(t *testing.T) {
	svc, _ := remoteSvc(t)
	info, err := svc.TestRemoteConnection(context.Background())
	if err != nil || info != "fake ok" {
		t.Fatalf("TestRemoteConnection: %q %v", info, err)
	}
}

func TestGDriveAuthorizeRequiresClientID(t *testing.T) {
	svc, _ := remoteSvc(t)
	if _, err := svc.GDriveAuthorizeURL(context.Background()); err == nil {
		t.Fatal("expected error when no gdrive client id is configured")
	}
}

func TestGDriveExchangeStoresTokenAndFolder(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	storageOverride = nil
	t.Cleanup(func() { storageOverride = nil })

	// The exchange flow builds its gdrive client through the newGDriveForConfig
	// seam — override it with a fake that records the stored token/folder.
	fake := &fakeStorage{}
	newGDriveForConfig = func(cfg remotestorage.Config, exec executor.CommandExecutor, hc *http.Client) gdriveClient {
		return &fakeGDriveExchange{fake: fake, refreshToken: "rt-1", folderID: "fld-1", email: "me@gmail.com"}
	}
	defer func() { newGDriveForConfig = remotestorage.NewGDrive }()

	if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('backup_remote_gdrive_client_id','cid','now')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('backup_remote_gdrive_client_secret','csec','now')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	email, err := svc.GDriveExchange(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("GDriveExchange: %v", err)
	}
	if email != "me@gmail.com" {
		t.Errorf("email = %q", email)
	}
	cfg, _ := svc.remoteConfig(context.Background())
	if cfg.GDriveRefreshToken != "rt-1" || cfg.GDriveFolderID != "fld-1" || cfg.Type != "gdrive" {
		t.Errorf("config after exchange: %+v", cfg)
	}
}
```

With a helper type in the same test file:

```go
// fakeGDriveExchange simulates a connected gdrive backend for the exchange
// flow (ExchangeCode + EnsureFolder + Test).
type fakeGDriveExchange struct {
	fake         *fakeStorage
	refreshToken string
	folderID     string
	email        string
}

func (f *fakeGDriveExchange) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	return f.fake.Upload(ctx, localPath, size, name)
}
func (f *fakeGDriveExchange) Download(ctx context.Context, name string, w io.Writer) error {
	return f.fake.Download(ctx, name, w)
}
func (f *fakeGDriveExchange) Delete(ctx context.Context, name string) error {
	return f.fake.Delete(ctx, name)
}
func (f *fakeGDriveExchange) Test(ctx context.Context) (string, error) { return f.email, nil }
func (f *fakeGDriveExchange) ExchangeCode(ctx context.Context, code string) (string, error) {
	return f.refreshToken, nil
}
func (f *fakeGDriveExchange) EnsureFolder(ctx context.Context) (string, error) { return f.folderID, nil }
```

This requires the exchange flow to go through `storageOverride` too — design the service method so the gdrive storage it builds passes through the same override seam (see Step 3).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/backup/ -run 'TestRemoteConfig|TestSaveRemote|TestTestRemote|TestGDrive' -v`
Expected: FAIL — methods do not exist.

- [ ] **Step 3: Implement the service layer**

Append to `internal/backup/remote.go`:

```go
// RemoteConfigRequest is the write-only config payload from the UI. Secret
// fields left empty preserve the stored values; they are never returned.
type RemoteConfigRequest struct {
	Type        string `json:"type"`
	Endpoint    string `json:"endpoint"`
	Bucket      string `json:"bucket"`
	Region      string `json:"region"`
	AccessKey   string `json:"access_key"`
	SecretKey   string `json:"s3_secret_key"`
	Prefix      string `json:"prefix"`
	UseTLS      bool   `json:"use_tls"`
	GDClientID  string `json:"gdrive_client_id"`
	GDClientSecret string `json:"gdrive_client_secret"`
	RcloneRemote string `json:"rclone_remote"`
	RclonePath  string `json:"rclone_path"`
	DeleteLocalAfterUpload bool `json:"delete_local_after_upload"`
}

// RemoteConfigView is the read model: no secrets, only *_set booleans.
type RemoteConfigView struct {
	Type        string `json:"type"`
	Endpoint    string `json:"endpoint"`
	Bucket      string `json:"bucket"`
	Region      string `json:"region"`
	AccessKey   string `json:"access_key"`
	Prefix      string `json:"prefix"`
	UseTLS      bool   `json:"use_tls"`
	SecretSet   bool   `json:"s3_secret_set"`
	GDClientID  string `json:"gdrive_client_id"`
	GDClientSecretSet bool `json:"gdrive_client_secret_set"`
	GDConnected bool   `json:"gdrive_connected"`
	GDFolderID  string `json:"gdrive_folder_id"`
	RcloneRemote string `json:"rclone_remote"`
	RclonePath  string `json:"rclone_path"`
	DeleteLocalAfterUpload bool `json:"delete_local_after_upload"`
}

// RemoteConfigView returns the masked configuration for the UI.
func (s *Service) RemoteConfigView(ctx context.Context) (RemoteConfigView, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return RemoteConfigView{}, err
	}
	return RemoteConfigView{
		Type: cfg.Type, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, Region: cfg.Region,
		AccessKey: cfg.AccessKey, Prefix: cfg.Prefix, UseTLS: cfg.UseTLS,
		SecretSet: cfg.SecretKey != "",
		GDClientID: cfg.GDriveClientID, GDClientSecretSet: cfg.GDriveClientSecret != "",
		GDConnected: cfg.GDriveRefreshToken != "", GDFolderID: cfg.GDriveFolderID,
		RcloneRemote: cfg.RcloneRemote, RclonePath: cfg.RclonePath,
		DeleteLocalAfterUpload: cfg.DeleteLocalAfterUpload,
	}, nil
}

// SaveRemoteConfig validates and stores the UI payload.
func (s *Service) SaveRemoteConfig(ctx context.Context, req RemoteConfigRequest) error {
	if req.Type != "" && req.Type != "s3" && req.Type != "gdrive" && req.Type != "rclone" {
		return model.NewValidationError("type must be empty, s3, gdrive, or rclone")
	}
	// gdrive client secret: preserve when empty (same rule as the store).
	cfg := remotestorage.Config{
		Type: req.Type, Endpoint: req.Endpoint, Bucket: req.Bucket, Region: req.Region,
		AccessKey: req.AccessKey, SecretKey: req.SecretKey, Prefix: req.Prefix, UseTLS: req.UseTLS,
		GDriveClientID: req.GDClientID, GDriveClientSecret: req.GDClientSecret,
		RcloneRemote: req.RcloneRemote, RclonePath: req.RclonePath,
		DeleteLocalAfterUpload: req.DeleteLocalAfterUpload,
	}
	if err := s.remoteStore.Save(ctx, cfg); err != nil {
		return fmt.Errorf("save remote config: %w", err)
	}
	return nil
}

// TestRemoteConnection verifies the currently configured backend.
func (s *Service) TestRemoteConnection(ctx context.Context) (string, error) {
	st, _, err := s.remoteStorage(ctx)
	if err != nil {
		return "", err
	}
	return st.Test(ctx)
}

// GDriveAuthorizeURL returns the Google consent URL for the stored client id.
func (s *Service) GDriveAuthorizeURL(ctx context.Context) (string, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return "", err
	}
	if cfg.GDriveClientID == "" {
		return "", model.NewValidationError("save the Google Drive client ID first")
	}
	return newGDriveForConfig(cfg, s.exec, s.httpClient).AuthorizeURL(), nil
}

// GDriveExchange completes the copy-paste OAuth flow: swaps the code for a
// refresh token, ensures the target folder exists, stores both, and returns
// the connected account email.
func (s *Service) GDriveExchange(ctx context.Context, code string) (string, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return "", err
	}
	if cfg.GDriveClientID == "" || cfg.GDriveClientSecret == "" {
		return "", model.NewValidationError("save the Google Drive client ID and secret first")
	}
	gd := newGDriveForConfig(cfg, s.exec, s.httpClient)
	refreshToken, err := gd.ExchangeCode(ctx, code)
	if err != nil {
		return "", err
	}
	cfg.GDriveRefreshToken = refreshToken
	cfg.Type = "gdrive"
	gd = newGDriveForConfig(cfg, s.exec, s.httpClient)
	folderID, err := gd.EnsureFolder(ctx)
	if err != nil {
		return "", fmt.Errorf("prepare drive folder: %w", err)
	}
	cfg.GDriveFolderID = folderID
	if err := s.remoteStore.Save(ctx, cfg); err != nil {
		return "", fmt.Errorf("store tokens: %w", err)
	}
	// Save() does not clear the refresh token, but it also never writes it —
	// persist it explicitly here:
	if err := s.remoteStore.SaveRefreshToken(ctx, refreshToken); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	info, err := gd.Test(ctx)
	if err != nil {
		return "", fmt.Errorf("connection test after connect: %w", err)
	}
	return info, nil
}
```

`SaveRefreshToken` is a small addition to `remotestorage/config.go`:

```go
// SaveRefreshToken persists the Drive refresh token (written only by the
// OAuth exchange, never by the config form).
func (s *ConfigStore) SaveRefreshToken(ctx context.Context, token string) error {
	return s.upsert(keyGDRefreshToken, token)
}
```

(refactor the upsert loop body of `Save` into a private `upsert(key, value string) error` helper and reuse it.)

And `newGDriveForConfig` in `internal/backup/remote.go` so tests can override:

```go
// gdriveClient is the subset of *remotestorage's gdrive storage the exchange
// flow needs; *remotestorage.GDriveStorage satisfies it.
type gdriveClient interface {
	AuthorizeURL() string
	ExchangeCode(ctx context.Context, code string) (string, error)
	EnsureFolder(ctx context.Context) (string, error)
	Test(ctx context.Context) (string, error)
}

var newGDriveForConfig = func(cfg remotestorage.Config, exec executor.CommandExecutor, hc *http.Client) gdriveClient {
	return remotestorage.NewGDrive(cfg, exec, hc)
}
```

(`GDriveStorage` and `NewGDrive` were exported in Task 4, so no rename is needed here. `remote.go` imports `context`, `fmt`, `net/http`, `time`, `github.com/mohammadirham37/jenderal_panel/internal/auth`, `.../internal/executor`, `.../internal/model`, `.../internal/remotestorage`, `.../internal/taskrunner`.)

- [ ] **Step 4: Implement the handlers**

Create `internal/backup/remote_handler.go`:

```go
package backup

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// GetRemoteConfig handles GET /api/v1/backup-remote/config.
func (h *Handler) GetRemoteConfig(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.RemoteConfigView(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, view)
}

// SaveRemoteConfig handles PUT /api/v1/backup-remote/config.
func (h *Handler) SaveRemoteConfig(w http.ResponseWriter, r *http.Request) {
	var req RemoteConfigRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.SaveRemoteConfig(r.Context(), req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "save_remote_config", req.Type, "remote storage config saved (type "+req.Type+")")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// TestRemote handles POST /api/v1/backup-remote/test.
func (h *Handler) TestRemote(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.TestRemoteConnection(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"info": info})
}

// GDriveAuthorize handles GET /api/v1/backup-remote/gdrive/authorize.
func (h *Handler) GDriveAuthorize(w http.ResponseWriter, r *http.Request) {
	url, err := h.svc.GDriveAuthorizeURL(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"url": url})
}

// GDriveExchange handles POST /api/v1/backup-remote/gdrive/exchange.
func (h *Handler) GDriveExchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	info, err := h.svc.GDriveExchange(r.Context(), req.Code)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "gdrive_connect", info, "Google Drive connected as "+info)
	httputil.JSON(w, http.StatusOK, map[string]string{"info": info})
}
```

- [ ] **Step 5: Wire router, main.go, RBAC**

1. `internal/api/router.go` — after the existing backup routes (~line 863):

```go
			r.With(auth.RequirePermission(deps.RBAC, "backups.remote")).
				Get("/backup-remote/config", backupHandler.GetRemoteConfig)
			r.With(auth.RequirePermission(deps.RBAC, "backups.remote")).
				Put("/backup-remote/config", backupHandler.SaveRemoteConfig)
			r.With(auth.RequirePermission(deps.RBAC, "backups.remote")).
				Post("/backup-remote/test", backupHandler.TestRemote)
			r.With(auth.RequirePermission(deps.RBAC, "backups.remote")).
				Get("/backup-remote/gdrive/authorize", backupHandler.GDriveAuthorize)
			r.With(auth.RequirePermission(deps.RBAC, "backups.remote")).
				Post("/backup-remote/gdrive/exchange", backupHandler.GDriveExchange)
			r.With(auth.RequirePermission(deps.RBAC, "backups.create")).
				Post("/backups/{id}/upload", backupHandler.RetryUpload)
```

2. `cmd/jenderal/main.go` after `backupSvc := backup.NewService(...)` (line 262):

```go
	backupSvc.SetRemoteStore(remotestorage.NewConfigStore(db))
```

with import `"github.com/mohammadirham37/jenderal_panel/internal/remotestorage"`.

3. `internal/auth/rbac.go` permissions list after `{"backups.delete", "backups"},`:

```go
		{"backups.remote", "backups"},
```

`userRolePermissions` stays unchanged (backups.remote is admin-only). Admin gets all permissions automatically.

4. `AGENTS.md`: replace "63 RBAC permissions" with "74 RBAC permissions".

- [ ] **Step 6: Run all Go tests**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add AGENTS.md cmd/jenderal/main.go internal/
git commit -m "feat(backup): remote storage config API with connection test and Google Drive OAuth"
```

---

### Task 10: Frontend — Storage tab, badges, retry upload, i18n

**Files:**
- Create: `web/src/lib/components/BackupStoragePanel.svelte`
- Modify: `web/src/routes/backups/+page.svelte`
- Modify: `web/src/lib/i18n/domains/bksrv.ts` (both `en` and `id`)

**Interfaces:**
- Consumes: endpoints from Task 9 (`/api/v1/backup-remote/*`), `model.Backup.remote_only`, `POST /api/v1/backups/{id}/upload`.
- Produces: Storage tab UI; `remote_only` badge; retry-upload button + task progress.

- [ ] **Step 1: Create `BackupStoragePanel.svelte`**

```svelte
<script lang="ts">
	import { api } from '$lib/api';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface RemoteConfig {
		type: string;
		endpoint: string;
		bucket: string;
		region: string;
		access_key: string;
		prefix: string;
		use_tls: boolean;
		s3_secret_set: boolean;
		gdrive_client_id: string;
		gdrive_client_secret_set: boolean;
		gdrive_connected: boolean;
		gdrive_folder_id: string;
		rclone_remote: string;
		rclone_path: string;
		delete_local_after_upload: boolean;
	}

	let cfg = $state<RemoteConfig | null>(null);
	let loadError = $state('');
	let forbidden = $state(false);
	let saving = $state(false);
	let testing = $state(false);
	let testInfo = $state('');
	let testError = $state('');

	// S3 secret + Drive client secret are write-only; blank = keep stored.
	let s3Secret = $state('');
	let gdClientSecret = $state('');

	// Google Drive wizard
	let authUrl = $state('');
	let authCode = $state('');
	let connecting = $state(false);

	async function load() {
		loadError = '';
		forbidden = false;
		try {
			cfg = await api.get<RemoteConfig>('/api/v1/backup-remote/config');
		} catch (err) {
			if (err instanceof Error && /403|forbidden/i.test(err.message)) {
				forbidden = true;
			} else {
				loadError = err instanceof Error ? err.message : 'load failed';
			}
		}
	}

	async function save() {
		if (!cfg || saving) return;
		saving = true;
		try {
			await api.put('/api/v1/backup-remote/config', {
				type: cfg.type,
				endpoint: cfg.endpoint,
				bucket: cfg.bucket,
				region: cfg.region || 'us-east-1',
				access_key: cfg.access_key,
				s3_secret_key: s3Secret,
				prefix: cfg.prefix,
				use_tls: cfg.use_tls,
				gdrive_client_id: cfg.gdrive_client_id,
				gdrive_client_secret: gdClientSecret,
				rclone_remote: cfg.rclone_remote,
				rclone_path: cfg.rclone_path,
				delete_local_after_upload: cfg.delete_local_after_upload
			});
			s3Secret = '';
			gdClientSecret = '';
			toast.success(translate($language, 'bk.storage.saved'));
			await load();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'bk.storage.saveFailed'));
		} finally {
			saving = false;
		}
	}

	async function testConnection() {
		if (testing) return;
		testing = true;
		testInfo = '';
		testError = '';
		try {
			const res = await api.post<{ info: string }>('/api/v1/backup-remote/test', {});
			testInfo = res?.info ?? '';
		} catch (err) {
			testError = err instanceof Error ? err.message : translate($language, 'bk.storage.testFailed');
		} finally {
			testing = false;
		}
	}

	async function startAuthorize() {
		try {
			const res = await api.get<{ url: string }>('/api/v1/backup-remote/gdrive/authorize');
			authUrl = res?.url ?? '';
			window.open(authUrl, '_blank', 'noopener');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'bk.storage.authorizeFailed'));
		}
	}

	async function connectGDrive() {
		if (!authCode.trim() || connecting) return;
		connecting = true;
		try {
			const res = await api.post<{ info: string }>('/api/v1/backup-remote/gdrive/exchange', { code: authCode.trim() });
			toast.success(translate($language, 'bk.storage.connected').replace('{account}', res?.info ?? ''));
			authUrl = '';
			authCode = '';
			await load();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'bk.storage.connectFailed'));
		} finally {
			connecting = false;
		}
	}

	const inputCls =
		'w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none';
	const labelCls = 'mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400';

	onMount(load);
</script>

<script lang="ts">
	import { onMount } from 'svelte';
</script>
```

(Merge the two script blocks into one: put `import { onMount } from 'svelte';` in the first block. The markup:)

```svelte
<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
	{#if forbidden}
		<p class="text-sm text-gray-400">{translate($language, 'bk.storage.adminOnly')}</p>
	{:else if loadError}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{loadError}</div>
	{:else if cfg}
		<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
			<h3 class="text-lg font-semibold text-white">{translate($language, 'bk.storage.title')}</h3>
			{#if cfg.type}
				<span class="rounded-md bg-green-900/50 px-2 py-0.5 text-[10px] font-semibold text-green-300">{cfg.type}</span>
			{:else}
				<span class="rounded-md bg-gray-700 px-2 py-0.5 text-[10px] font-semibold text-gray-400">{translate($language, 'bk.storage.off')}</span>
			{/if}
		</div>

		<div class="grid gap-3 sm:grid-cols-2">
			<div>
				<label class={labelCls} for="rs-type">{translate($language, 'bk.storage.backend')}</label>
				<select id="rs-type" bind:value={cfg.type} class={inputCls}>
					<option value="">{translate($language, 'bk.storage.off')}</option>
					<option value="s3">S3</option>
					<option value="gdrive">Google Drive</option>
					<option value="rclone">rclone</option>
				</select>
			</div>
			<div class="flex items-end">
				<label class="flex cursor-pointer items-center gap-2 text-sm text-gray-300">
					<input type="checkbox" bind:checked={cfg.delete_local_after_upload} class="h-4 w-4 rounded border-gray-600 bg-gray-900" />
					{translate($language, 'bk.storage.deleteLocal')}
				</label>
			</div>
		</div>
		<p class="mt-1 text-xs text-gray-500">{translate($language, 'bk.storage.deleteLocalHint')}</p>

		{#if cfg.type === 's3'}
			<div class="mt-4 grid gap-3 sm:grid-cols-2">
				<div class="sm:col-span-2">
					<label class={labelCls} for="rs-endpoint">{translate($language, 'bk.storage.endpoint')}</label>
					<input id="rs-endpoint" bind:value={cfg.endpoint} placeholder="https://s3.wasabisys.com" class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-bucket">{translate($language, 'bk.storage.bucket')}</label>
					<input id="rs-bucket" bind:value={cfg.bucket} class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-region">{translate($language, 'bk.storage.region')}</label>
					<input id="rs-region" bind:value={cfg.region} placeholder="us-east-1" class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-ak">{translate($language, 'bk.storage.accessKey')}</label>
					<input id="rs-ak" bind:value={cfg.access_key} class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-sk">{translate($language, 'bk.storage.secretKey')}</label>
					<input id="rs-sk" type="password" bind:value={s3Secret} placeholder={cfg.s3_secret_set ? '••••••••' : ''} class={inputCls} />
					<p class="mt-1 text-[10px] text-gray-500">{translate($language, 'bk.storage.secretKeep')}</p>
				</div>
				<div>
					<label class={labelCls} for="rs-prefix">{translate($language, 'bk.storage.prefix')}</label>
					<input id="rs-prefix" bind:value={cfg.prefix} placeholder="vps-1/daily" class={inputCls} />
				</div>
				<div class="flex items-end">
					<label class="flex cursor-pointer items-center gap-2 text-sm text-gray-300">
						<input type="checkbox" bind:checked={cfg.use_tls} class="h-4 w-4 rounded border-gray-600 bg-gray-900" />
						{translate($language, 'bk.storage.useTLS')}
					</label>
				</div>
			</div>
		{:else if cfg.type === 'gdrive'}
			<div class="mt-4 space-y-3">
				<ol class="list-decimal space-y-1 pl-5 text-xs text-gray-400">
					<li>{translate($language, 'bk.storage.gdStep1')}</li>
					<li>{translate($language, 'bk.storage.gdStep2')}</li>
					<li>{translate($language, 'bk.storage.gdStep3')}</li>
					<li>{translate($language, 'bk.storage.gdStep4')}</li>
				</ol>
				<div class="grid gap-3 sm:grid-cols-2">
					<div>
						<label class={labelCls} for="rs-gd-cid">{translate($language, 'bk.storage.gdClientID')}</label>
						<input id="rs-gd-cid" bind:value={cfg.gdrive_client_id} class={inputCls} />
					</div>
					<div>
						<label class={labelCls} for="rs-gd-cs">{translate($language, 'bk.storage.gdClientSecret')}</label>
						<input id="rs-gd-cs" type="password" bind:value={gdClientSecret} placeholder={cfg.gdrive_client_secret_set ? '••••••••' : ''} class={inputCls} />
						<p class="mt-1 text-[10px] text-gray-500">{translate($language, 'bk.storage.secretKeep')}</p>
					</div>
				</div>
				<div class="flex flex-wrap items-center gap-2">
					<button type="button" onclick={startAuthorize}
						class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700">
						{translate($language, 'bk.storage.gdAuthorize')}
					</button>
					<input bind:value={authCode} placeholder={translate($language, 'bk.storage.gdCodePlaceholder')} class="w-72 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200" />
					<button type="button" onclick={connectGDrive} disabled={connecting || !authCode.trim()}
						class="cursor-pointer rounded-lg bg-green-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-green-700 disabled:opacity-40">
						{connecting ? translate($language, 'bk.storage.connecting') : translate($language, 'bk.storage.gdConnect')}
					</button>
					{#if cfg.gdrive_connected}
						<span class="rounded-md bg-green-900/50 px-2 py-0.5 text-[10px] font-semibold text-green-300">{translate($language, 'bk.storage.gdConnected')}</span>
					{/if}
				</div>
				{#if authUrl}
					<p class="text-xs text-gray-500">
						{translate($language, 'bk.storage.gdUrlFallback')}:
						<a href={authUrl} target="_blank" rel="noopener" class="text-blue-400 underline">{authUrl}</a>
					</p>
				{/if}
			</div>
		{:else if cfg.type === 'rclone'}
			<div class="mt-4 grid gap-3 sm:grid-cols-2">
				<div>
					<label class={labelCls} for="rs-rc-remote">{translate($language, 'bk.storage.rcRemote')}</label>
					<input id="rs-rc-remote" bind:value={cfg.rclone_remote} placeholder="gdrive" class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-rc-path">{translate($language, 'bk.storage.rcPath')}</label>
					<input id="rs-rc-path" bind:value={cfg.rclone_path} placeholder="backups" class={inputCls} />
				</div>
			</div>
			<p class="mt-1 text-xs text-gray-500">{translate($language, 'bk.storage.rcHint')}</p>
		{/if}

		<div class="mt-4 flex flex-wrap items-center gap-2">
			<button type="button" onclick={save} disabled={saving}
				class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-40">
				{saving ? translate($language, 'bk.storage.saving') : translate($language, 'bk.storage.save')}
			</button>
			<button type="button" onclick={testConnection} disabled={testing || !cfg.type}
				class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{testing ? translate($language, 'bk.storage.testing') : translate($language, 'bk.storage.test')}
			</button>
			{#if testInfo}<span class="text-xs text-green-400">{testInfo}</span>{/if}
			{#if testError}<span class="text-xs text-red-400">{testError}</span>{/if}
		</div>
	{/if}
</div>
```

- [ ] **Step 2: Integrate into the backups page**

In `web/src/routes/backups/+page.svelte`:

1. Import: `import BackupStoragePanel from '$lib/components/BackupStoragePanel.svelte';`
2. `activeTab` type: `$state<'backups' | 'schedules' | 'storage'>('backups')`.
3. Add third tab button after the schedules one:

```svelte
		<button
			type="button"
			onclick={() => (activeTab = 'storage')}
			class="cursor-pointer rounded-lg px-4 py-1.5 text-xs font-semibold transition {activeTab === 'storage' ? 'bg-blue-500/20 text-blue-200' : 'text-gray-400 hover:text-gray-200'}"
		>{translate($language, 'bk.tabStorage')}</button>
```

4. The schedules tab body currently opens with a bare `{:else}` (line ~867). Convert it and add the storage branch, so the chain ends:

```svelte
	{:else if activeTab === 'schedules'}
		<div class="rounded-xl border border-gray-700 bg-gray-800">
		<!-- existing schedules markup unchanged -->
		</div>
	{:else if activeTab === 'storage'}
		<BackupStoragePanel />
	{/if}

5. `Backup` interface gains `remote_only?: boolean;` after `remote_path?: string;`.
6. Location badge — replace the existing `{#if b.remote_path}` block (lines ~769-771):

```svelte
							{#if b.remote_only}
								<span class="rounded-md bg-indigo-900/50 px-1.5 py-0.5 text-[10px] font-semibold text-indigo-300" title={b.remote_path}>{translate($language, 'bk.remoteOnly')}</span>
							{:else if b.remote_path}
								<span class="rounded-md bg-teal-900/50 px-1.5 py-0.5 text-[10px] font-semibold text-teal-300" title={b.remote_path}>{translate($language, 'bk.offSite')}</span>
							{/if}
```

7. Retry-upload: add state near the other task states:

```ts
	let uploadConfirmId = $state<string | null>(null);
	let uploadTaskId = $state('');
```

Task progress block next to the restore one:

```svelte
	{#if uploadTaskId}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'bk.progressUpload')}</p>
			<TaskProgress bind:taskId={uploadTaskId} storageKey="backup-upload-task" onComplete={loadBackups} />
		</div>
	{/if}
```

Action:

```ts
	async function retryUpload(id: string) {
		try {
			const res = await api.post<{ task_id?: string }>(`/api/v1/backups/${id}/upload`, {});
			flash(translate($language, 'bk.toastUploadStarted'));
			if (res?.task_id) uploadTaskId = res.task_id;
			await loadBackups();
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorUpload'), true);
		}
	}
```

Row button, placed before the restore button in the actions area (condition: completed, no remote copy, not safety, has local file):

```svelte
									{#if b.status === 'completed' && !b.remote_path && b.kind !== 'safety' && b.remote_only !== true}
										<button
											type="button"
											onclick={() => retryUpload(b.id)}
											title={translate($language, 'bk.titleUpload')}
											class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-blue-600 hover:text-white"
										>
											<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 8V6a2 2 0 012-2h12a2 2 0 012 2v2M12 20V8m0 0l-4 4m4-4l4 4" /></svg>
										</button>
									{/if}
```

8. Remote-only restore note — extend `safetyNote` usage: add helper

```ts
	function remoteRestoreNote(b: Backup): string {
		return b.remote_only ? ' ' + translate($language, 'bk.remoteRestoreNote') : '';
	}
```

and change the confirm span (line ~787) to:

```svelte
										<span class="mr-1 text-[11px] text-yellow-400">
											{translate($language, 'bk.restoreConfirm')}{safetyNote(b)}{remoteRestoreNote(b)}?
										</span>
```

- [ ] **Step 3: Add i18n keys**

In `web/src/lib/i18n/domains/bksrv.ts`, add to `en` (backups section):

```ts
	// Remote storage
	'bk.tabStorage': 'Storage',
	'bk.remoteOnly': 'Remote',
	'bk.progressUpload': 'Upload progress',
	'bk.toastUploadStarted': 'Upload to remote storage started',
	'bk.errorUpload': 'Failed to start the upload',
	'bk.titleUpload': 'Upload this backup to remote storage',
	'bk.remoteRestoreNote': 'this will first download the backup from remote storage',
	'bk.storage.title': 'Remote backup storage',
	'bk.storage.off': 'Off',
	'bk.storage.backend': 'Backend',
	'bk.storage.deleteLocal': 'Delete local file after successful upload',
	'bk.storage.deleteLocalHint': 'Disk-saving mode: the remote copy becomes the only copy. Failed uploads keep the local file with a retry button.',
	'bk.storage.endpoint': 'Endpoint',
	'bk.storage.bucket': 'Bucket',
	'bk.storage.region': 'Region',
	'bk.storage.accessKey': 'Access key',
	'bk.storage.secretKey': 'Secret key',
	'bk.storage.secretKeep': 'Leave blank to keep the stored value',
	'bk.storage.prefix': 'Prefix',
	'bk.storage.useTLS': 'Use TLS',
	'bk.storage.gdStep1': 'In Google Cloud Console, enable the Drive API and create an OAuth client of type "Desktop app".',
	'bk.storage.gdStep2': 'Save the client ID and secret here.',
	'bk.storage.gdAuthorize': 'Open Google authorization page',
	'bk.storage.gdStep3': 'Authorize, then copy the code from the browser address bar (the page itself will not load — that is expected).',
	'bk.storage.gdStep4': 'Paste the code and connect.',
	'bk.storage.gdClientID': 'Client ID',
	'bk.storage.gdClientSecret': 'Client secret',
	'bk.storage.gdCodePlaceholder': 'Paste authorization code…',
	'bk.storage.gdConnect': 'Connect',
	'bk.storage.gdConnected': 'Connected',
	'bk.storage.gdUrlFallback': 'If the page did not open',
	'bk.storage.connected': 'Google Drive connected as {account}',
	'bk.storage.connecting': 'Connecting…',
	'bk.storage.connectFailed': 'Failed to connect Google Drive',
	'bk.storage.authorizeFailed': 'Failed to start authorization',
	'bk.storage.rcRemote': 'rclone remote name',
	'bk.storage.rcPath': 'Remote path',
	'bk.storage.rcHint': 'rclone must be installed on the server and the remote configured for root (sudo rclone config).',
	'bk.storage.save': 'Save',
	'bk.storage.saving': 'Saving…',
	'bk.storage.saved': 'Remote storage configuration saved',
	'bk.storage.saveFailed': 'Failed to save the configuration',
	'bk.storage.test': 'Test connection',
	'bk.storage.testing': 'Testing…',
	'bk.storage.testFailed': 'Connection test failed',
	'bk.storage.adminOnly': 'Remote storage is managed by administrators.',
```

And the matching `id` entries (TypeScript enforces exact key parity):

```ts
	// Remote storage
	'bk.tabStorage': 'Storage',
	'bk.remoteOnly': 'Remote',
	'bk.progressUpload': 'Progres upload',
	'bk.toastUploadStarted': 'Upload ke storage remote dimulai',
	'bk.errorUpload': 'Gagal memulai upload',
	'bk.titleUpload': 'Upload backup ini ke storage remote',
	'bk.remoteRestoreNote': 'backup akan diunduh dulu dari storage remote',
	'bk.storage.title': 'Storage backup remote',
	'bk.storage.off': 'Nonaktif',
	'bk.storage.backend': 'Backend',
	'bk.storage.deleteLocal': 'Hapus file lokal setelah upload sukses',
	'bk.storage.deleteLocalHint': 'Mode hemat disk: copy remote menjadi satu-satunya copy. Upload yang gagal tetap menyimpan file lokal dengan tombol retry.',
	'bk.storage.endpoint': 'Endpoint',
	'bk.storage.bucket': 'Bucket',
	'bk.storage.region': 'Region',
	'bk.storage.accessKey': 'Access key',
	'bk.storage.secretKey': 'Secret key',
	'bk.storage.secretKeep': 'Biarkan kosong untuk memakai nilai tersimpan',
	'bk.storage.prefix': 'Prefix',
	'bk.storage.useTLS': 'Gunakan TLS',
	'bk.storage.gdStep1': 'Di Google Cloud Console, aktifkan Drive API dan buat OAuth client bertipe "Desktop app".',
	'bk.storage.gdStep2': 'Simpan client ID dan secret di sini.',
	'bk.storage.gdAuthorize': 'Buka halaman izin Google',
	'bk.storage.gdStep3': 'Beri izin, lalu salin kode dari address bar browser (halamannya memang tidak akan terbuka — itu normal).',
	'bk.storage.gdStep4': 'Tempel kode lalu hubungkan.',
	'bk.storage.gdClientID': 'Client ID',
	'bk.storage.gdClientSecret': 'Client secret',
	'bk.storage.gdCodePlaceholder': 'Tempel kode otorisasi…',
	'bk.storage.gdConnect': 'Hubungkan',
	'bk.storage.gdConnected': 'Terhubung',
	'bk.storage.gdUrlFallback': 'Jika halaman tidak terbuka',
	'bk.storage.connected': 'Google Drive terhubung sebagai {account}',
	'bk.storage.connecting': 'Menghubungkan…',
	'bk.storage.connectFailed': 'Gagal menghubungkan Google Drive',
	'bk.storage.authorizeFailed': 'Gagal memulai otorisasi',
	'bk.storage.rcRemote': 'Nama remote rclone',
	'bk.storage.rcPath': 'Path remote',
	'bk.storage.rcHint': 'rclone harus terpasang di server dan remote dikonfigurasi untuk root (sudo rclone config).',
	'bk.storage.save': 'Simpan',
	'bk.storage.saving': 'Menyimpan…',
	'bk.storage.saved': 'Konfigurasi storage remote tersimpan',
	'bk.storage.saveFailed': 'Gagal menyimpan konfigurasi',
	'bk.storage.test': 'Uji koneksi',
	'bk.storage.testing': 'Menguji…',
	'bk.storage.testFailed': 'Uji koneksi gagal',
	'bk.storage.adminOnly': 'Storage remote dikelola oleh administrator.',
```

- [ ] **Step 4: Verify**

Run: `cd web && npm run check && npm run build`
Expected: 0 errors both.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/components/BackupStoragePanel.svelte web/src/routes/backups/+page.svelte web/src/lib/i18n/domains/bksrv.ts
git commit -m "feat(web): storage tab for remote backup configuration with remote badges and retry upload"
```

---

### Task 11: Frontend cleanup — remove the Settings-page form

**Files:**
- Modify: `web/src/routes/settings/+page.svelte`
- Modify: `web/src/lib/i18n/domains/bksrv.ts` (remove now-unused `set.remote.*` keys if truly unused)

- [ ] **Step 1: Remove the remote state, load, and save handling**

Delete from `web/src/routes/settings/+page.svelte`:

1. The `remoteKeys` const and `remote = $state<Record<string, string>>({...})` block (lines ~191-205).
2. In the settings-load function, the loop that copies `backup_remote*` keys into `remote` (lines ~209-217).
3. In the save function, the `for (const key of remoteKeys) payload[key] = remote[key] ?? '';` block (lines ~222-226) and its `set.remote.saved`/`set.remote.saveFailed` toast usage if it is a dedicated branch.
4. The markup section rendering `set.remote.title` (the `<h3>` at ~line 434 through the end of that card's `</div>` — locate by `{translate($language, 'set.remote.title')}`).

- [ ] **Step 2: Remove orphaned i18n keys**

Run: `grep -rn "set\.remote\.\|'set.type'" web/src --include='*.svelte' --include='*.ts' | grep -v bksrv.ts`

If `set.remote.*` (and `set.type`, if only the removed form used it) have no remaining usages, delete those keys from BOTH `en` and `id` in `web/src/lib/i18n/domains/bksrv.ts`. Keep any key that is still referenced.

- [ ] **Step 3: Verify**

Run: `cd web && npm run check && npm run build && npm test`
Expected: 0 errors, build and tests pass.

- [ ] **Step 4: Commit**

```bash
git add web/src/routes/settings/+page.svelte web/src/lib/i18n/domains/bksrv.ts
git commit -m "refactor(web): move remote backup storage config from settings to the backups page"
```

---

### Task 12: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full Go suite**

Run: `go test ./... -race && make lint`
Expected: PASS, no lint findings.

- [ ] **Step 2: Run the frontend gates**

Run: `cd web && npm run check && npm run build && npm test`
Expected: `npm run check` with 0 errors; build and tests pass.

- [ ] **Step 3: Manual sanity review of the diff**

Run: `git log --oneline 3eb64f0..HEAD && git diff 3eb64f0..HEAD --stat`
Check: migration file present and numbered `043_`; no secrets in any GET response path; no `sh -c`; RBAC seed includes `backups.remote` and `userRolePermissions` does not.

- [ ] **Step 4: Report**

Summarize: what was built, test results, and that the maintainer should test on Ubuntu (per AGENTS.md the maintainer tests manually; do not launch the panel).
