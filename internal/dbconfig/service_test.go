package dbconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type recordedCall struct {
	name string
	args []string
}

// scriptedExecutor records every call and answers via the script function.
func scriptedExec(script func(call recordedCall) (*executor.Result, error)) (*executor.MockExecutor, *[]recordedCall) {
	calls := &[]recordedCall{}
	track := func(name string, args []string) (*executor.Result, error) {
		*calls = append(*calls, recordedCall{name, args})
		return script((*calls)[len(*calls)-1])
	}
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return track(name, args)
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return track(name, args)
		},
	}, calls
}

func okResult() *executor.Result { return &executor.Result{ExitCode: 0} }

func newMockAlwaysOK() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return okResult(), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return okResult(), nil
		},
	}
}

func TestGetMySQLConfig(t *testing.T) {
	mock, _ := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		if call.name == "test" {
			return okResult(), nil // mysqld.cnf exists
		}
		if call.name == "cat" {
			return &executor.Result{ExitCode: 0, Stdout: "[mysqld]\nmax_connections = 151\n"}, nil
		}
		return okResult(), nil
	})
	svc := NewService(mock, nil)

	cfg, err := svc.Get(context.Background(), EngineMySQL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !cfg.Available {
		t.Error("config must be available")
	}
	if cfg.Path != "/etc/mysql/mysql.conf.d/mysqld.cnf" {
		t.Errorf("path = %q", cfg.Path)
	}
	if !strings.Contains(cfg.Content, "max_connections") {
		t.Errorf("content = %q", cfg.Content)
	}
}

func TestGetPostgreSQLConfigPicksNewestVersion(t *testing.T) {
	mock, _ := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		if call.name == "bash" {
			return &executor.Result{ExitCode: 0, Stdout: "9.6\n16\n"}, nil
		}
		if call.name == "test" {
			// Only the 16 config exists.
			if strings.Contains(strings.Join(call.args, " "), "/16/") {
				return okResult(), nil
			}
			return &executor.Result{ExitCode: 1}, nil
		}
		if call.name == "cat" {
			return &executor.Result{ExitCode: 0, Stdout: "max_connections = 100\n"}, nil
		}
		return okResult(), nil
	})
	svc := NewService(mock, nil)

	cfg, err := svc.Get(context.Background(), EnginePostgreSQL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg.Path != "/etc/postgresql/16/main/postgresql.conf" {
		t.Errorf("path = %q, want the newest version's config", cfg.Path)
	}
}

func TestGetReturnsNotAvailableWhenMissing(t *testing.T) {
	mock, _ := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		return &executor.Result{ExitCode: 1}, nil
	})
	svc := NewService(mock, nil)

	for _, engine := range []string{EngineMySQL, EnginePostgreSQL} {
		cfg, err := svc.Get(context.Background(), engine)
		if err != nil {
			t.Fatalf("Get(%s): %v", engine, err)
		}
		if cfg.Available {
			t.Errorf("%s config must not be available", engine)
		}
	}
}

func TestApplyRejectsUnknownEngine(t *testing.T) {
	svc := NewService(newMockAlwaysOK(), nil)
	if _, err := svc.Get(context.Background(), "redis"); err == nil {
		t.Error("unknown engine must be rejected")
	}
	if err := svc.Apply(context.Background(), "redis", "x"); err == nil {
		t.Error("unknown engine must be rejected")
	}
}

func TestApplyRejectsEmptyContent(t *testing.T) {
	svc := NewService(newMockAlwaysOK(), nil)
	if err := svc.Apply(context.Background(), EngineMySQL, "   \n"); err == nil {
		t.Error("empty content must be rejected")
	}
}

func TestApplyRestartsService(t *testing.T) {
	mock, calls := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		if call.name == "test" {
			return okResult(), nil
		}
		if call.name == "mysqld" {
			return okResult(), nil // --validate-config passes
		}
		if call.name == "systemctl" && call.args[0] == "is-active" {
			return &executor.Result{ExitCode: 0, Stdout: "active\n"}, nil
		}
		return okResult(), nil
	})
	svc := NewService(mock, nil)

	if err := svc.Apply(context.Background(), EngineMySQL, "[mysqld]\nmax_connections = 200\n"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	joined := make([]string, 0, len(*calls))
	for _, call := range *calls {
		joined = append(joined, call.name+" "+strings.Join(call.args, " "))
	}
	sequence := strings.Join(joined, "\n")
	if !strings.Contains(sequence, "cp /etc/mysql/mysql.conf.d/mysqld.cnf /etc/mysql/mysql.conf.d/mysqld.cnf.jenderal-bak") {
		t.Errorf("config must be backed up first:\n%s", sequence)
	}
	if !strings.Contains(sequence, "systemctl restart mysql") {
		t.Errorf("service must be restarted:\n%s", sequence)
	}
	if !strings.Contains(sequence, "systemctl is-active mysql") {
		t.Errorf("service state must be verified:\n%s", sequence)
	}
}

func TestApplyRollsBackWhenServiceDoesNotComeUp(t *testing.T) {
	restarts := 0
	mock, calls := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		if call.name == "test" {
			return okResult(), nil
		}
		if call.name == "systemctl" && call.args[0] == "restart" {
			restarts++
			return okResult(), nil
		}
		if call.name == "systemctl" && call.args[0] == "is-active" {
			return &executor.Result{ExitCode: 1, Stderr: "failed"}, nil
		}
		return okResult(), nil
	})
	svc := NewService(mock, nil)

	err := svc.Apply(context.Background(), EngineMySQL, "[mysqld]\n")
	if err == nil {
		t.Fatal("Apply must fail when the service does not come up")
	}
	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "DB_CONFIG_APPLY_FAILED" {
		t.Fatalf("error = %v, want DB_CONFIG_APPLY_FAILED", err)
	}
	if restarts != 2 {
		t.Errorf("restarts = %d, want 2 (apply + rollback)", restarts)
	}
	// The restore copies the backup over the config after the failed check.
	found := false
	for _, call := range *calls {
		if call.name == "cp" && len(call.args) == 2 && call.args[1] == "/etc/mysql/mysql.conf.d/mysqld.cnf" {
			if call.args[0] == "/etc/mysql/mysql.conf.d/mysqld.cnf.jenderal-bak" {
				found = true
			}
		}
	}
	if !found {
		t.Error("backup must be restored over the config")
	}
}

func TestApplyMySQLInvalidConfigRollsBack(t *testing.T) {
	mock, _ := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		if call.name == "mysqld" {
			return &executor.Result{ExitCode: 1,
				Stderr: "[ERROR] unknown variable 'max_connectio=500'"}, nil
		}
		return okResult(), nil
	})
	svc := NewService(mock, nil)

	err := svc.Apply(context.Background(), EngineMySQL, "[mysqld]\nmax_connectio=500\n")
	if err == nil {
		t.Fatal("Apply must fail on an invalid config")
	}
	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "DB_CONFIG_INVALID" {
		t.Fatalf("error = %v, want DB_CONFIG_INVALID", err)
	}
	if !strings.Contains(domainErr.Message, "unknown variable") {
		t.Errorf("engine output must surface, got %q", domainErr.Message)
	}
}

func TestApplyMySQLValidateFlagUnsupportedProceeds(t *testing.T) {
	restarted := false
	mock, _ := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		if call.name == "mysqld" {
			// MariaDB rejects the flag itself.
			return &executor.Result{ExitCode: 1,
				Stderr: "mariadbd: unknown option '--validate-config'"}, nil
		}
		if call.name == "systemctl" && call.args[0] == "restart" {
			restarted = true
		}
		return okResult(), nil
	})
	svc := NewService(mock, nil)

	if err := svc.Apply(context.Background(), EngineMySQL, "[mysqld]\n"); err != nil {
		t.Fatalf("Apply must proceed when the validator flag is unsupported: %v", err)
	}
	if !restarted {
		t.Error("service must still be restarted")
	}
}

func TestApplyPostgreSQLSkipsValidationAndRestartsPostgresqlUnit(t *testing.T) {
	mock, calls := scriptedExec(func(call recordedCall) (*executor.Result, error) {
		if call.name == "bash" {
			return &executor.Result{ExitCode: 0, Stdout: "16\n"}, nil
		}
		if call.name == "test" {
			return okResult(), nil
		}
		if call.name == "cat" {
			return okResult(), nil
		}
		return okResult(), nil
	})
	svc := NewService(mock, nil)

	if err := svc.Apply(context.Background(), EnginePostgreSQL, "max_connections = 100\n"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	joined := make([]string, 0, len(*calls))
	for _, call := range *calls {
		joined = append(joined, call.name+" "+strings.Join(call.args, " "))
	}
	if !strings.Contains(strings.Join(joined, "\n"), "systemctl restart postgresql") {
		t.Error("postgresql unit must be restarted")
	}
	for _, call := range *calls {
		if call.name == "mysqld" {
			t.Error("mysqld validator must not run for postgresql")
		}
	}
}
