package dbmanager

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Export formats offered on the databases page. Plain SQL restores anywhere;
// the gzip variant keeps large dumps manageable in transit.
type ExportFormat string

const (
	ExportSQL   ExportFormat = "sql"
	ExportSQLGz ExportFormat = "sql.gz"
)

type exportOptions struct {
	ContentType string
	Extension   string
	Gzip        bool
}

// exportOptionsFor validates the requested format and resolves its delivery
// options.
func exportOptionsFor(format string) (exportOptions, error) {
	switch ExportFormat(format) {
	case ExportSQL:
		return exportOptions{ContentType: "application/sql", Extension: ".sql"}, nil
	case ExportSQLGz:
		return exportOptions{ContentType: "application/gzip", Extension: ".sql.gz", Gzip: true}, nil
	default:
		return exportOptions{}, model.NewValidationError("unsupported export format: " + format)
	}
}

// dumpCommand builds the engine-specific consistent dump command. MySQL dumps
// run as root against the local server; PostgreSQL runs as the postgres
// superuser, matching the engine modules' conventions.
func dumpCommand(engineName, database string) (string, []string, error) {
	switch engineName {
	case "mysql":
		return "mysqldump", []string{
			"--single-transaction", "--routines", "--triggers", "--events", database,
		}, nil
	case "postgresql":
		return "sudo", []string{"-u", "postgres", "pg_dump", "--dbname", database}, nil
	default:
		return "", nil, model.NewValidationError("export is available for mysql and postgresql only")
	}
}

// ExportDatabase dumps a managed database in the requested format and returns
// the download filename, content type, and file contents.
func (s *Service) ExportDatabase(ctx context.Context, id, format string) (string, string, []byte, error) {
	opts, err := exportOptionsFor(format)
	if err != nil {
		return "", "", nil, err
	}

	var name, engineName string
	err = s.db.QueryRowContext(ctx,
		`SELECT name, engine FROM managed_databases WHERE id = ?`, id,
	).Scan(&name, &engineName)
	if err == sql.ErrNoRows {
		return "", "", nil, model.ErrNotFound
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("query managed database: %w", err)
	}

	bin, args, err := dumpCommand(engineName, name)
	if err != nil {
		return "", "", nil, err
	}

	result, err := s.exec.RunSudo(ctx, bin, args...)
	if err != nil {
		return "", "", nil, fmt.Errorf("database dump: %w", err)
	}
	if result.ExitCode != 0 {
		return "", "", nil, model.NewDomainError("DB_DUMP_FAILED",
			strings.TrimSpace(result.Stderr), nil)
	}

	data := []byte(result.Stdout)
	if opts.Gzip {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, werr := zw.Write(data); werr != nil {
			return "", "", nil, fmt.Errorf("compress dump: %w", werr)
		}
		if cerr := zw.Close(); cerr != nil {
			return "", "", nil, fmt.Errorf("compress dump: %w", cerr)
		}
		data = buf.Bytes()
	}

	filename := fmt.Sprintf("%s-%s%s", name,
		time.Now().UTC().Format("20060102-150405"), opts.Extension)
	return filename, opts.ContentType, data, nil
}
