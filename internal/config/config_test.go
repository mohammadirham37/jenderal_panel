package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_FromFile(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 9443
database:
  path: "/tmp/test.db"
auth:
  session_ttl: "12h"
  rate_limit_login: 3
  rate_limit_api: 50
  argon2:
    memory: 65536
    iterations: 3
    parallelism: 2
metrics:
  collect_interval: "10s"
  store_interval: "120s"
  retention_days: 14
logging:
  level: "debug"
  file: "/tmp/test.log"
  max_size_mb: 50
  max_backups: 2
services:
  allowed:
    - nginx
    - mysql
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
	}
	if cfg.Server.Port != 9443 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 9443)
	}
	if cfg.Database.Path != "/tmp/test.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "/tmp/test.db")
	}
	if cfg.Auth.SessionTTL != 12*time.Hour {
		t.Errorf("Auth.SessionTTL = %v, want %v", cfg.Auth.SessionTTL, 12*time.Hour)
	}
	if cfg.Auth.RateLimitLogin != 3 {
		t.Errorf("Auth.RateLimitLogin = %d, want %d", cfg.Auth.RateLimitLogin, 3)
	}
	if cfg.Metrics.RetentionDays != 14 {
		t.Errorf("Metrics.RetentionDays = %d, want %d", cfg.Metrics.RetentionDays, 14)
	}
	if len(cfg.Services.Allowed) != 2 {
		t.Errorf("Services.Allowed len = %d, want 2", len(cfg.Services.Allowed))
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	content := `
server:
  host: "0.0.0.0"
  port: 8443
database:
  path: "./test.db"
auth:
  session_ttl: "24h"
  rate_limit_login: 5
  rate_limit_api: 100
  argon2:
    memory: 65536
    iterations: 3
    parallelism: 2
metrics:
  collect_interval: "5s"
  store_interval: "60s"
  retention_days: 7
logging:
  level: "info"
services:
  allowed:
    - nginx
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("JENDERAL_SERVER_PORT", "9999")
	t.Setenv("JENDERAL_LOGGING_LEVEL", "debug")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want %d (env override)", cfg.Server.Port, 9999)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q (env override)", cfg.Logging.Level, "debug")
	}
}

func TestLoad_Defaults(t *testing.T) {
	content := `
database:
  path: "./test.db"
auth:
  session_ttl: "24h"
  argon2:
    memory: 65536
    iterations: 3
    parallelism: 2
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("default Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8443 {
		t.Errorf("default Server.Port = %d, want %d", cfg.Server.Port, 8443)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("default Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	if err == nil {
		t.Error("Load() should fail for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: [invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail for invalid YAML")
	}
}
