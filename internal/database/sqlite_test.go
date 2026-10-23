package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

// The panel database holds session hashes, TOTP secrets, and remote
// credentials; other local accounts (website shells) must not read it.
func TestOpenRestrictsDatabaseFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.db")

	// Simulate a database created by an older install with default modes.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(path+suffix, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := Open(config.DatabaseConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			t.Errorf("%s has mode %04o, want no group or world access", path+suffix, mode)
		}
	}
}

func TestOpenRestrictsFreshDatabaseAndSidecars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	db, err := Open(config.DatabaseConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Skipf("no WAL sidecar to check: %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			t.Errorf("%s has mode %04o, want no group or world access", path+suffix, mode)
		}
	}
}
