package website

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The .env is written by /bin/sh as the site account (run here as the
// current user, without the sudo prefix): content from stdin, mode 600,
// existing file replaced, and the path passed as data.
func TestEnvWriteArgsWritesStdinWithMode600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, `odd "dir"; touch pwned`, ".env")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("OLD=1\nLONGER_OLD_CONTENT=1\n"), 0o644)

	args := envWriteArgs("web_example_com", path)
	if strings.Join(args[:3], " ") != "-u web_example_com --" {
		t.Fatalf("must run as the site account: %v", args)
	}
	content := "APP_NAME=Shop\nVITE_APP_NAME=\"${APP_NAME}\"\n"
	cmd := exec.Command(args[3], args[4:]...)
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Errorf("content = %q", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("the path was interpreted by the shell")
	}
	for _, arg := range args {
		if arg == "/dev/stdin" || arg == "install" {
			t.Error("install from /dev/stdin fails with the Rust coreutils of Ubuntu 26.04")
		}
	}
}
