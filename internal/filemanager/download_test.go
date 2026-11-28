package filemanager

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// localPythonExec runs the "sudo -u user -- python3 ..." stream locally,
// without sudo, so the real zip script is exercised.
type localPythonExec struct {
	recordingExec
	args []string
}

func (l *localPythonExec) RunSudoStreamSplit(ctx context.Context, stdoutW, stderrW io.Writer, name string, args ...string) (int, error) {
	l.args = append([]string{name}, args...)
	cmd := exec.CommandContext(ctx, args[2], args[3:]...) // drop "-u user --"
	cmd.Stdout, cmd.Stderr = stdoutW, stderrW
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

func TestStreamZipArchivesSelectedFoldersAndFiles(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	home := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(home, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/public/index.php", "<?php echo 1;")
	write("app/routes/web.php", "routes")
	write("app/.env", "SECRET=1")
	write("notes.txt", "hello")
	os.MkdirAll(filepath.Join(home, "app/empty"), 0o755)
	os.Symlink("/etc/hosts", filepath.Join(home, "app/hosts-link"))

	l := &localPythonExec{}
	svc := &Service{exec: l}
	var out bytes.Buffer
	warnings, err := svc.StreamZip(context.Background(), "/home/web_site",
		[]string{filepath.Join(home, "app"), filepath.Join(home, "notes.txt")}, &out)
	if err != nil {
		t.Fatalf("StreamZip: %v (%s)", err, warnings)
	}
	if strings.Join(l.args[:4], " ") != "-u web_site -- python3" {
		t.Errorf("must run as the website user: %v", l.args[:4])
	}

	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	var names []string
	contents := map[string]string{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		if !f.FileInfo().IsDir() {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			contents[f.Name] = string(b)
		}
	}
	sort.Strings(names)
	want := []string{"app/", "app/.env", "app/empty/", "app/public/", "app/public/index.php", "app/routes/", "app/routes/web.php", "notes.txt"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("entries = %v\nwant      %v", names, want)
	}
	if contents["app/public/index.php"] != "<?php echo 1;" || contents["notes.txt"] != "hello" {
		t.Errorf("contents = %v", contents)
	}
}

func TestValidateDownloadPaths(t *testing.T) {
	svc := &Service{exec: &recordingExec{}}
	ctx := context.Background()

	got, err := svc.ValidateDownloadPaths(ctx, "/home/web_site", []string{"app", "/logs"})
	if err != nil || strings.Join(got, ",") != "/home/web_site/app,/home/web_site/logs" {
		t.Fatalf("got %v, %v", got, err)
	}
	for name, paths := range map[string][]string{
		"none":         nil,
		"website root": {"/"},
		"escape":       {"../web_other/app"},
		"duplicate":    {"a/app", "b/app"},
	} {
		if _, err := svc.ValidateDownloadPaths(ctx, "/home/web_site", paths); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	many := make([]string, maxDownloadPaths+1)
	for i := range many {
		many[i] = "f" + strings.Repeat("x", i%5) + string(rune('a'+i%26))
	}
	if _, err := svc.ValidateDownloadPaths(ctx, "/home/web_site", many); err == nil {
		t.Error("too many entries must be rejected")
	}
}

func TestArchiveDownloadName(t *testing.T) {
	if got := ArchiveDownloadName([]string{"/app/public"}); got != "public.zip" {
		t.Errorf("single = %q", got)
	}
	if got := ArchiveDownloadName([]string{"/app/public", "/app/routes"}); !strings.HasPrefix(got, "app-") || !strings.HasSuffix(got, ".zip") {
		t.Errorf("multiple = %q", got)
	}
	if got := ArchiveDownloadName([]string{"/app", "/logs"}); !strings.HasPrefix(got, "files-") {
		t.Errorf("root selection = %q", got)
	}
}

var _ executor.CommandExecutor = (*localPythonExec)(nil)
