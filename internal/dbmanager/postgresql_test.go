package dbmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Since PostgreSQL 15 removed the default CREATE grant on the public schema,
// granting database privileges must also hand the database to the user and
// grant on the public schema — otherwise application migrations fail with
// "permission denied for schema public".
func TestPostgreSQLGrantPrivilegesTransfersOwnershipAndSchema(t *testing.T) {
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

	joined := strings.Join(calls[0], " ")
	if !strings.Contains(joined, "ALTER DATABASE vapedist OWNER TO web_vapedist") {
		t.Errorf("first call must transfer database ownership, got %q", joined)
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
