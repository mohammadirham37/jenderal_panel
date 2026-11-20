package dbmanager

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Access levels for a database user's grant on a managed database.
const (
	AccessFull     = "full"
	AccessReadOnly = "readonly"
)

// readOnlyGranter is implemented by engines that can give a user read-only
// access to a database.
type readOnlyGranter interface {
	GrantReadOnly(ctx context.Context, username, database string) error
}

// GrantReadOnly leaves the user with SELECT and SHOW VIEW only. The first
// GRANT makes sure a grant row exists, so the REVOKE that strips earlier
// full access cannot fail with "no such grant" on MySQL or MariaDB.
func (m *MySQLEngine) GrantReadOnly(ctx context.Context, username, database string) error {
	if err := checkMySQLUser(username); err != nil {
		return err
	}
	if err := checkMySQLDatabase(database); err != nil {
		return err
	}
	target := fmt.Sprintf("`%s`.* TO '%s'@'localhost'", database, username)
	stmt := fmt.Sprintf("GRANT SELECT ON %s; REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'localhost'; GRANT SELECT, SHOW VIEW ON %s; FLUSH PRIVILEGES",
		target, database, username, target)
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", stmt)
	if err != nil {
		return fmt.Errorf("mysql grant read-only: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to grant read-only access: "+result.Stderr, nil)
	}
	return nil
}

// GrantReadOnly gives the user CONNECT, schema USAGE and SELECT on every
// table and sequence in the public schema, plus default privileges so
// tables the current owners create later are readable too.
//
// A role always keeps full rights on objects it owns, so a user that owns
// tables in the database cannot be made read-only.
func (p *PostgreSQLEngine) GrantReadOnly(ctx context.Context, username, database string) error {
	if err := checkPostgresName("user", username); err != nil {
		return err
	}
	if err := checkPostgresName("database", database); err != nil {
		return err
	}
	owned, err := p.queryInDatabase(ctx, database, fmt.Sprintf(
		"SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND pg_get_userbyid(c.relowner) = %s",
		postgresString(username)))
	if err != nil {
		return err
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(owned)); n > 0 {
		return model.NewValidationError("this user owns tables in the database, so it cannot be read-only; use a separate user for read-only access")
	}

	// Drop whatever the user held before (full access, default privileges).
	if err := p.RevokePrivileges(ctx, username, database); err != nil {
		return err
	}

	ownersOut, err := p.queryInDatabase(ctx, database,
		"SELECT DISTINCT pg_get_userbyid(c.relowner) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace "+
			"WHERE n.nspname = 'public' AND c.relkind IN ('r','p','v','m','S','f') "+
			"UNION SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()")
	if err != nil {
		return err
	}

	stmts := []string{
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", database, username),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", username),
		// PostgreSQL 14 and older let PUBLIC create in the public schema;
		// panel users with full access get CREATE explicitly.
		"REVOKE CREATE ON SCHEMA public FROM PUBLIC",
		fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA public TO %s", username),
		fmt.Sprintf("GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO %s", username),
	}
	for _, owner := range strings.Fields(ownersOut) {
		if owner == username || checkPostgresName("user", owner) != nil {
			continue
		}
		stmts = append(stmts,
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT ON TABLES TO %s", owner, username),
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT ON SEQUENCES TO %s", owner, username))
	}
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-d", database, "-c", strings.Join(stmts, "; "))
	if err != nil {
		return fmt.Errorf("postgresql grant read-only: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to grant read-only access: "+result.Stderr, nil)
	}
	return nil
}

// revokeDefaultPrivilegesSQL removes the default privileges a read-only
// grant left for username in the current database; PostgreSQL refuses to
// drop a role that still appears in them. username must already be
// validated.
func revokeDefaultPrivilegesSQL(username string) string {
	return "DO $$DECLARE r text; BEGIN FOR r IN " +
		"SELECT DISTINCT pg_get_userbyid(d.defaclrole) FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) a " +
		"WHERE d.defaclnamespace = 'public'::regnamespace AND a.grantee = (SELECT oid FROM pg_roles WHERE rolname = " + postgresString(username) + ") LOOP " +
		"EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL ON TABLES FROM %I', r, " + postgresString(username) + "); " +
		"EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL ON SEQUENCES FROM %I', r, " + postgresString(username) + "); " +
		"END LOOP; END$$"
}

// queryInDatabase runs a single-value query inside database and returns
// its unaligned output.
func (p *PostgreSQLEngine) queryInDatabase(ctx context.Context, database, query string) (string, error) {
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-d", database, "-tAc", query)
	if err != nil {
		return "", fmt.Errorf("postgresql query: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("POSTGRESQL_ERROR", "query failed: "+result.Stderr, nil)
	}
	return result.Stdout, nil
}
