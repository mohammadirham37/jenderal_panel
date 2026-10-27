package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
)

// The panel's HTML carries a few inline scripts (theme and title bootstrap,
// SvelteKit's start-up call). Instead of allowing every inline script with
// 'unsafe-inline', the CSP lists the SHA-256 hash of each one, computed from
// the embedded build when the panel starts, so an injected script cannot run.

var inlineScriptPattern = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)

// InlineScriptHashes returns the CSP source expressions ('sha256-...') of
// every inline script in the HTML files of fsys.
func InlineScriptHashes(fsys fs.FS) ([]string, error) {
	seen := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		for _, m := range inlineScriptPattern.FindAllStringSubmatch(string(data), -1) {
			if strings.Contains(strings.ToLower(m[1]), "src=") {
				continue // external script, covered by 'self'
			}
			sum := sha256.Sum256([]byte(m[2]))
			seen["'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'"] = true
		}
		return nil
	})
	hashes := make([]string, 0, len(seen))
	for h := range seen {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	return hashes, err
}

// cspHeader holds the Content-Security-Policy sent on every response.
var cspHeader atomic.Value

func init() { cspHeader.Store(buildCSP(nil)) }

// SetInlineScriptHashes allows exactly these inline scripts. Without hashes
// (no HTML embedded, as in tests) no inline script may run.
func SetInlineScriptHashes(hashes []string) { cspHeader.Store(buildCSP(hashes)) }

func buildCSP(hashes []string) string {
	script := "script-src 'self'"
	if len(hashes) > 0 {
		script += " " + strings.Join(hashes, " ")
	}
	return "default-src 'self'; " +
		script + "; " +
		// Svelte sets style attributes at runtime; styles cannot run code.
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; " +
		"font-src 'self' data:; " +
		"connect-src 'self' ws: wss:; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'; " +
		"object-src 'none'"
}
