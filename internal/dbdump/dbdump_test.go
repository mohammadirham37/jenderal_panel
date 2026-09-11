package dbdump

import (
	"strings"
	"testing"
)

func TestDumpCommand(t *testing.T) {
	bin, args, err := DumpCommand("mysql", "app_db")
	if err != nil || bin != "mysqldump" || !strings.HasPrefix(strings.Join(args, " "), "--single-transaction") {
		t.Fatalf("mysql DumpCommand = %s %v, %v", bin, args, err)
	}
	if args[len(args)-1] != "app_db" {
		t.Errorf("dump args must end with the database: %v", args)
	}

	// Engine aliases resolve to the same canonical command.
	bin2, args2, err := DumpCommand("mariadb", "app_db")
	if err != nil || bin2 != bin || strings.Join(args2, " ") != strings.Join(args, " ") {
		t.Errorf("mariadb alias diverged: %s %v", bin2, args2)
	}

	bin, args, err = DumpCommand("postgres", "app_db")
	if err != nil || bin != "sudo" {
		t.Fatalf("postgres DumpCommand = %s %v, %v", bin, args, err)
	}
	if strings.Join(args, " ") != "-u postgres pg_dump --dbname app_db" {
		t.Errorf("pg_dump args wrong: %v", args)
	}

	if _, _, err := DumpCommand("redis", "cache"); err == nil {
		t.Error("redis must be rejected")
	}
}

func TestDumpToFileCommandUsesPositionalParams(t *testing.T) {
	bin, args, err := DumpToFileCommand("mysql", "ap; rm -rf /", "/var/backups/x.sql")
	if err != nil || bin != "sh" {
		t.Fatalf("DumpToFileCommand = %s, %v", bin, err)
	}
	// sh -c <script> <$0=dbdump> <$1=database> <$2=path>
	if len(args) != 5 || args[0] != "-c" || args[2] != "dbdump" {
		t.Fatalf("expected sh -c <script> dbdump <db> <path>, got %v", args)
	}
	script := args[1]
	if !strings.Contains(script, `"$1"`) || !strings.Contains(script, `"$2"`) {
		t.Fatalf("script must reference $1/$2, got %q", script)
	}
	// The hostile value travels only as positional parameters and is
	// single-quote-quoted; it must never appear unquoted in the script body.
	if strings.Contains(script, "ap; rm -rf /") {
		t.Errorf("user value leaked into the script body: %q", script)
	}
	if args[3] != "ap; rm -rf /" || args[4] != "/var/backups/x.sql" {
		t.Errorf("positional params wrong: %v", args)
	}
}

func TestRestoreCommand(t *testing.T) {
	bin, args, err := RestoreCommand("mysql", "app_db")
	if err != nil || bin != "mysql" || strings.Join(args, " ") != "--database app_db" {
		t.Fatalf("mysql RestoreCommand = %s %v, %v", bin, args, err)
	}

	bin, args, err = RestoreCommand("postgresql", "app_db")
	if err != nil || bin != "sudo" {
		t.Fatalf("postgresql RestoreCommand = %s %v, %v", bin, args, err)
	}
	if !strings.Contains(strings.Join(args, " "), "ON_ERROR_STOP=on") {
		t.Errorf("psql must stop on errors: %v", args)
	}
}

func TestDecompressIfNeeded(t *testing.T) {
	plain := []byte("SELECT 1;")
	out, err := DecompressIfNeeded(plain)
	if err != nil || string(out) != "SELECT 1;" {
		t.Fatalf("plain passthrough = %q, %v", string(out), err)
	}
	if _, err := DecompressIfNeeded([]byte{0x1f, 0x8b, 0x00}); err == nil {
		t.Error("corrupt gzip must be rejected")
	}
}
