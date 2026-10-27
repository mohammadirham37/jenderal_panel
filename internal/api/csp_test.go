package api

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestInlineScriptHashesCoverInlineScriptsOnly(t *testing.T) {
	body := "\n\t\t\t(function () { document.title = location.hostname; })();\n\t\t"
	fsys := fstest.MapFS{
		"index.html": {Data: []byte(`<html><head><script>` + body + `</script>` +
			`<script type="module" src="/_app/start.js"></script></head>` +
			`<body><div><SCRIPT>kit.start()</SCRIPT></div></body></html>`)},
		"_app/x.js": {Data: []byte(`<script>not html</script>`)},
	}
	hashes, err := InlineScriptHashes(fsys)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if len(hashes) != 2 || (hashes[0] != want && hashes[1] != want) {
		t.Fatalf("hashes = %v, want two including %s", hashes, want)
	}

	SetInlineScriptHashes(hashes)
	t.Cleanup(func() { SetInlineScriptHashes(nil) })
	csp := cspHeader.Load().(string)
	if !strings.Contains(csp, "script-src 'self' "+hashes[0]+" "+hashes[1]+";") {
		t.Errorf("CSP does not list the hashes: %s", csp)
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("CSP still allows every inline script: %s", csp)
	}
}

// The real build must hash every inline script it ships, or the panel UI
// would not start under the CSP.
func TestInlineScriptHashesOfTheRealBuild(t *testing.T) {
	const dir = "../../web/build"
	index, err := os.ReadFile(dir + "/index.html")
	if err != nil {
		t.Skip("web/build not present")
	}
	inline := 0
	for _, m := range inlineScriptPattern.FindAllStringSubmatch(string(index), -1) {
		if !strings.Contains(strings.ToLower(m[1]), "src=") {
			inline++
		}
	}
	hashes, err := InlineScriptHashes(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if inline == 0 || len(hashes) < inline {
		t.Errorf("index.html has %d inline scripts but %d hashes", inline, len(hashes))
	}
}
