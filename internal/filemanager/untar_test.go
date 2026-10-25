package filemanager

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The tar extractor is run for real: regular files and directories land in
// the destination, while path escapes, absolute paths, links out of the
// destination, and setuid bits do not.
func TestUntarScriptExtractsSafely(t *testing.T) {
	// Servers run Python 3.12 (data filter); older interpreters take the
	// manual path. Exercise every interpreter available here.
	ran := false
	for _, py := range []string{"python3", "python3.12", "python3.11"} {
		if _, err := exec.LookPath(py); err != nil {
			continue
		}
		ran = true
		t.Run(py, func(t *testing.T) { untarSafely(t, py) })
	}
	if !ran {
		t.Skip("python3 not available")
	}
}

func untarSafely(t *testing.T, python string) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "site.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			_, _ = tw.Write([]byte(body))
		}
	}
	add(&tar.Header{Name: "app/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "app/index.php", Typeflag: tar.TypeReg, Mode: 0o644}, "<?php echo 1;")
	add(&tar.Header{Name: "app/run.sh", Typeflag: tar.TypeReg, Mode: 0o4755}, "#!/bin/sh")
	add(&tar.Header{Name: "../escape.txt", Typeflag: tar.TypeReg, Mode: 0o644}, "x")
	add(&tar.Header{Name: filepath.Join(root, "abs.txt"), Typeflag: tar.TypeReg, Mode: 0o644}, "x")
	add(&tar.Header{Name: "app/passwd", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, "")
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	dest := filepath.Join(root, "out")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(python, "-c", untarScript, archivePath, dest).CombinedOutput()
	if err != nil {
		t.Fatalf("extract failed instead of skipping unsafe entries: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dest, "app", "index.php")); err != nil {
		t.Errorf("safe file not extracted\n%s", out)
	}

	for _, escaped := range []string{filepath.Join(root, "escape.txt"), filepath.Join(root, "abs.txt")} {
		if _, err := os.Stat(escaped); err == nil {
			t.Errorf("%s was written outside the destination\n%s", escaped, out)
		}
	}
	if target, err := os.Readlink(filepath.Join(dest, "app", "passwd")); err == nil {
		t.Errorf("a link to %s was extracted\n%s", target, out)
	}
	if info, err := os.Stat(filepath.Join(dest, "app", "run.sh")); err == nil && info.Mode()&os.ModeSetuid != 0 {
		t.Error("setuid bit survived extraction")
	}
}

// A normal project archive extracts completely.
func TestUntarScriptExtractsAnOrdinaryArchive(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src", "app")
	_ = os.MkdirAll(filepath.Join(src, "public"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "public", "index.php"), []byte("<?php"), 0o644)
	archivePath := filepath.Join(root, "app.tar.gz")
	if out, err := exec.Command("tar", "-czf", archivePath, "-C", filepath.Join(root, "src"), "app").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	dest := filepath.Join(root, "out")
	_ = os.Mkdir(dest, 0o755)
	if out, err := exec.Command("python3", "-c", untarScript, archivePath, dest).CombinedOutput(); err != nil {
		t.Fatalf("extract: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dest, "app", "public", "index.php")); err != nil {
		t.Errorf("index.php not extracted: %v", err)
	}
}
