package remotestorage

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestRcloneUploadDownloadDeleteTest(t *testing.T) {
	var calls []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if name == "rclone" && args[0] == "lsjson" {
				return &executor.Result{ExitCode: 0, Stdout: "[]"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoStreamFunc: func(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return io.WriteString(w, "backup-bytes")
		},
	}

	st, err := New(Config{Type: "rclone", RcloneRemote: "gdrive", RclonePath: "backups"}, mock, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ref, err := st.Upload(context.Background(), "/tmp/f.tar.gz", 12, "website/f.tar.gz")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if ref != "gdrive:backups/website/f.tar.gz" {
		t.Errorf("ref = %q", ref)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rclone copyto /tmp/f.tar.gz gdrive:backups/website/f.tar.gz") {
		t.Errorf("calls = %v", calls)
	}

	var buf strings.Builder
	if err := st.Download(context.Background(), ref, &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != "backup-bytes" {
		t.Errorf("downloaded = %q", buf.String())
	}

	calls = nil
	if err := st.Delete(context.Background(), ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rclone deletefile gdrive:backups/website/f.tar.gz") {
		t.Errorf("calls = %v", calls)
	}

	calls = nil
	info, err := st.Test(context.Background())
	if err != nil || !strings.Contains(info, "gdrive:backups") {
		t.Fatalf("Test: %q %v", info, err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rclone lsjson") {
		t.Errorf("test calls = %v", calls)
	}
}

func TestRcloneUploadErrorSurfaces(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 3, Stderr: "directory not found"}, nil
		},
	}
	st, _ := New(Config{Type: "rclone", RcloneRemote: "gdrive"}, mock, nil)
	if _, err := st.Upload(context.Background(), "/x", 1, "x"); err == nil || !strings.Contains(err.Error(), "exit 3") {
		t.Fatalf("expected exit-3 error, got %v", err)
	}
}
