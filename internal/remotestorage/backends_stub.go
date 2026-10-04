package remotestorage

// Temporary stub for the rclone backend so the package compiles until Task 5
// replaces it.

import (
	"context"
	"io"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

type rcloneStorage struct {
	cfg  Config
	exec executor.CommandExecutor
}

func (r *rcloneStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	return "", ErrNotConfigured
}
func (r *rcloneStorage) Download(ctx context.Context, name string, w io.Writer) error {
	return ErrNotConfigured
}
func (r *rcloneStorage) Delete(ctx context.Context, name string) error { return ErrNotConfigured }
func (r *rcloneStorage) Test(ctx context.Context) (string, error)      { return "", ErrNotConfigured }
