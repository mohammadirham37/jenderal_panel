package dbdump

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// The mysql and psql command-line clients interpret their own commands
// inside the SQL they read, including ones that run shell commands or read
// and write files. Restores feed operator-supplied dumps into those clients,
// so the stream is checked first and a dump that contains such a command is
// refused instead of executed.
//
// Only lines that start a new statement are checked, so ordinary identifiers
// such as a column named "source" are left alone.

// mysqlClientCommands are the mysql client's own commands that touch the
// shell or the file system.
var mysqlClientCommands = map[string]bool{
	"system": true, "source": true, "tee": true, "pager": true,
	"edit": true, "shell": true,
}

// NewSafeSQLReader returns a reader over dump that fails with a validation
// error when the dump contains a client command for the engine's CLI.
func NewSafeSQLReader(engine string, dump io.Reader) (io.Reader, error) {
	engine, err := normalizeEngine(engine)
	if err != nil {
		return nil, err
	}
	return &safeSQLReader{engine: engine, src: bufio.NewReader(dump), statementStart: true}, nil
}

type safeSQLReader struct {
	engine string
	src    *bufio.Reader
	// statementStart is true while no statement is open, which is where the
	// clients look for their own commands.
	statementStart bool
	line           int
	pending        []byte
	err            error
}

func (r *safeSQLReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		line, err := r.src.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			r.err = err
			return 0, r.err
		}
		r.line++
		if err != nil && err != io.EOF {
			r.err = err
		}
		if blocked := r.check(string(line)); blocked != "" {
			r.err = model.NewValidationError(fmt.Sprintf(
				"the dump contains the database client command %q on line %d; "+
					"restore only accepts SQL statements", blocked, r.line))
			return 0, r.err
		}
		r.pending = line
		if err == io.EOF {
			r.err = io.EOF
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// check returns the rejected client command, or "" when the line is SQL. It
// also tracks whether the next line starts a new statement.
func (r *safeSQLReader) check(line string) string {
	trimmed := strings.TrimSpace(line)
	atStart := r.statementStart

	// Blank lines and comments keep the current statement state.
	if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	if atStart {
		if blocked := r.clientCommand(trimmed); blocked != "" {
			return blocked
		}
	}
	// A statement ends at a trailing semicolon; anything else continues it.
	r.statementStart = strings.HasSuffix(trimmed, ";")
	// PostgreSQL COPY ... FROM stdin is followed by raw data terminated by
	// "\.", so no statement starts until the terminator is seen.
	if r.engine == "postgresql" {
		if atStart && isCopyFromStdin(trimmed) {
			r.statementStart = false
		} else if !atStart && trimmed == `\.` {
			r.statementStart = true
		}
	}
	return ""
}

// clientCommand reports the client command a statement-start line carries.
func (r *safeSQLReader) clientCommand(trimmed string) string {
	if strings.HasPrefix(trimmed, `\`) {
		// psql ends COPY data with "\."; the reader only reaches here
		// outside COPY, where psql rejects it as well.
		if r.engine == "postgresql" && trimmed == `\.` {
			return ""
		}
		return strings.Fields(trimmed)[0]
	}
	if r.engine == "mysql" {
		first := strings.ToLower(strings.Fields(trimmed)[0])
		if mysqlClientCommands[first] {
			return first
		}
	}
	return ""
}

// isCopyFromStdin reports whether the statement starts a COPY data block.
func isCopyFromStdin(trimmed string) bool {
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "copy ") && strings.Contains(lower, "from stdin")
}
