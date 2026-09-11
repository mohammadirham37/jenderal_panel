package sshserver

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	schema := `
	CREATE TABLE websites (
		id           TEXT PRIMARY KEY,
		octane_port  INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE nodejs_apps (
		id   TEXT PRIMARY KEY,
		port INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestService(t *testing.T, exec executor.CommandExecutor) *Service {
	t.Helper()
	return NewService(setupTestDB(t), exec, nil, 8443)
}

func TestCurrentPortParsesSshdTOutput(t *testing.T) {
	svc := newTestService(t, &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if name != "sshd" || args[0] != "-T" {
			t.Fatalf("expected sshd -T, got %s %v", name, args)
		}
		out := "port 2222\naddressfamily any\nlistenaddress 0.0.0.0:22\n"
		return &executor.Result{ExitCode: 0, Stdout: out}, nil
	}})

	port, err := svc.CurrentPort(context.Background())
	if err != nil {
		t.Fatalf("CurrentPort: %v", err)
	}
	if port != 2222 {
		t.Errorf("CurrentPort = %d, want 2222", port)
	}
}

func TestValidatePortRejectsUnsafeChoices(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(`INSERT INTO websites (id, octane_port) VALUES ('w1', 8100)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nodejs_apps (id, port) VALUES ('app1', 3000)`); err != nil {
		t.Fatal(err)
	}
	svc := &Service{db: db, panelPort: 8443}

	cases := map[int]bool{
		0:     false,
		22:    false, // current port
		8443:  false, // panel port
		8100:  false, // octane port
		3000:  false, // node app port
		2222:  true,
		65535: true,
		65536: false,
	}
	for port, want := range cases {
		err := svc.validatePort(context.Background(), port, 22)
		if (err == nil) != want {
			t.Errorf("validatePort(%d) error = %v, want valid=%v", port, err, want)
		}
	}
}

func TestPendingChangePersistence(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		t.Fatalf("unexpected sudo call: %s %v", name, args)
		return nil, nil
	}})

	if _, _, pending, err := svc.pendingChange(ctx); err != nil || pending {
		t.Fatalf("expected no pending change, got pending=%v err=%v", pending, err)
	}
	if err := svc.savePendingChange(ctx, 22, 2222); err != nil {
		t.Fatalf("savePendingChange: %v", err)
	}
	oldPort, newPort, pending, err := svc.pendingChange(ctx)
	if err != nil || !pending {
		t.Fatalf("expected pending change, got pending=%v err=%v", pending, err)
	}
	if oldPort != 22 || newPort != 2222 {
		t.Errorf("pending change = %d->%d, want 22->2222", oldPort, newPort)
	}
	if err := svc.clearPendingChange(ctx); err != nil {
		t.Fatalf("clearPendingChange: %v", err)
	}
	if _, _, pending, _ := svc.pendingChange(ctx); pending {
		t.Error("pending change should be cleared")
	}
}

func TestInstallDropInContentIsTwoPhase(t *testing.T) {
	var written string
	svc := newTestService(t, &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "mkdir", "mv":
				return &executor.Result{ExitCode: 0}, nil
			default:
				t.Fatalf("unexpected sudo command %s %v", name, args)
				return nil, nil
			}
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			written = input
			return &executor.Result{ExitCode: 0}, nil
		},
	})

	if err := svc.writeDropIn(context.Background(), "Port 22\nPort 2222\n"); err != nil {
		t.Fatalf("writeDropIn: %v", err)
	}
	if !strings.Contains(written, "Port 22\n") || !strings.Contains(written, "Port 2222\n") {
		t.Errorf("drop-in = %q, want both ports during phase one", written)
	}
}
