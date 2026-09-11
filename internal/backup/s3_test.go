package backup

import (
	"strings"
	"testing"
	"time"
)

func testConfig() RemoteConfig {
	return RemoteConfig{
		Type: "s3", Endpoint: "s3.wasabisys.com", Bucket: "panel-backups",
		Region: "ap-southeast-1", AccessKey: "AKIDEXAMPLE", SecretKey: "secret",
		Prefix: "vps-1/daily", UseTLS: true,
	}
}

func TestURIEncodePathKeepsSlashes(t *testing.T) {
	got := uriEncodePath("website/example.com/file 1.tar.gz")
	want := "website/example.com/file%201.tar.gz"
	if got != want {
		t.Fatalf("uriEncodePath = %q, want %q", got, want)
	}
}

func TestCanonicalURIEncodesObjectKey(t *testing.T) {
	cfg := testConfig()
	got := canonicalURI(cfg.s3ObjectURL("database/app db.sql"))
	want := "/panel-backups/vps-1/daily/database/app%20db.sql"
	if got != want {
		t.Fatalf("canonical URI = %q, want %q", got, want)
	}
}

func TestSignS3PUTIsDeterministicAndSigned(t *testing.T) {
	cfg := testConfig()
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)

	h1 := signS3PUT(cfg, cfg.host(), "/panel-backups/daily/db.sql", 1024, now)
	h2 := signS3PUT(cfg, cfg.host(), "/panel-backups/daily/db.sql", 1024, now)
	if h1.Get("Authorization") != h2.Get("Authorization") {
		t.Error("same inputs must produce the same signature")
	}

	auth := h1.Get("Authorization")
	for _, part := range []string{
		"AWS4-HMAC-SHA256",
		"Credential=AKIDEXAMPLE/20260911/ap-southeast-1/s3/aws4_request",
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date",
	} {
		if !strings.Contains(auth, part) {
			t.Errorf("authorization missing %q: %s", part, auth)
		}
	}
	if h1.Get("X-Amz-Content-Sha256") != "UNSIGNED-PAYLOAD" {
		t.Errorf("streamed uploads must announce UNSIGNED-PAYLOAD, got %q", h1.Get("X-Amz-Content-Sha256"))
	}
	if h1.Get("X-Amz-Date") != "20260911T100000Z" {
		t.Errorf("x-amz-date = %q", h1.Get("X-Amz-Date"))
	}
}

func TestRemoteConfigEnabled(t *testing.T) {
	cfg := testConfig()
	if !cfg.enabled() {
		t.Error("fully populated config should be enabled")
	}
	cfg.SecretKey = ""
	if cfg.enabled() {
		t.Error("config without a secret key must not be enabled")
	}
}
