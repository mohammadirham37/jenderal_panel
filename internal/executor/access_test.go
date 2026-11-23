package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The probes are run here without the sudo prefix, as the current user.
func TestAccessProbesAskTheKernel(t *testing.T) {
	dir := t.TempDir()
	open := filepath.Join(dir, "open")
	closed := filepath.Join(dir, "closed")
	os.Mkdir(open, 0o755)
	os.Mkdir(closed, 0o000)
	defer os.Chmod(closed, 0o755)
	file := filepath.Join(dir, "index.php")
	os.WriteFile(file, []byte("x"), 0o644)
	secret := filepath.Join(dir, "secret")
	os.WriteFile(secret, []byte("x"), 0o000)
	weird := filepath.Join(dir, `a "b"; touch pwned`)
	os.Mkdir(weird, 0o755)

	run := func(args []string) bool {
		if args[0] != "-u" || args[1] != "web" || args[2] != "--" {
			t.Fatalf("args = %v", args)
		}
		return exec.Command(args[3], args[4:]...).Run() == nil
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	if !run(CanEnterArgs("web", open)) || run(CanEnterArgs("web", closed)) {
		t.Error("CanEnterArgs")
	}
	if !run(CanEnterArgs("web", weird)) {
		t.Error("a path with quotes and semicolons must be passed as data")
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("the path was interpreted by the shell")
	}
	if !run(CanReadArgs("web", file)) || run(CanReadArgs("web", secret)) {
		t.Error("CanReadArgs")
	}
}
