package dbmanager

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Every local account can read another process's command line, so database
// passwords must reach the client through the environment instead.
func TestManageQueriesKeepPasswordsOutOfArgv(t *testing.T) {
	var argv []string
	var envs []map[string]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			argv = append(argv, name+" "+strings.Join(args, " "))
			return &executor.Result{}, nil
		},
		RunSudoWithEnvFunc: func(ctx context.Context, env map[string]string, name string, args ...string) (*executor.Result, error) {
			argv = append(argv, name+" "+strings.Join(args, " "))
			envs = append(envs, env)
			return &executor.Result{Stdout: "1\n"}, nil
		},
		RunSudoWithInputStreamEnvFunc: func(ctx context.Context, env map[string]string, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			argv = append(argv, name+" "+strings.Join(args, " "))
			envs = append(envs, env)
			_, _ = io.Copy(io.Discard, stdin)
			return 0, nil
		},
	}
	svc := newTestService(t, mock)
	const secret = "sup3r-s3cret-pw"
	ctx := context.Background()

	session := &manageSession{Engine: "mysql", Username: "shop_user", Password: secret}
	if _, err := svc.runMySQL(ctx, session, "shop", "SELECT 1"); err != nil {
		t.Fatalf("runMySQL: %v", err)
	}
	pg := &manageSession{Engine: "postgresql", Username: "shop_user", Password: secret}
	if _, err := svc.runPostgres(ctx, pg, "shop", "SELECT 1"); err != nil {
		t.Fatalf("runPostgres: %v", err)
	}

	for _, line := range argv {
		if strings.Contains(line, secret) {
			t.Errorf("the password appears in the process arguments: %s", line)
		}
	}
	if len(envs) < 2 {
		t.Fatalf("expected both clients to receive the password via the environment, got %d calls", len(envs))
	}
	var sawMySQL, sawPostgres bool
	for _, env := range envs {
		if env["MYSQL_PWD"] == secret {
			sawMySQL = true
		}
		if env["PGPASSWORD"] == secret {
			sawPostgres = true
		}
	}
	if !sawMySQL || !sawPostgres {
		t.Errorf("password not passed through the environment: mysql=%v postgres=%v", sawMySQL, sawPostgres)
	}
}
