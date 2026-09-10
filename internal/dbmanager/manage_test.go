package dbmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestUnescapeMySQLField(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"NULL", "NULL"}, // NULL marker handled by row parser, not unescape
		{`\n`, "\n"},
		{`\t`, "\t"},
		{`a\\b`, `a\b`},
		{`\\n`, `\n`}, // escaped backslash followed by n stays literal "\n"
	}
	for _, c := range cases {
		if got := unescapeMySQLField(c.in); got != c.want {
			t.Errorf("unescapeMySQLField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseCSVLine(t *testing.T) {
	got := parseCSVLine(`plain,"quoted, comma","","say ""hi""",`)
	want := []string{"plain", "quoted, comma", "", `say "hi"`, ""}
	if len(got) != len(want) {
		t.Fatalf("parseCSVLine fields = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseResultSetsSkipsHeaderAndMarksNULL(t *testing.T) {
	out := parseResultSets("mysql", "id\tname\n1\tNULL\n")
	if !out.IsSelect || len(out.Columns) != 2 || out.Columns[0] != "id" {
		t.Fatalf("unexpected header parse: %#v", out)
	}
	if len(out.Rows) != 1 || out.Rows[0][0] == nil || out.Rows[0][1] != nil {
		t.Fatalf("unexpected row parse: %#v", out.Rows)
	}
}

func TestParsePostgresOutputCommandTag(t *testing.T) {
	out := parsePostgresOutput("UPDATE 5\n")
	if out.IsSelect || out.Affected != 5 || out.Tag != "UPDATE 5" {
		t.Fatalf("unexpected tag parse: %#v", out)
	}

	out = parsePostgresOutput("CREATE TABLE\n")
	if out.IsSelect || out.Affected != 0 || out.Tag != "CREATE TABLE" {
		t.Fatalf("unexpected tag parse: %#v", out)
	}

	out = parsePostgresOutput("INSERT 0 3\n")
	if out.Affected != 3 {
		t.Fatalf("INSERT tag affected = %d, want 3", out.Affected)
	}
}

func TestParsePostgresOutputCSV(t *testing.T) {
	out := parsePostgresOutput("datname\napp_db\nshop\n")
	if !out.IsSelect || len(out.Columns) != 1 || len(out.Rows) != 2 || *out.Rows[1][0] != "shop" {
		t.Fatalf("unexpected csv parse: %#v", out)
	}
}

func TestParseCSVResultDropsFooter(t *testing.T) {
	out := parseCSVResult("id\n1\n2\n(2 rows)\n")
	if len(out.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(out.Rows))
	}
}

func TestValidateIdentifier(t *testing.T) {
	valid := []string{"users", "app_db", "my-db", "Tbl1", "a.b"} // dotted allowed for safety-checked paths
	for _, name := range valid {
		if err := validateIdentifier(name); err != nil {
			t.Errorf("validateIdentifier(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "has space", "back`tick", "semi;colon", `quote"`, strings.Repeat("x", 200)}
	for _, name := range invalid {
		if err := validateIdentifier(name); err == nil {
			t.Errorf("validateIdentifier(%q) = nil, want error", name)
		}
	}
}

func TestQuoteHelpers(t *testing.T) {
	if got := quoteMySQL("us`r"); got != "`us``r`" {
		t.Errorf("quoteMySQL = %q", got)
	}
	if got := quotePG(`us"r`); got != `"us""r"` {
		t.Errorf("quotePG = %q", got)
	}
	if got := pgConnInfo("u", "p'w\\x", "db"); !strings.Contains(got, `password='p\'w\\x'`) {
		t.Errorf("pgConnInfo password escaping broken: %q", got)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`100% _done\`); got != `100\% \_done\\` {
		t.Errorf("escapeLike = %q", got)
	}
}

// stubManageExec answers mysql/psql invocations by matching the statement
// passed after --execute/--command against the given markers.
func stubManageExec(statements map[string]string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			stmt := ""
			for i, arg := range args {
				if (arg == "--execute" || arg == "--command") && i+1 < len(args) {
					stmt = args[i+1]
					break
				}
			}
			for marker, out := range statements {
				if strings.Contains(stmt, marker) {
					return okResult(out), nil
				}
			}
			return okResult(""), nil
		},
	}
}

func newManageSession(engine string) string {
	return manageSessions.put(&manageSession{
		PanelUserID: "tester",
		Engine:      engine,
		Username:    "app_user",
		Password:    "pw",
	})
}

func TestManageRowsReturnsParsedRows(t *testing.T) {
	mock := stubManageExec(map[string]string{
		"SHOW FULL COLUMNS": "Field\tType\tCollation\tNull\tKey\tDefault\tExtra\tPrivileges\tComment\n" +
			"id\tbigint\tNULL\tNO\tPRI\tNULL\t\tselect,insert,update\t\n",
		"COUNT(*)": "COUNT(*)\n2\n",
		"SELECT *": "id\tname\n1\talice\n2\tbob\n",
	})
	svc := &Service{exec: mock}

	token := newManageSession("mysql")
	defer manageSessions.drop(token)

	got, err := svc.ManageRows(context.Background(), token, "app_db", "items", RowsQuery{Page: 1, PerPage: 25})
	if err != nil {
		t.Fatalf("ManageRows: %v", err)
	}
	if len(got.Columns) != 2 || got.Columns[0] != "id" || got.Columns[1] != "name" {
		t.Fatalf("columns = %v, want [id name]", got.Columns)
	}
	if got.Total != 2 {
		t.Errorf("total = %d, want 2", got.Total)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("rows: got %d rows, want 2 — parsed rows must be returned to the client", len(got.Rows))
	}
	if got.Rows[0][0] == nil || *got.Rows[0][0] != "1" || got.Rows[1][1] == nil || *got.Rows[1][1] != "bob" {
		t.Fatalf("row data = %#v, want [[1 alice] [2 bob]]", got.Rows)
	}
}

func TestManageStructureMapsEngineColumnOrder(t *testing.T) {
	t.Run("mysql", func(t *testing.T) {
		// SHOW FULL COLUMNS order: Field, Type, Collation, Null, Key, Default, ...
		mock := stubManageExec(map[string]string{
			"SHOW FULL COLUMNS": "Field\tType\tCollation\tNull\tKey\tDefault\tExtra\tPrivileges\tComment\n" +
				"email\tvarchar(190)\tutf8mb4_0900_ai_ci\tYES\tUNI\tteam@example.com\t\tselect,insert\t\n",
		})
		svc := &Service{exec: mock}
		token := newManageSession("mysql")
		defer manageSessions.drop(token)

		cols, err := svc.ManageStructure(context.Background(), token, "app_db", "users")
		if err != nil {
			t.Fatalf("ManageStructure: %v", err)
		}
		if len(cols) != 1 {
			t.Fatalf("columns = %d, want 1", len(cols))
		}
		c := cols[0]
		if !c.Nullable {
			t.Errorf("nullable = false, want true (Null=YES)")
		}
		if c.Key != "UNI" {
			t.Errorf("key = %q, want UNI", c.Key)
		}
		if c.Default != "team@example.com" {
			t.Errorf("default = %q, want team@example.com", c.Default)
		}
	})

	t.Run("postgresql", func(t *testing.T) {
		// Custom query order: column_name, data_type, is_nullable, column_default, <PK flag>.
		mock := stubManageExec(map[string]string{
			"information_schema.columns": "column_name,data_type,is_nullable,column_default,\n" +
				"email,character varying(190),YES,team@example.com,\n",
		})
		svc := &Service{exec: mock}
		token := newManageSession("postgresql")
		defer manageSessions.drop(token)

		cols, err := svc.ManageStructure(context.Background(), token, "app_db", "users")
		if err != nil {
			t.Fatalf("ManageStructure: %v", err)
		}
		if len(cols) != 1 {
			t.Fatalf("columns = %d, want 1", len(cols))
		}
		c := cols[0]
		if !c.Nullable {
			t.Errorf("nullable = false, want true (is_nullable=YES)")
		}
		if c.Default != "team@example.com" {
			t.Errorf("default = %q, want team@example.com", c.Default)
		}
		if c.Key != "" {
			t.Errorf("key = %q, want empty (no primary key)", c.Key)
		}
	})
}

func TestQuoteTable(t *testing.T) {
	svc := &Service{}
	if got := svc.quoteTable("mysql", "app_db", "orders"); got != "`app_db`.`orders`" {
		t.Errorf("mysql quoteTable = %q, want `app_db`.`orders`", got)
	}
	// PostgreSQL rejects cross-database references, so the table may only be
	// schema-qualified.
	if got := svc.quoteTable("postgresql", "app_db", "orders"); got != `public."orders"` {
		t.Errorf("postgresql quoteTable = %q, want public.\"orders\"", got)
	}
}

func TestManageSessionTTLExpiry(t *testing.T) {
	session := &manageSession{Username: "u", Password: "p", Engine: "mysql"}
	token := manageSessions.put(session)
	if _, ok := manageSessions.get(token); !ok {
		t.Fatal("fresh session should be valid")
	}
	// Force expiry.
	manageSessions.mu.Lock()
	manageSessions.sessions[token].lastUsed = manageSessions.sessions[token].lastUsed.Add(-(manageSessionTTL + time.Minute))
	manageSessions.mu.Unlock()
	if _, ok := manageSessions.get(token); ok {
		t.Fatal("expired session should be rejected")
	}
}
