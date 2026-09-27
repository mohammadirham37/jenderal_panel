package dbmanager

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
)

// uca1400Marker prefixes the UCA 14.0 collation family MariaDB 11 introduced
// (utf8mb4_uca1400_ai_ci and friends). Dumps produced there fail mid-restore
// on MySQL and older MariaDB with ERROR 1273 "Unknown collation".
const uca1400Marker = "utf8mb4_uca1400_"

// uca1400Suffixes are the exact collation suffixes of the MariaDB 11 UCA 14.0
// family. Matching them exactly keeps longer identifiers (a table named
// "utf8mb4_uca1400_ai_ci_backup") from being rewritten.
var uca1400Suffixes = map[string]bool{
	"ai_ci": true,
	"as_ci": true,
	"as_cs": true,
	"ai_ws": true,
	"as_ws": true,
}

// collationFallbacks returns the collations to substitute for an unsupported
// uca1400 collation, closest semantic match first: accent/case sensitivity is
// preserved when the target has a 0900 variant, otherwise the widely
// available unicode_ci is used.
func collationFallbacks(suffix string) []string {
	if strings.Contains(suffix, "as_") {
		return []string{"utf8mb4_0900_as_cs", "utf8mb4_unicode_ci", "utf8mb4_general_ci"}
	}
	return []string{"utf8mb4_0900_ai_ci", "utf8mb4_unicode_ci", "utf8mb4_general_ci"}
}

func isCollationIdentByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

// rewriteUCA1400Collations replaces uca1400 collation tokens the target does
// not support with the closest supported one. INSERT lines are passed through
// untouched so data values that happen to contain a collation name are never
// modified, and a token the target already knows (a MariaDB 11 target) is
// kept as-is.
func rewriteUCA1400Collations(line []byte, supported map[string]bool, cache map[string]string) []byte {
	if bytes.HasPrefix(line, []byte("INSERT")) {
		return line
	}

	var out []byte
	last := 0
	for i := 0; i < len(line); {
		idx := bytes.Index(line[i:], []byte(uca1400Marker))
		if idx < 0 {
			break
		}
		start := i + idx
		// Part of a longer identifier (e.g. a custom collation or a string
		// glued to the marker) is not a bare collation token.
		if start > 0 && isCollationIdentByte(line[start-1]) {
			i = start + len(uca1400Marker)
			continue
		}
		end := start + len(uca1400Marker)
		for end < len(line) && isCollationIdentByte(line[end]) {
			end++
		}
		token := string(line[start:end])
		suffix := token[len(uca1400Marker):]
		if !uca1400Suffixes[suffix] {
			i = end
			continue
		}
		replacement, ok := cache[token]
		if !ok {
			replacement = token
			if !supported[token] {
				for _, candidate := range collationFallbacks(suffix) {
					if supported[candidate] {
						replacement = candidate
						break
					}
				}
			}
			cache[token] = replacement
		}
		if out == nil {
			out = append(out, line[:start]...)
		} else {
			out = append(out, line[last:start]...)
		}
		out = append(out, replacement...)
		last = end
		i = end
	}
	if out == nil {
		return line
	}
	return append(out, line[last:]...)
}

// collationCompatReader streams a MySQL dump and rewrites collation names the
// target server does not support, so dumps from newer MariaDB versions
// restore onto older servers instead of failing mid-file.
type collationCompatReader struct {
	src       *bufio.Reader
	supported map[string]bool
	cache     map[string]string
	pending   []byte
	done      bool
	err       error
}

// newCollationCompatReader wraps a dump stream. An empty supported set (the
// target's collations could not be read) leaves every line unchanged.
func newCollationCompatReader(src io.Reader, supported map[string]bool) *collationCompatReader {
	return &collationCompatReader{
		src:       bufio.NewReaderSize(src, 64*1024),
		supported: supported,
		cache:     map[string]string{},
	}
}

func (r *collationCompatReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.done {
			return 0, r.err
		}
		line, err := r.src.ReadString('\n')
		if len(line) == 0 && err != nil {
			r.done = true
			r.err = err
			return 0, err
		}
		// ReadString keeps the trailing newline, so untouched lines pass
		// through byte-for-byte.
		out := rewriteUCA1400Collations([]byte(line), r.supported, r.cache)
		r.pending = append(r.pending, out...)
		if err != nil {
			r.done = true
			r.err = err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// supportedCollations returns the collation names the target MySQL server
// knows, queried with the given extra auth arguments. On failure the set is
// empty and the compat rewriter leaves the dump untouched.
func (s *Service) supportedCollations(ctx context.Context, authArgs ...string) map[string]bool {
	args := append([]string{}, authArgs...)
	args = append(args, "--batch", "--skip-column-names", "-e",
		"SELECT collation_name FROM information_schema.COLLATIONS")
	result, err := s.exec.RunSudo(ctx, "mysql", args...)
	if err != nil || result.ExitCode != 0 {
		return map[string]bool{}
	}
	set := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			set[name] = true
		}
	}
	return set
}
