package noderuntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// fakeNVM is a stand-in for nvm.sh that resolves versions the way the prune
// script relies on: a major or exact version to the newest matching install.
const fakeNVM = `nvm() {
  case "$1" in
    version)
      want=${2#v}
      best=''
      for d in "$NVM_DIR"/versions/node/v*; do
        n=${d##*/}
        case "$n" in v"$want"|v"$want".*) best=$n;; esac
      done
      [ -n "$best" ] && printf '%s\n' "$best" || { echo N/A; return 3; }
      ;;
    cache) rm -rf -- "$NVM_DIR/.cache"/* ;;
  esac
}
`

// scriptPath returns a PATH for running the prune script. GNU du is what
// the servers have; where du lacks -b (macOS) a shim stands in for it.
func scriptPath(t *testing.T) string {
	t.Helper()
	path := "/usr/local/bin:/usr/bin:/bin"
	if exec.Command("du", "-sb", ".").Run() == nil {
		return path
	}
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "du"), []byte("#!/bin/sh\nprintf '4096\\t%s\\n' \"$3\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return shim + ":" + path
}

func runPruneScript(t *testing.T, version string, versions []string, nvmrc string) (string, []string) {
	t.Helper()
	home := t.TempDir()
	nvmDir := filepath.Join(home, ".nvm")
	for _, v := range versions {
		if err := os.MkdirAll(filepath.Join(nvmDir, "versions", "node", v, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(nvmDir, "versions", "node", v, "bin", "node"), make([]byte, 4096), 0o755)
	}
	_ = os.MkdirAll(filepath.Join(nvmDir, ".cache", "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(nvmDir, ".cache", "bin", "node.tar.xz"), make([]byte, 8192), 0o644)
	_ = os.WriteFile(filepath.Join(nvmDir, "nvm.sh"), []byte(fakeNVM), 0o644)
	_ = os.WriteFile(filepath.Join(nvmDir, ".jenderal-nvm-commit"), []byte(nvmCommit+"\n"), 0o644)
	if nvmrc != "" {
		_ = os.MkdirAll(filepath.Join(home, "app"), 0o755)
		_ = os.WriteFile(filepath.Join(home, "app", ".nvmrc"), []byte(nvmrc+"\n"), 0o644)
	}

	cmd := exec.Command("bash", "-c", pruneScript, "--", "web_x", version, home)
	cmd.Env = []string{"HOME=" + home, "NVM_DIR=" + nvmDir, "PATH=" + scriptPath(t)}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prune script: %v\n%s", err, out)
	}
	entries, _ := os.ReadDir(filepath.Join(nvmDir, "versions", "node"))
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	sort.Strings(left)
	return string(out), left
}

func TestPruneScriptKeepsOnlyTheActiveAndPinnedVersions(t *testing.T) {
	out, left := runPruneScript(t, "22",
		[]string{"v20.19.0", "v22.20.0", "v22.23.4", "v24.1.0"}, "20")
	if strings.Join(left, ",") != "v20.19.0,v22.23.4" {
		t.Errorf("left %v, want the active v22.23.4 and the .nvmrc-pinned v20.19.0", left)
	}
	result := parsePruneOutput(out, nil)
	if result.Kept != "v22.23.4" || len(result.Removed) != 2 || result.BytesFreed <= 0 {
		t.Errorf("unexpected result %+v from:\n%s", result, out)
	}
}

func TestPruneScriptRefusesWhenTheSelectedVersionIsMissing(t *testing.T) {
	home := t.TempDir()
	nvmDir := filepath.Join(home, ".nvm")
	_ = os.MkdirAll(filepath.Join(nvmDir, "versions", "node", "v20.19.0"), 0o755)
	_ = os.WriteFile(filepath.Join(nvmDir, "nvm.sh"), []byte(fakeNVM), 0o644)
	_ = os.WriteFile(filepath.Join(nvmDir, ".jenderal-nvm-commit"), []byte(nvmCommit+"\n"), 0o644)
	cmd := exec.Command("bash", "-c", pruneScript, "--", "web_x", "22", home)
	cmd.Env = []string{"HOME=" + home, "NVM_DIR=" + nvmDir, "PATH=" + scriptPath(t)}
	if err := cmd.Run(); err == nil {
		t.Fatal("pruning must refuse when the selected major is not installed")
	}
	if _, err := os.Stat(filepath.Join(nvmDir, "versions", "node", "v20.19.0")); err != nil {
		t.Error("the only installed version was removed")
	}
}

func TestPruneScriptIsValidBash(t *testing.T) {
	if out, err := exec.Command("bash", "-n", "-c", pruneScript).CombinedOutput(); err != nil {
		t.Fatalf("prune script is not valid bash: %v: %s", err, out)
	}
}

func TestPruneRunsAsTheWebsiteUser(t *testing.T) {
	var got []string
	svc := New(fakeExecutor{runSudo: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		got = append([]string{name}, args...)
		return &executor.Result{Stdout: "kept=v22.23.4\nremoved=v22.20.0 209715200\ncache=31457280\n"}, nil
	}})
	result, err := svc.Prune(context.Background(), "web_shop", "22", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 || got[0] != "-u" || got[1] != "web_shop" {
		t.Errorf("prune must run as the website user, got %v", got)
	}
	if result.BytesFreed != 209715200+31457280 || result.Kept != "v22.23.4" {
		t.Errorf("unexpected result %+v", result)
	}
	if _, err := svc.Prune(context.Background(), "root", "22", nil); err == nil {
		t.Error("a non-website user must be refused")
	}
}
