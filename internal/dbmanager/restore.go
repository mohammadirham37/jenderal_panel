package dbmanager

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/dbdump"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// isGzip sniffs the gzip magic bytes via the shared dbdump package.
func isGzip(data []byte) bool {
	return dbdump.IsGzip(data)
}

// gunzipIfNeeded decompresses gzipped dumps and passes plain SQL through.
func gunzipIfNeeded(data []byte) ([]byte, error) {
	return dbdump.DecompressIfNeeded(data)
}

// restoreCommand builds the engine-specific restore command via the shared
// dbdump package. The dump is fed on stdin by RunSudoWithInput.
func restoreCommand(engineName, database string) (string, []string, error) {
	return dbdump.RestoreCommand(engineName, database)
}

// RestoreDatabase replaces the contents of a managed database with the given
// dump (plain or gzipped SQL, auto-detected). The dump is executed as the
// database superuser, so objects owned by other roles are dropped/recreated
// just like a manual superuser restore.
func (s *Service) RestoreDatabase(ctx context.Context, id string, content []byte) error {
	if len(content) == 0 {
		return model.NewValidationError("dump file is empty")
	}

	var name, engineName string
	err := s.db.QueryRowContext(ctx,
		`SELECT name, engine FROM managed_databases WHERE id = ?`, id,
	).Scan(&name, &engineName)
	if err == sql.ErrNoRows {
		return model.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query managed database: %w", err)
	}

	sqlDump, err := gunzipIfNeeded(content)
	if err != nil {
		return err
	}

	bin, args, err := restoreCommand(engineName, name)
	if err != nil {
		return err
	}

	result, err := s.exec.RunSudoWithInput(ctx, string(sqlDump), bin, args...)
	if err != nil {
		return fmt.Errorf("database restore: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DB_RESTORE_FAILED",
			strings.TrimSpace(result.Stderr), nil)
	}
	return nil
}
