package remotestorage

// Temporary stubs for the remaining backends so the package compiles before
// Tasks 4-5 land. Each block is deleted when the real implementation arrives.

import (
	"context"
	"io"
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

type GDriveStorage struct {
	cfg  Config
	exec executor.CommandExecutor
}

func NewGDrive(cfg Config, exec executor.CommandExecutor, hc *http.Client) *GDriveStorage {
	return &GDriveStorage{cfg: cfg, exec: exec}
}
func (g *GDriveStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	return "", ErrNotConfigured
}
func (g *GDriveStorage) Download(ctx context.Context, name string, w io.Writer) error {
	return ErrNotConfigured
}
func (g *GDriveStorage) Delete(ctx context.Context, name string) error { return ErrNotConfigured }
func (g *GDriveStorage) Test(ctx context.Context) (string, error)      { return "", ErrNotConfigured }

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
