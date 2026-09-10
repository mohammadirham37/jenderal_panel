package dbmanager

import (
	"strings"
	"testing"
	"time"
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
