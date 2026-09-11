package dbmanager

import (
	"strings"
	"testing"
)

func TestExportOptionsFor(t *testing.T) {
	sql, err := exportOptionsFor("sql")
	if err != nil {
		t.Fatalf("sql: %v", err)
	}
	if sql.ContentType != "application/sql" || sql.Extension != ".sql" || sql.Gzip {
		t.Errorf("sql options wrong: %+v", sql)
	}

	gz, err := exportOptionsFor("sql.gz")
	if err != nil {
		t.Fatalf("sql.gz: %v", err)
	}
	if gz.ContentType != "application/gzip" || gz.Extension != ".sql.gz" || !gz.Gzip {
		t.Errorf("sql.gz options wrong: %+v", gz)
	}

	if _, err := exportOptionsFor("zip"); err == nil {
		t.Error("unsupported format must be rejected")
	}
	if _, err := exportOptionsFor(""); err == nil {
		t.Error("empty format must be rejected")
	}
}

func TestDumpCommandPerEngine(t *testing.T) {
	bin, args, err := dumpCommand("mysql", "app_db")
	if err != nil || bin != "mysqldump" {
		t.Fatalf("mysql dump = %s %v, err %v", bin, args, err)
	}
	if !contains(args, "--single-transaction") || !contains(args, "app_db") {
		t.Errorf("mysqldump args missing essentials: %v", args)
	}

	// PostgreSQL dumps run as the postgres superuser via the same nested
	// sudo pattern the engine modules use.
	bin, args, err = dumpCommand("postgresql", "app_db")
	if err != nil || bin != "sudo" {
		t.Fatalf("postgresql dump = %s %v, err %v", bin, args, err)
	}
	if strings.Join(args, " ") != "-u postgres pg_dump --dbname app_db" {
		t.Errorf("pg_dump args wrong: %v", args)
	}

	if _, _, err := dumpCommand("redis", "cache"); err == nil {
		t.Error("redis export must be rejected")
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
