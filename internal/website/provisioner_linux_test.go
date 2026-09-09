//go:build linux

package website

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGNUHardLinkNoTargetDirectoryDoesNotFollowDirectories(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "staged")
	if err := os.WriteFile(source, []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		target func() string
	}{
		{
			name: "directory",
			target: func() string {
				path := filepath.Join(root, "directory")
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
				return path
			},
		},
		{
			name: "symlink to directory",
			target: func() string {
				directory := filepath.Join(root, "linked-directory")
				if err := os.Mkdir(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "directory-link")
				if err := os.Symlink(directory, path); err != nil {
					t.Fatal(err)
				}
				return path
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := test.target()
			if err := exec.Command("ln", "-T", "--", source, target).Run(); err == nil {
				t.Fatal("ln -T unexpectedly replaced or followed a directory destination")
			}
			if _, err := os.Lstat(filepath.Join(target, filepath.Base(source))); !os.IsNotExist(err) {
				t.Fatalf("hard link was unexpectedly created inside destination: %v", err)
			}
		})
	}
}
