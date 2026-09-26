package dbmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Grants must NOT transfer database ownership: ALTER DATABASE ... OWNER TO
// silently strips the previous owner's access each time the same database is
// granted to someone else. Only plain GRANT statements are issued.
func TestPostgreSQLGrantPrivilegesDoesNotTransferOwnership(t *testing.T) {
	var calls [][]string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	if err := NewPostgreSQLEngine(mock).GrantPrivileges(context.Background(), "web_vapedist", "vapedist"); err != nil {
		t.Fatalf("GrantPrivileges() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 psql calls, got %v", calls)
	}

	for i, call := range calls {
		if strings.Contains(strings.Join(call, " "), "ALTER DATABASE") {
			t.Errorf("call %d must not alter database ownership, got %q", i, call)
		}
	}

	joined := strings.Join(calls[0], " ")
	if !strings.Contains(joined, "GRANT ALL PRIVILEGES ON DATABASE vapedist TO web_vapedist") {
		t.Errorf("first call must grant database privileges, got %q", joined)
	}

	joined = strings.Join(calls[1], " ")
	if !strings.Contains(joined, "-d vapedist") {
		t.Errorf("schema grant must run inside the target database, got %q", joined)
	}
	if !strings.Contains(joined, "GRANT ALL ON SCHEMA public TO web_vapedist") {
		t.Errorf("second call must grant on the public schema, got %q", joined)
	}
}

func TestPostgreSQLGrantPrivilegesSurfacesServerErrors(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1, Stderr: "database does not exist"}, nil
		},
	}
	err := NewPostgreSQLEngine(mock).GrantPrivileges(context.Background(), "u", "missing")
	if err == nil || !strings.Contains(err.Error(), "database does not exist") {
		t.Fatalf("expected the psql stderr to surface, got %v", err)
	}
}

func TestPostgreSQLRevokePrivileges(t *testing.T) {
	var calls [][]string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			if strings.Contains(strings.Join(args, " "), "pg_get_userbyid") {
				return &executor.Result{ExitCode: 0, Stdout: "web_vapedist\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	if err := NewPostgreSQLEngine(mock).RevokePrivileges(context.Background(), "web_vapedist", "vapedist"); err != nil {
		t.Fatalf("RevokePrivileges() error = %v", err)
	}

	joined := strings.Join(calls[1], " ")
	if !strings.Contains(joined, "ALTER DATABASE vapedist OWNER TO postgres") {
		t.Errorf("must transfer legacy ownership back to postgres first, got %q", joined)
	}

	joined = strings.Join(calls[2], " ")
	if !strings.Contains(joined, "REVOKE ALL PRIVILEGES ON DATABASE vapedist FROM web_vapedist") {
		t.Errorf("must revoke database privileges, got %q", joined)
	}
	if !strings.Contains(joined, "REVOKE ALL ON SCHEMA public FROM web_vapedist") {
		t.Errorf("must revoke schema privileges, got %q", joined)
	}
}

func TestPostgreSQLRevokePrivilegesSkipsOwnershipTransferForNonOwner(t *testing.T) {
	var calls [][]string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			if strings.Contains(strings.Join(args, " "), "pg_get_userbyid") {
				return &executor.Result{ExitCode: 0, Stdout: "postgres\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	if err := NewPostgreSQLEngine(mock).RevokePrivileges(context.Background(), "web_user", "vapedist"); err != nil {
		t.Fatalf("RevokePrivileges() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected owner lookup + revoke only, got %v", calls)
	}
}
