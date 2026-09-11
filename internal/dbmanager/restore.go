package dbmanager

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// isGzip sniffs the gzip magic bytes (1f 8b) so the restore accepts both
// plain and compressed dumps regardless of file extension.
func isGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

// gunzipIfNeeded decompresses gzipped dumps and passes plain SQL through.
func gunzipIfNeeded(data []byte) ([]byte, error) {
	if !isGzip(data) {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, model.NewValidationError("dump file is not valid gzip: " + err.Error())
	}
	sql, err := io.ReadAll(zr)
	if err != nil {
		return nil, model.NewValidationError("dump file is not valid gzip: " + err.Error())
	}
	return sql, nil
}

// restoreCommand builds the engine-specific restore command. The dump is fed
// on stdin by RunSudoWithInput. psql stops at the first error so a failed
// restore is visible instead of silently half-applied.
func restoreCommand(engineName, database string) (string, []string, error) {
	switch engineName {
	case "mysql":
		return "mysql", []string{"--database", database}, nil
	case "postgresql":
		return "sudo", []string{
			"-u", "postgres", "psql", "--set", "ON_ERROR_STOP=on", "--dbname", database,
		}, nil
	default:
		return "", nil, model.NewValidationError("restore is available for mysql and postgresql only")
	}
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
