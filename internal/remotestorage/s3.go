package remotestorage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// S3 path-style addressing with AWS SigV4. UNSIGNED-PAYLOAD allows streamed
// uploads; GET/DELETE sign the empty-body hash. Works with every
// S3-compatible provider (AWS, Wasabi, R2, MinIO).

const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type s3Storage struct {
	cfg  Config
	http *http.Client
	exec executor.CommandExecutor
}

func (c Config) s3Host() string {
	host := strings.TrimPrefix(strings.TrimPrefix(c.Endpoint, "https://"), "http://")
	return strings.TrimSuffix(host, "/")
}

// s3ObjectURL builds the path-style URL for a full object key (prefix already
// included — see s3ObjectKey).
func (c Config) s3ObjectURL(objectKey string) string {
	scheme := "https"
	if !c.UseTLS {
		scheme = "http"
	}
	return scheme + "://" + c.s3Host() + "/" + c.Bucket + "/" + strings.TrimPrefix(objectKey, "/")
}

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
	amzDateFormat   = "20060102T150405Z"
	scopeDateFormat = "20060102"
)

// signS3Request returns the headers for an SigV4-authenticated request.
// payloadHash is "UNSIGNED-PAYLOAD" for streamed uploads or the empty-body
// hash for everything else.
func signS3Request(cfg Config, method, host, canonicalURI, canonicalQuery, payloadHash string, now time.Time) http.Header {
	amzDate := now.UTC().Format(amzDateFormat)
	dateStamp := now.UTC().Format(scopeDateFormat)

	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
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
	return header
}

// s3ObjectKey composes the full object key (prefix + name).
func (c Config) s3ObjectKey(name string) string {
	prefix := strings.TrimPrefix(c.Prefix, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// Upload streams the local file (opened as root — backup files are
// root-owned) into a SigV4-signed PUT. Large archives never buffer in memory.
func (s *s3Storage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	objectKey := s.cfg.s3ObjectKey(name)
	rawURL := s.cfg.s3ObjectURL(objectKey)
	host := s.cfg.s3Host()
	canonical := canonicalURI(rawURL)

	pr, pw := io.Pipe()
	go func() {
		_, err := s.exec.RunSudoStream(ctx, pw, "cat", localPath)
		_ = pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, pr)
	if err != nil {
		return "", fmt.Errorf("build upload request: %w", err)
	}
	for key, values := range signS3Request(s.cfg, http.MethodPut, host, canonical, "", "UNSIGNED-PAYLOAD", time.Now().UTC()) {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	if size > 0 {
		req.ContentLength = size
		req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return rawURL, nil
}

func (s *s3Storage) do(ctx context.Context, method, rawURL, canonicalQuery string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for key, values := range signS3Request(s.cfg, method, s.cfg.s3Host(), canonicalURI(rawURL), canonicalQuery, emptyPayloadHash, time.Now().UTC()) {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return s.http.Do(req)
}

// Download streams the object into w.
func (s *s3Storage) Download(ctx context.Context, name string, w io.Writer) error {
	resp, err := s.do(ctx, http.MethodGet, s.cfg.s3ObjectURL(s.cfg.s3ObjectKey(name)), "")
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("download stream: %w", err)
	}
	return nil
}

// Delete removes the object.
func (s *s3Storage) Delete(ctx context.Context, name string) error {
	resp, err := s.do(ctx, http.MethodDelete, s.cfg.s3ObjectURL(s.cfg.s3ObjectKey(name)), "")
	if err != nil {
		return fmt.Errorf("delete request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Test lists one object to verify credentials and returns the bucket name.
func (s *s3Storage) Test(ctx context.Context) (string, error) {
	base := s.cfg.s3ObjectURL("")
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	rawURL := base + "?list-type=2&max-keys=1"
	resp, err := s.do(ctx, http.MethodGet, rawURL, "list-type=2&max-keys=1")
	if err != nil {
		return "", fmt.Errorf("test request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("remote storage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return "bucket " + s.cfg.Bucket + " (list ok)", nil
}
