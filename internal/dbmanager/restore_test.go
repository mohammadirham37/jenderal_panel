package dbmanager

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestIsGzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("dump")); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	if !isGzip(buf.Bytes()) {
		t.Error("gzipped data must be detected as gzip")
	}
	if isGzip([]byte("-- SQL comment\nCREATE TABLE t;")) {
		t.Error("plain sql must not be detected as gzip")
	}
	if isGzip(nil) {
		t.Error("empty data must not be detected as gzip")
	}
}

func TestGunzipIfNeeded(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("CREATE TABLE items;")); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	sql, err := gunzipIfNeeded(buf.Bytes())
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if string(sql) != "CREATE TABLE items;" {
		t.Fatalf("decompressed = %q", string(sql))
	}

	// Plain SQL passes through untouched.
	sql, err = gunzipIfNeeded([]byte("SELECT 1;"))
	if err != nil {
		t.Fatalf("plain passthrough: %v", err)
	}
	if string(sql) != "SELECT 1;" {
		t.Fatalf("plain = %q", string(sql))
	}

	// Data that claims to be gzip but is corrupt must be rejected.
	if _, err := gunzipIfNeeded([]byte{0x1f, 0x8b, 0x00, 0x01}); err == nil {
		t.Error("corrupt gzip must return an error")
	}
}

func TestRestoreCommandPerEngine(t *testing.T) {
	bin, args, err := restoreCommand("mysql", "app_db")
	if err != nil || bin != "mysql" {
		t.Fatalf("mysql restore = %s %v, err %v", bin, args, err)
	}
	if strings.Join(args, " ") != "--database app_db" {
		t.Errorf("mysql args wrong: %v", args)
	}

	bin, args, err = restoreCommand("postgresql", "app_db")
	if err != nil || bin != "sudo" {
		t.Fatalf("postgresql restore = %s %v, err %v", bin, args, err)
	}
	if !containsArg(args, "ON_ERROR_STOP=on") {
		t.Error("psql must stop on the first error so failed restores are visible")
	}
	if !containsArg(args, "app_db") {
		t.Errorf("psql args missing database: %v", args)
	}

	if _, _, err := restoreCommand("redis", "cache"); err == nil {
		t.Error("redis restore must be rejected")
	}
}

func containsArg(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestManageRestoreStreamsIntoSessionEngine(t *testing.T) {
	token := manageSessions.put(&manageSession{
		Engine: "postgresql", Username: "web_vapedist", Password: "secret",
	})
	defer manageSessions.drop(token)

	var streamArgs []string
	var fedInput string
	mock := &executor.MockExecutor{
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			streamArgs = append([]string{name}, args...)
			raw, readErr := io.ReadAll(stdin)
			if readErr != nil {
				return 1, readErr
			}
			fedInput = string(raw)
			return 0, nil
		},
	}
	svc := NewService(nil, mock, nil)

	dump := "-- dump\nCREATE TABLE items (id serial);"
	if err := svc.ManageRestore(context.Background(), token, "vapedist", strings.NewReader(dump)); err != nil {
		t.Fatalf("ManageRestore() error = %v", err)
	}

	joined := strings.Join(streamArgs, " ")
	if !strings.Contains(joined, "dbname='vapedist'") || !strings.Contains(joined, "user='web_vapedist'") {
		t.Errorf("restore must connect as the session user to the target database, got %q", joined)
	}
	if fedInput != dump {
		t.Errorf("dump must be streamed untouched, got %q", fedInput)
	}

	// A bogus token must be rejected by the session check.
	if err := svc.ManageRestore(context.Background(), "bogus", "vapedist", strings.NewReader(dump)); err == nil {
		t.Error("invalid manage session must be rejected")
	}
}

func TestManageRestoreMySQLGunzipsDump(t *testing.T) {
	token := manageSessions.put(&manageSession{
		Engine: "mysql", Username: "dbuser", Password: "p",
	})
	defer manageSessions.drop(token)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("CREATE TABLE t;")); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	var fedInput string
	mock := &executor.MockExecutor{
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			if name != "mysql" {
				t.Errorf("engine binary = %q, want mysql", name)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--database=mydb") {
				t.Errorf("mysql restore must select the database, got %q", joined)
			}
			raw, _ := io.ReadAll(stdin)
			fedInput = string(raw)
			return 0, nil
		},
	}
	svc := NewService(nil, mock, nil)

	if err := svc.ManageRestore(context.Background(), token, "mydb", bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("ManageRestore() error = %v", err)
	}
	if fedInput != "CREATE TABLE t;" {
		t.Errorf("gzipped dump must be decompressed before streaming, got %q", fedInput)
	}
}

func TestManageRestoreReportsEngineErrors(t *testing.T) {
	token := manageSessions.put(&manageSession{
		Engine: "postgresql", Username: "u", Password: "p",
	})
	defer manageSessions.drop(token)

	mock := &executor.MockExecutor{
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			stderrW.Write([]byte("permission denied for schema public"))
			return 1, nil
		},
	}
	svc := NewService(nil, mock, nil)

	err := svc.ManageRestore(context.Background(), token, "db", strings.NewReader("SELECT 1;"))
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("engine stderr must surface, got %v", err)
	}
}

func TestRealPsqlErrorsFiltersVersionNoise(t *testing.T) {
	if got := realPsqlErrors(""); got != "" {
		t.Errorf("empty stderr = %q, want empty", got)
	}
	// A PG18 dump restored onto an older server: unknown meta-command and a
	// missing SET parameter never affect the applied data.
	benign := "psql:<stdin>:5: invalid command \\restrict\n" +
		"psql:<stdin>:13: ERROR:  unrecognized configuration parameter \"transaction_timeout\"\n"
	if got := realPsqlErrors(benign); got != "" {
		t.Errorf("benign noise must not fail the restore, got %q", got)
	}
	// Real errors must be surfaced.
	real := benign + "psql:<stdin>:85: ERROR:  permission denied for schema bed\n"
	got := realPsqlErrors(real)
	if got == "" || !strings.Contains(got, "permission denied for schema bed") {
		t.Errorf("real error must surface, got %q", got)
	}
	if strings.Contains(got, "invalid command") || strings.Contains(got, "transaction_timeout") {
		t.Errorf("benign noise must be filtered out, got %q", got)
	}
}

func TestRestorePrivilegeHint(t *testing.T) {
	withHint := "ERROR:  permission denied for database vapedist"
	if got := restorePrivilegeHint(withHint); got == "" {
		t.Error("permission denied for database must produce a hint")
	}
	withHint2 := "ERROR:  must be owner of schema public"
	if got := restorePrivilegeHint(withHint2); got == "" {
		t.Error("must be owner of schema must produce a hint")
	}
	unrelated := "ERROR:  relation \"bed.t_hutang\" does not exist"
	if got := restorePrivilegeHint(unrelated); got != "" {
		t.Errorf("unrelated errors must not produce a hint, got %q", got)
	}
}

func TestManageRestoreFailsWhenStatementsError(t *testing.T) {
	token := manageSessions.put(&manageSession{
		Engine: "postgresql", Username: "u", Password: "p",
	})
	defer manageSessions.drop(token)

	var benignOnly = "psql:<stdin>:5: invalid command \\restrict\n" +
		"psql:<stdin>:13: ERROR:  unrecognized configuration parameter \"transaction_timeout\"\n"
	var withReal = benignOnly + "psql:<stdin>:85: ERROR:  permission denied for schema bed\n"

	calls := 0
	mock := &executor.MockExecutor{
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			calls++
			if calls == 1 {
				stderrW.Write([]byte(benignOnly))
				return 0, nil
			}
			stderrW.Write([]byte(withReal))
			return 0, nil
		},
	}
	svc := NewService(nil, mock, nil)

	if err := svc.ManageRestore(context.Background(), token, "vapedist", strings.NewReader("SELECT 1;")); err != nil {
		t.Fatalf("benign-only stderr must not fail the restore: %v", err)
	}
	err := svc.ManageRestore(context.Background(), token, "vapedist", strings.NewReader("SELECT 1;"))
	if err == nil || !strings.Contains(err.Error(), "permission denied for schema bed") {
		t.Fatalf("real stderr errors must fail the restore, got %v", err)
	}
}
