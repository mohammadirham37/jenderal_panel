package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The cleanup step is run for real with stubbed tools: build outputs are
// removed, a Go cache over its cap is cleared, one under it is kept, the
// legacy caches in root's home are cleared once, and nothing fails the
// update.
func TestCleanBuildLeftoversForReal(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for _, dir := range []string{"cmd/jenderal/web_build", "web/build", "web/.svelte-kit/output", "web/node_modules/x"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "bin")
	_ = os.Mkdir(bin, 0o755)
	goLog := filepath.Join(root, "go.log")
	stubs := map[string]string{
		// GOCACHE reports 3000MB (over its 2048 cap); everything else 10MB.
		"du": `case "$2" in *go-build) printf '3000\t%s\n' "$2";; *) printf '10\t%s\n' "$2";; esac`,
		"df": `echo Avail; echo 1000M`,
		"go": `echo "GOCACHE=$GOCACHE GOMODCACHE=$GOMODCACHE $*" >> ` + goLog,
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/bash\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	script := strings.ReplaceAll(cleanupFunctions(), "%%", "%")
	// The panel-account npm and nvm steps need sudo; they are not under test.
	script = strings.ReplaceAll(script, "'/usr/bin/sudo'", "true")
	cacheDir := filepath.Join(root, "cache")
	_ = os.Mkdir(cacheDir, 0o755)
	full := script + "\nclean_build_leftovers " + source + "\n"
	cmd := exec.Command("/bin/bash", "-c", full)
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"BUILD_CACHE_DIR=" + cacheDir,
		"GOCACHE=" + filepath.Join(cacheDir, "go-build"),
		"GOMODCACHE=" + filepath.Join(cacheDir, "go-mod"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cleanup failed: %v\n%s", err, out)
	}

	for _, gone := range []string{"cmd/jenderal/web_build", "web/build", "web/.svelte-kit/output"} {
		if _, err := os.Stat(filepath.Join(source, gone)); !os.IsNotExist(err) {
			t.Errorf("%s was not removed", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(source, "web/node_modules/x")); err != nil {
		t.Error("node_modules must be kept for the next update")
	}
	logged, _ := os.ReadFile(goLog)
	if !strings.Contains(string(logged), " clean -cache") {
		t.Errorf("over-cap build cache not cleared; go calls:\n%s", logged)
	}
	if strings.Contains(string(logged), "go-mod clean -modcache") {
		t.Errorf("under-cap module cache must be kept; go calls:\n%s", logged)
	}
	if !strings.Contains(string(out), "cap 2048MB") {
		t.Errorf("no cap message:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, ".legacy-root-go-cache-removed")); err != nil {
		t.Error("legacy cleanup marker not written")
	}
}
