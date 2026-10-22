package dbmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// recordingExec counts sudo invocations so tests can assert that rejected
// input never reaches the database client.
func recordingExec(calls *[]string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			*calls = append(*calls, name+" "+strings.Join(args, " "))
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			*calls = append(*calls, name+" "+strings.Join(args, " ")+" <<< "+input)
			return &executor.Result{}, nil
		},
	}
}

func isValidation(err error) bool {
	var domainErr *model.DomainError
	return errors.As(err, &domainErr)
}

func TestMySQLEngineRejectsUnsafeIdentifiers(t *testing.T) {
	var calls []string
	engine := NewMySQLEngine(recordingExec(&calls))
	ctx := context.Background()
	for _, name := range []string{"", "a`b", "a'b", "a b", "a;b", "a\nb", strings.Repeat("x", 65)} {
		if err := engine.CreateDatabase(ctx, name, "utf8mb4"); !isValidation(err) {
			t.Errorf("CreateDatabase(%q) err = %v, want validation error", name, err)
		}
	}
	for _, charset := range []string{"utf8mb4 x", "a;b", "a`b"} {
		if err := engine.CreateDatabase(ctx, "shop", charset); !isValidation(err) {
			t.Errorf("CreateDatabase charset %q err = %v, want validation error", charset, err)
		}
	}
	for _, user := range []string{"", "a'b", "a b", "a@b", strings.Repeat("u", 33)} {
		if err := engine.CreateUser(ctx, user, "Secret-123"); !isValidation(err) {
			t.Errorf("CreateUser(%q) err = %v, want validation error", user, err)
		}
	}
	if err := engine.CreateUser(ctx, "shop_user", "line\nbreak"); !isValidation(err) {
		t.Errorf("password with a control character: err = %v, want validation error", err)
	}
	if len(calls) != 0 {
		t.Fatalf("rejected input reached the client: %v", calls)
	}
}

func TestMySQLEngineAcceptsExistingNamesAndEscapesPasswords(t *testing.T) {
	var calls []string
	engine := NewMySQLEngine(recordingExec(&calls))
	ctx := context.Background()
	for _, name := range []string{"shop", "shop_db", "Shop-2", "wp_123"} {
		if err := engine.CreateDatabase(ctx, name, "utf8mb4"); err != nil {
			t.Errorf("CreateDatabase(%q) unexpected error: %v", name, err)
		}
	}
	if err := engine.CreateUser(ctx, "shop.user-1", `it's a \ secret`); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	last := calls[len(calls)-1]
	if !strings.Contains(last, `'it''s a \\ secret'`) {
		t.Errorf("password not escaped as a string literal: %s", last)
	}
	if strings.Contains(strings.SplitN(last, " <<< ", 2)[0], "secret") {
		t.Errorf("password must not appear in the process arguments: %s", last)
	}
}

func TestPostgreSQLEngineRejectsUnsafeIdentifiers(t *testing.T) {
	var calls []string
	engine := NewPostgreSQLEngine(recordingExec(&calls))
	ctx := context.Background()
	for _, user := range []string{"", "1abc", "a-b", "a b", "a;b", `a"b`, strings.Repeat("u", 64)} {
		if err := engine.CreateUser(ctx, user, "Secret-123"); !isValidation(err) {
			t.Errorf("CreateUser(%q) err = %v, want validation error", user, err)
		}
	}
	for _, db := range []string{"a;b", "a b", "-x"} {
		if err := engine.CreateDatabase(ctx, db, ""); !isValidation(err) {
			t.Errorf("CreateDatabase(%q) err = %v, want validation error", db, err)
		}
		if err := engine.GrantPrivileges(ctx, "web_user", db); !isValidation(err) {
			t.Errorf("GrantPrivileges(db %q) err = %v, want validation error", db, err)
		}
	}
	if err := engine.ResetPassword(ctx, "web_user", "a\x00b"); !isValidation(err) {
		t.Errorf("password with NUL: err = %v, want validation error", err)
	}
	if len(calls) != 0 {
		t.Fatalf("rejected input reached the client: %v", calls)
	}

	if err := engine.CreateUser(ctx, "web_user", "it's"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	last := calls[len(calls)-1]
	if !strings.Contains(last, "'it''s'") || strings.Contains(strings.SplitN(last, " <<< ", 2)[0], "it''s") {
		t.Errorf("postgres password must be escaped and passed on stdin: %s", last)
	}
}
