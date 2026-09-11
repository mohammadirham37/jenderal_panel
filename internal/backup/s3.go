package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Settings keys for the off-site copy. Type is "" (off) or "s3"; the S3
// client speaks the AWS SigV4 dialect, which every S3-compatible provider
// (Wasabi, R2, MinIO, AWS) accepts with path-style addressing.
const (
	settingRemoteType      = "backup_remote_type"
	settingRemoteEndpoint  = "backup_remote_s3_endpoint"
	settingRemoteBucket    = "backup_remote_s3_bucket"
	settingRemoteRegion    = "backup_remote_s3_region"
	settingRemoteAccessKey = "backup_remote_s3_access_key"
	settingRemoteSecretKey = "backup_remote_s3_secret_key"
	settingRemotePrefix    = "backup_remote_s3_prefix"
	settingRcloneRemote    = "backup_remote_rclone_remote"
	settingRclonePath      = "backup_remote_rclone_path"
)

// RemoteConfig is the off-site copy configuration read from settings.
type RemoteConfig struct {
	Type         string `json:"type"` // "" | "s3" | "rclone"
	Endpoint     string `json:"endpoint"`
	Bucket       string `json:"bucket"`
	Region       string `json:"region"`
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	Prefix       string `json:"prefix"`
	UseTLS       bool   `json:"use_tls"`
	RcloneRemote string `json:"rclone_remote"`
	RclonePath   string `json:"rclone_path"`
}

func (c RemoteConfig) enabled() bool {
	switch c.Type {
	case "s3":
		return c.Endpoint != "" && c.Bucket != "" && c.AccessKey != "" && c.SecretKey != ""
	case "rclone":
		return c.RcloneRemote != ""
	default:
		return false
	}
}

func (c RemoteConfig) host() string {
	host := strings.TrimPrefix(strings.TrimPrefix(c.Endpoint, "https://"), "http://")
	return strings.TrimSuffix(host, "/")
}

// GetRemoteConfig reads the off-site copy configuration from settings.
func (s *Service) GetRemoteConfig(ctx context.Context) (RemoteConfig, error) {
	cfg := RemoteConfig{Type: "", Region: "us-east-1", UseTLS: true}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings WHERE key LIKE 'backup_remote%'`)
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
		case settingRemoteType:
			cfg.Type = value
		case settingRemoteBucket:
			cfg.Bucket = value
		case settingRemoteRegion:
			if value != "" {
				cfg.Region = value
			}
		case settingRemoteAccessKey:
			cfg.AccessKey = value
		case settingRemoteSecretKey:
			cfg.SecretKey = value
		case settingRemotePrefix:
			cfg.Prefix = strings.Trim(value, "/")
		case settingRcloneRemote:
			cfg.RcloneRemote = value
		case settingRclonePath:
			cfg.RclonePath = strings.Trim(value, "/")
		case settingRemoteEndpoint:
			cfg.Endpoint = value
			if strings.HasPrefix(value, "http://") {
				cfg.UseTLS = false
			}
		}
	}
	return cfg, rows.Err()
}

// SaveRemoteConfig upserts the off-site copy configuration.
func (s *Service) SaveRemoteConfig(ctx context.Context, cfg RemoteConfig) error {
	if cfg.Type != "" && cfg.Type != "s3" && cfg.Type != "rclone" {
		return fmt.Errorf("unsupported remote type: %s", cfg.Type)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	pairs := map[string]string{
		settingRemoteType:      cfg.Type,
		settingRemoteEndpoint:  cfg.Endpoint,
		settingRemoteBucket:    cfg.Bucket,
		settingRemoteRegion:    cfg.Region,
		settingRemoteAccessKey: cfg.AccessKey,
		settingRemoteSecretKey: cfg.SecretKey,
		settingRemotePrefix:    cfg.Prefix,
		settingRcloneRemote:    cfg.RcloneRemote,
		settingRclonePath:      cfg.RclonePath,
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range pairs {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			key, value, now,
		); err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
	}
	return nil
}

// ─── SigV4 signing (S3 flavor) ────────────────────────────────────

// uriEncodePath percent-encodes every byte of each path segment except
// unreserved characters and "/", as required by the SigV4 canonical URI for
// path-style S3 addressing.
func uriEncodePath(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '/' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// s3ObjectURL builds the path-style object URL for the request.
func (c RemoteConfig) s3ObjectURL(objectKey string) string {
	scheme := "https"
	if !c.UseTLS {
		scheme = "http"
	}
	return scheme + "://" + c.host() + "/" + c.Bucket + "/" + strings.TrimPrefix(c.Prefix, "/") + "/" + objectKey
}

// canonicalURI converts the object URL into the SigV4 canonical URI.
func canonicalURI(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "/"
	}
	return uriEncodePath(u.Path)
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

const (
	amzDateFormat  = "20060102T150405Z"
	scopeDateFormat = "20060102"
)

// signS3PUT returns the headers required for an SigV4-authenticated PUT of a
// body announced as UNSIGNED-PAYLOAD (allowing streamed uploads).
func signS3PUT(cfg RemoteConfig, host, canonicalURI string, payloadLen int64, now time.Time) http.Header {
	amzDate := now.UTC().Format(amzDateFormat)
	dateStamp := now.UTC().Format(scopeDateFormat)

	payloadHash := "UNSIGNED-PAYLOAD"
	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := strings.Join([]string{
		"PUT",
		canonicalURI,
		"", // no query string
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	key := hmacSHA256([]byte("AWS4"+cfg.SecretKey), []byte(dateStamp))
	key = hmacSHA256(key, []byte(cfg.Region))
	key = hmacSHA256(key, []byte("s3"))
	key = hmacSHA256(key, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(key, []byte(stringToSign)))

	header := http.Header{}
	header.Set("Host", host)
	header.Set("X-Amz-Date", amzDate)
	header.Set("X-Amz-Content-Sha256", payloadHash)
	header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cfg.AccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
	if payloadLen > 0 {
		header.Set("Content-Length", strconv.FormatInt(payloadLen, 10))
	}
	return header
}

// UploadToRemote streams the backup archive to the configured off-site
// destination and returns the remote path. The body is announced as
// UNSIGNED-PAYLOAD and piped from disk, so even large archives never buffer
// in panel memory.
func (s *Service) UploadToRemote(ctx context.Context, b model.Backup, cfg RemoteConfig) (string, error) {
	if cfg.Type == "rclone" {
		return s.uploadRclone(ctx, b, cfg)
	}
	if !cfg.enabled() {
		return "", model.NewValidationError("remote storage is not configured")
	}

	size := b.SizeBytes
	if size <= 0 {
		size = s.getFileSize(ctx, b.Path)
	}

	pr, pw := io.Pipe()
	go func() {
		_, err := s.exec.RunSudoStream(ctx, pw, "cat", b.Path)
		_ = pw.CloseWithError(err)
	}()

	objectKey := b.Type + "/" + filepath.Base(b.Path)
	rawURL := cfg.s3ObjectURL(objectKey)
	host := cfg.host()
	canonical := canonicalURI(rawURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, pr)
	if err != nil {
		return "", fmt.Errorf("build upload request: %w", err)
	}
	for key, values := range signS3PUT(cfg, host, canonical, size, time.Now().UTC()) {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	req.ContentLength = size

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	remotePath := cfg.host() + "/" + cfg.Bucket + "/" + strings.TrimPrefix(cfg.Prefix, "/") + "/" + objectKey
	return remotePath, nil
}

// uploadRclone copies the backup to an rclone remote (configured on the
// server, e.g. "gdrive:backups" or "s3:panel") with rclone copyto.
func (s *Service) uploadRclone(ctx context.Context, b model.Backup, cfg RemoteConfig) (string, error) {
	if cfg.RcloneRemote == "" {
		return "", model.NewValidationError("rclone remote name is not configured")
	}
	objectPath := strings.Trim(cfg.RclonePath, "/") + "/" + filepath.Base(b.Path)
	if strings.Trim(cfg.RclonePath, "/") == "" {
		objectPath = filepath.Base(b.Path)
	}
	dest := cfg.RcloneRemote + ":" + objectPath

	result, err := s.exec.RunSudo(ctx, "rclone", "copyto", b.Path, dest)
	if err != nil {
		return "", fmt.Errorf("rclone copyto: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("rclone copyto failed (exit %d): %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return dest, nil
}
