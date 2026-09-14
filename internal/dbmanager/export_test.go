package dbmanager

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
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

// PrepareExportDatabase must stream the dump into the panel-owned temp file
// itself instead of a shell redirect whose writing identity depends on how
// sudo runs it — the redirect used to fail with permission denied.
func TestPrepareExportDatabaseStreamsDumpIntoTempFile(t *testing.T) {
	var dumpContent = "-- PostgreSQL database dump\nCREATE TABLE bed.items (id serial);"
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoStreamSplitFunc: func(ctx context.Context, stdoutW, stderrW io.Writer, name string, args ...string) (int, error) {
			_, writeErr := io.WriteString(stdoutW, dumpContent)
			return 0, writeErr
		},
	}
	svc := newTestService(t, mock)
	ctx := context.Background()

	mdb, err := svc.CreateDatabase(ctx, "exportdb", "postgresql", "utf8", "admin-user")
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	exp, err := svc.PrepareExportDatabase(ctx, mdb.ID, "sql")
	if err != nil {
		t.Fatalf("PrepareExportDatabase: %v", err)
	}
	if exp.Filename != "exportdb-...sql" && !strings.HasPrefix(exp.Filename, "exportdb-") {
		t.Errorf("filename = %q, want exportdb-<timestamp>.sql", exp.Filename)
	}

	var out bytes.Buffer
	if err := exp.Write(&out); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out.String() != dumpContent {
		t.Errorf("streamed dump = %q, want %q", out.String(), dumpContent)
	}
}

func TestPrepareExportDatabaseSurfacesDumpErrors(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoStreamSplitFunc: func(ctx context.Context, stdoutW, stderrW io.Writer, name string, args ...string) (int, error) {
			stderrW.Write([]byte("pg_dump: error: connection to database failed"))
			return 1, nil
		},
	}
	svc := newTestService(t, mock)
	ctx := context.Background()

	mdb, err := svc.CreateDatabase(ctx, "brokendb", "postgresql", "utf8", "admin-user")
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	_, err = svc.PrepareExportDatabase(ctx, mdb.ID, "sql")
	if err == nil {
		t.Fatal("a failed dump must surface as an error")
	}
	if !strings.Contains(err.Error(), "connection to database failed") {
		t.Errorf("error must carry the engine stderr, got %v", err)
	}
}
