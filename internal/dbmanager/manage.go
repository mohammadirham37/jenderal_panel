package dbmanager

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Manage session: the panel never stores database user passwords, so the
// operator unlocks a management session by supplying the password once. The
// credentials live only in process memory behind a random token and expire
// after the TTL without activity.
const (
	manageSessionTTL = 60 * time.Minute
	manageRowCap     = 1000
)

type manageSession struct {
	PanelUserID string
	Engine      string
	Username    string
	Password    string
	lastUsed    time.Time
}

type manageStore struct {
	mu       sync.Mutex
	sessions map[string]*manageSession
}

var manageSessions = &manageStore{sessions: map[string]*manageSession{}}

func (m *manageStore) put(session *manageSession) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	token := hex.EncodeToString(buf)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	session.lastUsed = time.Now()
	m.sessions[token] = session
	return token
}

func (m *manageStore) get(token string) (*manageSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[token]
	if !ok {
		return nil, false
	}
	if time.Since(session.lastUsed) > manageSessionTTL {
		delete(m.sessions, token)
		return nil, false
	}
	session.lastUsed = time.Now()
	return session, true
}

func (m *manageStore) drop(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

// sweepLocked drops expired sessions. Called with the lock held.
func (m *manageStore) sweepLocked() {
	now := time.Now()
	for token, session := range m.sessions {
		if now.Sub(session.lastUsed) > manageSessionTTL {
			delete(m.sessions, token)
		}
	}
}

// ─── Result types ─────────────────────────────────────────────────

// ManagedTable describes one table inside a managed database.
type ManagedTable struct {
	Name    string `json:"name"`
	Engine  string `json:"engine"`
	Rows    int64  `json:"rows"`
	Bytes   int64  `json:"bytes"`
	Comment string `json:"comment"`
}

// ManagedColumn describes one column of a table.
type ManagedColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Key      string `json:"key"`
	Default  string `json:"default"`
}

// ManagedRows is one page of table data. Cells are nil for SQL NULL.
type ManagedRows struct {
	Columns    []string    `json:"columns"`
	Rows       [][]*string `json:"rows"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PerPage    int         `json:"per_page"`
	TotalPages int64       `json:"total_pages"`
}

// QueryResult carries the outcome of an arbitrary SQL statement.
type QueryResult struct {
	IsSelect bool        `json:"is_select"`
	Columns  []string    `json:"columns"`
	Rows     [][]*string `json:"rows"`
	Affected int64       `json:"affected"`
	Tag      string      `json:"tag"`
	Capped   bool        `json:"capped"`
}

// ─── Unlock / lock ────────────────────────────────────────────────

// UnlockManage verifies the database user's password by connecting with it
// and returns a management session token.
func (s *Service) UnlockManage(ctx context.Context, userID, password, panelUserID string) (token, engine, username string, err error) {
	if password == "" {
		return "", "", "", model.NewValidationError("password is required")
	}

	var storedEngine string
	err = s.db.QueryRowContext(ctx,
		`SELECT username, engine FROM db_users WHERE id = ?`, userID,
	).Scan(&username, &storedEngine)
	if err == sql.ErrNoRows {
		return "", "", "", model.NewValidationError("user not found")
	}
	if err != nil {
		return "", "", "", fmt.Errorf("query db user: %w", err)
	}

	switch storedEngine {
	case "mysql":
		result, execErr := s.exec.RunSudo(ctx, "mysql",
			"--user="+username, "--password="+password, "--execute", "SELECT 1")
		if execErr != nil || result.ExitCode != 0 {
			return "", "", "", model.NewValidationError("invalid password for "+username)
		}
	case "postgresql":
		result, execErr := s.exec.RunSudo(ctx, "psql", pgConnInfo(username, password, "postgres"),
			"--tuples-only", "--no-align", "--command", "SELECT 1")
		if execErr != nil || result.ExitCode != 0 {
			return "", "", "", model.NewValidationError("invalid password for "+username)
		}
	default:
		return "", "", "", model.NewValidationError("management is available for mysql and postgresql only")
	}

	token = manageSessions.put(&manageSession{
		PanelUserID: panelUserID,
		Engine:      storedEngine,
		Username:    username,
		Password:    password,
	})
	return token, storedEngine, username, nil
}

// CloseManage drops a management session.
func (s *Service) CloseManage(token string) {
	manageSessions.drop(token)
}

// manageSessionFor validates the token and returns the live session.
func (s *Service) manageSessionFor(token string) (*manageSession, error) {
	session, ok := manageSessions.get(token)
	if !ok {
		return nil, model.NewDomainError("MANAGE_SESSION_EXPIRED",
			"management session expired or invalid", nil)
	}
	return session, nil
}

// ─── Databases ────────────────────────────────────────────────────

// ManageDatabases lists the databases visible to the managed user.
func (s *Service) ManageDatabases(ctx context.Context, token string) ([]string, error) {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return nil, err
	}

	var result *executor.Result
	switch session.Engine {
	case "mysql":
		result, err = s.runMySQL(ctx, session, "", "SHOW DATABASES")
	case "postgresql":
		result, err = s.runPostgres(ctx, session, "",
			"SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname")
	default:
		return nil, model.NewValidationError("unsupported engine")
	}
	if err != nil {
		return nil, err
	}

	names := []string{}
	lines := splitLines(result.Stdout)
	for i, line := range lines {
		if i == 0 {
			continue // column header
		}
		name := strings.TrimSpace(line)
		if name == "" || systemDatabase(session.Engine, name) {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func systemDatabase(engine, name string) bool {
	if engine == "postgresql" {
		return pgSystemDBs[name]
	}
	return mysqlSystemDBs[name]
}

// ─── Tables ───────────────────────────────────────────────────────

// ManageTables lists tables of a database with row estimates and sizes.
func (s *Service) ManageTables(ctx context.Context, token, database string) ([]ManagedTable, error) {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return nil, err
	}
	if err := validateIdentifier(database); err != nil {
		return nil, err
	}

	var result *executor.Result
	switch session.Engine {
	case "mysql":
		result, err = s.runMySQL(ctx, session, database,
			"SELECT TABLE_NAME, COALESCE(TABLE_ROWS,0), COALESCE(DATA_LENGTH+INDEX_LENGTH,0), COALESCE(TABLE_COMMENT,''), COALESCE(ENGINE,'') "+
				"FROM information_schema.TABLES WHERE TABLE_SCHEMA = "+sqlString(database)+" ORDER BY TABLE_NAME")
	case "postgresql":
		result, err = s.runPostgres(ctx, session, database,
			"SELECT c.relname, GREATEST(c.reltuples,0)::bigint, COALESCE(pg_total_relation_size(c.oid),0), COALESCE(obj_description(c.oid),''), '' "+
				"FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace "+
				"WHERE n.nspname = 'public' AND c.relkind = 'r' ORDER BY c.relname")
	default:
		return nil, model.NewValidationError("unsupported engine")
	}
	if err != nil {
		return nil, err
	}

	tables := []ManagedTable{}
	lines := splitLines(result.Stdout)
	for i, line := range lines {
		if i == 0 {
			continue // column header
		}
		fields := splitFields(session.Engine, line)
		if len(fields) < 1 || fields[0] == "" {
			continue
		}
		table := ManagedTable{Name: fields[0]}
		if len(fields) > 1 {
			table.Rows = parseInt64(fields[1])
		}
		if len(fields) > 2 {
			table.Bytes = parseInt64(fields[2])
		}
		if len(fields) > 3 {
			table.Comment = fields[3]
		}
		if len(fields) > 4 {
			table.Engine = fields[4]
		}
		tables = append(tables, table)
	}
	return tables, nil
}

// ─── Structure ────────────────────────────────────────────────────

// ManageStructure lists the columns of a table.
func (s *Service) ManageStructure(ctx context.Context, token, database, table string) ([]ManagedColumn, error) {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return nil, err
	}
	if err := validateIdentifier(database); err != nil {
		return nil, err
	}
	if err := validateIdentifier(table); err != nil {
		return nil, err
	}

	var result *executor.Result
	switch session.Engine {
	case "mysql":
		result, err = s.runMySQL(ctx, session, database, "SHOW FULL COLUMNS FROM "+quoteMySQL(table))
	case "postgresql":
		result, err = s.runPostgres(ctx, session, database, `
			SELECT c.column_name,
			       COALESCE(c.data_type || CASE WHEN c.character_maximum_length IS NOT NULL
			             THEN '(' || c.character_maximum_length || ')' ELSE '' END, c.data_type),
			       c.is_nullable,
			       COALESCE(c.column_default,''),
			       CASE WHEN EXISTS (
			           SELECT 1 FROM information_schema.table_constraints tc
			           JOIN information_schema.key_column_usage k
			             ON tc.constraint_name = k.constraint_name AND tc.table_schema = k.table_schema
			            WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = c.table_schema
			              AND tc.table_name = c.table_name AND k.column_name = c.column_name)
			           THEN 'PRI' ELSE '' END
			FROM information_schema.columns c
			WHERE c.table_schema = 'public' AND c.table_name = ` + sqlString(table) + `
			ORDER BY c.ordinal_position`)
	default:
		return nil, model.NewValidationError("unsupported engine")
	}
	if err != nil {
		return nil, err
	}

	columns := []ManagedColumn{}
	lines := splitLines(result.Stdout)
	for i, line := range lines {
		if i == 0 {
			continue // column header
		}
		fields := splitFields(session.Engine, line)
		if len(fields) < 2 {
			continue
		}
		col := ManagedColumn{Name: fields[0], Type: fields[1], Nullable: len(fields) > 2 && strings.EqualFold(fields[2], "YES")}
		if len(fields) > 3 {
			col.Key = fields[3]
		}
		if len(fields) > 4 {
			col.Default = fields[4]
		}
		columns = append(columns, col)
	}
	return columns, nil
}

// ─── Rows ─────────────────────────────────────────────────────────

// RowsQuery describes a browse request for one table.
type RowsQuery struct {
	Page    int
	PerPage int
	Sort    string
	Desc    bool
	Search  string
}

// ManageRows returns one page of rows from a table.
func (s *Service) ManageRows(ctx context.Context, token, database, table string, q RowsQuery) (ManagedRows, error) {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return ManagedRows{}, err
	}
	if err := validateIdentifier(database); err != nil {
		return ManagedRows{}, err
	}
	if err := validateIdentifier(table); err != nil {
		return ManagedRows{}, err
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 {
		q.PerPage = 25
	}

	columns, err := s.ManageStructure(ctx, token, database, table)
	if err != nil {
		return ManagedRows{}, err
	}

	// Sort column must be a real column of the table.
	sortColumn := ""
	if q.Sort != "" {
		for _, col := range columns {
			if col.Name == q.Sort {
				sortColumn = col.Name
				break
			}
		}
	}

	where := ""
	if q.Search != "" {
		like := "%" + escapeLike(q.Search) + "%"
		var parts []string
		for _, col := range columns {
			if session.Engine == "mysql" {
				parts = append(parts, "CAST("+quoteMySQL(col.Name)+" AS CHAR) LIKE "+sqlString(like))
			} else {
				parts = append(parts, quotePG(col.Name)+"::text ILIKE "+sqlString(like))
			}
		}
		if len(parts) > 0 {
			where = " WHERE " + strings.Join(parts, " OR ")
		}
	}

	quotedTable := s.quoteTable(session.Engine, database, table)

	var result *executor.Result

	// Total row count matching the filter.
	countSQL := "SELECT COUNT(*) FROM " + quotedTable + where
	switch session.Engine {
	case "mysql":
		result, err = s.runMySQL(ctx, session, database, countSQL)
	case "postgresql":
		result, err = s.runPostgres(ctx, session, database, countSQL)
	default:
		return ManagedRows{}, model.NewValidationError("unsupported engine")
	}
	if err != nil {
		return ManagedRows{}, err
	}
	var total int64
	if countLines := splitLines(result.Stdout); len(countLines) >= 2 {
		total = parseInt64(countLines[len(countLines)-1])
	}

	// One page of data.
	order := ""
	if sortColumn != "" {
		direction := "ASC"
		if q.Desc {
			direction = "DESC"
		}
		order = " ORDER BY " + s.quoteIdent(session.Engine, sortColumn) + " " + direction
	}
	offset := (q.Page - 1) * q.PerPage
	limit := ""
	if session.Engine == "mysql" {
		limit = fmt.Sprintf(" LIMIT %d, %d", offset, q.PerPage)
	} else {
		limit = fmt.Sprintf(" LIMIT %d OFFSET %d", q.PerPage, offset)
	}

	switch session.Engine {
	case "mysql":
		result, err = s.runMySQL(ctx, session, database, "SELECT * FROM "+quotedTable+where+order+limit)
	case "postgresql":
		result, err = s.runPostgres(ctx, session, database, "SELECT * FROM "+quotedTable+where+order+limit)
	}
	if err != nil {
		return ManagedRows{}, err
	}

	out := parseResultSets(session.Engine, result.Stdout)
	rows := ManagedRows{Columns: out.Columns, Total: total, Page: q.Page, PerPage: q.PerPage}
	if total > 0 {
		rows.TotalPages = (total + int64(q.PerPage) - 1) / int64(q.PerPage)
	}
	if rows.Rows == nil {
		rows.Rows = [][]*string{}
	}
	if rows.Columns == nil {
		rows.Columns = []string{}
	}
	return rows, nil
}

// ─── Raw SQL ──────────────────────────────────────────────────────

var (
	mysqlSelectRe  = regexp.MustCompile(`(?is)^\s*(SELECT|WITH)\b`)
	pgSelectRe     = regexp.MustCompile(`(?is)^\s*(SELECT|WITH)\b`)
	pgTagVerbs     = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "GRANT": true, "REVOKE": true, "COMMENT": true, "VACUUM": true, "ANALYZE": true, "COPY": true, "BEGIN": true, "COMMIT": true, "ROLLBACK": true, "RESET": true, "SET": true, "REINDEX": true, "CALL": true, "DO": true, "LOAD": true, "CHECKPOINT": true, "DISCARD": true}
	mysqlLimitRe   = regexp.MustCompile(`(?is)\bLIMIT\s+\d+`)
	pgLimitRe      = regexp.MustCompile(`(?is)\b(LIMIT\s+\d+|FETCH\s+FIRST)`)
	trailingSemiRe = regexp.MustCompile(`(?is)[;\s]+$`)
)

// parseCommandTag interprets a psql command tag such as "UPDATE 5",
// "INSERT 0 3", or "CREATE TABLE". It returns affected rows and ok.
func parseCommandTag(tag string) (affected int64, ok bool) {
	fields := strings.Fields(strings.ToUpper(tag))
	if len(fields) == 0 || !pgTagVerbs[fields[0]] {
		return 0, false
	}
	// INSERT carries an OID before the count; other tags carry the count.
	last := fields[len(fields)-1]
	if len(fields) >= 2 && last != fields[0] {
		if n := parseInt64(last); n != 0 || last == "0" {
			return n, true
		}
	}
	return 0, true
}

// ManageQuery runs an arbitrary SQL statement as the managed user.
func (s *Service) ManageQuery(ctx context.Context, token, database, statement string) (QueryResult, error) {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return QueryResult{}, err
	}
	if err := validateIdentifier(database); err != nil {
		return QueryResult{}, err
	}
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return QueryResult{}, model.NewValidationError("sql statement is required")
	}

	switch session.Engine {
	case "mysql":
		trimmed := trailingSemiRe.ReplaceAllString(statement, "")
		if mysqlSelectRe.MatchString(trimmed) && !mysqlLimitRe.MatchString(trimmed) {
			// Cap result sets for plain SELECT/WITH queries. On wrap failure
			// (rare constructs) fall back to running the statement raw.
			out, qErr := s.runMySQL(ctx, session, database,
				"SELECT * FROM ("+trimmed+") _panel_q LIMIT "+itoa(manageRowCap))
			if qErr == nil {
				parsed := parseResultSets("mysql", out.Stdout)
				parsed.Capped = true
				return parsed, nil
			}
		}
		return s.runMySQLQuery(ctx, session, database, statement)

	case "postgresql":
		trimmed := trailingSemiRe.ReplaceAllString(statement, "")
		if pgSelectRe.MatchString(trimmed) && !pgLimitRe.MatchString(trimmed) {
			out, qErr := s.runPostgres(ctx, session, database,
				"SELECT * FROM ("+trimmed+") _panel_q LIMIT "+itoa(manageRowCap))
			if qErr == nil {
				parsed := parseCSVResult(out.Stdout)
				parsed.Capped = true
				return parsed, nil
			}
		}
		out, qErr := s.runPostgres(ctx, session, database, statement)
		if qErr != nil {
			return QueryResult{}, qErr
		}
		return parsePostgresOutput(out.Stdout), nil

	default:
		return QueryResult{}, model.NewValidationError("unsupported engine")
	}
}

// runMySQLQuery executes a statement and interprets the batch output,
// deriving affected-row counts for data-changing statements.
func (s *Service) runMySQLQuery(ctx context.Context, session *manageSession, database, statement string) (QueryResult, error) {
	result, err := s.runMySQL(ctx, session, database, statement+"; SELECT ROW_COUNT() AS `panel_affected`;")
	if err != nil {
		return QueryResult{}, err
	}

	lines := splitLines(result.Stdout)
	for i, line := range lines {
		if strings.TrimSpace(line) != "panel_affected" || i+1 >= len(lines) {
			continue
		}
		return QueryResult{
			IsSelect: false,
			Affected: parseInt64(lines[i+1]),
			Tag:      "OK",
		}, nil
	}

	// No ROW_COUNT() result found: the statement produced rows after all.
	out := parseResultSets("mysql", result.Stdout)
	return out, nil
}

// ─── Drop / empty ─────────────────────────────────────────────────

// ManageDropTable drops a table as the managed user.
func (s *Service) ManageDropTable(ctx context.Context, token, database, table string) error {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return err
	}
	if err := validateIdentifier(database); err != nil {
		return err
	}
	if err := validateIdentifier(table); err != nil {
		return err
	}

	switch session.Engine {
	case "mysql":
		_, err = s.runMySQL(ctx, session, database, "DROP TABLE "+quoteMySQL(table))
	case "postgresql":
		_, err = s.runPostgres(ctx, session, database, "DROP TABLE public."+quotePG(table))
	default:
		err = model.NewValidationError("unsupported engine")
	}
	return err
}

// ManageEmptyTable removes all rows from a table (TRUNCATE).
func (s *Service) ManageEmptyTable(ctx context.Context, token, database, table string) error {
	session, err := s.manageSessionFor(token)
	if err != nil {
		return err
	}
	if err := validateIdentifier(database); err != nil {
		return err
	}
	if err := validateIdentifier(table); err != nil {
		return err
	}

	switch session.Engine {
	case "mysql":
		_, err = s.runMySQL(ctx, session, database, "TRUNCATE TABLE "+quoteMySQL(table))
	case "postgresql":
		_, err = s.runPostgres(ctx, session, database, "TRUNCATE public."+quotePG(table))
	default:
		err = model.NewValidationError("unsupported engine")
	}
	return err
}

// ─── Execution + quoting helpers ──────────────────────────────────

func (s *Service) runMySQL(ctx context.Context, session *manageSession, database, statement string) (*executor.Result, error) {
	args := []string{"--user=" + session.Username, "--password=" + session.Password, "--batch"}
	if database != "" {
		args = append(args, "--database="+database)
	}
	args = append(args, "--execute", statement)
	result, err := s.exec.RunSudo(ctx, "mysql", args...)
	if err != nil {
		return nil, fmt.Errorf("mysql query: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("DB_QUERY_FAILED", strings.TrimSpace(result.Stderr), nil)
	}
	return result, nil
}

func (s *Service) runPostgres(ctx context.Context, session *manageSession, database, statement string) (*executor.Result, error) {
	if database == "" {
		database = "postgres"
	}
	args := []string{pgConnInfo(session.Username, session.Password, database), "--csv", "--command", statement}
	result, err := s.exec.RunSudo(ctx, "psql", args...)
	if err != nil {
		return nil, fmt.Errorf("postgresql query: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("DB_QUERY_FAILED", strings.TrimSpace(result.Stderr), nil)
	}
	return result, nil
}

// pgConnInfo builds a libpq connection string with safely escaped values.
func pgConnInfo(username, password, database string) string {
	return "user=" + quoteConninfo(username) +
		" password=" + quoteConninfo(password) +
		" dbname=" + quoteConninfo(database) +
		" host=127.0.0.1"
}

// quoteConninfo quotes a single connection-info parameter value.
func quoteConninfo(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}

func (s *Service) quoteIdent(engine, name string) string {
	if engine == "postgresql" {
		return quotePG(name)
	}
	return quoteMySQL(name)
}

func (s *Service) quoteTable(engine, database, table string) string {
	if engine == "postgresql" {
		return quotePG(database) + ".public." + quotePG(table)
	}
	return quoteMySQL(database) + "." + quoteMySQL(table)
}

func quoteMySQL(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quotePG(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

var identifierRe = regexp.MustCompile(`^[\w$][\w$.-]{0,127}$`)

func validateIdentifier(name string) error {
	if name == "" {
		return model.NewValidationError("identifier is required")
	}
	if strings.ContainsAny(name, "`\"'\\;\n\r\x00 ") {
		return model.NewValidationError("identifier contains unsupported characters")
	}
	if !identifierRe.MatchString(name) {
		return model.NewValidationError("invalid identifier: "+name)
	}
	return nil
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// ─── Output parsing ───────────────────────────────────────────────

// splitFields splits one output line into cell values for the engine format.
func splitFields(engine, line string) []string {
	line = strings.TrimSuffix(line, "\r")
	if engine == "postgresql" {
		return parseCSVLine(line)
	}
	raw := strings.Split(line, "\t")
	out := make([]string, len(raw))
	for i, field := range raw {
		out[i] = unescapeMySQLField(field)
	}
	return out
}

// unescapeMySQLField reverses mysql --batch escaping.
func unescapeMySQLField(field string) string {
	if !strings.Contains(field, `\`) {
		return field
	}
	// Two passes: park escaped backslashes in a sentinel, decode the other
	// escapes, then restore the backslashes.
	const sentinel = "\x01"
	step := strings.ReplaceAll(field, `\\`, sentinel)
	step = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\r`, "\r", `\0`, "\x00").Replace(step)
	return strings.ReplaceAll(step, sentinel, `\`)
}

// parseCSVLine parses a single RFC-4180-ish CSV line (psql --csv).
func parseCSVLine(line string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inQuotes:
			if ch == '"' {
				if i+1 < len(line) && line[i+1] == '"' {
					current.WriteByte('"')
					i++
				} else {
					inQuotes = false
				}
			} else {
				current.WriteByte(ch)
			}
		case ch == '"':
			inQuotes = true
		case ch == ',':
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}
	fields = append(fields, current.String())
	return fields
}

// splitLines returns non-empty output lines with CR trimmed.
func splitLines(stdout string) []string {
	var lines []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// parseResultSets parses mysql --batch output (header + rows).
func parseResultSets(engine, stdout string) QueryResult {
	lines := splitLines(stdout)
	out := QueryResult{}
	if len(lines) == 0 {
		out.Rows = [][]*string{}
		out.Columns = []string{}
		return out
	}
	out.Columns = splitFields(engine, lines[0])
	out.IsSelect = true
	for _, line := range lines[1:] {
		fields := splitFields(engine, line)
		row := make([]*string, len(fields))
		for i, field := range fields {
			if field == "NULL" {
				continue // stays nil
			}
			value := field
			row[i] = &value
		}
		out.Rows = append(out.Rows, row)
	}
	if out.Rows == nil {
		out.Rows = [][]*string{}
	}
	return out
}

// parsePostgresOutput interprets psql --csv output, distinguishing result
// sets from command tags such as "UPDATE 5".
func parsePostgresOutput(stdout string) QueryResult {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return QueryResult{IsSelect: false, Rows: [][]*string{}, Columns: []string{}}
	}
	// A single bare command tag line such as "UPDATE 5" or "CREATE TABLE".
	if !strings.Contains(trimmed, "\n") {
		if affected, ok := parseCommandTag(trimmed); ok {
			return QueryResult{IsSelect: false, Affected: affected, Tag: trimmed, Rows: [][]*string{}, Columns: []string{}}
		}
	}
	return parseCSVResult(trimmed)
}

// parseCSVResult parses psql --csv output into a result set.
func parseCSVResult(stdout string) QueryResult {
	lines := splitLines(stdout)
	out := QueryResult{Rows: [][]*string{}, Columns: []string{}}
	if len(lines) == 0 {
		return out
	}
	// Skip a trailing psql row-count footer if present.
	if len(lines) > 1 && rowFooterRe.MatchString(strings.TrimSpace(lines[len(lines)-1])) {
		lines = lines[:len(lines)-1]
	}
	out.Columns = parseCSVLine(lines[0])
	out.IsSelect = true
	for _, line := range lines[1:] {
		if rowFooterRe.MatchString(strings.TrimSpace(line)) {
			continue
		}
		fields := parseCSVLine(line)
		row := make([]*string, len(fields))
		for i, field := range fields {
			value := field
			row[i] = &value
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

var rowFooterRe = regexp.MustCompile(`^\(\d+ rows?\)$`)

func parseInt64(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	var n int64
	var negative bool
	if strings.HasPrefix(value, "-") {
		negative = true
		value = value[1:]
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int64(ch-'0')
	}
	if negative {
		n = -n
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
