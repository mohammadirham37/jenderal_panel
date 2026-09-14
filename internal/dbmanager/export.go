package dbmanager

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/dbdump"
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

// dumpCommand builds the engine-specific consistent dump command via the
// shared dbdump package.
func dumpCommand(engineName, database string) (string, []string, error) {
	return dbdump.DumpCommand(engineName, database)
}

// ExportStream is a prepared database export: headers can be set from
// Filename/ContentType before Write streams the dump to the client.
type ExportStream struct {
	Filename    string
	ContentType string
	Write       func(w io.Writer) error
}

// PrepareExportDatabase dumps a managed database in the requested format to
// a temporary file and returns a stream for serving it. The dump happens
// during preparation so dump failures produce a clean error instead of a
// truncated download; the file is then streamed (optionally gzip-compressed)
// without loading it whole into memory.
func (s *Service) PrepareExportDatabase(ctx context.Context, id, format string) (*ExportStream, error) {
	opts, err := exportOptionsFor(format)
	if err != nil {
		return nil, err
	}

	var name, engineName string
	err = s.db.QueryRowContext(ctx,
		`SELECT name, engine FROM managed_databases WHERE id = ?`, id,
	).Scan(&name, &engineName)
	if err == sql.ErrNoRows {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query managed database: %w", err)
	}

	tmp, err := os.CreateTemp("", "jenderal-export-*.sql")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Stream the dump straight from the engine into the temp file the panel
	// process owns. A shell redirect here would make the writing identity
	// depend on how sudo runs the script, which can silently differ from the
	// file owner and fail the redirect with permission denied.
	bin, args, err := dbdump.DumpCommand(engineName, name)
	if err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	var stderrBuf bytes.Buffer
	exit, streamErr := s.exec.RunSudoStreamSplit(ctx, tmp, &stderrBuf, bin, args...)
	tmp.Close()
	if streamErr != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("database dump: %w", streamErr)
	}
	if exit != 0 {
		os.Remove(tmpPath)
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = fmt.Sprintf("exit status %d", exit)
		}
		return nil, model.NewDomainError("DB_DUMP_FAILED", detail, nil)
	}

	filename := fmt.Sprintf("%s-%s%s", name,
		time.Now().UTC().Format("20060102-150405"), opts.Extension)

	return &ExportStream{
		Filename:    filename,
		ContentType: opts.ContentType,
		Write: func(w io.Writer) error {
			defer os.Remove(tmpPath)
			f, err := os.Open(tmpPath)
			if err != nil {
				return fmt.Errorf("open dump: %w", err)
			}
			defer f.Close()

			dest := w
			if opts.Gzip {
				zw := gzip.NewWriter(w)
				defer zw.Close()
				dest = zw
			}
			if _, err := io.Copy(dest, f); err != nil {
				return fmt.Errorf("stream dump: %w", err)
			}
			return nil
		},
	}, nil
}
