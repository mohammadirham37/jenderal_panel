package backup

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/remotestorage"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Remote storage integration: configuration lives in the settings KV (via
// remotestorage.ConfigStore); the backup service decides when to upload and
// what to record.

// storageOverride short-circuits remotestorage.New in tests.
var storageOverride func(cfg remotestorage.Config) (remotestorage.Storage, error)

// SetRemoteStore attaches the settings-backed config store. Without it the
// panel has no remote storage.
func (s *Service) SetRemoteStore(store *remotestorage.ConfigStore) {
	s.remoteStore = store
}

func (s *Service) remoteConfig(ctx context.Context) (remotestorage.Config, error) {
	if s.remoteStore == nil {
		return remotestorage.Config{}, remotestorage.ErrNotConfigured
	}
	return s.remoteStore.Load(ctx)
}

// remoteStorage builds the Storage for the currently configured backend.
func (s *Service) remoteStorage(ctx context.Context) (remotestorage.Storage, remotestorage.Config, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return nil, cfg, err
	}
	if !cfg.Enabled() {
		return nil, cfg, remotestorage.ErrNotConfigured
	}
	if storageOverride != nil {
		st, err := storageOverride(cfg)
		return st, cfg, err
	}
	st, err := remotestorage.New(cfg, s.exec, s.httpClient)
	return st, cfg, err
}

// uploadAfterBackup uploads a completed local backup to the remote
// destination. Upload failure is returned; the caller decides whether it is
// fatal (retry task) or best-effort (regular backup run). On success with
// disk-saving mode enabled, the local file is removed and the backup becomes
// remote-only.
func (s *Service) uploadAfterBackup(ctx context.Context, b model.Backup, write func(string)) error {
	st, cfg, err := s.remoteStorage(ctx)
	if err != nil {
		if err == remotestorage.ErrNotConfigured {
			return nil
		}
		write("Remote storage unavailable: " + err.Error())
		return err
	}

	size := b.SizeBytes
	if size <= 0 {
		size = s.getFileSize(ctx, b.Path)
	}

	write("Uploading off-site copy…")
	objectName := b.Type + "/" + filepath.Base(b.Path)
	remotePath, upErr := st.Upload(ctx, b.Path, size, objectName)
	if upErr != nil {
		write("Off-site upload failed: " + upErr.Error())
		return upErr
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE backups SET remote_path = ? WHERE id = ?`, remotePath, b.ID); err != nil {
		return fmt.Errorf("record remote path: %w", err)
	}
	write("Off-site copy uploaded: " + remotePath)

	if cfg.DeleteLocalAfterUpload {
		if _, err := s.exec.RunSudo(ctx, "rm", "-f", b.Path); err != nil {
			write("Failed to remove the local copy: " + err.Error())
			return nil
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE backups SET remote_only = 1 WHERE id = ?`, b.ID); err != nil {
			return fmt.Errorf("mark remote-only: %w", err)
		}
		write("Local copy removed (disk-saving mode).")
	}
	return nil
}

// RetryUpload uploads an existing completed local backup that has no remote
// copy yet. Runs as a visible task; returns the task ID.
func (s *Service) RetryUpload(ctx context.Context, caller Caller, id string) (string, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !auth.CanManageResource(caller.Admin, caller.UserID, b.CreatedBy) {
		return "", model.ErrForbidden
	}
	if b.Status != "completed" {
		return "", model.NewValidationError("only completed backups can be uploaded")
	}
	if b.Kind == KindSafety {
		return "", model.NewValidationError("safety backups always stay local")
	}
	if b.RemotePath != "" {
		return "", model.NewValidationError("backup already has a remote copy")
	}
	if s.getFileSize(ctx, b.Path) <= 0 {
		return "", model.NewValidationError("local backup file no longer exists")
	}

	if s.tasks == nil {
		return "", fmt.Errorf("task runner not available")
	}
	taskID := s.tasks.RunFuncWithOptions(taskrunner.Options{
		Name:    "Upload backup to remote — " + b.Target,
		Module:  "backup",
		Timeout: 2 * time.Hour,
	}, func(taskCtx context.Context, write func(string)) error {
		if err := s.uploadAfterBackup(taskCtx, b, write); err != nil {
			return err
		}
		got, err := s.Get(taskCtx, b.ID)
		if err != nil {
			return err
		}
		if got.RemotePath == "" {
			return fmt.Errorf("upload did not complete")
		}
		return nil
	})
	return taskID, nil
}
