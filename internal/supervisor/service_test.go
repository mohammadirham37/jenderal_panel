package supervisor

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func validRequest() model.SupervisorProcessRequest {
	yes := true
	return model.SupervisorProcessRequest{
		Name: "worker-1", Command: "node server.js", WorkingDir: "/opt/app",
		RunAs: "deploy", Env: "PORT=3000\nLOG_LEVEL=info", AutoRestart: &yes,
	}
}

func TestValidateRequestAcceptsValidProcess(t *testing.T) {
	auto, err := validateRequest(validRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !auto {
		t.Fatal("auto restart should default to true")
	}
}

func TestValidateRequestRejectsBadInput(t *testing.T) {
	cases := map[string]model.SupervisorProcessRequest{
		"empty name":    {Name: "  ", Command: "x", RunAs: "deploy"},
		"bad name":      {Name: "bad name!", Command: "x", RunAs: "deploy"},
		"empty command": {Name: "a", Command: "   ", RunAs: "deploy"},
		"multiline cmd": {Name: "a", Command: "one\ntwo", RunAs: "deploy"},
		"relative dir":  {Name: "a", Command: "x", WorkingDir: "opt/app", RunAs: "deploy"},
		"weird dir":     {Name: "a", Command: "x", WorkingDir: "/opt/a;rm", RunAs: "deploy"},
		"root user":     {Name: "a", Command: "x", RunAs: "root"},
		"bad user":      {Name: "a", Command: "x", RunAs: "Deploy User"},
		"bad env key":   {Name: "a", Command: "x", RunAs: "deploy", Env: "1BAD=1"},
	}
	for name, req := range cases {
		if _, err := validateRequest(req); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestRenderProcessUnit(t *testing.T) {
	unit := RenderProcessUnit("01ID", "worker-1", "node server.js --port '3000'", "/opt/app", "deploy", true)
	for _, want := range []string{
		"Description=Jenderal Process worker-1",
		"User=deploy",
		"WorkingDirectory=/opt/app",
		"EnvironmentFile=" + envPath("01ID"),
		"ExecStart=/bin/bash -lc 'node server.js --port '\\''3000'\\'''",
		"Restart=always",
		"RestartSec=5",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}

	noRestart := RenderProcessUnit("01ID", "w", "cmd", "", "deploy", false)
	if strings.Contains(noRestart, "RestartSec") || !strings.Contains(noRestart, "Restart=no") {
		t.Fatalf("auto_restart=false should disable restart:\n%s", noRestart)
	}
	if strings.Contains(noRestart, "WorkingDirectory") {
		t.Fatal("empty working dir must be omitted")
	}
}

func TestRenderProcEnvKeepsValidLines(t *testing.T) {
	got := renderProcEnv("A=1\n\n  B=x y \n")
	if got != "A=1\nB=x y\n" {
		t.Fatalf("env = %q", got)
	}
}

func newMock() *executor.MockExecutor {
	var systemctlCmds [][]string
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: ""}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "systemctl" {
				systemctlCmds = append(systemctlCmds, args)
				if args[0] == "is-active" {
					return &executor.Result{ExitCode: 0, Stdout: "active\n"}, nil
				}
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Skipf("sqlite driver unavailable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE supervisor_processes (
		id TEXT PRIMARY KEY, name TEXT UNIQUE, command TEXT, working_dir TEXT DEFAULT '',
		run_as TEXT, env TEXT DEFAULT '', auto_restart INTEGER DEFAULT 1,
		status TEXT DEFAULT 'stopped', created_by TEXT DEFAULT '',
		created_at TEXT, updated_at TEXT)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestCreateInstallsAssetsAndStarts(t *testing.T) {
	db := newDB(t)
	if db == nil {
		return
	}
	s := NewService(db, newMock(), nil)
	p, err := s.Create(context.Background(), validRequest(), "admin-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Name != "worker-1" || p.RunAs != "deploy" || !p.AutoRestart {
		t.Fatalf("bad process: %+v", p)
	}
	if p.Status != "active" {
		t.Fatalf("expected live status active, got %q", p.Status)
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	db := newDB(t)
	if db == nil {
		return
	}
	s := NewService(db, newMock(), nil)
	if _, err := s.Create(context.Background(), validRequest(), ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := s.Create(context.Background(), validRequest(), ""); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestDeleteRemovesRow(t *testing.T) {
	db := newDB(t)
	if db == nil {
		return
	}
	s := NewService(db, newMock(), nil)
	p, err := s.Create(context.Background(), validRequest(), "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Delete(context.Background(), p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(context.Background(), p.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestActionValidates(t *testing.T) {
	db := newDB(t)
	if db == nil {
		return
	}
	s := NewService(db, newMock(), nil)
	if err := s.Action(context.Background(), "x", "dance"); err == nil {
		t.Fatal("expected invalid action error")
	}
}
