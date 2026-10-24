package website

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// The cache script is run for real against a throwaway home: it must build
// a module, report what it cleared, and leave a module cache that plain rm
// can delete (-modcacherw).
func TestGoCacheScriptClearsCachesForReal(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	home := t.TempDir()
	// Servers have GNU du; elsewhere (macOS) a shim prints the same
	// "total" line from file sizes.
	pathEnv := "PATH=/usr/bin:/bin"
	if exec.Command("du", "-sb", ".").Run() != nil {
		shim := t.TempDir()
		duShim := "#!/bin/bash\nshift\ntotal=0\nfor p in \"$@\"; do [ -e \"$p\" ] || continue; for s in $(find \"$p\" -type f -exec stat -f %z {} +); do total=$((total+s)); done; done\nprintf '%s\\ttotal\\n' \"$total\"\n"
		if err := os.WriteFile(filepath.Join(shim, "du"), []byte(duShim), 0o755); err != nil {
			t.Fatal(err)
		}
		pathEnv = "PATH=" + shim + ":/usr/bin:/bin"
	}
	project := filepath.Join(home, "app")
	_ = os.MkdirAll(project, 0o755)
	_ = os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/app\n\ngo 1.21\n"), 0o644)
	_ = os.WriteFile(filepath.Join(project, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	env := strings.ReplaceAll(goBuildEnv("web_x"), "/home/web_x", home)
	env = strings.ReplaceAll(env, "/usr/local/go/bin", filepath.Dir(goBin))
	build := exec.Command("/bin/bash", "-c", "cd "+project+" && export "+env+" && go build -o server .")
	build.Env = []string{pathEnv}
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	script := strings.ReplaceAll(goCacheScript("web_x", true), "/home/web_x", home)
	script = strings.ReplaceAll(script, "/usr/local/go/bin", filepath.Dir(goBin))
	clean := exec.Command("/bin/bash", "-c", script)
	clean.Env = []string{pathEnv}
	out, err := clean.CombinedOutput()
	if err != nil {
		t.Fatalf("clean: %v\n%s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[0] == "0" {
		t.Errorf("expected 'before after' with a non-empty cache, got %q", out)
	}
	if err := os.RemoveAll(home); err != nil {
		t.Errorf("home not removable after clean: %v", err)
	}
}

func TestClearGoCacheReportsFreedBytesAndRunsAsTheSiteUser(t *testing.T) {
	var got []string
	svc := &Service{exec: &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			got = append([]string{name}, args...)
			return &executor.Result{Stdout: "524288000 1048576\n"}, nil
		},
	}}
	freed, err := svc.clearGoCache(context.Background(), "web_shop", true)
	if err != nil {
		t.Fatal(err)
	}
	if freed != 524288000-1048576 {
		t.Errorf("freed = %d", freed)
	}
	if len(got) < 3 || got[0] != "-u" || got[1] != "web_shop" || !strings.Contains(strings.Join(got, " "), "go clean -cache -modcache") {
		t.Errorf("unexpected command %v", got)
	}
	if _, err := svc.clearGoCache(context.Background(), "root", true); err == nil {
		t.Error("a non-website user must be refused")
	}
}
