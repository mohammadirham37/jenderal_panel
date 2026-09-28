package filemanager

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// ---------- validatePath tests ----------

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		subPath  string
		want     string
		wantErr  bool
	}{
		{
			name:     "empty subpath returns base",
			basePath: "/home/webuser",
			subPath:  "",
			want:     "/home/webuser",
		},
		{
			name:     "valid relative subpath",
			basePath: "/home/webuser",
			subPath:  "public",
			want:     "/home/webuser/public",
		},
		{
			name:     "valid nested subpath",
			basePath: "/home/webuser",
			subPath:  "public/css/style.css",
			want:     "/home/webuser/public/css/style.css",
		},
		{
			name:     "reject traversal with ..",
			basePath: "/home/webuser",
			subPath:  "../../../etc/passwd",
			wantErr:  true,
		},
		{
			name:     "reject double dot in middle",
			basePath: "/home/webuser",
			subPath:  "public/../../etc/shadow",
			wantErr:  true,
		},
		{
			name:     "reject bare ..",
			basePath: "/home/webuser",
			subPath:  "..",
			wantErr:  true,
		},
		{
			name:     "base path with trailing slash",
			basePath: "/home/webuser/",
			subPath:  "public",
			want:     "/home/webuser/public",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validatePath(tt.basePath, tt.subPath)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for subPath %q, got path %q", tt.subPath, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// ---------- Browse tests ----------

func TestBrowse(t *testing.T) {
	basePath := t.TempDir()
	if err := os.Mkdir(filepath.Join(basePath, "public"), 0755); err != nil {
		t.Fatal(err)
	}
	lsOutput := `total 20
drwxr-xr-x  4 webuser webuser 4096 Jan 15 10:30 .
drwxr-xr-x  3 root    root    4096 Jan 10 08:00 ..
-rw-r--r--  1 webuser webuser  234 Jan 15 10:30 index.html
drwxr-xr-x  2 webuser webuser 4096 Jan 14 09:00 css
-rw-r--r--  1 webuser webuser 1024 Jan 13 14:20 app.js
lrwxrwxrwx  1 webuser webuser   11 Jan 12 08:00 link -> /tmp/target
`

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   lsOutput,
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	entries, err := svc.Browse(context.Background(), basePath, "public")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 4 entries: index.html, css, app.js, link
	// (. and .. are skipped, total line is skipped)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Verify index.html
	if entries[0].Name != "index.html" {
		t.Fatalf("expected first entry name 'index.html', got %q", entries[0].Name)
	}
	if entries[0].IsDir {
		t.Fatal("expected index.html to not be a directory")
	}
	if entries[0].Size != 234 {
		t.Fatalf("expected size 234, got %d", entries[0].Size)
	}
	if entries[0].Permissions != "-rw-r--r--" {
		t.Fatalf("expected permissions '-rw-r--r--', got %q", entries[0].Permissions)
	}
	if entries[0].Owner != "webuser" {
		t.Fatalf("expected owner 'webuser', got %q", entries[0].Owner)
	}
	if entries[0].ModTime != "Jan 15 10:30" {
		t.Fatalf("expected mod time 'Jan 15 10:30', got %q", entries[0].ModTime)
	}

	// Verify css directory
	if entries[1].Name != "css" {
		t.Fatalf("expected second entry name 'css', got %q", entries[1].Name)
	}
	if !entries[1].IsDir {
		t.Fatal("expected css to be a directory")
	}

	// Verify app.js
	if entries[2].Name != "app.js" {
		t.Fatalf("expected third entry name 'app.js', got %q", entries[2].Name)
	}
	if entries[2].Size != 1024 {
		t.Fatalf("expected size 1024, got %d", entries[2].Size)
	}

	// Verify symlink (name without -> target)
	if entries[3].Name != "link" {
		t.Fatalf("expected fourth entry name 'link', got %q", entries[3].Name)
	}
}

func TestBrowse_PathTraversal(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			t.Fatal("RunSudo should not be called for invalid paths")
			return nil, nil
		},
	}

	svc := NewService(mock, nil)
	_, err := svc.Browse(context.Background(), "/home/webuser", "../../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal attempt")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Fatalf("expected error about '..', got: %v", err)
	}
}

func TestBrowse_EmptyDir(t *testing.T) {
	basePath := t.TempDir()
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   "total 0\n",
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	entries, err := svc.Browse(context.Background(), basePath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	basePath := t.TempDir()
	outsidePath := t.TempDir()
	if err := os.Symlink(outsidePath, filepath.Join(basePath, "escape")); err != nil {
		t.Fatal(err)
	}

	if _, err := resolvePath(basePath, "escape/secret.txt", true); err == nil {
		t.Fatal("resolvePath accepted a symlink that escapes the website home")
	}
}

func TestResolveWebsitePathFallbackRejectsSymlinkInPrivateDirectory(t *testing.T) {
	basePath := t.TempDir()
	privatePath := filepath.Join(basePath, "private")
	if err := os.Mkdir(privatePath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(basePath, filepath.Join(privatePath, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privatePath, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(privatePath, 0700)

	mock := &executor.MockExecutor{
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name != "-u" || len(args) < 5 || args[1] != "--" || args[2] != "test" {
				t.Fatalf("unexpected fallback command %q %q", name, args)
			}
			if args[3] == "-L" && strings.HasSuffix(args[4], string(filepath.Separator)+"alias") {
				return &executor.Result{ExitCode: 0}, nil
			}
			if args[3] == "-e" && strings.HasSuffix(args[4], string(filepath.Separator)+"private") {
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
	}

	_, err := NewService(mock, nil).resolveWebsitePath(context.Background(), basePath, "private/alias", false)
	if err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("private-directory symlink error = %v, want symbolic-link rejection", err)
	}
}

func TestDeleteFileRejectsWebsiteRoot(t *testing.T) {
	basePath := t.TempDir()
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			t.Fatal("Run should not be called when deleting the website root")
			return nil, nil
		},
		RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			t.Fatal("RunSudo should not be called when deleting the website root")
			return nil, nil
		},
	}

	if err := NewService(mock, nil).DeleteFile(context.Background(), basePath, "/"); err == nil {
		t.Fatal("DeleteFile accepted the website root")
	}
}

func TestRejectResolvedWebsiteRootBlocksCanonicalAlias(t *testing.T) {
	basePath := t.TempDir()
	resolvedBasePath, err := filepath.EvalSymlinks(basePath)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(&executor.MockExecutor{}, nil)
	if err := svc.rejectResolvedWebsiteRoot(context.Background(), basePath, resolvedBasePath); err == nil {
		t.Fatal("canonical alias to the website root was accepted")
	}
}

func TestWriteFileTreatsFilenameAndContentAsLiteralData(t *testing.T) {
	basePath := t.TempDir()
	resolvedBasePath, err := filepath.EvalSymlinks(basePath)
	if err != nil {
		t.Fatal(err)
	}
	content := "$(touch /tmp/should-not-run) ' literal content"
	var writtenContent string
	chmodCalled := false
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			t.Fatal("WriteFile must not invoke a shell")
			return nil, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name != "-u" || len(args) < 3 || args[1] != "--" {
				t.Fatalf("unexpected website-user command %q %q", name, args)
			}
			switch args[2] {
			case "stat":
				return &executor.Result{ExitCode: 1}, nil
			case "chmod":
				chmodCalled = true
				if len(args) != 6 || args[3] != "0644" || args[4] != "--" {
					t.Fatalf("new-file chmod args = %q, want chmod 0644 -- <target>", args)
				}
				return &executor.Result{ExitCode: 0}, nil
			default:
				t.Fatalf("unexpected website-user command %q", args[2])
				return nil, nil
			}
		},
		RunSudoWithInputFunc: func(_ context.Context, input, name string, args ...string) (*executor.Result, error) {
			writtenContent = input
			if name != "-u" || len(args) != 5 || args[1] != "--" || args[2] != "tee" || args[3] != "--" {
				t.Fatalf("RunSudoWithInput = %q %q, want -u <user> -- tee -- <target>", name, args)
			}
			if args[4] != filepath.Join(resolvedBasePath, "name;touch injected") {
				t.Fatalf("target = %q, want literal filename", args[4])
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	err = NewService(mock, nil).WriteFile(context.Background(), basePath, "name;touch injected", content)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if writtenContent != content {
		t.Fatalf("written content = %q, want %q", writtenContent, content)
	}
	if !chmodCalled {
		t.Fatal("WriteFile did not set a readable mode on the new file")
	}
}

func TestWriteFilePreservesExistingMode(t *testing.T) {
	basePath := t.TempDir()
	filePath := filepath.Join(basePath, "existing.sh")
	if err := os.WriteFile(filePath, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name != "-u" || len(args) < 3 {
				t.Fatalf("unexpected command %q %q", name, args)
			}
			if args[2] == "chmod" {
				t.Fatal("WriteFile changed the mode of an existing file")
			}
			if args[2] != "stat" {
				t.Fatalf("unexpected command %q", args[2])
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	if err := NewService(mock, nil).WriteFile(context.Background(), basePath, "existing.sh", "new"); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

// ---------- parseLsLine tests ----------

func TestParseLsLine(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		isDir bool
		fname string
		size  int64
	}{
		{
			name:  "regular file",
			line:  "-rw-r--r--  1 user group  1234 Jan 15 10:30 myfile.txt",
			isDir: false,
			fname: "myfile.txt",
			size:  1234,
		},
		{
			name:  "directory",
			line:  "drwxr-xr-x  2 user group 4096 Feb 20 14:00 mydir",
			isDir: true,
			fname: "mydir",
			size:  4096,
		},
		{
			name:  "file with spaces",
			line:  "-rw-r--r--  1 user group  100 Mar  5 09:00 my file name.txt",
			isDir: false,
			fname: "my file name.txt",
			size:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := parseLsLine(tt.line, "/home/user")
			if entry == nil {
				t.Fatal("expected non-nil entry")
			}
			if entry.Name != tt.fname {
				t.Fatalf("expected name %q, got %q", tt.fname, entry.Name)
			}
			if entry.IsDir != tt.isDir {
				t.Fatalf("expected isDir=%v, got %v", tt.isDir, entry.IsDir)
			}
			if entry.Size != tt.size {
				t.Fatalf("expected size %d, got %d", tt.size, entry.Size)
			}
		})
	}
}

// ---------- zip / unzip ----------

// RecordingExec captures sudo commands while answering like the site owner's
// filesystem would for the simple cases the zip tests rely on.
type recordingExec struct {
	commands [][]string
	missing  map[string]bool // paths reported as non-existent
}

func (r *recordingExec) Run(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	return &executor.Result{ExitCode: 0}, nil
}

func (r *recordingExec) RunSudo(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	r.commands = append(r.commands, append([]string{name}, args...))
	return r.answer(append([]string{name}, args...))
}

func (r *recordingExec) RunSudoWithInput(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
	r.commands = append(r.commands, append([]string{name}, args...))
	return &executor.Result{ExitCode: 0}, nil
}

func (r *recordingExec) RunSudoStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *recordingExec) RunSudoStreamSplit(ctx context.Context, stdoutW, stderrW io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *recordingExec) RunSudoWithInputStream(ctx context.Context, input io.Reader, w io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *recordingExec) answer(args []string) (*executor.Result, error) {
	// args begin with "-u <user> --" before the real command.
	rest := args
	for i, arg := range args {
		if arg == "--" {
			rest = args[i+1:]
			break
		}
	}
	if len(rest) < 2 {
		return &executor.Result{ExitCode: 0}, nil
	}
	path := rest[len(rest)-1]
	switch {
	case rest[0] == "test" && rest[1] == "-L":
		return &executor.Result{ExitCode: 1}, nil // no symlinks
	case rest[0] == "test" && (rest[1] == "-d" || rest[1] == "-e"):
		if r.missing[path] {
			return &executor.Result{ExitCode: 1}, nil
		}
		return &executor.Result{ExitCode: 0}, nil
	case rest[0] == "realpath":
		// Echo the path back; good enough for in-home targets.
		return &executor.Result{ExitCode: 0, Stdout: path + "\n"}, nil
	}
	return &executor.Result{ExitCode: 0}, nil
}

func TestZipBuildsPythonInvocation(t *testing.T) {
	exec := &recordingExec{}
	svc := &Service{exec: exec}

	created, err := svc.Zip(context.Background(), "/home/web_site", "app", "")
	if err != nil {
		t.Fatalf("Zip: %v", err)
	}
	if created != "/home/web_site/app.zip" {
		t.Fatalf("default archive = %q, want /home/web_site/app.zip", created)
	}

	var pyArgs []string
	for _, cmd := range exec.commands {
		// Recorded as ["-u", user, "--", "python3", ...].
		if len(cmd) > 3 && cmd[3] == "python3" {
			pyArgs = cmd[4:]
		}
	}
	if pyArgs == nil {
		t.Fatalf("zip never invoked python3: %v", exec.commands)
	}
	// python3 -c <script> <dest> <source> — no "--" separator because python
	// passes everything after -c verbatim to sys.argv.
	if pyArgs[0] != "-c" || pyArgs[2] != "/home/web_site/app.zip" || pyArgs[3] != "/home/web_site/app" {
		t.Fatalf("unexpected python3 invocation: %v", pyArgs)
	}
	for _, arg := range pyArgs {
		if arg == "--" {
			t.Fatalf("-- must not be passed to python3: %v", pyArgs)
		}
	}
}

func TestZipRejectsUnsafeTargets(t *testing.T) {
	svc := &Service{exec: &recordingExec{}}

	if _, err := svc.Zip(context.Background(), "/home/web_site", "", ""); err == nil {
		t.Fatal("zipping the website root must be rejected")
	}
	if _, err := svc.Zip(context.Background(), "/home/web_site", "app", "archive.tar"); err == nil {
		t.Fatal("a non-.zip target must be rejected")
	}
}

func TestUnzipBuildsPythonInvocation(t *testing.T) {
	exec := &recordingExec{}
	svc := &Service{exec: exec}

	dest, err := svc.Unzip(context.Background(), "/home/web_site", "uploads/release.zip", "public")
	if err != nil {
		t.Fatalf("Unzip: %v", err)
	}
	if dest != "/home/web_site/public" {
		t.Fatalf("dest = %q, want /home/web_site/public", dest)
	}

	var pyArgs []string
	for _, cmd := range exec.commands {
		// Recorded as ["-u", user, "--", "python3", ...].
		if len(cmd) > 3 && cmd[3] == "python3" {
			pyArgs = cmd[4:]
		}
	}
	if pyArgs == nil {
		t.Fatalf("unzip never invoked python3: %v", exec.commands)
	}
	if pyArgs[2] != "/home/web_site/uploads/release.zip" || pyArgs[3] != "/home/web_site/public" {
		t.Fatalf("unexpected python3 invocation: %v", pyArgs)
	}
}

func TestUnzipRejectsNonZipArchives(t *testing.T) {
	svc := &Service{exec: &recordingExec{}}

	if _, err := svc.Unzip(context.Background(), "/home/web_site", "uploads/backup.tar.gz", ""); err == nil {
		t.Fatal("a non-.zip archive must be rejected")
	}
}

// ---------- copy / move ----------

func TestCopyAndMoveBuildInvocations(t *testing.T) {
	// The destination leaf must be reported as missing, or the
	// already-exists guard rejects the transfer.
	exec := &recordingExec{missing: map[string]bool{"/home/web_site/public/app.php": true}}
	svc := &Service{exec: exec}

	if err := svc.Copy(context.Background(), "/home/web_site", "app.php", "public/app.php"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	var cpArgs []string
	for _, cmd := range exec.commands {
		// Recorded as ["-u", user, "--", "cp", ...].
		if len(cmd) > 3 && cmd[3] == "cp" {
			cpArgs = cmd[4:]
		}
	}
	if cpArgs == nil {
		t.Fatalf("copy never invoked cp: %v", exec.commands)
	}
	if cpArgs[0] != "-a" || cpArgs[2] != "/home/web_site/app.php" || cpArgs[3] != "/home/web_site/public/app.php" {
		t.Fatalf("unexpected cp invocation: %v", cpArgs)
	}

	if err := svc.Move(context.Background(), "/home/web_site", "app.php", "public/app.php"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	var mvArgs []string
	for _, cmd := range exec.commands {
		if len(cmd) > 3 && cmd[3] == "mv" {
			mvArgs = cmd[4:]
		}
	}
	if mvArgs == nil {
		t.Fatalf("move never invoked mv: %v", exec.commands)
	}
	if mvArgs[0] != "--" || mvArgs[1] != "/home/web_site/app.php" || mvArgs[2] != "/home/web_site/public/app.php" {
		t.Fatalf("unexpected mv invocation: %v", mvArgs)
	}
}

func TestTransferRejectsUnsafeTargets(t *testing.T) {
	// An existing recordingExec answers test -e with success for every
	// path, so any transfer must be rejected as an existing target.
	svc := &Service{exec: &recordingExec{}}

	if err := svc.Copy(context.Background(), "/home/web_site", "app.php", "public/app.php"); err == nil {
		t.Fatal("copy onto an existing target must be rejected")
	}
	if err := svc.Move(context.Background(), "/home/web_site", "app.php", "public/app.php"); err == nil {
		t.Fatal("move onto an existing target must be rejected")
	}
	// Same source and target.
	missing := &recordingExec{missing: map[string]bool{"/home/web_site/app.php": true}}
	if err := (&Service{exec: missing}).Copy(context.Background(), "/home/web_site", "app.php", "app.php"); err == nil {
		t.Fatal("copy onto the source itself must be rejected")
	}
	// The website root can never be a source or a target.
	if err := (&Service{exec: missing}).Copy(context.Background(), "/home/web_site", "", "app.php"); err == nil {
		t.Fatal("copying the website root must be rejected")
	}
}

func TestTransferRejectsTargetInsideSource(t *testing.T) {
	exec := &recordingExec{missing: map[string]bool{"/home/web_site/app/inner/x.php": true}}
	svc := &Service{exec: exec}

	if err := svc.Copy(context.Background(), "/home/web_site", "app", "app/inner/x.php"); err == nil {
		t.Fatal("a target inside the source must be rejected")
	}
	for _, cmd := range exec.commands {
		if len(cmd) > 3 && (cmd[3] == "cp" || cmd[3] == "mv") {
			t.Fatalf("no copy command may run for an invalid target: %v", exec.commands)
		}
	}
}
