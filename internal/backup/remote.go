package backup

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
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

// RemoteConfigRequest is the write-only config payload from the UI. Secret
// fields left empty preserve the stored values; they are never returned.
type RemoteConfigRequest struct {
	Type                   string `json:"type"`
	Endpoint               string `json:"endpoint"`
	Bucket                 string `json:"bucket"`
	Region                 string `json:"region"`
	AccessKey              string `json:"access_key"`
	SecretKey              string `json:"s3_secret_key"`
	Prefix                 string `json:"prefix"`
	UseTLS                 bool   `json:"use_tls"`
	GDClientID             string `json:"gdrive_client_id"`
	GDClientSecret         string `json:"gdrive_client_secret"`
	RcloneRemote           string `json:"rclone_remote"`
	RclonePath             string `json:"rclone_path"`
	DeleteLocalAfterUpload bool   `json:"delete_local_after_upload"`
}

// RemoteConfigView is the read model: no secrets, only *_set booleans.
type RemoteConfigView struct {
	Type                   string `json:"type"`
	Endpoint               string `json:"endpoint"`
	Bucket                 string `json:"bucket"`
	Region                 string `json:"region"`
	AccessKey              string `json:"access_key"`
	Prefix                 string `json:"prefix"`
	UseTLS                 bool   `json:"use_tls"`
	SecretSet              bool   `json:"s3_secret_set"`
	GDClientID             string `json:"gdrive_client_id"`
	GDClientSecretSet      bool   `json:"gdrive_client_secret_set"`
	GDConnected            bool   `json:"gdrive_connected"`
	GDFolderID             string `json:"gdrive_folder_id"`
	RcloneRemote           string `json:"rclone_remote"`
	RclonePath             string `json:"rclone_path"`
	DeleteLocalAfterUpload bool   `json:"delete_local_after_upload"`
}

// RemoteConfigView returns the masked configuration for the UI.
func (s *Service) RemoteConfigView(ctx context.Context) (RemoteConfigView, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return RemoteConfigView{}, err
	}
	return RemoteConfigView{
		Type: cfg.Type, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, Region: cfg.Region,
		AccessKey: cfg.AccessKey, Prefix: cfg.Prefix, UseTLS: cfg.UseTLS,
		SecretSet:  cfg.SecretKey != "",
		GDClientID: cfg.GDriveClientID, GDClientSecretSet: cfg.GDriveClientSecret != "",
		GDConnected: cfg.GDriveRefreshToken != "", GDFolderID: cfg.GDriveFolderID,
		RcloneRemote: cfg.RcloneRemote, RclonePath: cfg.RclonePath,
		DeleteLocalAfterUpload: cfg.DeleteLocalAfterUpload,
	}, nil
}

// SaveRemoteConfig validates and stores the UI payload.
func (s *Service) SaveRemoteConfig(ctx context.Context, req RemoteConfigRequest) error {
	if req.Type != "" && req.Type != "s3" && req.Type != "gdrive" && req.Type != "rclone" {
		return model.NewValidationError("type must be empty, s3, gdrive, or rclone")
	}
	cfg := remotestorage.Config{
		Type: req.Type, Endpoint: req.Endpoint, Bucket: req.Bucket, Region: req.Region,
		AccessKey: req.AccessKey, SecretKey: req.SecretKey, Prefix: req.Prefix, UseTLS: req.UseTLS,
		GDriveClientID: req.GDClientID, GDriveClientSecret: req.GDClientSecret,
		RcloneRemote: req.RcloneRemote, RclonePath: req.RclonePath,
		DeleteLocalAfterUpload: req.DeleteLocalAfterUpload,
	}
	if err := s.remoteStore.Save(ctx, cfg); err != nil {
		return fmt.Errorf("save remote config: %w", err)
	}
	return nil
}

// TestRemoteConnection verifies the currently configured backend.
func (s *Service) TestRemoteConnection(ctx context.Context) (string, error) {
	st, _, err := s.remoteStorage(ctx)
	if err != nil {
		return "", err
	}
	return st.Test(ctx)
}

// gdriveClient is the slice of the Drive backend the OAuth flow needs;
// *remotestorage.GDriveStorage satisfies it. A seam so tests can fake it.
type gdriveClient interface {
	AuthorizeURL() string
	ExchangeCode(ctx context.Context, code string) (string, error)
	EnsureFolder(ctx context.Context) (string, error)
	Test(ctx context.Context) (string, error)
}

var defaultNewGDrive = func(cfg remotestorage.Config, exec executor.CommandExecutor, hc *http.Client) gdriveClient {
	return remotestorage.NewGDrive(cfg, exec, hc)
}

var newGDriveForConfig = defaultNewGDrive

// GDriveAuthorizeURL returns the Google consent URL for the stored client id.
func (s *Service) GDriveAuthorizeURL(ctx context.Context) (string, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return "", err
	}
	if cfg.GDriveClientID == "" {
		return "", model.NewValidationError("save the Google Drive client ID first")
	}
	return newGDriveForConfig(cfg, s.exec, s.httpClient).AuthorizeURL(), nil
}

// GDriveExchange completes the copy-paste OAuth flow: swaps the code for a
// refresh token, ensures the target folder exists, stores both, and returns
// the connected account email.
func (s *Service) GDriveExchange(ctx context.Context, code string) (string, error) {
	cfg, err := s.remoteConfig(ctx)
	if err != nil {
		return "", err
	}
	if cfg.GDriveClientID == "" || cfg.GDriveClientSecret == "" {
		return "", model.NewValidationError("save the Google Drive client ID and secret first")
	}
	gd := newGDriveForConfig(cfg, s.exec, s.httpClient)
	refreshToken, err := gd.ExchangeCode(ctx, code)
	if err != nil {
		return "", err
	}
	cfg.GDriveRefreshToken = refreshToken
	cfg.Type = "gdrive"
	gd = newGDriveForConfig(cfg, s.exec, s.httpClient)
	folderID, err := gd.EnsureFolder(ctx)
	if err != nil {
		return "", fmt.Errorf("prepare drive folder: %w", err)
	}
	cfg.GDriveFolderID = folderID
	if err := s.remoteStore.Save(ctx, cfg); err != nil {
		return "", fmt.Errorf("store tokens: %w", err)
	}
	// Save() never writes the refresh token by design; persist it explicitly.
	if err := s.remoteStore.SaveRefreshToken(ctx, refreshToken); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	info, err := gd.Test(ctx)
	if err != nil {
		return "", fmt.Errorf("connection test after connect: %w", err)
	}
	return info, nil
}
