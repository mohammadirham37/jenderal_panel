package database

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

func Open(cfg config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", cfg.Path+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	// The database holds session hashes, TOTP secrets, and remote
	// credentials. Website shells run as other local accounts on the same
	// host, so the files must not be readable by them — including databases
	// created by older installs with the default mode.
	if err := restrictPermissions(cfg.Path); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// restrictPermissions sets the database and its WAL sidecars to 0600. The
// sidecars appear on first write, so this runs again after the connection is
// established; missing files are not an error.
func restrictPermissions(path string) error {
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(name, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restrict %s permissions: %w", name, err)
		}
	}
	return nil
}
