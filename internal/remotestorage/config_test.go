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
	if !(Config{Type: "rclone", RcloneRemote: "gdrive"}).Enabled() {
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
	if _, err := New(Config{}, nil, nil); err != ErrNotConfigured {
		t.Errorf("empty config must yield ErrNotConfigured, got %v", err)
	}
}
