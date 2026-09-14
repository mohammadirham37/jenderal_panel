package dbmanager

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"regexp"
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
// dump (plain or gzipped SQL, auto-detected by magic bytes). The dump is
// streamed into the engine without buffering it whole in memory and executed
// as the database superuser, so objects owned by other roles are
// dropped/recreated just like a manual superuser restore.
func (s *Service) RestoreDatabase(ctx context.Context, id string, dump io.Reader) error {
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

	// Peek the first bytes for the gzip magic number without consuming them.
	br := bufio.NewReader(dump)
	head, err := br.Peek(2)
	if err != nil {
		if err == io.EOF {
			return model.NewValidationError("dump file is empty")
		}
		return fmt.Errorf("read dump: %w", err)
	}

	var in io.Reader = br
	if dbdump.IsGzip(head) {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return model.NewValidationError("dump file is not valid gzip: " + err.Error())
		}
		defer zr.Close()
		in = zr
	}

	bin, args, err := restoreCommand(engineName, name)
	if err != nil {
		return err
	}

	var stderrBuf bytes.Buffer
	exit, err := s.exec.RunSudoWithInputStream(ctx, in, &stderrBuf, bin, args...)
	if err != nil {
		return fmt.Errorf("database restore: %w", err)
	}
	if exit != 0 {
		return model.NewDomainError("DB_RESTORE_FAILED",
			strings.TrimSpace(stderrBuf.String()), nil)
	}
	return nil
}

// ManageRestore replaces the contents of one of the management session's
// databases with the given dump (plain or gzipped SQL, auto-detected by
// magic bytes). It runs with the session's own credentials, so only
// databases the database user can write to are affected and the engine's
// permission errors surface as-is.
func (s *Service) ManageRestore(ctx context.Context, token, database string, dump io.Reader) error {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return err
	}
	if err := validateIdentifier(database); err != nil {
		return err
	}
	switch session.Engine {
	case "mysql", "postgresql":
	default:
		return model.NewValidationError("restore is available for mysql and postgresql only")
	}

	// Peek the first bytes for the gzip magic number without consuming them.
	br := bufio.NewReader(dump)
	head, err := br.Peek(2)
	if err != nil {
		if err == io.EOF {
			return model.NewValidationError("dump file is empty")
		}
		return fmt.Errorf("read dump: %w", err)
	}

	var in io.Reader = br
	if dbdump.IsGzip(head) {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return model.NewValidationError("dump file is not valid gzip: " + err.Error())
		}
		defer zr.Close()
		in = zr
	}

	var bin string
	var args []string
	switch session.Engine {
	case "mysql":
		bin = "mysql"
		args = []string{"--user=" + session.Username, "--password=" + session.Password, "--database=" + database}
	case "postgresql":
		bin = "psql"
		args = []string{pgConnInfo(session.Username, session.Password, database)}
	}

	var stderrBuf bytes.Buffer
	exit, err := s.exec.RunSudoWithInputStream(ctx, in, &stderrBuf, bin, args...)
	if err != nil {
		return fmt.Errorf("database restore: %w", err)
	}
	if exit != 0 {
		return model.NewDomainError("DB_RESTORE_FAILED",
			strings.TrimSpace(stderrBuf.String()), nil)
	}
	// psql exits 0 even when statements fail, so a restore that applied
	// nothing must not look successful: surface the real errors from stderr.
	if session.Engine == "postgresql" {
		if msg := realPsqlErrors(stderrBuf.String()); msg != "" {
			return model.NewDomainError("DB_RESTORE_FAILED", msg+restorePrivilegeHint(msg), nil)
		}
	}
	return nil
}

// restorePrivilegeHint appends actionable guidance for the most common
// restore failure: the database user lacking ownership/CREATE rights on the
// target database, so the dump's schema and tables cannot be created.
func restorePrivilegeHint(msg string) string {
	if !strings.Contains(msg, "permission denied for database") &&
		!strings.Contains(msg, "must be owner of schema") {
		return ""
	}
	return "\n\nHint: grant the database user ownership of this database from the Databases page (Grant Privileges), then retry the restore."
}

// benignPsqlNoiseRe matches error output that never affects the applied
// data: psql meta-commands from a newer client (\restrict) and SETs of
// server parameters the target version does not have. Dumps taken on
// newer PostgreSQL versions hit both when restored onto older ones.
var benignPsqlNoiseRe = regexp.MustCompile(`invalid command \\|unrecognized configuration parameter`)

// realPsqlErrors extracts actionable errors from psql stderr, or "" when
// only benign version-compat noise occurred.
func realPsqlErrors(stderr string) string {
	if !strings.Contains(stderr, "ERROR") && !strings.Contains(stderr, "invalid command") {
		return ""
	}
	var kept []string
	for _, line := range strings.Split(stderr, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || benignPsqlNoiseRe.MatchString(trimmed) {
			continue
		}
		kept = append(kept, trimmed)
	}
	if len(kept) == 0 {
		return ""
	}
	const maxLen = 2000
	msg := strings.Join(kept, "\n")
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "…"
	}
	return msg
}
