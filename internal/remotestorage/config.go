package remotestorage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Settings keys. S3 and rclone keys are unchanged from the previous
// implementation so existing installations keep working.
const (
	keyType         = "backup_remote_type"
	keyEndpoint     = "backup_remote_s3_endpoint"
	keyBucket       = "backup_remote_s3_bucket"
	keyRegion       = "backup_remote_s3_region"
	keyAccessKey    = "backup_remote_s3_access_key"
	keySecretKey    = "backup_remote_s3_secret_key"
	keyPrefix       = "backup_remote_s3_prefix"
	keyRcloneRemote = "backup_remote_rclone_remote"
	keyRclonePath   = "backup_remote_rclone_path"

	keyGDClientID     = "backup_remote_gdrive_client_id"
	keyGDClientSecret = "backup_remote_gdrive_client_secret"
	keyGDRefreshToken = "backup_remote_gdrive_refresh_token"
	keyGDFolderID     = "backup_remote_gdrive_folder_id"

	keyDeleteLocal = "backup_remote_delete_local"
)

// Config is the remote storage configuration.
type Config struct {
	Type string `json:"type"` // "" | "s3" | "gdrive" | "rclone"

	// S3
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Prefix    string `json:"prefix"`
	UseTLS    bool   `json:"use_tls"`

	// Google Drive
	GDriveClientID     string `json:"gdrive_client_id"`
	GDriveClientSecret string `json:"gdrive_client_secret"`
	GDriveRefreshToken string `json:"gdrive_refresh_token"`
	GDriveFolderID     string `json:"gdrive_folder_id"`

	// rclone
	RcloneRemote string `json:"rclone_remote"`
	RclonePath   string `json:"rclone_path"`

	// Behavior: remove the local file after a successful upload.
	DeleteLocalAfterUpload bool `json:"delete_local_after_upload"`
}

// Enabled reports whether cfg is complete enough to upload with.
func (c Config) Enabled() bool {
	switch c.Type {
	case "s3":
		return c.Endpoint != "" && c.Bucket != "" && c.AccessKey != "" && c.SecretKey != ""
	case "gdrive":
		return c.GDriveClientID != "" && c.GDriveClientSecret != "" && c.GDriveRefreshToken != ""
	case "rclone":
		return c.RcloneRemote != ""
	default:
		return false
	}
}

// ConfigStore loads and saves Config in the settings KV table.
type ConfigStore struct {
	db *sql.DB
}

// NewConfigStore creates a ConfigStore.
func NewConfigStore(db *sql.DB) *ConfigStore {
	return &ConfigStore{db: db}
}

// Load reads the configuration; missing keys leave zero values.
func (s *ConfigStore) Load(ctx context.Context) (Config, error) {
	cfg := Config{Region: "us-east-1", UseTLS: true}
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value FROM settings WHERE key LIKE 'backup_remote%'`)
	if err != nil {
		return cfg, fmt.Errorf("read remote config: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return cfg, fmt.Errorf("scan remote config: %w", err)
		}
		switch key {
		case keyType:
			cfg.Type = value
		case keyEndpoint:
			cfg.Endpoint = value
			if strings.HasPrefix(value, "http://") {
				cfg.UseTLS = false
			}
		case keyBucket:
			cfg.Bucket = value
		case keyRegion:
			if value != "" {
				cfg.Region = value
			}
		case keyAccessKey:
			cfg.AccessKey = value
		case keySecretKey:
			cfg.SecretKey = value
		case keyPrefix:
			cfg.Prefix = strings.Trim(value, "/")
		case keyGDClientID:
			cfg.GDriveClientID = value
		case keyGDClientSecret:
			cfg.GDriveClientSecret = value
		case keyGDRefreshToken:
			cfg.GDriveRefreshToken = value
		case keyGDFolderID:
			cfg.GDriveFolderID = value
		case keyRcloneRemote:
			cfg.RcloneRemote = value
		case keyRclonePath:
			cfg.RclonePath = strings.Trim(value, "/")
		case keyDeleteLocal:
			cfg.DeleteLocalAfterUpload = value == "1"
		}
	}
	return cfg, rows.Err()
}

// Save upserts the configuration. Empty secret fields (S3 secret key, Drive
// client secret) preserve the stored values so the API never needs to echo
// secrets back to the browser. The Drive refresh token is not touched here —
// it is written only by the OAuth exchange (SaveRefreshToken).
func (s *ConfigStore) Save(ctx context.Context, cfg Config) error {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.SecretKey == "" || cfg.GDriveClientSecret == "" {
		old, err := s.Load(ctx)
		if err != nil {
			return fmt.Errorf("read previous config: %w", err)
		}
		if cfg.SecretKey == "" {
			cfg.SecretKey = old.SecretKey
		}
		if cfg.GDriveClientSecret == "" {
			cfg.GDriveClientSecret = old.GDriveClientSecret
		}
	}

	pairs := map[string]string{
		keyType:           cfg.Type,
		keyEndpoint:       cfg.Endpoint,
		keyBucket:         cfg.Bucket,
		keyRegion:         cfg.Region,
		keyAccessKey:      cfg.AccessKey,
		keySecretKey:      cfg.SecretKey,
		keyPrefix:         strings.Trim(cfg.Prefix, "/"),
		keyGDClientID:     cfg.GDriveClientID,
		keyGDClientSecret: cfg.GDriveClientSecret,
		keyGDFolderID:     cfg.GDriveFolderID,
		keyRcloneRemote:   cfg.RcloneRemote,
		keyRclonePath:     strings.Trim(cfg.RclonePath, "/"),
		keyDeleteLocal:    boolString(cfg.DeleteLocalAfterUpload),
	}
	for key, value := range pairs {
		if err := s.upsert(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

// SaveRefreshToken persists the Drive refresh token (written only by the
// OAuth exchange, never by the config form).
func (s *ConfigStore) SaveRefreshToken(ctx context.Context, token string) error {
	return s.upsert(ctx, keyGDRefreshToken, token)
}

func (s *ConfigStore) upsert(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, now,
	); err != nil {
		return fmt.Errorf("save %s: %w", key, err)
	}
	return nil
}

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
