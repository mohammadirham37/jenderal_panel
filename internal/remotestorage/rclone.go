package remotestorage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// rcloneStorage drives a server-configured rclone remote (e.g. "gdrive:backups").
// rclone must be installed on the server; its config is root's, so every
// command runs through sudo.
type rcloneStorage struct {
	cfg  Config
	exec executor.CommandExecutor
}

func (r *rcloneStorage) dest(name string) string {
	path := strings.Trim(r.cfg.RclonePath, "/")
	if path == "" {
		return r.cfg.RcloneRemote + ":" + name
	}
	return r.cfg.RcloneRemote + ":" + path + "/" + name
}

func (r *rcloneStorage) run(ctx context.Context, args ...string) error {
	result, err := r.exec.RunSudo(ctx, "rclone", args...)
	if err != nil {
		return fmt.Errorf("rclone %s: %w", args[0], err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("rclone %s failed (exit %d): %s", args[0], result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Upload copies the local file to the remote with rclone copyto.
func (r *rcloneStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	dest := r.dest(name)
	if err := r.run(ctx, "copyto", localPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Download streams the remote object (rclone cat) into w. `name` is a full
// remote reference as returned by ParseRemoteRef (it already carries
// remote:path, so no prefix is added here).
func (r *rcloneStorage) Download(ctx context.Context, name string, w io.Writer) error {
	if _, err := r.exec.RunSudoStream(ctx, w, "rclone", "cat", name); err != nil {
		return fmt.Errorf("rclone cat: %w", err)
	}
	return nil
}

// Delete removes the remote object identified by the full remote reference.
func (r *rcloneStorage) Delete(ctx context.Context, name string) error {
	return r.run(ctx, "deletefile", name)
}

// Test verifies the remote is reachable.
func (r *rcloneStorage) Test(ctx context.Context) (string, error) {
	path := strings.Trim(r.cfg.RclonePath, "/")
	target := r.cfg.RcloneRemote + ":" + path
	if _, err := r.exec.RunSudo(ctx, "rclone", "lsjson", "--max-depth", "1", target); err != nil {
		return "", fmt.Errorf("rclone lsjson: %w", err)
	}
	return target + " (reachable)", nil
}
