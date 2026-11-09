package ssl

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Under the panel service's UMask=0077 the challenge used to be written
// 0700/0600 and nginx answered Let's Encrypt with 404.
func TestWebrootProviderWritesWorldReadableChallenges(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	root := filepath.Join(t.TempDir(), "acme-challenges")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	p := newWebrootProvider(root)
	if err := p.Present("example.com", "tok123", "tok123.key"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, ".well-known", "acme-challenge", "tok123")
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "tok123.key" {
		t.Fatalf("challenge = %q, %v", data, err)
	}
	for path, want := range map[string]os.FileMode{
		root:                               0o755,
		filepath.Join(root, ".well-known"): 0o755,
		filepath.Join(root, ".well-known", "acme-challenge"): 0o755,
		file: 0o644,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
	if err := p.CleanUp("example.com", "tok123", "tok123.key"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("challenge file not removed")
	}
	if err := p.CleanUp("example.com", "tok123", "tok123.key"); err != nil {
		t.Errorf("cleaning up twice must not fail: %v", err)
	}
}
