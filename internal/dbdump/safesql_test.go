package dbdump

import (
	"io"
	"strings"
	"testing"
)

func readAll(t *testing.T, engine, sql string) (string, error) {
	t.Helper()
	r, err := NewSafeSQLReader(engine, strings.NewReader(sql))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	return string(out), err
}

func TestSafeSQLReaderPassesRealDumps(t *testing.T) {
	mysqlDump := `-- MySQL dump 10.13
/*!40101 SET NAMES utf8mb4 */;
DROP TABLE IF EXISTS ` + "`posts`" + `;
CREATE TABLE ` + "`posts`" + ` (
  ` + "`id`" + ` int NOT NULL AUTO_INCREMENT,
  source varchar(255) DEFAULT NULL,
  system tinyint(1) DEFAULT 0,
  PRIMARY KEY (` + "`id`" + `)
) ENGINE=InnoDB;
INSERT INTO ` + "`posts`" + ` VALUES (1,'a\\nb','x');
`
	if out, err := readAll(t, "mysql", mysqlDump); err != nil || out != mysqlDump {
		t.Fatalf("valid MySQL dump rejected: %v", err)
	}

	pgDump := `--
-- PostgreSQL database dump
--
SET statement_timeout = 0;
CREATE TABLE public.posts (
    id integer NOT NULL,
    source text
);
COPY public.posts (id, source) FROM stdin;
1	hello
2	\N
\.
ALTER TABLE ONLY public.posts ADD CONSTRAINT posts_pkey PRIMARY KEY (id);
`
	if out, err := readAll(t, "postgresql", pgDump); err != nil || out != pgDump {
		t.Fatalf("valid PostgreSQL dump rejected: %v", err)
	}
}

func TestSafeSQLReaderRejectsClientCommands(t *testing.T) {
	cases := []struct{ engine, sql string }{
		{"mysql", "SELECT 1;\n\\! id\n"},
		{"mysql", "SELECT 1;\nsystem id\n"},
		{"mysql", "SELECT 1;\n  SOURCE /tmp/evil.sql\n"},
		{"mysql", "SELECT 1;\ntee /root/out.txt\n"},
		{"mysql", "\\. /etc/passwd\n"},
		{"postgresql", "SELECT 1;\n\\! id\n"},
		{"postgresql", "SELECT 1;\n\\copy t FROM PROGRAM 'id'\n"},
		{"postgresql", "SELECT 1;\n  \\i /etc/passwd\n"},
	}
	for _, tc := range cases {
		out, err := readAll(t, tc.engine, tc.sql)
		if err == nil {
			t.Errorf("%s: %q was accepted (read %q)", tc.engine, tc.sql, out)
			continue
		}
		if !strings.Contains(err.Error(), "command") {
			t.Errorf("%s: error %v should explain the rejected command", tc.engine, err)
		}
	}
}

func TestSafeSQLReaderKeepsColumnNamesInsideStatements(t *testing.T) {
	// "source" and "system" are ordinary identifiers when a statement is
	// still open, even at the start of a line.
	sql := "CREATE TABLE t (\nsource varchar(10),\nsystem int\n);\n"
	if _, err := readAll(t, "mysql", sql); err != nil {
		t.Fatalf("identifier at line start rejected: %v", err)
	}
}

// pg_dump from the August 2025 security releases wraps the dump in
// \restrict / \unrestrict; those lines are dropped, everything else passes.
func TestSafeSQLReaderDropsPgDumpRestrictLines(t *testing.T) {
	dump := "--\n-- PostgreSQL database dump\n--\n\n\\restrict 4hT7mWqP9cBz2LkV8xYnR3sD\n\n" +
		"SET statement_timeout = 0;\nCREATE TABLE public.t (id integer);\n" +
		"COPY public.t (id) FROM stdin;\n1\n\\.\n\n\\unrestrict 4hT7mWqP9cBz2LkV8xYnR3sD\n"
	out, err := readAll(t, "postgresql", dump)
	if err != nil {
		t.Fatalf("pg_dump output with \\restrict rejected: %v", err)
	}
	if strings.Contains(out, "restrict") {
		t.Errorf("restrict lines must be dropped:\n%s", out)
	}
	if !strings.Contains(out, "CREATE TABLE public.t") || !strings.Contains(out, "COPY public.t") {
		t.Errorf("SQL lost:\n%s", out)
	}

	// Other client commands stay refused, including look-alikes.
	for _, bad := range []string{"\\restrict key; \\! id\n", "\\restrict\n", "\\! id\n", "\\copy t to '/tmp/x'\n"} {
		if _, err := readAll(t, "postgresql", bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	// MySQL dumps get no such exception.
	if _, err := readAll(t, "mysql", "\\restrict abc\n"); err == nil {
		t.Error("mysql must not accept \\restrict")
	}
}
