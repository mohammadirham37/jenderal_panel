package dbmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// RedisEngine implements DatabaseEngine for Redis. Since Redis does not support
// traditional databases or user management, most operations return a validation
// error. Use GetInfo and FlushAll for Redis-specific operations.
type RedisEngine struct {
	exec executor.CommandExecutor
}

// NewRedisEngine creates a new Redis engine backed by the given executor.
func NewRedisEngine(exec executor.CommandExecutor) *RedisEngine {
	return &RedisEngine{exec: exec}
}

// Install installs Redis server via apt-get.
func (r *RedisEngine) Install(ctx context.Context) error {
	result, err := r.exec.RunSudo(ctx, "apt-get", "install", "-y", "redis-server")
	if err != nil {
		return fmt.Errorf("redis install: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("REDIS_ERROR", "failed to install redis: "+result.Stderr, nil)
	}
	return nil
}

// Status returns the current status of Redis including whether it is
// installed, running, and its version string.
func (r *RedisEngine) Status(ctx context.Context) (model.EngineStatus, error) {
	status := model.EngineStatus{Name: "redis"}

	// Check if installed.
	verResult, err := r.exec.Run(ctx, "redis-cli", "--version")
	if err != nil {
		return status, nil
	}
	if verResult.ExitCode != 0 {
		return status, nil
	}
	status.Installed = true
	status.Version = strings.TrimSpace(verResult.Stdout)

	// Check if running.
	sysResult, err := r.exec.RunSudo(ctx, "systemctl", "is-active", "redis-server")
	if err != nil {
		return status, nil
	}
	status.Running = strings.TrimSpace(sysResult.Stdout) == "active"

	return status, nil
}

// Start starts the Redis service.
func (r *RedisEngine) Start(ctx context.Context) error {
	result, err := r.exec.RunSudo(ctx, "systemctl", "start", "redis-server")
	if err != nil {
		return fmt.Errorf("redis start: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("REDIS_ERROR", "failed to start redis: "+result.Stderr, nil)
	}
	return nil
}

// Stop stops the Redis service.
func (r *RedisEngine) Stop(ctx context.Context) error {
	result, err := r.exec.RunSudo(ctx, "systemctl", "stop", "redis-server")
	if err != nil {
		return fmt.Errorf("redis stop: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("REDIS_ERROR", "failed to stop redis: "+result.Stderr, nil)
	}
	return nil
}

// Restart restarts the Redis service.
func (r *RedisEngine) Restart(ctx context.Context) error {
	result, err := r.exec.RunSudo(ctx, "systemctl", "restart", "redis-server")
	if err != nil {
		return fmt.Errorf("redis restart: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("REDIS_ERROR", "failed to restart redis: "+result.Stderr, nil)
	}
	return nil
}

// CreateDatabase returns a validation error because Redis does not support
// traditional databases.
func (r *RedisEngine) CreateDatabase(_ context.Context, _, _ string) error {
	return model.NewValidationError("redis does not support named databases")
}

// DropDatabase returns a validation error because Redis does not support
// traditional databases.
func (r *RedisEngine) DropDatabase(_ context.Context, _ string) error {
	return model.NewValidationError("redis does not support named databases")
}

// ListDatabases returns a validation error because Redis does not support
// traditional databases.
func (r *RedisEngine) ListDatabases(_ context.Context) ([]string, error) {
	return nil, model.NewValidationError("redis does not support named databases")
}

// CreateUser returns a validation error because Redis does not support
// traditional user management.
func (r *RedisEngine) CreateUser(_ context.Context, _, _ string) error {
	return model.NewValidationError("redis does not support user management")
}

// DropUser returns a validation error because Redis does not support
// traditional user management.
func (r *RedisEngine) DropUser(_ context.Context, _ string) error {
	return model.NewValidationError("redis does not support user management")
}

// ListUsers returns a validation error because Redis does not support
// traditional user management.
func (r *RedisEngine) ListUsers(_ context.Context) ([]string, error) {
	return nil, model.NewValidationError("redis does not support user management")
}

// GrantPrivileges returns a validation error because Redis does not support
// traditional privilege management.
func (r *RedisEngine) GrantPrivileges(_ context.Context, _, _ string) error {
	return model.NewValidationError("redis does not support privilege management")
}

// ResetPassword returns a validation error because Redis does not support
// traditional user management.
func (r *RedisEngine) ResetPassword(_ context.Context, _, _ string) error {
	return model.NewValidationError("redis does not support user management")
}

// GetInfo returns the output of redis-cli INFO.
func (r *RedisEngine) GetInfo(ctx context.Context) (string, error) {
	result, err := r.exec.Run(ctx, "redis-cli", "INFO")
	if err != nil {
		return "", fmt.Errorf("redis info: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("REDIS_ERROR", "failed to get redis info: "+result.Stderr, nil)
	}
	return result.Stdout, nil
}

// FlushAll executes FLUSHALL on the Redis instance, removing all data.
func (r *RedisEngine) FlushAll(ctx context.Context) error {
	result, err := r.exec.Run(ctx, "redis-cli", "FLUSHALL")
	if err != nil {
		return fmt.Errorf("redis flushall: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("REDIS_ERROR", "failed to flush redis: "+result.Stderr, nil)
	}
	return nil
}
