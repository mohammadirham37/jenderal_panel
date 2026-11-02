package phpcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestQualifyCommand(t *testing.T) {
	cases := map[string]string{
		"php artisan queue:work":    "/usr/bin/php8.2 artisan queue:work",
		"  php artisan horizon":     "/usr/bin/php8.2 artisan horizon",
		"php":                       "/usr/bin/php8.2",
		"phpunit":                   "phpunit",
		"/usr/bin/php8.3 artisan x": "/usr/bin/php8.3 artisan x",
		"node server.js":            "node server.js",
	}
	for in, want := range cases {
		if got := QualifyCommand(in, "8.2"); got != want {
			t.Errorf("QualifyCommand(%q) = %q, want %q", in, got, want)
		}
	}
	if got := QualifyCommand("php x", "8.2; rm"); got != "php x" {
		t.Errorf("invalid version must leave the command alone, got %q", got)
	}
}

func TestEnsureSiteShimRejectsBadInput(t *testing.T) {
	m := &executor.MockExecutor{}
	if err := EnsureSiteShim(context.Background(), m, "root", "8.2"); err == nil {
		t.Error("non-website user accepted")
	}
	if err := EnsureSiteShim(context.Background(), m, "web_shop", "8.2;id"); err == nil {
		t.Error("bad version accepted")
	}
}

// The shim script is run for real in a temporary home: it links php and
// adds the PATH line to the profiles once.
func TestEnsureSiteShimScriptForReal(t *testing.T) {
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, ".profile"), []byte("# profile\n"), 0o644)
	var script string
	var args []string
	m := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, a ...string) (*executor.Result, error) {
		for i, v := range a {
			if v == "-c" {
				script, args = a[i+1], a[i+2:]
			}
		}
		return &executor.Result{}, nil
	}}
	if err := EnsureSiteShim(context.Background(), m, "web_shop", "8.2"); err != nil {
		t.Fatal(err)
	}
	// Re-target the paths at the temporary home and run it twice.
	args[1] = filepath.Join(home, ".jenderal/bin")
	for i := 0; i < 2; i++ {
		cmd := exec.Command("/bin/bash", append([]string{"-c", script}, args...)...)
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shim script: %v\n%s", err, out)
		}
	}
	target, err := os.Readlink(filepath.Join(home, ".jenderal/bin/php"))
	if err != nil || target != "/usr/bin/php8.2" {
		t.Errorf("php link = %q, %v", target, err)
	}
	for _, f := range []string{".profile", ".bashrc"} {
		data, _ := os.ReadFile(filepath.Join(home, f))
		if n := strings.Count(string(data), profileMarker); n != 1 {
			t.Errorf("%s has the PATH line %d times:\n%s", f, n, data)
		}
	}
}
