// Package remotestorage speaks to off-site backup destinations: S3-compatible
// object storage, Google Drive (native API), and rclone remotes. It is a pure
// storage client — orchestration (when to upload, what to record) lives in
// internal/backup.
package remotestorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// ErrNotConfigured is returned when no remote destination is set up.
var ErrNotConfigured = errors.New("remote storage is not configured")

// Storage is one configured remote destination. `name` is the object name
// relative to the destination root (e.g. "website/20260102_x.tar.gz");
// remote references returned by Upload are scheme-tagged so deletes and
// downloads keep working later:
//
//	S3:     https://<host>/<bucket>/<prefix>/<name>
//	Drive:  gdrive://<fileId>
//	rclone: <remote>:<path>/<name>
type Storage interface {
	Upload(ctx context.Context, localPath string, size int64, name string) (string, error)
	Download(ctx context.Context, name string, w io.Writer) error
	Delete(ctx context.Context, name string) error
	Test(ctx context.Context) (string, error)
}

// New builds a Storage for cfg. cfg must be complete for its type — callers
// check Enabled() first. httpClient may be nil (http.DefaultClient).
func New(cfg Config, exec executor.CommandExecutor, httpClient *http.Client) (Storage, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	switch cfg.Type {
	case "s3":
		if !cfg.Enabled() {
			return nil, fmt.Errorf("s3 config incomplete: endpoint, bucket, access key and secret key are required")
		}
		return &s3Storage{cfg: cfg, http: httpClient, exec: exec}, nil
	case "gdrive":
		if cfg.GDriveClientID == "" || cfg.GDriveClientSecret == "" {
			return nil, fmt.Errorf("gdrive config incomplete: client id and client secret are required")
		}
		return NewGDrive(cfg, exec, httpClient), nil
	case "rclone":
		if cfg.RcloneRemote == "" {
			return nil, fmt.Errorf("rclone config incomplete: remote name is required")
		}
		return &rcloneStorage{cfg: cfg, exec: exec}, nil
	case "":
		return nil, ErrNotConfigured
	default:
		return nil, fmt.Errorf("unsupported remote type: %s", cfg.Type)
	}
}

// ParseRemoteRef splits a stored remote reference into its backend type and
// object name. Legacy rows written before scheme tagging (a bare
// "host/bucket/key") are recognised as S3.
func ParseRemoteRef(ref string) (backend, name string, ok bool) {
	switch {
	case strings.HasPrefix(ref, "gdrive://"):
		id := strings.TrimPrefix(ref, "gdrive://")
		if id == "" {
			return "", "", false
		}
		return "gdrive", id, true
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		u, err := url.Parse(ref)
		if err != nil {
			return "", "", false
		}
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
		if len(parts) != 2 || parts[1] == "" {
			return "", "", false
		}
		return "s3", parts[1], true
	case strings.Contains(ref, ":"):
		return "rclone", ref, true
	default:
		parts := strings.SplitN(ref, "/", 3)
		if len(parts) == 3 && parts[2] != "" {
			return "s3", parts[2], true
		}
		return "", "", false
	}
}
