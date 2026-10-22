package dbmanager

import (
	"regexp"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Identifier rules per engine. Values are interpolated into SQL that runs
// with administrative privileges, so anything outside these sets is
// rejected before a client is started.
var (
	// MySQL database names are backtick-quoted: letters, digits, _, $, -.
	mysqlDatabaseName = regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`)
	// MySQL account names are single-quoted; MySQL caps them at 32 chars.
	mysqlUserName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)
	// PostgreSQL names are used unquoted, so they must be plain identifiers.
	postgresIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	charsetName        = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)
)

func checkName(kind, value string, pattern *regexp.Regexp) error {
	if !pattern.MatchString(value) {
		return model.NewValidationError("invalid " + kind + " name: use letters, digits, and underscores")
	}
	return nil
}

func checkMySQLDatabase(name string) error { return checkName("database", name, mysqlDatabaseName) }
func checkMySQLUser(name string) error     { return checkName("user", name, mysqlUserName) }
func checkPostgresName(kind, name string) error {
	return checkName(kind, name, postgresIdentifier)
}

func checkCharset(charset string) error {
	if !charsetName.MatchString(charset) {
		return model.NewValidationError("invalid charset")
	}
	return nil
}

// checkPassword accepts any printable password; control characters are
// refused because they have no place in a password and could break the
// statement the password is embedded in.
func checkPassword(password string) error {
	if password == "" {
		return model.NewValidationError("password is required")
	}
	if len(password) > 256 {
		return model.NewValidationError("password is too long (max 256 characters)")
	}
	for _, r := range password {
		if r < 0x20 || r == 0x7f {
			return model.NewValidationError("password must not contain control characters")
		}
	}
	return nil
}

// mysqlString quotes a value as a MySQL string literal (default sql_mode:
// backslash is an escape character).
func mysqlString(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(value) + "'"
}

// postgresString quotes a value as a PostgreSQL standard string literal
// (standard_conforming_strings is on: backslashes are literal).
func postgresString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
