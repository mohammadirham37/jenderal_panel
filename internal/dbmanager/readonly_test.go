package dbmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestMySQLGrantReadOnlyStripsFullAccess(t *testing.T) {
	var stmt string
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		stmt = args[len(args)-1]
		return &executor.Result{}, nil
	}}
	if err := NewMySQLEngine(mock).GrantReadOnly(context.Background(), "reader", "shop"); err != nil {
		t.Fatal(err)
	}
	revoke := strings.Index(stmt, "REVOKE ALL PRIVILEGES ON `shop`.* FROM 'reader'@'localhost'")
	final := strings.LastIndex(stmt, "GRANT SELECT, SHOW VIEW ON `shop`.* TO 'reader'@'localhost'")
	if revoke < 0 || final < revoke {
		t.Errorf("expected revoke then SELECT, SHOW VIEW grant, got %q", stmt)
	}
	if strings.Contains(stmt, "GRANT ALL") {
		t.Errorf("read-only must not grant all: %q", stmt)
	}
	if err := NewMySQLEngine(mock).GrantReadOnly(context.Background(), "x' OR 1", "shop"); err == nil {
		t.Error("an invalid user name must be refused")
	}
}

func TestPostgreSQLGrantReadOnly(t *testing.T) {
	var calls []string
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, joined)
		switch {
		case strings.Contains(joined, "SELECT count(*)"):
			return &executor.Result{Stdout: "0\n"}, nil
		case strings.Contains(joined, "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'shop'"):
			return &executor.Result{Stdout: "postgres\n"}, nil
		case strings.Contains(joined, "SELECT DISTINCT pg_get_userbyid(c.relowner)"):
			return &executor.Result{Stdout: "web_shop\npostgres\nbad-name\n"}, nil
		}
		return &executor.Result{}, nil
	}}
	if err := NewPostgreSQLEngine(mock).GrantReadOnly(context.Background(), "reader", "shop"); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	for _, want := range []string{
		"-d shop",
		"GRANT CONNECT ON DATABASE shop TO reader",
		"GRANT USAGE ON SCHEMA public TO reader",
		"GRANT SELECT ON ALL TABLES IN SCHEMA public TO reader",
		"ALTER DEFAULT PRIVILEGES FOR ROLE web_shop IN SCHEMA public GRANT SELECT ON TABLES TO reader",
		"ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public GRANT SELECT ON TABLES TO reader",
	} {
		if !strings.Contains(last, want) {
			t.Errorf("grant statement lacks %q: %s", want, last)
		}
	}
	if strings.Contains(last, "bad-name") || strings.Contains(last, "GRANT ALL") {
		t.Errorf("unexpected grant statement: %s", last)
	}
	revoked := false
	for _, c := range calls {
		if strings.Contains(c, "REVOKE ALL PRIVILEGES ON DATABASE shop FROM reader") {
			revoked = true
		}
	}
	if !revoked {
		t.Error("earlier privileges must be revoked first")
	}
}

func TestPostgreSQLGrantReadOnlyRefusesTableOwner(t *testing.T) {
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if strings.Contains(strings.Join(args, " "), "SELECT count(*)") {
			return &executor.Result{Stdout: "3\n"}, nil
		}
		t.Fatalf("nothing may change for a table owner: %v", args)
		return nil, nil
	}}
	if err := NewPostgreSQLEngine(mock).GrantReadOnly(context.Background(), "web_shop", "shop"); err == nil {
		t.Error("a table owner cannot be read-only")
	}
}

func TestGrantAccessRecordsAndSwitchesLevel(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()
	u, err := svc.CreateDBUser(ctx, "reader", "pass", "mysql", "admin-user")
	if err != nil {
		t.Fatal(err)
	}
	mdb, err := svc.CreateDatabase(ctx, "shopdb", "mysql", "utf8mb4", "admin-user")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.GrantAccess(ctx, u.ID, mdb.ID, "admin"); err == nil {
		t.Fatal("an unknown access level must be refused")
	}
	if err := svc.GrantAccess(ctx, u.ID, mdb.ID, AccessReadOnly); err != nil {
		t.Fatal(err)
	}
	grants, err := svc.ListGrants(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Access != AccessReadOnly {
		t.Fatalf("grants = %+v", grants)
	}
	if err := svc.GrantAccess(ctx, u.ID, mdb.ID, AccessFull); err != nil {
		t.Fatal(err)
	}
	grants, _ = svc.ListGrants(ctx, "")
	if len(grants) != 1 || grants[0].Access != AccessFull {
		t.Fatalf("switching to full must update the grant, got %+v", grants)
	}
}
