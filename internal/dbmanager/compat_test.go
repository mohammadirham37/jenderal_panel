package dbmanager

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

var mysql8Collations = map[string]bool{
	"utf8mb4_0900_ai_ci": true,
	"utf8mb4_0900_as_cs": true,
	"utf8mb4_unicode_ci": true,
	"utf8mb4_general_ci": true,
}

func TestRewriteUCA1400Collations(t *testing.T) {
	cache := map[string]string{}
	ddl := []byte(") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;")
	got := string(rewriteUCA1400Collations(ddl, mysql8Collations, cache))
	want := ") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;"
	if got != want {
		t.Errorf("rewritten = %q, want %q", got, want)
	}

	// INSERT lines carry data values: they must never be touched.
	insert := []byte("INSERT INTO `t` VALUES (1, 'utf8mb4_uca1400_ai_ci');")
	if got := rewriteUCA1400Collations(insert, mysql8Collations, cache); !bytes.Equal(got, insert) {
		t.Errorf("INSERT line must pass through untouched, got %q", string(got))
	}

	// A longer identifier containing the marker is not a collation token.
	glued := []byte("CREATE TABLE utf8mb4_uca1400_ai_ci_backup (id int);")
	if got := rewriteUCA1400Collations(glued, mysql8Collations, cache); !bytes.Equal(got, glued) {
		t.Errorf("glued identifier must pass through, got %q", string(got))
	}

	// A target that knows the collation (MariaDB 11) keeps it as-is.
	maria11 := map[string]bool{"utf8mb4_uca1400_ai_ci": true}
	cache2 := map[string]string{}
	if got := string(rewriteUCA1400Collations(ddl, maria11, cache2)); got != string(ddl) {
		t.Errorf("supported collation must be kept, got %q", got)
	}

	// Accent/case-sensitive uca1400 variants prefer the matching 0900
	// variant, falling back to unicode_ci when it does not exist.
	asLine := []byte("COLLATE=utf8mb4_uca1400_as_cs;")
	cache3 := map[string]string{}
	if got := string(rewriteUCA1400Collations(asLine, mysql8Collations, cache3)); got != "COLLATE=utf8mb4_0900_as_cs;" {
		t.Errorf("as_cs rewrite = %q", got)
	}
	onlyUnicode := map[string]bool{"utf8mb4_unicode_ci": true}
	cache4 := map[string]string{}
	if got := string(rewriteUCA1400Collations(asLine, onlyUnicode, cache4)); got != "COLLATE=utf8mb4_unicode_ci;" {
		t.Errorf("as_cs fallback = %q", got)
	}

	// Several occurrences and distinct tokens in one line.
	multi := []byte("COLLATE=utf8mb4_uca1400_ai_ci DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_as_cs;")
	cache5 := map[string]string{}
	got = string(rewriteUCA1400Collations(multi, mysql8Collations, cache5))
	want = "COLLATE=utf8mb4_0900_ai_ci DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;"
	if got != want {
		t.Errorf("multi rewrite = %q, want %q", got, want)
	}

	// An empty supported set leaves everything unchanged.
	cache6 := map[string]string{}
	if got := string(rewriteUCA1400Collations(ddl, map[string]bool{}, cache6)); got != string(ddl) {
		t.Errorf("unknown target must keep the dump untouched, got %q", got)
	}
}

func TestCollationCompatReaderAcrossChunks(t *testing.T) {
	dump := "-- comment\n" +
		"CREATE TABLE t (c int) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;\n" +
		"INSERT INTO `t` VALUES ('utf8mb4_uca1400_ai_ci');\n"

	// OneByteReader splits the stream at every byte, exercising the
	// reader's buffering across token boundaries.
	r := newCollationCompatReader(iotest.OneByteReader(strings.NewReader(dump)), mysql8Collations)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	want := "-- comment\n" +
		"CREATE TABLE t (c int) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;\n" +
		"INSERT INTO `t` VALUES ('utf8mb4_uca1400_ai_ci');\n"
	if string(got) != want {
		t.Errorf("stream = %q, want %q", string(got), want)
	}
}

func TestCollationCompatReaderPassthroughOnError(t *testing.T) {
	// Without any known collations (probe failed) the stream is untouched.
	dump := "COLLATE=utf8mb4_uca1400_ai_ci;\n"
	r := newCollationCompatReader(strings.NewReader(dump), map[string]bool{})
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != dump {
		t.Errorf("stream = %q, want untouched %q", string(got), dump)
	}
}
