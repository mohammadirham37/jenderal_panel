// Package dbdump holds the engine-specific database dump and restore command
// builders shared by the databases page and the backup module, so the two
// never drift. All builders return direct argv (no shell interpolation); the
// only shell usage is the dump-to-file variant whose user values travel as
// positional parameters.
package dbdump

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// normalizeEngine maps engine name aliases to the canonical form.
func normalizeEngine(engine string) (string, error) {
	switch strings.ToLower(engine) {
	case "mysql", "mariadb":
		return "mysql", nil
	case "postgresql", "postgres":
		return "postgresql", nil
	default:
		return "", model.NewValidationError("unsupported database engine: " + engine)
	}
}

// quoteShellArg single-quote-escapes a value for safe use inside a shell
// command string.
func quoteShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// DumpCommand returns the argv that writes a consistent SQL dump of database
// to standard output.
func DumpCommand(engine, database string) (string, []string, error) {
	engine, err := normalizeEngine(engine)
	if err != nil {
		return "", nil, err
	}
	switch engine {
	case "mysql":
		return "mysqldump", []string{
			"--single-transaction", "--routines", "--triggers", "--events", database,
		}, nil
	default: // postgresql
		return "sudo", []string{
			"-u", "postgres", "pg_dump", "--dbname", database,
		}, nil
	}
}

// DumpToFileCommand returns a command that writes the dump of database to
// path. A shell is used only for the redirect; both user values travel as
// positional parameters, never inside the command string.
func DumpToFileCommand(engine, database, path string) (string, []string, error) {
	bin, args, err := DumpCommand(engine, database)
	if err != nil {
		return "", nil, err
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg == database {
			// The user-controlled database travels as positional parameter $1,
			// never embedded in the script body.
			quoted[i] = `"$1"`
		} else {
			quoted[i] = quoteShellArg(arg)
		}
	}
	script := fmt.Sprintf("exec %s %s > \"$2\"", bin, strings.Join(quoted, " "))
	return "sh", []string{"-c", script, "dbdump", database, path}, nil
}

// RestoreCommand returns the argv that restores a plain SQL dump fed on
// standard input. psql stops at the first error so failed restores surface
// instead of half-applying.
func RestoreCommand(engine, database string) (string, []string, error) {
	engine, err := normalizeEngine(engine)
	if err != nil {
		return "", nil, err
	}
	switch engine {
	case "mysql":
		return "mysql", []string{"--database", database}, nil
	default: // postgresql
		return "sudo", []string{
			"-u", "postgres", "psql", "--set", "ON_ERROR_STOP=on", "--dbname", database,
		}, nil
	}
}

// IsGzip sniffs the gzip magic bytes (1f 8b) so dumps are detected by
// content rather than file extension.
func IsGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

// DecompressIfNeeded decompresses gzipped dumps and passes plain SQL through.
func DecompressIfNeeded(data []byte) ([]byte, error) {
	if !IsGzip(data) {
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
