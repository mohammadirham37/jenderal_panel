package dbmanager

import (
	"context"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// DatabaseEngine defines the interface for managing a database engine
// (MySQL, PostgreSQL, Redis, etc.) through system commands.
type DatabaseEngine interface {
	Install(ctx context.Context) error
	Status(ctx context.Context) (model.EngineStatus, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	CreateDatabase(ctx context.Context, name, charset string) error
	DropDatabase(ctx context.Context, name string) error
	ListDatabases(ctx context.Context) ([]string, error)
	CreateUser(ctx context.Context, username, password string) error
	DropUser(ctx context.Context, username string) error
	ListUsers(ctx context.Context) ([]string, error)
	GrantPrivileges(ctx context.Context, username, database string) error
	ResetPassword(ctx context.Context, username, password string) error
}
