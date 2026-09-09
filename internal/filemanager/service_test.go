package filemanager

import (
	"context"
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

func TestWriteFileTreatsFilenameAndContentAsLiteralData(t *testing.T) {
	basePath := t.TempDir()
	resolvedBasePath, err := filepath.EvalSymlinks(basePath)
	if err != nil {
		t.Fatal(err)
	}
	content := "$(touch /tmp/should-not-run) ' literal content"
	var writtenContent string
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			t.Fatal("WriteFile must not invoke a shell")
			return nil, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			t.Fatalf("WriteFile must use the input-aware executor, got %q %q", name, args)
			return nil, nil
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
