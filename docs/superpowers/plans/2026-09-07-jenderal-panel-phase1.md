# Jenderal Panel Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the core foundation of Jenderal Panel — a VPS control panel with auth, RBAC, server monitoring, service management, and a SvelteKit dashboard.

**Architecture:** Monolith layered Go application. Chi HTTP router, SQLite (WAL) internal database, server-side sessions with argon2id passwords, structured slog logging, SvelteKit frontend embedded via `go:embed`. Privileged OS operations via sudo whitelist.

**Tech Stack:** Go 1.23+, Chi v5, SQLite (mattn/go-sqlite3), SvelteKit, TypeScript, Tailwind CSS v4, uPlot, gorilla/websocket

---

## File Structure

```
jenderal_panel/
├── cmd/jenderal/main.go                    # CLI entry: serve, migrate, admin, version
├── internal/
│   ├── config/config.go                    # YAML+env config loader + validation
│   ├── config/config_test.go
│   ├── logging/logger.go                   # slog JSON setup + context helpers
│   ├── database/sqlite.go                  # Open, Close, WAL mode
│   ├── database/migrations.go             # Embedded SQL migration runner
│   ├── database/migrations_test.go
│   ├── model/models.go                     # All domain structs (User, Role, etc.)
│   ├── model/errors.go                     # DomainError type
│   ├── executor/command.go                 # CommandExecutor interface + impl
│   ├── executor/command_test.go
│   ├── service/systemd.go                  # ServiceManager interface + impl
│   ├── service/systemd_test.go
│   ├── auth/service.go                     # Password hash, session CRUD
│   ├── auth/service_test.go
│   ├── auth/rbac.go                        # RBAC permission checker
│   ├── auth/rbac_test.go
│   ├── auth/handler.go                     # Login, logout, me endpoints
│   ├── auth/middleware.go                  # Session auth + CSRF middleware
│   ├── audit/service.go                    # Audit log writer + query
│   ├── audit/service_test.go
│   ├── system/info.go                      # Server info (hostname, OS, IP)
│   ├── system/info_test.go
│   ├── system/metrics.go                   # Metrics collector, ring buffer
│   ├── system/metrics_test.go
│   ├── system/handler.go                   # Server info + dashboard handlers
│   ├── system/ws.go                        # WebSocket metrics handler
│   ├── service/handler.go                  # Service start/stop/restart handlers
│   ├── user/handler.go                     # User CRUD handlers
│   ├── audit/handler.go                    # Audit log list handler
│   ├── settings/handler.go                 # Settings get/put handler
│   ├── settings/service.go                 # Settings CRUD
│   ├── api/router.go                       # Chi router + all route mounting
│   ├── api/middleware.go                   # RequestID, logger, recovery, rate limit
│   ├── api/middleware_test.go
│   ├── api/response.go                     # JSON response helpers
│   └── server/server.go                    # HTTP/TLS server, graceful shutdown
├── migrations/
│   ├── 001_users.sql
│   ├── 002_roles_permissions.sql
│   ├── 003_sessions.sql
│   ├── 004_audit_logs.sql
│   ├── 005_settings.sql
│   └── 006_server_metrics.sql
├── web/                                     # SvelteKit project (detail in Task 27+)
├── configs/jenderal.yaml.example
├── scripts/install.sh
├── systemd/jenderal.service
├── Makefile
├── go.mod
└── go.sum
```

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `configs/jenderal.yaml.example`
- Create: `.gitignore`

- [ ] **Step 1: Initialize git and go module**

```bash
cd /Users/irhamakbar/Documents/go_project/jenderal_panel
git init
git remote add origin git@github.com:mohammadirham37/jenderal_panel.git
go mod init github.com/mohammadirham37/jenderal_panel
```

- [ ] **Step 2: Create .gitignore**

```gitignore
# Go
/jenderal
*.exe
*.test
*.out
vendor/

# SvelteKit
web/node_modules/
web/.svelte-kit/
web/build/

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# Config
jenderal.yaml
*.db
*.db-wal
*.db-shm

# TLS
*.pem
*.key
*.crt
```

- [ ] **Step 3: Create Makefile**

```makefile
.PHONY: dev build test lint clean frontend

BINARY=jenderal
VERSION?=0.1.0
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

build: frontend
	go build $(LDFLAGS) -o $(BINARY) ./cmd/jenderal

dev:
	go run ./cmd/jenderal serve --config configs/jenderal.yaml.example

test:
	go test ./... -v -race

lint:
	go vet ./...
	golangci-lint run

frontend:
	cd web && npm install && npm run build

clean:
	rm -f $(BINARY)
	rm -rf web/build web/node_modules web/.svelte-kit

release: test build
	@echo "Built $(BINARY) v$(VERSION)"
```

- [ ] **Step 4: Create example config**

```yaml
server:
  host: "0.0.0.0"
  port: 8443
  tls:
    enabled: false
    cert: ""
    key: ""

database:
  path: "./jenderal.db"

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
  level: "debug"
  file: ""
  max_size_mb: 100
  max_backups: 3

services:
  allowed:
    - nginx
    - "php*-fpm"
    - mysql
    - postgresql
    - redis-server
```

- [ ] **Step 5: Create directory structure**

```bash
mkdir -p cmd/jenderal
mkdir -p internal/{config,logging,database,model,executor,service,auth,audit,system,api,server,user,settings}
mkdir -p migrations
mkdir -p web
mkdir -p scripts
mkdir -p systemd
```

- [ ] **Step 6: Create placeholder main.go so build works**

`cmd/jenderal/main.go`:
```go
package main

import "fmt"

var version = "dev"

func main() {
	fmt.Printf("Jenderal Panel %s\n", version)
}
```

- [ ] **Step 7: Verify build**

Run: `go build ./cmd/jenderal`
Expected: builds successfully, produces `jenderal` binary

- [ ] **Step 8: Commit**

```bash
git add .
git commit -m "feat: initialize project scaffolding

Set up go.mod, Makefile, directory structure, gitignore,
example config, and placeholder main.go."
```

---

### Task 2: Domain Models & Errors

**Files:**
- Create: `internal/model/models.go`
- Create: `internal/model/errors.go`

- [ ] **Step 1: Create domain models**

`internal/model/models.go`:
```go
package model

import "time"

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Permission struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Module string `json:"module"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditEntry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	Module    string    `json:"module"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	IPAddress string    `json:"ip_address"`
	CreatedAt time.Time `json:"created_at"`
}

type Setting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ServerMetrics struct {
	CPU       float64       `json:"cpu"`
	RAMUsed   uint64        `json:"ram_used"`
	RAMTotal  uint64        `json:"ram_total"`
	SwapUsed  uint64        `json:"swap_used"`
	SwapTotal uint64        `json:"swap_total"`
	DiskUsed  uint64        `json:"disk_used"`
	DiskTotal uint64        `json:"disk_total"`
	Load1     float64       `json:"load_1"`
	Load5     float64       `json:"load_5"`
	Load15    float64       `json:"load_15"`
	NetRx     uint64        `json:"net_rx"`
	NetTx     uint64        `json:"net_tx"`
	Uptime    time.Duration `json:"uptime"`
	Timestamp time.Time     `json:"timestamp"`
}

type ServerInfo struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	CPU      string `json:"cpu"`
	RAM      string `json:"ram"`
	Disk     string `json:"disk"`
	Uptime   string `json:"uptime"`
	Timezone string `json:"timezone"`
}

type ServiceStatus struct {
	Name    string        `json:"name"`
	Active  bool          `json:"active"`
	Running bool          `json:"running"`
	Enabled bool          `json:"enabled"`
	Uptime  time.Duration `json:"uptime"`
	PID     int           `json:"pid"`
}

type UserWithRoles struct {
	User        User         `json:"user"`
	Roles       []Role       `json:"roles"`
	Permissions []Permission `json:"permissions"`
}
```

- [ ] **Step 2: Create domain errors**

`internal/model/errors.go`:
```go
package model

import "fmt"

type DomainError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *DomainError) Unwrap() error {
	return e.Err
}

var (
	ErrNotFound           = &DomainError{Code: "NOT_FOUND", Message: "resource not found"}
	ErrInvalidCredentials = &DomainError{Code: "INVALID_CREDENTIALS", Message: "invalid username or password"}
	ErrUserExists         = &DomainError{Code: "USER_EXISTS", Message: "user already exists"}
	ErrSessionExpired     = &DomainError{Code: "SESSION_EXPIRED", Message: "session has expired"}
	ErrUnauthorized       = &DomainError{Code: "UNAUTHORIZED", Message: "authentication required"}
	ErrForbidden          = &DomainError{Code: "FORBIDDEN", Message: "insufficient permissions"}
	ErrValidation         = &DomainError{Code: "VALIDATION_ERROR", Message: "validation failed"}
	ErrRateLimited        = &DomainError{Code: "RATE_LIMITED", Message: "too many requests"}
	ErrServiceNotAllowed  = &DomainError{Code: "SERVICE_NOT_ALLOWED", Message: "service not in allowed list"}
	ErrUserInactive       = &DomainError{Code: "USER_INACTIVE", Message: "user account is inactive"}
)

func NewDomainError(code, message string, err error) *DomainError {
	return &DomainError{Code: code, Message: message, Err: err}
}

func NewValidationError(message string) *DomainError {
	return &DomainError{Code: "VALIDATION_ERROR", Message: message}
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/model/
git commit -m "feat: add domain models and error types

Define User, Role, Permission, Session, AuditEntry, Setting,
ServerMetrics, ServerInfo, ServiceStatus structs and
domain error types for API error translation."
```

---

### Task 3: Config Module

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Install YAML dependency**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Write config test**

`internal/config/config_test.go`:
```go
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
	if err := os.WriteFile(path, []byte(":::invalid:::"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail for invalid YAML")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `Load` not defined

- [ ] **Step 4: Implement config module**

`internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Metrics  MetricsConfig  `yaml:"metrics"`
	Logging  LoggingConfig  `yaml:"logging"`
	Services ServicesConfig `yaml:"services"`
}

type ServerConfig struct {
	Host string    `yaml:"host"`
	Port int       `yaml:"port"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type AuthConfig struct {
	SessionTTL     time.Duration `yaml:"-"`
	SessionTTLRaw  string        `yaml:"session_ttl"`
	RateLimitLogin int           `yaml:"rate_limit_login"`
	RateLimitAPI   int           `yaml:"rate_limit_api"`
	Argon2         Argon2Config  `yaml:"argon2"`
}

type Argon2Config struct {
	Memory      uint32 `yaml:"memory"`
	Iterations  uint32 `yaml:"iterations"`
	Parallelism uint8  `yaml:"parallelism"`
}

type MetricsConfig struct {
	CollectInterval    time.Duration `yaml:"-"`
	CollectIntervalRaw string        `yaml:"collect_interval"`
	StoreInterval      time.Duration `yaml:"-"`
	StoreIntervalRaw   string        `yaml:"store_interval"`
	RetentionDays      int           `yaml:"retention_days"`
}

type LoggingConfig struct {
	Level      string `yaml:"level"`
	File       string `yaml:"file"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
}

type ServicesConfig struct {
	Allowed []string `yaml:"allowed"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	if err := parseDurations(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8443
	}
	if cfg.Auth.SessionTTLRaw == "" {
		cfg.Auth.SessionTTLRaw = "24h"
	}
	if cfg.Auth.RateLimitLogin == 0 {
		cfg.Auth.RateLimitLogin = 5
	}
	if cfg.Auth.RateLimitAPI == 0 {
		cfg.Auth.RateLimitAPI = 100
	}
	if cfg.Auth.Argon2.Memory == 0 {
		cfg.Auth.Argon2.Memory = 65536
	}
	if cfg.Auth.Argon2.Iterations == 0 {
		cfg.Auth.Argon2.Iterations = 3
	}
	if cfg.Auth.Argon2.Parallelism == 0 {
		cfg.Auth.Argon2.Parallelism = 2
	}
	if cfg.Metrics.CollectIntervalRaw == "" {
		cfg.Metrics.CollectIntervalRaw = "5s"
	}
	if cfg.Metrics.StoreIntervalRaw == "" {
		cfg.Metrics.StoreIntervalRaw = "60s"
	}
	if cfg.Metrics.RetentionDays == 0 {
		cfg.Metrics.RetentionDays = 7
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.MaxSizeMB == 0 {
		cfg.Logging.MaxSizeMB = 100
	}
	if cfg.Logging.MaxBackups == 0 {
		cfg.Logging.MaxBackups = 3
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("JENDERAL_SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("JENDERAL_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("JENDERAL_DATABASE_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("JENDERAL_AUTH_SESSION_TTL"); v != "" {
		cfg.Auth.SessionTTLRaw = v
	}
	if v := os.Getenv("JENDERAL_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = strings.ToLower(v)
	}
	if v := os.Getenv("JENDERAL_LOGGING_FILE"); v != "" {
		cfg.Logging.File = v
	}
}

func parseDurations(cfg *Config) error {
	var err error
	cfg.Auth.SessionTTL, err = time.ParseDuration(cfg.Auth.SessionTTLRaw)
	if err != nil {
		return fmt.Errorf("parse auth.session_ttl %q: %w", cfg.Auth.SessionTTLRaw, err)
	}
	cfg.Metrics.CollectInterval, err = time.ParseDuration(cfg.Metrics.CollectIntervalRaw)
	if err != nil {
		return fmt.Errorf("parse metrics.collect_interval %q: %w", cfg.Metrics.CollectIntervalRaw, err)
	}
	cfg.Metrics.StoreInterval, err = time.ParseDuration(cfg.Metrics.StoreIntervalRaw)
	if err != nil {
		return fmt.Errorf("parse metrics.store_interval %q: %w", cfg.Metrics.StoreIntervalRaw, err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/config/ -v -race`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: add config module with YAML loading and env overrides

Load from YAML file, apply sensible defaults, override via
JENDERAL_* environment variables, parse duration strings."
```

---

### Task 4: Logging Module

**Files:**
- Create: `internal/logging/logger.go`

- [ ] **Step 1: Implement logger**

`internal/logging/logger.go`:
```go
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

type contextKey string

const (
	RequestIDKey contextKey = "request_id"
	UserIDKey    contextKey = "user_id"
)

func New(cfg config.LoggingConfig) *slog.Logger {
	var writer io.Writer = os.Stdout

	if cfg.File != "" {
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("failed to open log file, falling back to stdout", "file", cfg.File, "error", err)
		} else {
			writer = io.MultiWriter(os.Stdout, f)
		}
	}

	level := parseLevel(cfg.Level)

	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(RequestIDKey).(string)
	return v
}

func UserID(ctx context.Context) string {
	v, _ := ctx.Value(UserIDKey).(string)
	return v
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/logging/
git commit -m "feat: add structured JSON logging with slog

Context helpers for request_id and user_id propagation.
Multi-writer support for stdout + file logging."
```

---

### Task 5: Database & Migrations

**Files:**
- Create: `internal/database/sqlite.go`
- Create: `internal/database/migrations.go`
- Create: `internal/database/migrations_test.go`
- Create: `migrations/001_users.sql` through `006_server_metrics.sql`

- [ ] **Step 1: Install SQLite driver**

```bash
go get github.com/mattn/go-sqlite3
```

- [ ] **Step 2: Create migration SQL files**

`migrations/001_users.sql`:
```sql
CREATE TABLE IF NOT EXISTS users (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
```

`migrations/002_roles_permissions.sql`:
```sql
CREATE TABLE IF NOT EXISTS roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS permissions (
    id       TEXT PRIMARY KEY,
    name     TEXT NOT NULL UNIQUE,
    module   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id       TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);
```

`migrations/003_sessions.sql`:
```sql
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip_address TEXT,
    user_agent TEXT,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
```

`migrations/004_audit_logs.sql`:
```sql
CREATE TABLE IF NOT EXISTS audit_logs (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id),
    action     TEXT NOT NULL,
    module     TEXT NOT NULL,
    target     TEXT,
    detail     TEXT,
    ip_address TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_module ON audit_logs(module);
```

`migrations/005_settings.sql`:
```sql
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

`migrations/006_server_metrics.sql`:
```sql
CREATE TABLE IF NOT EXISTS server_metrics (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cpu        REAL NOT NULL,
    ram_used   INTEGER NOT NULL,
    ram_total  INTEGER NOT NULL,
    swap_used  INTEGER NOT NULL,
    swap_total INTEGER NOT NULL,
    disk_used  INTEGER NOT NULL,
    disk_total INTEGER NOT NULL,
    load_1     REAL NOT NULL,
    load_5     REAL NOT NULL,
    load_15    REAL NOT NULL,
    net_rx     INTEGER NOT NULL,
    net_tx     INTEGER NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_server_metrics_created_at ON server_metrics(created_at);
```

- [ ] **Step 3: Write migration test**

`internal/database/migrations_test.go`:
```go
package database

import (
	"database/sql"
	"testing"
)

func TestMigrate(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}

	tables := []string{
		"users", "roles", "permissions", "user_roles",
		"role_permissions", "sessions", "audit_logs",
		"settings", "server_metrics",
	}

	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate() error: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate() error: %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/database/ -v`
Expected: FAIL — `Migrate` not defined

- [ ] **Step 5: Implement database module**

`internal/database/sqlite.go`:
```go
package database

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

func Open(cfg config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", cfg.Path+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
```

`internal/database/migrations.go`:
```go
package database

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed ../../migrations/*.sql
var migrationsFS embed.FS

func Migrate(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		content, err := migrationsFS.ReadFile("migrations/" + f)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", f, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("execute migration %q: %w", f, err)
		}
	}

	return nil
}
```

**Note:** The `//go:embed` path `../../migrations/*.sql` works because Go resolves embed paths relative to the source file. Since `migrations.go` is in `internal/database/`, the path goes up two levels to reach the project root `migrations/` directory.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/database/ -v -race`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/database/ migrations/ go.mod go.sum
git commit -m "feat: add SQLite database with embedded SQL migrations

WAL mode, foreign keys enabled, 6 migration files covering
users, roles, permissions, sessions, audit_logs, settings,
and server_metrics tables. Migrations are idempotent."
```

---

### Task 6: Command Executor

**Files:**
- Create: `internal/executor/command.go`
- Create: `internal/executor/command_test.go`

- [ ] **Step 1: Write executor test**

`internal/executor/command_test.go`:
```go
package executor

import (
	"context"
	"testing"
	"time"
)

func TestExecutor_Run_Echo(t *testing.T) {
	exec := New(30 * time.Second)
	result, err := exec.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if got := result.Stdout; got != "hello\n" {
		t.Errorf("Stdout = %q, want %q", got, "hello\n")
	}
}

func TestExecutor_Run_Timeout(t *testing.T) {
	exec := New(100 * time.Millisecond)
	_, err := exec.Run(context.Background(), "sleep", "10")
	if err == nil {
		t.Error("Run() should fail on timeout")
	}
}

func TestExecutor_Run_ContextCancel(t *testing.T) {
	exec := New(30 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := exec.Run(ctx, "sleep", "10")
	if err == nil {
		t.Error("Run() should fail on cancelled context")
	}
}

func TestExecutor_Run_NonZeroExit(t *testing.T) {
	exec := New(30 * time.Second)
	result, err := exec.Run(context.Background(), "sh", "-c", "exit 42")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", result.ExitCode)
	}
}

func TestMockExecutor(t *testing.T) {
	mock := &MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (Result, error) {
			return Result{Stdout: "mocked", ExitCode: 0}, nil
		},
	}

	result, err := mock.Run(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Stdout != "mocked" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "mocked")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -v`
Expected: FAIL

- [ ] **Step 3: Implement executor**

`internal/executor/command.go`:
```go
package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

type CommandExecutor interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
	RunSudo(ctx context.Context, name string, args ...string) (Result, error)
}

type Executor struct {
	defaultTimeout time.Duration
}

func New(defaultTimeout time.Duration) *Executor {
	return &Executor{defaultTimeout: defaultTimeout}
}

func (e *Executor) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return e.run(ctx, name, args...)
}

func (e *Executor) RunSudo(ctx context.Context, name string, args ...string) (Result, error) {
	sudoArgs := append([]string{name}, args...)
	return e.run(ctx, "/usr/bin/sudo", sudoArgs...)
}

func (e *Executor) run(ctx context.Context, name string, args ...string) (Result, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.defaultTimeout)
		defer cancel()
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("execute %q: %w", name, err)
	}

	return result, nil
}

// MockExecutor for testing
type MockExecutor struct {
	RunFunc     func(ctx context.Context, name string, args ...string) (Result, error)
	RunSudoFunc func(ctx context.Context, name string, args ...string) (Result, error)
}

func (m *MockExecutor) Run(ctx context.Context, name string, args ...string) (Result, error) {
	if m.RunFunc != nil {
		return m.RunFunc(ctx, name, args...)
	}
	return Result{}, nil
}

func (m *MockExecutor) RunSudo(ctx context.Context, name string, args ...string) (Result, error) {
	if m.RunSudoFunc != nil {
		return m.RunSudoFunc(ctx, name, args...)
	}
	return Result{}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/executor/ -v -race`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/executor/
git commit -m "feat: add command executor with timeout and sudo support

Separated argument passing (no shell injection), context
cancellation, configurable timeout, MockExecutor for tests."
```

---

### Task 7: Service Manager

**Files:**
- Create: `internal/service/systemd.go`
- Create: `internal/service/systemd_test.go`

- [ ] **Step 1: Write service manager test**

`internal/service/systemd_test.go`:
```go
package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestSystemdManager_Status_Parse(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (executor.Result, error) {
			return executor.Result{
				Stdout: `ActiveState=active
SubState=running
MainPID=1234
UnitFileState=enabled
ActiveEnterTimestamp=Mon 2026-09-07 10:00:00 UTC`,
				ExitCode: 0,
			}, nil
		},
	}

	mgr := NewSystemd(mock, []string{"nginx"})
	status, err := mgr.Status(context.Background(), "nginx")
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if !status.Active {
		t.Error("Active should be true")
	}
	if !status.Running {
		t.Error("Running should be true")
	}
	if status.PID != 1234 {
		t.Errorf("PID = %d, want 1234", status.PID)
	}
	if !status.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestSystemdManager_NotAllowed(t *testing.T) {
	mock := &executor.MockExecutor{}
	mgr := NewSystemd(mock, []string{"nginx"})

	_, err := mgr.Status(context.Background(), "malicious-service")
	if err == nil {
		t.Error("Status() should reject service not in allowed list")
	}
}

func TestSystemdManager_Restart(t *testing.T) {
	var calledArgs []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (executor.Result, error) {
			calledArgs = args
			return executor.Result{ExitCode: 0}, nil
		},
	}

	mgr := NewSystemd(mock, []string{"nginx"})
	err := mgr.Restart(context.Background(), "nginx")
	if err != nil {
		t.Fatalf("Restart() error: %v", err)
	}

	if len(calledArgs) != 2 || calledArgs[0] != "restart" || calledArgs[1] != "nginx" {
		t.Errorf("called with %v, want [restart nginx]", calledArgs)
	}
}

func TestSystemdManager_GlobMatch(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (executor.Result, error) {
			return executor.Result{ExitCode: 0}, nil
		},
	}

	mgr := NewSystemd(mock, []string{"nginx", "php*-fpm"})

	err := mgr.Restart(context.Background(), "php8.4-fpm")
	if err != nil {
		t.Errorf("Restart(php8.4-fpm) should be allowed via glob: %v", err)
	}

	err = mgr.Restart(context.Background(), "redis-server")
	if err == nil {
		t.Error("Restart(redis-server) should be rejected")
	}
}

func TestSystemdManager_FailedCommand(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (executor.Result, error) {
			return executor.Result{ExitCode: 1, Stderr: "Failed to restart"}, fmt.Errorf("exit 1")
		},
	}

	mgr := NewSystemd(mock, []string{"nginx"})
	err := mgr.Restart(context.Background(), "nginx")
	if err == nil {
		t.Error("Restart() should return error on failed command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -v`
Expected: FAIL

- [ ] **Step 3: Implement service manager**

`internal/service/systemd.go`:
```go
package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type ServiceManager interface {
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Reload(ctx context.Context, name string) error
	Status(ctx context.Context, name string) (model.ServiceStatus, error)
	List(ctx context.Context) ([]model.ServiceStatus, error)
}

type Systemd struct {
	executor executor.CommandExecutor
	allowed  []string
}

func NewSystemd(exec executor.CommandExecutor, allowed []string) *Systemd {
	return &Systemd{executor: exec, allowed: allowed}
}

func (s *Systemd) isAllowed(name string) bool {
	for _, pattern := range s.allowed {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

func (s *Systemd) checkAllowed(name string) error {
	if !s.isAllowed(name) {
		return model.ErrServiceNotAllowed
	}
	return nil
}

func (s *Systemd) Start(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	_, err := s.executor.RunSudo(ctx, "systemctl", "start", name)
	if err != nil {
		return fmt.Errorf("start service %q: %w", name, err)
	}
	return nil
}

func (s *Systemd) Stop(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	_, err := s.executor.RunSudo(ctx, "systemctl", "stop", name)
	if err != nil {
		return fmt.Errorf("stop service %q: %w", name, err)
	}
	return nil
}

func (s *Systemd) Restart(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	_, err := s.executor.RunSudo(ctx, "systemctl", "restart", name)
	if err != nil {
		return fmt.Errorf("restart service %q: %w", name, err)
	}
	return nil
}

func (s *Systemd) Reload(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	_, err := s.executor.RunSudo(ctx, "systemctl", "reload", name)
	if err != nil {
		return fmt.Errorf("reload service %q: %w", name, err)
	}
	return nil
}

func (s *Systemd) Status(ctx context.Context, name string) (model.ServiceStatus, error) {
	if err := s.checkAllowed(name); err != nil {
		return model.ServiceStatus{}, err
	}

	result, err := s.executor.RunSudo(ctx, "systemctl", "show",
		"--property=ActiveState,SubState,MainPID,UnitFileState,ActiveEnterTimestamp",
		name,
	)
	if err != nil {
		return model.ServiceStatus{}, fmt.Errorf("status service %q: %w", name, err)
	}

	return parseShowOutput(name, result.Stdout), nil
}

func (s *Systemd) List(ctx context.Context) ([]model.ServiceStatus, error) {
	var statuses []model.ServiceStatus
	for _, pattern := range s.allowed {
		// For glob patterns, try exact match first
		status, err := s.Status(ctx, pattern)
		if err != nil {
			// Skip services that don't exist
			continue
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func parseShowOutput(name, output string) model.ServiceStatus {
	props := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			props[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	pid, _ := strconv.Atoi(props["MainPID"])

	return model.ServiceStatus{
		Name:    name,
		Active:  props["ActiveState"] == "active",
		Running: props["SubState"] == "running",
		Enabled: props["UnitFileState"] == "enabled",
		PID:     pid,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/ -v -race`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/
git commit -m "feat: add systemd service manager with allow-list

Glob pattern matching for service names (e.g. php*-fpm),
parses systemctl show output, MockExecutor integration."
```

---

### Task 8: Auth Service (Password Hashing + Sessions)

**Files:**
- Create: `internal/auth/service.go`
- Create: `internal/auth/service_test.go`

- [ ] **Step 1: Install dependencies**

```bash
go get github.com/oklog/ulid/v2
go get golang.org/x/crypto
```

- [ ] **Step 2: Write auth service test**

`internal/auth/service_test.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		SessionTTL:     24 * time.Hour,
		RateLimitLogin: 5,
		RateLimitAPI:   100,
		Argon2: config.Argon2Config{
			Memory:      64 * 1024,
			Iterations:  1, // fast for tests
			Parallelism: 1,
		},
	}
}

func TestHashPassword_And_Verify(t *testing.T) {
	svc := NewService(nil, testAuthConfig())

	hash, err := svc.HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	if !svc.VerifyPassword(hash, "secret123") {
		t.Error("VerifyPassword() should return true for correct password")
	}
	if svc.VerifyPassword(hash, "wrong") {
		t.Error("VerifyPassword() should return false for wrong password")
	}
}

func TestCreateUser_And_Authenticate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db, testAuthConfig())

	user, err := svc.CreateUser(context.Background(), "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("Username = %q, want %q", user.Username, "admin")
	}
	if user.ID == "" {
		t.Error("ID should not be empty")
	}

	authed, err := svc.Authenticate(context.Background(), "admin", "password123")
	if err != nil {
		t.Fatalf("Authenticate() error: %v", err)
	}
	if authed.ID != user.ID {
		t.Error("Authenticate() should return same user")
	}
}

func TestAuthenticate_WrongPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db, testAuthConfig())
	svc.CreateUser(context.Background(), "admin", "admin@test.com", "password123")

	_, err := svc.Authenticate(context.Background(), "admin", "wrong")
	if err == nil {
		t.Error("Authenticate() should fail with wrong password")
	}
}

func TestCreateSession_And_GetSession(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db, testAuthConfig())
	user, _ := svc.CreateUser(context.Background(), "admin", "admin@test.com", "pass")

	session, err := svc.CreateSession(context.Background(), user.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}

	got, err := svc.GetSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("GetSession() error: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", got.UserID, user.ID)
	}
}

func TestDeleteSession(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db, testAuthConfig())
	user, _ := svc.CreateUser(context.Background(), "admin", "admin@test.com", "pass")
	session, _ := svc.CreateSession(context.Background(), user.ID, "127.0.0.1", "agent")

	if err := svc.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatalf("DeleteSession() error: %v", err)
	}

	_, err := svc.GetSession(context.Background(), session.ID)
	if err == nil {
		t.Error("GetSession() should fail after delete")
	}
}

func TestCreateUser_Duplicate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db, testAuthConfig())
	svc.CreateUser(context.Background(), "admin", "admin@test.com", "pass")

	_, err := svc.CreateUser(context.Background(), "admin", "other@test.com", "pass")
	if err == nil {
		t.Error("CreateUser() should fail on duplicate username")
	}
}

func TestGetUserByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db, testAuthConfig())
	user, _ := svc.CreateUser(context.Background(), "admin", "admin@test.com", "pass")

	got, err := svc.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error: %v", err)
	}
	if got.Username != "admin" {
		t.Errorf("Username = %q, want %q", got.Username, "admin")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/ -v`
Expected: FAIL

- [ ] **Step 4: Implement auth service**

`internal/auth/service.go`:
```go
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/argon2"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Service struct {
	db  *sql.DB
	cfg config.AuthConfig
}

func NewService(db *sql.DB, cfg config.AuthConfig) *Service {
	return &Service{db: db, cfg: cfg}
}

func (s *Service) HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		s.cfg.Argon2.Iterations,
		s.cfg.Argon2.Memory,
		s.cfg.Argon2.Parallelism,
		32,
	)

	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%x$%x",
		s.cfg.Argon2.Memory,
		s.cfg.Argon2.Iterations,
		s.cfg.Argon2.Parallelism,
		salt,
		hash,
	)
	return encoded, nil
}

func (s *Service) VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return false
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)

	var salt []byte
	fmt.Sscanf(parts[4], "%x", &salt)

	var storedHash []byte
	fmt.Sscanf(parts[5], "%x", &storedHash)

	hash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, 32)

	if len(hash) != len(storedHash) {
		return false
	}
	// Constant-time comparison
	var diff byte
	for i := range hash {
		diff |= hash[i] ^ storedHash[i]
	}
	return diff == 0
}

func (s *Service) CreateUser(ctx context.Context, username, email, password string) (model.User, error) {
	hash, err := s.HashPassword(password)
	if err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().UTC()
	user := model.User{
		ID:        ulid.Make().String(),
		Username:  username,
		Email:     email,
		Password:  hash,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.Email, user.Password,
		boolToInt(user.IsActive), now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return model.User{}, model.ErrUserExists
		}
		return model.User{}, fmt.Errorf("insert user: %w", err)
	}

	return user, nil
}

func (s *Service) Authenticate(ctx context.Context, username, password string) (model.User, error) {
	var user model.User
	var isActive int
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, is_active, created_at, updated_at
		 FROM users WHERE username = ?`, username,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Password,
		&isActive, &createdAt, &updatedAt)
	if err == sql.ErrNoRow {
		return model.User{}, model.ErrInvalidCredentials
	}
	if err != nil {
		return model.User{}, fmt.Errorf("query user: %w", err)
	}

	user.IsActive = isActive == 1
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if !user.IsActive {
		return model.User{}, model.ErrUserInactive
	}

	if !s.VerifyPassword(user.Password, password) {
		return model.User{}, model.ErrInvalidCredentials
	}

	user.Password = "" // never return hash
	return user, nil
}

func (s *Service) GetUserByID(ctx context.Context, id string) (model.User, error) {
	var user model.User
	var isActive int
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, is_active, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email,
		&isActive, &createdAt, &updatedAt)
	if err == sql.ErrNoRow {
		return model.User{}, model.ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("query user: %w", err)
	}

	user.IsActive = isActive == 1
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return user, nil
}

func (s *Service) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, email, is_active, created_at, updated_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		var isActive int
		var createdAt, updatedAt string
		if err := rows.Scan(&u.ID, &u.Username, &u.Email,
			&isActive, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.IsActive = isActive == 1
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Service) UpdateUser(ctx context.Context, id, username, email string, isActive bool) (model.User, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE users SET username=?, email=?, is_active=?, updated_at=? WHERE id=?`,
		username, email, boolToInt(isActive), now.Format(time.RFC3339), id,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return model.User{}, model.ErrUserExists
		}
		return model.User{}, fmt.Errorf("update user: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return model.User{}, model.ErrNotFound
	}
	return s.GetUserByID(ctx, id)
}

func (s *Service) UpdatePassword(ctx context.Context, id, password string) error {
	hash, err := s.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE users SET password=?, updated_at=? WHERE id=?`,
		hash, now.Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (s *Service) DeleteUser(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (s *Service) CreateSession(ctx context.Context, userID, ip, userAgent string) (model.Session, error) {
	now := time.Now().UTC()
	session := model.Session{
		ID:        ulid.Make().String(),
		UserID:    userID,
		IPAddress: ip,
		UserAgent: userAgent,
		ExpiresAt: now.Add(s.cfg.SessionTTL),
		CreatedAt: now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, ip_address, user_agent, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.IPAddress, session.UserAgent,
		session.ExpiresAt.Format(time.RFC3339), session.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return model.Session{}, fmt.Errorf("create session: %w", err)
	}

	return session, nil
}

func (s *Service) GetSession(ctx context.Context, id string) (model.Session, error) {
	var session model.Session
	var expiresAt, createdAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, ip_address, user_agent, expires_at, created_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&session.ID, &session.UserID, &session.IPAddress,
		&session.UserAgent, &expiresAt, &createdAt)
	if err == sql.ErrNoRow {
		return model.Session{}, model.ErrSessionExpired
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("query session: %w", err)
	}

	session.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	session.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	if time.Now().UTC().After(session.ExpiresAt) {
		s.DeleteSession(ctx, id)
		return model.Session{}, model.ErrSessionExpired
	}

	return session, nil
}

func (s *Service) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Service) CleanExpiredSessions(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
	if err != nil {
		return fmt.Errorf("clean expired sessions: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

**Note:** `sql.ErrNoRow` should be `sql.ErrNoRows` — fix during implementation.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/auth/ -v -race`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/auth/service.go internal/auth/service_test.go go.mod go.sum
git commit -m "feat: add auth service with argon2id and server-side sessions

User CRUD, password hashing with argon2id, session create/get/delete,
expired session cleanup, ULID-based IDs, constant-time password compare."
```

---

### Task 9: RBAC Service

**Files:**
- Create: `internal/auth/rbac.go`
- Create: `internal/auth/rbac_test.go`

- [ ] **Step 1: Write RBAC test**

`internal/auth/rbac_test.go`:
```go
package auth

import (
	"context"
	"testing"
)

func TestRBAC_SeedAndCheck(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rbac := NewRBAC(db)

	if err := rbac.Seed(context.Background()); err != nil {
		t.Fatalf("Seed() error: %v", err)
	}

	svc := NewService(db, testAuthConfig())
	user, _ := svc.CreateUser(context.Background(), "admin", "admin@test.com", "pass")

	if err := rbac.AssignRole(context.Background(), user.ID, "admin"); err != nil {
		t.Fatalf("AssignRole() error: %v", err)
	}

	has, err := rbac.HasPermission(context.Background(), user.ID, "server.reboot")
	if err != nil {
		t.Fatalf("HasPermission() error: %v", err)
	}
	if !has {
		t.Error("admin should have server.reboot permission")
	}
}

func TestRBAC_UserRoleLimited(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rbac := NewRBAC(db)
	rbac.Seed(context.Background())

	svc := NewService(db, testAuthConfig())
	user, _ := svc.CreateUser(context.Background(), "viewer", "viewer@test.com", "pass")
	rbac.AssignRole(context.Background(), user.ID, "user")

	has, _ := rbac.HasPermission(context.Background(), user.ID, "server.reboot")
	if has {
		t.Error("user role should NOT have server.reboot permission")
	}

	has, _ = rbac.HasPermission(context.Background(), user.ID, "dashboard.view")
	if !has {
		t.Error("user role should have dashboard.view permission")
	}
}

func TestRBAC_GetUserPermissions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rbac := NewRBAC(db)
	rbac.Seed(context.Background())

	svc := NewService(db, testAuthConfig())
	user, _ := svc.CreateUser(context.Background(), "admin", "admin@test.com", "pass")
	rbac.AssignRole(context.Background(), user.ID, "admin")

	perms, err := rbac.GetUserPermissions(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetUserPermissions() error: %v", err)
	}
	if len(perms) == 0 {
		t.Error("admin should have permissions")
	}
}

func TestRBAC_GetUserRoles(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rbac := NewRBAC(db)
	rbac.Seed(context.Background())

	svc := NewService(db, testAuthConfig())
	user, _ := svc.CreateUser(context.Background(), "admin", "admin@test.com", "pass")
	rbac.AssignRole(context.Background(), user.ID, "admin")

	roles, err := rbac.GetUserRoles(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles() error: %v", err)
	}
	if len(roles) != 1 || roles[0].Name != "admin" {
		t.Errorf("roles = %v, want [admin]", roles)
	}
}

func TestRBAC_Seed_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rbac := NewRBAC(db)
	if err := rbac.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := rbac.Seed(context.Background()); err != nil {
		t.Fatalf("second Seed() should be idempotent: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -v -run TestRBAC`
Expected: FAIL

- [ ] **Step 3: Implement RBAC**

`internal/auth/rbac.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type RBAC struct {
	db *sql.DB
}

func NewRBAC(db *sql.DB) *RBAC {
	return &RBAC{db: db}
}

var defaultPermissions = []struct {
	Name   string
	Module string
}{
	{"dashboard.view", "dashboard"},
	{"server.view", "server"},
	{"server.reboot", "server"},
	{"server.hostname", "server"},
	{"server.timezone", "server"},
	{"services.view", "services"},
	{"services.manage", "services"},
	{"users.view", "users"},
	{"users.create", "users"},
	{"users.update", "users"},
	{"users.delete", "users"},
	{"audit.view", "audit"},
	{"settings.view", "settings"},
	{"settings.update", "settings"},
}

var userPermissions = []string{
	"dashboard.view",
	"server.view",
	"services.view",
}

func (r *RBAC) Seed(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Create roles
	for _, role := range []struct{ name, desc string }{
		{"admin", "Full system access"},
		{"user", "Limited view access"},
	} {
		_, err := r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO roles (id, name, description, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			ulid.Make().String(), role.name, role.desc, now, now,
		)
		if err != nil {
			return fmt.Errorf("seed role %q: %w", role.name, err)
		}
	}

	// Create permissions
	for _, p := range defaultPermissions {
		_, err := r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO permissions (id, name, module) VALUES (?, ?, ?)`,
			ulid.Make().String(), p.Name, p.Module,
		)
		if err != nil {
			return fmt.Errorf("seed permission %q: %w", p.Name, err)
		}
	}

	// Assign all permissions to admin role
	var adminRoleID string
	r.db.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = 'admin'`).Scan(&adminRoleID)

	rows, _ := r.db.QueryContext(ctx, `SELECT id FROM permissions`)
	defer rows.Close()
	for rows.Next() {
		var permID string
		rows.Scan(&permID)
		r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)`,
			adminRoleID, permID,
		)
	}

	// Assign view permissions to user role
	var userRoleID string
	r.db.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = 'user'`).Scan(&userRoleID)

	for _, permName := range userPermissions {
		var permID string
		r.db.QueryRowContext(ctx, `SELECT id FROM permissions WHERE name = ?`, permName).Scan(&permID)
		if permID != "" {
			r.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)`,
				userRoleID, permID,
			)
		}
	}

	return nil
}

func (r *RBAC) AssignRole(ctx context.Context, userID, roleName string) error {
	var roleID string
	err := r.db.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = ?`, roleName).Scan(&roleID)
	if err != nil {
		return fmt.Errorf("find role %q: %w", roleName, err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)`,
		userID, roleID,
	)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	return nil
}

func (r *RBAC) HasPermission(ctx context.Context, userID, permName string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_roles ur
		JOIN role_permissions rp ON ur.role_id = rp.role_id
		JOIN permissions p ON rp.permission_id = p.id
		WHERE ur.user_id = ? AND p.name = ?
	`, userID, permName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check permission: %w", err)
	}
	return count > 0, nil
}

func (r *RBAC) GetUserPermissions(ctx context.Context, userID string) ([]model.Permission, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT p.id, p.name, p.module FROM permissions p
		JOIN role_permissions rp ON p.id = rp.permission_id
		JOIN user_roles ur ON rp.role_id = ur.role_id
		WHERE ur.user_id = ?
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get permissions: %w", err)
	}
	defer rows.Close()

	var perms []model.Permission
	for rows.Next() {
		var p model.Permission
		if err := rows.Scan(&p.ID, &p.Name, &p.Module); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

func (r *RBAC) GetUserRoles(ctx context.Context, userID string) ([]model.Role, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.name, r.description, r.created_at, r.updated_at FROM roles r
		JOIN user_roles ur ON r.id = ur.role_id
		WHERE ur.user_id = ?
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get roles: %w", err)
	}
	defer rows.Close()

	var roles []model.Role
	for rows.Next() {
		var r model.Role
		var createdAt, updatedAt string
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		roles = append(roles, r)
	}
	return roles, rows.Err()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/auth/ -v -race`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/auth/rbac.go internal/auth/rbac_test.go
git commit -m "feat: add RBAC with role seeding and permission checking

Seed admin (all perms) and user (view-only) roles.
AssignRole, HasPermission, GetUserPermissions, GetUserRoles.
Idempotent seeding with INSERT OR IGNORE."
```

---

### Task 10: Audit Service

**Files:**
- Create: `internal/audit/service.go`
- Create: `internal/audit/service_test.go`

- [ ] **Step 1: Write audit test**

`internal/audit/service_test.go`:
```go
package audit

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLog_And_List(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db)

	err := svc.Log(context.Background(), LogEntry{
		UserID: "user-1",
		Action: "login",
		Module: "auth",
		Target: "",
		Detail: "",
		IP:     "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	err = svc.Log(context.Background(), LogEntry{
		UserID: "user-1",
		Action: "website.create",
		Module: "website",
		Target: "example.com",
		Detail: `{"php":"8.4"}`,
		IP:     "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	entries, total, err := svc.List(context.Background(), ListParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(entries) != 2 {
		t.Errorf("len(entries) = %d, want 2", len(entries))
	}
	// Most recent first
	if entries[0].Action != "website.create" {
		t.Errorf("first entry action = %q, want %q", entries[0].Action, "website.create")
	}
}

func TestList_Pagination(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(db)

	for i := 0; i < 15; i++ {
		svc.Log(context.Background(), LogEntry{
			Action: "test",
			Module: "test",
			IP:     "127.0.0.1",
		})
	}

	entries, total, err := svc.List(context.Background(), ListParams{Page: 2, PerPage: 10})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if total != 15 {
		t.Errorf("total = %d, want 15", total)
	}
	if len(entries) != 5 {
		t.Errorf("page 2 len = %d, want 5", len(entries))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/audit/ -v`
Expected: FAIL

- [ ] **Step 3: Implement audit service**

`internal/audit/service.go`:
```go
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

type LogEntry struct {
	UserID string
	Action string
	Module string
	Target string
	Detail string
	IP     string
}

type ListParams struct {
	Page    int
	PerPage int
	Module  string
}

func (s *Service) Log(ctx context.Context, entry LogEntry) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (id, user_id, action, module, target, detail, ip_address, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ulid.Make().String(),
		nullableString(entry.UserID),
		entry.Action, entry.Module,
		nullableString(entry.Target),
		nullableString(entry.Detail),
		nullableString(entry.IP),
		now.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func (s *Service) List(ctx context.Context, params ListParams) ([]model.AuditEntry, int, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PerPage < 1 {
		params.PerPage = 20
	}

	whereClause := ""
	var args []any

	if params.Module != "" {
		whereClause = " WHERE module = ?"
		args = append(args, params.Module)
	}

	var total int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_logs"+whereClause, args...,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	offset := (params.Page - 1) * params.PerPage
	queryArgs := append(args, params.PerPage, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, action, module, target, detail, ip_address, created_at
		 FROM audit_logs`+whereClause+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var entries []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		var userID, target, detail, ip sql.NullString
		var createdAt string

		if err := rows.Scan(&e.ID, &userID, &e.Action, &e.Module,
			&target, &detail, &ip, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}
		e.UserID = userID.String
		e.Target = target.String
		e.Detail = detail.String
		e.IPAddress = ip.String
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}

	return entries, total, rows.Err()
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/audit/ -v -race`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/audit/
git commit -m "feat: add audit log service with pagination

Log entries with ULID IDs, paginated listing sorted by
newest first, optional module filter."
```

---

### Task 11: System Info

**Files:**
- Create: `internal/system/info.go`
- Create: `internal/system/info_test.go`

- [ ] **Step 1: Write system info test**

`internal/system/info_test.go`:
```go
package system

import (
	"context"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestGetInfo(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (executor.Result, error) {
			switch name {
			case "hostname":
				return executor.Result{Stdout: "vps01\n"}, nil
			case "uname":
				return executor.Result{Stdout: "6.8.0-41-generic\n"}, nil
			default:
				return executor.Result{Stdout: "unknown\n"}, nil
			}
		},
	}

	info := NewInfo(mock)
	result, err := info.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if result.Hostname != "vps01" {
		t.Errorf("Hostname = %q, want %q", result.Hostname, "vps01")
	}
	if result.Kernel != "6.8.0-41-generic" {
		t.Errorf("Kernel = %q, want %q", result.Kernel, "6.8.0-41-generic")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/system/ -v -run TestGetInfo`
Expected: FAIL

- [ ] **Step 3: Implement system info**

`internal/system/info.go`:
```go
package system

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Info struct {
	executor executor.CommandExecutor
}

func NewInfo(exec executor.CommandExecutor) *Info {
	return &Info{executor: exec}
}

func (i *Info) Get(ctx context.Context) (model.ServerInfo, error) {
	info := model.ServerInfo{}

	if result, err := i.executor.Run(ctx, "hostname"); err == nil {
		info.Hostname = strings.TrimSpace(result.Stdout)
	}

	if result, err := i.executor.Run(ctx, "uname", "-r"); err == nil {
		info.Kernel = strings.TrimSpace(result.Stdout)
	}

	info.OS = readOSRelease()
	info.IP = getOutboundIP()

	if result, err := i.executor.Run(ctx, "timedatectl", "show", "--property=Timezone", "--value"); err == nil {
		info.Timezone = strings.TrimSpace(result.Stdout)
	}

	return info, nil
}

func readOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			name := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(name, `"`)
		}
	}
	return "Unknown"
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "unknown"
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}

func (i *Info) SetHostname(ctx context.Context, hostname string) error {
	_, err := i.executor.RunSudo(ctx, "hostnamectl", "set-hostname", hostname)
	if err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}
	return nil
}

func (i *Info) SetTimezone(ctx context.Context, timezone string) error {
	_, err := i.executor.RunSudo(ctx, "timedatectl", "set-timezone", timezone)
	if err != nil {
		return fmt.Errorf("set timezone: %w", err)
	}
	return nil
}

func (i *Info) Reboot(ctx context.Context) error {
	_, err := i.executor.RunSudo(ctx, "reboot")
	if err != nil {
		return fmt.Errorf("reboot: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/system/ -v -run TestGetInfo -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/system/info.go internal/system/info_test.go
git commit -m "feat: add system info service

Hostname, kernel, OS from /etc/os-release, outbound IP,
timezone. SetHostname, SetTimezone, Reboot via sudo."
```

---

### Task 12: Metrics Collector & Ring Buffer

**Files:**
- Create: `internal/system/metrics.go`
- Create: `internal/system/metrics_test.go`

- [ ] **Step 1: Write metrics test**

`internal/system/metrics_test.go`:
```go
package system

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRingBuffer(t *testing.T) {
	rb := newRingBuffer(3)

	m1 := model.ServerMetrics{CPU: 10, Timestamp: time.Now()}
	m2 := model.ServerMetrics{CPU: 20, Timestamp: time.Now()}
	m3 := model.ServerMetrics{CPU: 30, Timestamp: time.Now()}
	m4 := model.ServerMetrics{CPU: 40, Timestamp: time.Now()}

	rb.Add(m1)
	rb.Add(m2)
	rb.Add(m3)

	items := rb.All()
	if len(items) != 3 {
		t.Fatalf("len = %d, want 3", len(items))
	}

	rb.Add(m4) // overwrite m1
	items = rb.All()
	if len(items) != 3 {
		t.Fatalf("len = %d, want 3", len(items))
	}
	if items[0].CPU != 20 {
		t.Errorf("first CPU = %f, want 20 (oldest)", items[0].CPU)
	}
	if items[2].CPU != 40 {
		t.Errorf("last CPU = %f, want 40 (newest)", items[2].CPU)
	}
}

func TestRingBuffer_Latest(t *testing.T) {
	rb := newRingBuffer(5)
	rb.Add(model.ServerMetrics{CPU: 10})
	rb.Add(model.ServerMetrics{CPU: 20})

	latest := rb.Latest()
	if latest.CPU != 20 {
		t.Errorf("Latest CPU = %f, want 20", latest.CPU)
	}
}

func TestMetricsCollector_StoreAndQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := config.MetricsConfig{
		CollectInterval: 1 * time.Second,
		StoreInterval:   1 * time.Second,
		RetentionDays:   7,
	}

	mc := NewMetricsCollector(db, cfg)

	m := model.ServerMetrics{
		CPU: 50.5, RAMUsed: 1024, RAMTotal: 4096,
		SwapUsed: 0, SwapTotal: 2048,
		DiskUsed: 5000, DiskTotal: 10000,
		Load1: 0.5, Load5: 0.3, Load15: 0.2,
		NetRx: 1000, NetTx: 2000,
		Timestamp: time.Now().UTC(),
	}

	if err := mc.store(context.Background(), m); err != nil {
		t.Fatalf("store() error: %v", err)
	}

	recent, err := mc.GetRecent(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetRecent() error: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("len = %d, want 1", len(recent))
	}
	if recent[0].CPU != 50.5 {
		t.Errorf("CPU = %f, want 50.5", recent[0].CPU)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/system/ -v -run "TestRing|TestMetrics"`
Expected: FAIL

- [ ] **Step 3: Implement metrics collector**

`internal/system/metrics.go`:
```go
package system

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type MetricsCollector struct {
	db     *sql.DB
	cfg    config.MetricsConfig
	buffer *ringBuffer
}

func NewMetricsCollector(db *sql.DB, cfg config.MetricsConfig) *MetricsCollector {
	bufferSize := 60 // 5 min at 5s intervals
	return &MetricsCollector{
		db:     db,
		cfg:    cfg,
		buffer: newRingBuffer(bufferSize),
	}
}

func (mc *MetricsCollector) Start(ctx context.Context) {
	collectTicker := time.NewTicker(mc.cfg.CollectInterval)
	storeTicker := time.NewTicker(mc.cfg.StoreInterval)

	go func() {
		defer collectTicker.Stop()
		defer storeTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-collectTicker.C:
				m := collect()
				mc.buffer.Add(m)
			case <-storeTicker.C:
				latest := mc.buffer.Latest()
				if latest.Timestamp.IsZero() {
					continue
				}
				if err := mc.store(ctx, latest); err != nil {
					slog.Error("store metrics", "error", err)
				}
			}
		}
	}()

	// Cleanup goroutine
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mc.cleanup(ctx)
			}
		}
	}()
}

func (mc *MetricsCollector) Buffer() *ringBuffer {
	return mc.buffer
}

func (mc *MetricsCollector) store(ctx context.Context, m model.ServerMetrics) error {
	_, err := mc.db.ExecContext(ctx,
		`INSERT INTO server_metrics
		 (cpu, ram_used, ram_total, swap_used, swap_total,
		  disk_used, disk_total, load_1, load_5, load_15,
		  net_rx, net_tx, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.CPU, m.RAMUsed, m.RAMTotal, m.SwapUsed, m.SwapTotal,
		m.DiskUsed, m.DiskTotal, m.Load1, m.Load5, m.Load15,
		m.NetRx, m.NetTx, m.Timestamp.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert metrics: %w", err)
	}
	return nil
}

func (mc *MetricsCollector) GetRecent(ctx context.Context, limit int) ([]model.ServerMetrics, error) {
	rows, err := mc.db.QueryContext(ctx,
		`SELECT cpu, ram_used, ram_total, swap_used, swap_total,
		        disk_used, disk_total, load_1, load_5, load_15,
		        net_rx, net_tx, created_at
		 FROM server_metrics ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query metrics: %w", err)
	}
	defer rows.Close()

	var metrics []model.ServerMetrics
	for rows.Next() {
		var m model.ServerMetrics
		var createdAt string
		if err := rows.Scan(&m.CPU, &m.RAMUsed, &m.RAMTotal,
			&m.SwapUsed, &m.SwapTotal, &m.DiskUsed, &m.DiskTotal,
			&m.Load1, &m.Load5, &m.Load15, &m.NetRx, &m.NetTx,
			&createdAt); err != nil {
			return nil, fmt.Errorf("scan metrics: %w", err)
		}
		m.Timestamp, _ = time.Parse(time.RFC3339, createdAt)
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

func (mc *MetricsCollector) cleanup(ctx context.Context) {
	cutoff := time.Now().UTC().AddDate(0, 0, -mc.cfg.RetentionDays).Format(time.RFC3339)
	mc.db.ExecContext(ctx, `DELETE FROM server_metrics WHERE created_at < ?`, cutoff)
}

// collect reads system metrics from /proc.
// Returns zero values on macOS/non-Linux (for development).
func collect() model.ServerMetrics {
	m := model.ServerMetrics{Timestamp: time.Now().UTC()}
	m.CPU = readCPU()
	m.RAMUsed, m.RAMTotal = readMemory()
	m.SwapUsed, m.SwapTotal = readSwap()
	m.DiskUsed, m.DiskTotal = readDisk()
	m.Load1, m.Load5, m.Load15 = readLoadAvg()
	m.NetRx, m.NetTx = readNetwork()
	m.Uptime = readUptime()
	return m
}

// Ring buffer for in-memory metrics

type ringBuffer struct {
	mu    sync.RWMutex
	items []model.ServerMetrics
	size  int
	pos   int
	count int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		items: make([]model.ServerMetrics, size),
		size:  size,
	}
}

func (rb *ringBuffer) Add(m model.ServerMetrics) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.items[rb.pos] = m
	rb.pos = (rb.pos + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

func (rb *ringBuffer) Latest() model.ServerMetrics {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if rb.count == 0 {
		return model.ServerMetrics{}
	}
	idx := (rb.pos - 1 + rb.size) % rb.size
	return rb.items[idx]
}

func (rb *ringBuffer) All() []model.ServerMetrics {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]model.ServerMetrics, rb.count)
	if rb.count < rb.size {
		copy(result, rb.items[:rb.count])
	} else {
		// Buffer is full, read from pos (oldest) wrapping around
		n := copy(result, rb.items[rb.pos:])
		copy(result[n:], rb.items[:rb.pos])
	}
	return result
}

// /proc readers — stub on non-Linux

func readCPU() float64       { return 0 }
func readMemory() (uint64, uint64) { return 0, 0 }
func readSwap() (uint64, uint64)   { return 0, 0 }
func readDisk() (uint64, uint64)   { return 0, 0 }
func readLoadAvg() (float64, float64, float64) { return 0, 0, 0 }
func readNetwork() (uint64, uint64) { return 0, 0 }
func readUptime() time.Duration    { return 0 }
```

**Note:** The `/proc` reader implementations will be in a separate `metrics_linux.go` file with build tags. The stubs above allow development and tests on macOS. The Linux implementations should be added when deploying to Ubuntu — this is a build-tag decision, not a Phase 1 blocker. Create `internal/system/metrics_linux.go` with `//go:build linux` that reads `/proc/stat`, `/proc/meminfo`, `/proc/loadavg`, `/proc/net/dev`, and `/proc/uptime`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/system/ -v -race`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/system/metrics.go internal/system/metrics_test.go
git commit -m "feat: add metrics collector with ring buffer and DB storage

Background collection goroutine, configurable intervals,
ring buffer for WebSocket streaming, SQLite storage with
retention cleanup, stub /proc readers for cross-platform dev."
```

---

### Task 13: API Response Helpers & Middleware

**Files:**
- Create: `internal/api/response.go`
- Create: `internal/api/middleware.go`
- Create: `internal/api/middleware_test.go`

- [ ] **Step 1: Install Chi**

```bash
go get github.com/go-chi/chi/v5
```

- [ ] **Step 2: Implement response helpers**

`internal/api/response.go`:
```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Response struct {
	Data any  `json:"data,omitempty"`
	Meta *Meta `json:"meta,omitempty"`
}

type Meta struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{Data: data})
}

func JSONList(w http.ResponseWriter, data any, page, perPage, total int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Response{
		Data: data,
		Meta: &Meta{Page: page, PerPage: perPage, Total: total},
	})
}

func JSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{Code: code, Message: message},
	})
}

func HandleError(w http.ResponseWriter, err error) {
	var domainErr *model.DomainError
	if errors.As(err, &domainErr) {
		status := domainErrorToStatus(domainErr.Code)
		JSONError(w, status, domainErr.Code, domainErr.Message)
		return
	}
	JSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an internal error occurred")
}

func domainErrorToStatus(code string) int {
	switch code {
	case "NOT_FOUND":
		return http.StatusNotFound
	case "INVALID_CREDENTIALS", "SESSION_EXPIRED", "UNAUTHORIZED":
		return http.StatusUnauthorized
	case "FORBIDDEN":
		return http.StatusForbidden
	case "VALIDATION_ERROR", "USER_EXISTS":
		return http.StatusBadRequest
	case "RATE_LIMITED":
		return http.StatusTooManyRequests
	case "SERVICE_NOT_ALLOWED":
		return http.StatusForbidden
	case "USER_INACTIVE":
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func DecodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return model.NewValidationError("invalid JSON body")
	}
	return nil
}
```

- [ ] **Step 3: Write middleware test**

`internal/api/middleware_test.go`:
```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddleware(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			t.Error("X-Request-ID should be set in request")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID should be set in response")
	}
}

func TestRecovererMiddleware(t *testing.T) {
	handler := RecovererMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/api/ -v`
Expected: FAIL

- [ ] **Step 5: Implement middleware**

`internal/api/middleware.go`:
```go
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/logging"
)

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = ulid.Make().String()
		}
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		ctx := logging.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", logging.RequestID(r.Context()),
				"ip", r.RemoteAddr,
			)
		})
	}
}

func RecovererMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				slog.Error("panic recovered",
					"error", rvr,
					"path", r.URL.Path,
					"request_id", logging.RequestID(r.Context()),
				)
				JSONError(w, http.StatusInternalServerError,
					"INTERNAL_ERROR", "an internal error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/api/ -v -race`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/ go.mod go.sum
git commit -m "feat: add API response helpers and core middleware

JSON/JSONList/JSONError response helpers, domain error to HTTP
status mapping, RequestID/Logging/Recoverer middleware."
```

---

### Task 14: Auth Middleware (Session + CSRF)

**Files:**
- Create: `internal/auth/middleware.go`

- [ ] **Step 1: Implement auth middleware**

`internal/auth/middleware.go`:
```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/logging"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type contextKey string

const (
	userContextKey    contextKey = "user"
	sessionContextKey contextKey = "session"
)

func UserFromContext(ctx context.Context) (model.User, bool) {
	u, ok := ctx.Value(userContextKey).(model.User)
	return u, ok
}

func SessionFromContext(ctx context.Context) (model.Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(model.Session)
	return s, ok
}

func SessionMiddleware(authSvc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err != nil || cookie.Value == "" {
				api.HandleError(w, model.ErrUnauthorized)
				return
			}

			session, err := authSvc.GetSession(r.Context(), cookie.Value)
			if err != nil {
				api.HandleError(w, model.ErrSessionExpired)
				return
			}

			user, err := authSvc.GetUserByID(r.Context(), session.UserID)
			if err != nil {
				api.HandleError(w, model.ErrUnauthorized)
				return
			}

			if !user.IsActive {
				api.HandleError(w, model.ErrUserInactive)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			ctx = context.WithValue(ctx, sessionContextKey, session)
			ctx = logging.WithUserID(ctx, user.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequirePermission(rbac *RBAC, permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				api.HandleError(w, model.ErrUnauthorized)
				return
			}

			has, err := rbac.HasPermission(r.Context(), user.ID, permission)
			if err != nil || !has {
				api.HandleError(w, model.ErrForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie("csrf_token")
		if err != nil {
			api.JSONError(w, http.StatusForbidden, "CSRF_ERROR", "missing CSRF token")
			return
		}

		header := r.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			api.JSONError(w, http.StatusForbidden, "CSRF_ERROR", "invalid CSRF token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func GenerateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/auth/middleware.go
git commit -m "feat: add session auth, RBAC, and CSRF middleware

Session cookie validation, user context injection,
RequirePermission middleware, CSRF double-submit pattern."
```

---

### Task 15: Auth Handler (Login/Logout/Me)

**Files:**
- Create: `internal/auth/handler.go`

- [ ] **Step 1: Implement auth handler**

`internal/auth/handler.go`:
```go
package auth

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
)

type Handler struct {
	auth  *Service
	rbac  *RBAC
	audit *audit.Service
}

func NewHandler(authSvc *Service, rbac *RBAC, auditSvc *audit.Service) *Handler {
	return &Handler{auth: authSvc, rbac: rbac, audit: auditSvc}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	User        userResponse   `json:"user"`
	Permissions []string       `json:"permissions"`
	CSRFToken   string         `json:"csrf_token"`
}

type userResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	IsActive bool   `json:"is_active"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.HandleError(w, err)
		return
	}

	if req.Username == "" || req.Password == "" {
		api.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "username and password required")
		return
	}

	user, err := h.auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		h.audit.Log(r.Context(), audit.LogEntry{
			Action: "login.failed",
			Module: "auth",
			Target: req.Username,
			IP:     r.RemoteAddr,
		})
		api.HandleError(w, err)
		return
	}

	session, err := h.auth.CreateSession(r.Context(), user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		api.HandleError(w, err)
		return
	}

	csrfToken := GenerateCSRFToken()

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.auth.cfg.SessionTTL.Seconds()),
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    csrfToken,
		Path:     "/",
		HttpOnly: false, // JS needs to read this
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.auth.cfg.SessionTTL.Seconds()),
	})

	perms, _ := h.rbac.GetUserPermissions(r.Context(), user.ID)
	var permNames []string
	for _, p := range perms {
		permNames = append(permNames, p.Name)
	}

	h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "login",
		Module: "auth",
		IP:     r.RemoteAddr,
	})

	api.JSON(w, http.StatusOK, loginResponse{
		User: userResponse{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
			IsActive: user.IsActive,
		},
		Permissions: permNames,
		CSRFToken:   csrfToken,
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	session, ok := SessionFromContext(r.Context())
	if ok {
		h.auth.DeleteSession(r.Context(), session.ID)
		user, _ := UserFromContext(r.Context())
		h.audit.Log(r.Context(), audit.LogEntry{
			UserID: user.ID,
			Action: "logout",
			Module: "auth",
			IP:     r.RemoteAddr,
		})
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:   "csrf_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		api.HandleError(w, nil)
		return
	}

	perms, _ := h.rbac.GetUserPermissions(r.Context(), user.ID)
	roles, _ := h.rbac.GetUserRoles(r.Context(), user.ID)

	var permNames []string
	for _, p := range perms {
		permNames = append(permNames, p.Name)
	}
	var roleNames []string
	for _, r := range roles {
		roleNames = append(roleNames, r.Name)
	}

	api.JSON(w, http.StatusOK, map[string]any{
		"user": userResponse{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
			IsActive: user.IsActive,
		},
		"roles":       roleNames,
		"permissions": permNames,
	})
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/auth/handler.go
git commit -m "feat: add auth handlers for login, logout, and me

Login sets session + CSRF cookies, returns user + permissions.
Logout clears cookies. Me returns current user with roles/perms.
All actions audit-logged."
```

---

### Task 16: Settings, User CRUD, Audit, System, Service Handlers

**Files:**
- Create: `internal/settings/service.go`
- Create: `internal/settings/handler.go`
- Create: `internal/user/handler.go`
- Create: `internal/audit/handler.go`
- Create: `internal/system/handler.go`
- Create: `internal/service/handler.go`

- [ ] **Step 1: Implement settings service**

`internal/settings/service.go`:
```go
package settings

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) GetAll(ctx context.Context) ([]model.Setting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	var settings []model.Setting
	for rows.Next() {
		var st model.Setting
		var updatedAt string
		if err := rows.Scan(&st.Key, &st.Value, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		st.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		settings = append(settings, st)
	}
	return settings, rows.Err()
}

func (s *Service) Set(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=?, updated_at=?`,
		key, value, now, value, now,
	)
	if err != nil {
		return fmt.Errorf("upsert setting: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Implement settings handler**

`internal/settings/handler.go`:
```go
package settings

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.GetAll(r.Context())
	if err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSON(w, http.StatusOK, settings)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := api.DecodeJSON(r, &req); err != nil {
		api.HandleError(w, err)
		return
	}
	for k, v := range req {
		if err := h.svc.Set(r.Context(), k, v); err != nil {
			api.HandleError(w, err)
			return
		}
	}
	api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 3: Implement user CRUD handler**

`internal/user/handler.go`:
```go
package user

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	authpkg "github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Handler struct {
	auth     *authpkg.Service
	rbac     *authpkg.RBAC
	auditSvc *audit.Service
}

func NewHandler(authSvc *authpkg.Service, rbac *authpkg.RBAC, auditSvc *audit.Service) *Handler {
	return &Handler{auth: authSvc, rbac: rbac, auditSvc: auditSvc}
}

type createRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type updateRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	IsActive bool   `json:"is_active"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.auth.ListUsers(r.Context())
	if err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSON(w, http.StatusOK, users)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSON(w, http.StatusOK, user)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.HandleError(w, err)
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		api.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "username, email, and password required")
		return
	}

	user, err := h.auth.CreateUser(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		api.HandleError(w, err)
		return
	}

	role := req.Role
	if role == "" {
		role = "user"
	}
	h.rbac.AssignRole(r.Context(), user.ID, role)

	caller, _ := authpkg.UserFromContext(r.Context())
	h.auditSvc.Log(r.Context(), audit.LogEntry{
		UserID: caller.ID,
		Action: "users.create",
		Module: "users",
		Target: user.Username,
		IP:     r.RemoteAddr,
	})

	api.JSON(w, http.StatusCreated, user)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.HandleError(w, err)
		return
	}

	user, err := h.auth.UpdateUser(r.Context(), id, req.Username, req.Email, req.IsActive)
	if err != nil {
		api.HandleError(w, err)
		return
	}

	caller, _ := authpkg.UserFromContext(r.Context())
	h.auditSvc.Log(r.Context(), audit.LogEntry{
		UserID: caller.ID,
		Action: "users.update",
		Module: "users",
		Target: user.Username,
		IP:     r.RemoteAddr,
	})

	api.JSON(w, http.StatusOK, user)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	caller, _ := authpkg.UserFromContext(r.Context())
	if caller.ID == id {
		api.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "cannot delete yourself")
		return
	}

	target, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil {
		api.HandleError(w, err)
		return
	}

	if err := h.auth.DeleteUser(r.Context(), id); err != nil {
		api.HandleError(w, err)
		return
	}

	h.auditSvc.Log(r.Context(), audit.LogEntry{
		UserID: caller.ID,
		Action: "users.delete",
		Module: "users",
		Target: target.Username,
		IP:     r.RemoteAddr,
	})

	_ = target // used for audit
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Password string `json:"password"`
	}
	if err := api.DecodeJSON(r, &req); err != nil {
		api.HandleError(w, err)
		return
	}
	if req.Password == "" {
		api.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "password required")
		return
	}

	if err := h.auth.UpdatePassword(r.Context(), id, req.Password); err != nil {
		api.HandleError(w, err)
		return
	}

	_ = model.ErrNotFound // keep import
	api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Implement audit handler**

`internal/audit/handler.go`:
```go
package audit

import (
	"net/http"
	"strconv"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 {
		perPage = 20
	}
	module := r.URL.Query().Get("module")

	entries, total, err := h.svc.List(r.Context(), ListParams{
		Page:    page,
		PerPage: perPage,
		Module:  module,
	})
	if err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSONList(w, entries, page, perPage, total)
}
```

- [ ] **Step 5: Implement system handler**

`internal/system/handler.go`:
```go
package system

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
)

type Handler struct {
	info     *Info
	metrics  *MetricsCollector
	auditSvc *audit.Service
}

func NewHandler(info *Info, metrics *MetricsCollector, auditSvc *audit.Service) *Handler {
	return &Handler{info: info, metrics: metrics, auditSvc: auditSvc}
}

func (h *Handler) GetInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.info.Get(r.Context())
	if err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSON(w, http.StatusOK, info)
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	info, _ := h.info.Get(r.Context())
	latest := h.metrics.Buffer().Latest()

	api.JSON(w, http.StatusOK, map[string]any{
		"server":  info,
		"metrics": latest,
		"counts": map[string]int{
			"websites":         0,
			"databases":        0,
			"users":            0,
			"ssl_certificates": 0,
		},
	})
}

func (h *Handler) DashboardMetrics(w http.ResponseWriter, r *http.Request) {
	recent, err := h.metrics.GetRecent(r.Context(), 60)
	if err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSON(w, http.StatusOK, recent)
}

func (h *Handler) Reboot(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	h.auditSvc.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "server.reboot",
		Module: "server",
		IP:     r.RemoteAddr,
	})

	if err := h.info.Reboot(r.Context()); err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSON(w, http.StatusOK, map[string]string{"status": "rebooting"})
}

func (h *Handler) SetHostname(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := api.DecodeJSON(r, &req); err != nil {
		api.HandleError(w, err)
		return
	}
	if req.Hostname == "" {
		api.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "hostname required")
		return
	}

	if err := h.info.SetHostname(r.Context(), req.Hostname); err != nil {
		api.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	h.auditSvc.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "server.hostname",
		Module: "server",
		Target: req.Hostname,
		IP:     r.RemoteAddr,
	})
	api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SetTimezone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timezone string `json:"timezone"`
	}
	if err := api.DecodeJSON(r, &req); err != nil {
		api.HandleError(w, err)
		return
	}
	if req.Timezone == "" {
		api.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "timezone required")
		return
	}

	if err := h.info.SetTimezone(r.Context(), req.Timezone); err != nil {
		api.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	h.auditSvc.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "server.timezone",
		Module: "server",
		Target: req.Timezone,
		IP:     r.RemoteAddr,
	})
	api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 6: Implement service handler**

`internal/service/handler.go`:
```go
package service

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
)

type Handler struct {
	mgr      ServiceManager
	auditSvc *audit.Service
}

func NewHandler(mgr ServiceManager, auditSvc *audit.Service) *Handler {
	return &Handler{mgr: mgr, auditSvc: auditSvc}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.mgr.List(r.Context())
	if err != nil {
		api.HandleError(w, err)
		return
	}
	api.JSON(w, http.StatusOK, statuses)
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "start", h.mgr.Start)
}

func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "stop", h.mgr.Stop)
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "restart", h.mgr.Restart)
}

func (h *Handler) Reload(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "reload", h.mgr.Reload)
}

func (h *Handler) action(w http.ResponseWriter, r *http.Request, actionName string, fn func(ctx interface{ Deadline() (interface{}, bool) }, name string) error) {
	name := chi.URLParam(r, "name")

	if err := fn(r.Context(), name); err != nil {
		api.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	h.auditSvc.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "services." + actionName,
		Module: "services",
		Target: name,
		IP:     r.RemoteAddr,
	})
	api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

**Note:** The `action` helper's `fn` parameter type needs to match `context.Context` — use the correct signature during implementation:

```go
func (h *Handler) action(w http.ResponseWriter, r *http.Request, actionName string, fn func(ctx context.Context, name string) error) {
```

- [ ] **Step 7: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/settings/ internal/user/ internal/audit/handler.go internal/system/handler.go internal/service/handler.go
git commit -m "feat: add handlers for settings, users, audit, system, services

Settings key-value CRUD. User CRUD with role assignment.
Audit log paginated list. System info, reboot, hostname,
timezone. Service start/stop/restart/reload with audit."
```

---

### Task 17: WebSocket Metrics

**Files:**
- Create: `internal/system/ws.go`

- [ ] **Step 1: Install WebSocket dependency**

```bash
go get github.com/gorilla/websocket
```

- [ ] **Step 2: Implement WebSocket handler**

`internal/system/ws.go`:
```go
package system

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // tighten in production via config
	},
}

func (h *Handler) WSMetrics(w http.ResponseWriter, r *http.Request) {
	// Auth check via cookie
	_, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade", "error", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Send initial data
	latest := h.metrics.Buffer().Latest()
	conn.WriteJSON(map[string]any{
		"type":      "metrics",
		"data":      latest,
		"timestamp": latest.Timestamp,
	})

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			latest := h.metrics.Buffer().Latest()
			if err := conn.WriteJSON(map[string]any{
				"type":      "metrics",
				"data":      latest,
				"timestamp": latest.Timestamp,
			}); err != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/system/ws.go go.mod go.sum
git commit -m "feat: add WebSocket metrics streaming

Push server metrics every 5s from ring buffer.
Auth via session cookie, auto-close on context done."
```

---

### Task 18: Router Wiring

**Files:**
- Create: `internal/api/router.go`

- [ ] **Step 1: Implement router**

`internal/api/router.go`:
```go
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
	"github.com/mohammadirham37/jenderal_panel/internal/system"
	"github.com/mohammadirham37/jenderal_panel/internal/user"
)

type Dependencies struct {
	Logger      *slog.Logger
	AuthSvc     *auth.Service
	RBAC        *auth.RBAC
	AuditSvc    *audit.Service
	SystemInfo  *system.Info
	Metrics     *system.MetricsCollector
	ServiceMgr  service.ServiceManager
	SettingsSvc *settings.Service
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RealIP)
	r.Use(RequestIDMiddleware)
	r.Use(RecovererMiddleware)
	r.Use(LoggingMiddleware(deps.Logger))

	// Handlers
	authHandler := auth.NewHandler(deps.AuthSvc, deps.RBAC, deps.AuditSvc)
	systemHandler := system.NewHandler(deps.SystemInfo, deps.Metrics, deps.AuditSvc)
	serviceHandler := service.NewHandler(deps.ServiceMgr, deps.AuditSvc)
	userHandler := user.NewHandler(deps.AuthSvc, deps.RBAC, deps.AuditSvc)
	auditHandler := audit.NewHandler(deps.AuditSvc)
	settingsHandler := settings.NewHandler(deps.SettingsSvc)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public
		r.Post("/auth/login", authHandler.Login)

		// Authenticated
		r.Group(func(r chi.Router) {
			r.Use(auth.SessionMiddleware(deps.AuthSvc))
			r.Use(auth.CSRFMiddleware)

			r.Post("/auth/logout", authHandler.Logout)
			r.Get("/auth/me", authHandler.Me)

			// Dashboard
			r.Get("/dashboard", systemHandler.Dashboard)
			r.Get("/dashboard/metrics", systemHandler.DashboardMetrics)

			// Server
			r.Get("/server/info", systemHandler.GetInfo)
			r.With(auth.RequirePermission(deps.RBAC, "server.reboot")).
				Post("/server/reboot", systemHandler.Reboot)
			r.With(auth.RequirePermission(deps.RBAC, "server.hostname")).
				Post("/server/hostname", systemHandler.SetHostname)
			r.With(auth.RequirePermission(deps.RBAC, "server.timezone")).
				Post("/server/timezone", systemHandler.SetTimezone)

			// Services
			r.Get("/services", serviceHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/start", serviceHandler.Start)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/stop", serviceHandler.Stop)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/restart", serviceHandler.Restart)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/reload", serviceHandler.Reload)

			// Users
			r.With(auth.RequirePermission(deps.RBAC, "users.view")).
				Get("/users", userHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "users.create")).
				Post("/users", userHandler.Create)
			r.With(auth.RequirePermission(deps.RBAC, "users.view")).
				Get("/users/{id}", userHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "users.update")).
				Put("/users/{id}", userHandler.Update)
			r.With(auth.RequirePermission(deps.RBAC, "users.update")).
				Put("/users/{id}/password", userHandler.UpdatePassword)
			r.With(auth.RequirePermission(deps.RBAC, "users.delete")).
				Delete("/users/{id}", userHandler.Delete)

			// Audit logs
			r.With(auth.RequirePermission(deps.RBAC, "audit.view")).
				Get("/audit-logs", auditHandler.List)

			// Settings
			r.With(auth.RequirePermission(deps.RBAC, "settings.view")).
				Get("/settings", settingsHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "settings.update")).
				Put("/settings", settingsHandler.Update)
		})
	})

	// WebSocket (auth via session middleware, no CSRF)
	r.Group(func(r chi.Router) {
		r.Use(auth.SessionMiddleware(deps.AuthSvc))
		r.Get("/ws/metrics", systemHandler.WSMetrics)
	})

	return r
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: wire all API routes with auth and RBAC middleware

Chi router with versioned /api/v1 routes, session auth,
CSRF on mutating requests, per-route permission checks,
WebSocket endpoint with session-only auth."
```

---

### Task 19: HTTP Server & CLI

**Files:**
- Create: `internal/server/server.go`
- Modify: `cmd/jenderal/main.go`

- [ ] **Step 1: Implement HTTP server**

`internal/server/server.go`:
```go
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

func New(cfg config.ServerConfig, handler http.Handler, logger *slog.Logger) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if cfg.TLS.Enabled && cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		srv.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	return srv
}

func ListenAndServe(ctx context.Context, srv *http.Server, cfg config.ServerConfig, logger *slog.Logger) error {
	errCh := make(chan error, 1)

	go func() {
		logger.Info("server starting", "addr", srv.Addr, "tls", cfg.TLS.Enabled)
		if cfg.TLS.Enabled && cfg.TLS.Cert != "" {
			errCh <- srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

- [ ] **Step 2: Implement CLI main.go**

`cmd/jenderal/main.go`:
```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/logging"
	"github.com/mohammadirham37/jenderal_panel/internal/server"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
	"github.com/mohammadirham37/jenderal_panel/internal/system"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "migrate":
		cmdMigrate()
	case "admin":
		if len(os.Args) < 3 || os.Args[2] != "create" {
			fmt.Println("Usage: jenderal admin create")
			os.Exit(1)
		}
		cmdAdminCreate()
	case "version":
		fmt.Printf("Jenderal Panel %s\n", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`Jenderal Panel %s

Usage:
  jenderal serve              Start the HTTP server
  jenderal migrate            Run database migrations
  jenderal admin create       Create admin user
  jenderal version            Print version
`, version)
}

func loadConfig() *config.Config {
	path := "/etc/jenderal/jenderal.yaml"
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			path = os.Args[i+1]
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func openDB(cfg *config.Config) *sql.DB {
	db, err := database.Open(cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	return db
}

func cmdServe() {
	cfg := loadConfig()
	logger := logging.New(cfg.Logging)

	db := openDB(cfg)
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}

	exec := executor.New(30 * time.Second)
	serviceMgr := service.NewSystemd(exec, cfg.Services.Allowed)
	auditSvc := audit.NewService(db)
	authSvc := auth.NewService(db, cfg.Auth)
	rbac := auth.NewRBAC(db)
	systemInfo := system.NewInfo(exec)
	metricsCollector := system.NewMetricsCollector(db, cfg.Metrics)
	settingsSvc := settings.NewService(db)

	// Seed RBAC
	if err := rbac.Seed(context.Background()); err != nil {
		logger.Error("RBAC seed failed", "error", err)
		os.Exit(1)
	}

	router := api.NewRouter(api.Dependencies{
		Logger:      logger,
		AuthSvc:     authSvc,
		RBAC:        rbac,
		AuditSvc:    auditSvc,
		SystemInfo:  systemInfo,
		Metrics:     metricsCollector,
		ServiceMgr:  serviceMgr,
		SettingsSvc: settingsSvc,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	metricsCollector.Start(ctx)

	srv := server.New(cfg.Server, router, logger)
	if err := server.ListenAndServe(ctx, srv, cfg.Server, logger); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func cmdMigrate() {
	cfg := loadConfig()
	db := openDB(cfg)
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed successfully.")
}

func cmdAdminCreate() {
	cfg := loadConfig()
	db := openDB(cfg)
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}

	rbac := auth.NewRBAC(db)
	rbac.Seed(context.Background())

	authSvc := auth.NewService(db, cfg.Auth)

	var username, email, password string
	fmt.Print("Username: ")
	fmt.Scanln(&username)
	fmt.Print("Email: ")
	fmt.Scanln(&email)
	fmt.Print("Password: ")
	fmt.Scanln(&password)

	if username == "" || email == "" || password == "" {
		fmt.Fprintln(os.Stderr, "All fields required.")
		os.Exit(1)
	}

	user, err := authSvc.CreateUser(context.Background(), username, email, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating user: %v\n", err)
		os.Exit(1)
	}

	if err := rbac.AssignRole(context.Background(), user.ID, "admin"); err != nil {
		fmt.Fprintf(os.Stderr, "Error assigning role: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Admin user '%s' created successfully.\n", username)
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/jenderal`
Expected: PASS

- [ ] **Step 4: Test CLI**

Run: `./jenderal version`
Expected: `Jenderal Panel dev`

- [ ] **Step 5: Commit**

```bash
git add internal/server/ cmd/jenderal/
git commit -m "feat: add HTTP server and CLI entry point

Serve command with full DI wiring, graceful shutdown.
Migrate command. Admin create command (interactive).
Version command. Config path via --config flag."
```

---

### Task 20: SvelteKit Frontend Setup

**Files:**
- Create: `web/` (SvelteKit project)

- [ ] **Step 1: Create SvelteKit project**

```bash
cd /Users/irhamakbar/Documents/go_project/jenderal_panel
npm create svelte@latest web -- --template skeleton --types typescript
cd web
npm install
npm install -D tailwindcss @tailwindcss/vite
npm install -D @sveltejs/adapter-static
npm install uplot
```

- [ ] **Step 2: Configure adapter-static**

`web/svelte.config.js`:
```js
import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
	preprocess: vitePreprocess(),
	kit: {
		adapter: adapter({
			pages: 'build',
			assets: 'build',
			fallback: 'index.html',
			precompress: false,
			strict: false
		})
	}
};
```

- [ ] **Step 3: Configure Tailwind in vite.config.ts**

`web/vite.config.ts`:
```ts
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		proxy: {
			'/api': 'http://localhost:8443',
			'/ws': {
				target: 'ws://localhost:8443',
				ws: true
			}
		}
	}
});
```

- [ ] **Step 4: Configure Tailwind CSS**

`web/src/app.css`:
```css
@import 'tailwindcss';
```

- [ ] **Step 5: Verify frontend builds**

```bash
cd /Users/irhamakbar/Documents/go_project/jenderal_panel/web
npm run build
```
Expected: builds to `web/build/`

- [ ] **Step 6: Commit**

```bash
git add web/
git commit -m "feat: scaffold SvelteKit frontend with Tailwind and adapter-static

Vite proxy to Go backend for dev, adapter-static for
production build, Tailwind CSS v4."
```

---

### Task 21: Frontend — API Client & Stores

**Files:**
- Create: `web/src/lib/api.ts`
- Create: `web/src/lib/stores/auth.ts`
- Create: `web/src/lib/stores/metrics.ts`
- Create: `web/src/lib/types.ts`

- [ ] **Step 1: Create types**

`web/src/lib/types.ts`:
```ts
export interface User {
	id: string;
	username: string;
	email: string;
	is_active: boolean;
}

export interface LoginResponse {
	user: User;
	permissions: string[];
	csrf_token: string;
}

export interface ServerInfo {
	hostname: string;
	ip: string;
	os: string;
	kernel: string;
	timezone: string;
}

export interface ServerMetrics {
	cpu: number;
	ram_used: number;
	ram_total: number;
	swap_used: number;
	swap_total: number;
	disk_used: number;
	disk_total: number;
	load_1: number;
	load_5: number;
	load_15: number;
	net_rx: number;
	net_tx: number;
	uptime: number;
	timestamp: string;
}

export interface ServiceStatus {
	name: string;
	active: boolean;
	running: boolean;
	enabled: boolean;
	pid: number;
}

export interface AuditEntry {
	id: string;
	user_id: string;
	action: string;
	module: string;
	target: string;
	detail: string;
	ip_address: string;
	created_at: string;
}

export interface ApiResponse<T> {
	data: T;
	meta?: { page: number; per_page: number; total: number };
}

export interface ApiError {
	error: { code: string; message: string };
}
```

- [ ] **Step 2: Create API client**

`web/src/lib/api.ts`:
```ts
import type { ApiResponse, ApiError } from './types';

let csrfToken = '';

export function setCSRFToken(token: string) {
	csrfToken = token;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};

	if (method !== 'GET' && csrfToken) {
		headers['X-CSRF-Token'] = csrfToken;
	}

	const res = await fetch(path, {
		method,
		headers,
		credentials: 'include',
		body: body ? JSON.stringify(body) : undefined
	});

	if (!res.ok) {
		const err: ApiError = await res.json();
		throw new Error(err.error.message);
	}

	const json: ApiResponse<T> = await res.json();
	return json.data;
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
	put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
	del: <T>(path: string) => request<T>('DELETE', path)
};
```

- [ ] **Step 3: Create auth store**

`web/src/lib/stores/auth.ts`:
```ts
import { writable } from 'svelte/store';
import { api, setCSRFToken } from '$lib/api';
import type { User, LoginResponse } from '$lib/types';

export const user = writable<User | null>(null);
export const permissions = writable<string[]>([]);
export const isAuthenticated = writable(false);

export async function login(username: string, password: string) {
	const data = await api.post<LoginResponse>('/api/v1/auth/login', { username, password });
	setCSRFToken(data.csrf_token);
	user.set(data.user);
	permissions.set(data.permissions);
	isAuthenticated.set(true);
}

export async function logout() {
	await api.post('/api/v1/auth/logout');
	user.set(null);
	permissions.set([]);
	isAuthenticated.set(false);
}

export async function checkAuth() {
	try {
		const data = await api.get<{ user: User; permissions: string[] }>('/api/v1/auth/me');
		user.set(data.user);
		permissions.set(data.permissions);
		isAuthenticated.set(true);
		// Restore CSRF from cookie
		const match = document.cookie.match(/csrf_token=([^;]+)/);
		if (match) setCSRFToken(match[1]);
	} catch {
		isAuthenticated.set(false);
	}
}

export function hasPermission(perms: string[], required: string): boolean {
	return perms.includes(required);
}
```

- [ ] **Step 4: Create metrics store**

`web/src/lib/stores/metrics.ts`:
```ts
import { writable } from 'svelte/store';
import type { ServerMetrics } from '$lib/types';

export const currentMetrics = writable<ServerMetrics | null>(null);
export const metricsHistory = writable<ServerMetrics[]>([]);

let ws: WebSocket | null = null;

export function connectMetrics() {
	const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
	ws = new WebSocket(`${protocol}//${location.host}/ws/metrics`);

	ws.onmessage = (event) => {
		const msg = JSON.parse(event.data);
		if (msg.type === 'metrics') {
			currentMetrics.set(msg.data);
			metricsHistory.update((h) => {
				const updated = [...h, msg.data];
				return updated.slice(-60); // keep 5 min
			});
		}
	};

	ws.onclose = () => {
		setTimeout(connectMetrics, 5000);
	};
}

export function disconnectMetrics() {
	ws?.close();
	ws = null;
}
```

- [ ] **Step 5: Verify build**

```bash
cd /Users/irhamakbar/Documents/go_project/jenderal_panel/web && npm run build
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/
git commit -m "feat: add frontend API client, auth store, and metrics WebSocket store

Typed API client with CSRF header injection, auth store
with login/logout/checkAuth, WebSocket metrics store
with auto-reconnect."
```

---

### Task 22: Frontend — UI Components

**Files:**
- Create core Svelte components in `web/src/lib/components/`

- [ ] **Step 1: Create Layout component**

`web/src/lib/components/Layout.svelte`:

Sidebar nav with links to Dashboard, Server, Services, Users, Audit Logs, Settings. Top bar with app name and user dropdown (logout). Dark mode default. Responsive sidebar collapse on mobile.

- [ ] **Step 2: Create Toast, Modal, ConfirmDialog, ServiceBadge, DataTable**

Small, focused UI primitives. Toast for success/error notifications. Modal for overlays. ConfirmDialog for destructive actions. ServiceBadge shows running/stopped with color. DataTable with pagination controls.

- [ ] **Step 3: Create MetricsChart component**

Wrapper around uPlot for CPU/RAM/Disk/Network time-series charts. Accept data array and title prop.

- [ ] **Step 4: Verify build**

```bash
cd /Users/irhamakbar/Documents/go_project/jenderal_panel/web && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/components/
git commit -m "feat: add core UI components

Layout with sidebar/topbar, Toast, Modal, ConfirmDialog,
ServiceBadge, DataTable with pagination, MetricsChart
with uPlot for time-series."
```

---

### Task 23: Frontend — Pages

**Files:**
- Create page routes in `web/src/routes/`

- [ ] **Step 1: Create root layout with auth guard**

`web/src/routes/+layout.svelte`: Check auth on mount. Redirect to /login if not authenticated. Wrap authenticated pages with Layout component.

`web/src/routes/+layout.ts`: Set `ssr = false` and `prerender = false` for SPA mode.

- [ ] **Step 2: Create login page**

`web/src/routes/login/+page.svelte`: Form with username/password, error display, redirect to /dashboard on success.

- [ ] **Step 3: Create dashboard page**

`web/src/routes/dashboard/+page.svelte`: Fetch dashboard data, display server info cards, 4 metrics charts (CPU, RAM, Disk, Network), service status badges, counts.

- [ ] **Step 4: Create server page**

`web/src/routes/server/+page.svelte`: Server info display, reboot button with confirm dialog, hostname/timezone edit.

- [ ] **Step 5: Create services page**

`web/src/routes/services/+page.svelte`: List services with status badges, start/stop/restart buttons.

- [ ] **Step 6: Create users page**

`web/src/routes/users/+page.svelte`: User table, create modal, edit modal, delete with confirm.

- [ ] **Step 7: Create audit logs page**

`web/src/routes/audit-logs/+page.svelte`: Paginated DataTable of audit entries.

- [ ] **Step 8: Create settings page**

`web/src/routes/settings/+page.svelte`: Key-value settings editor.

- [ ] **Step 9: Verify build**

```bash
cd /Users/irhamakbar/Documents/go_project/jenderal_panel/web && npm run build
```

- [ ] **Step 10: Commit**

```bash
git add web/src/routes/
git commit -m "feat: add all Phase 1 frontend pages

Login, Dashboard with realtime metrics charts, Server info
with actions, Services management, User CRUD, Audit logs
with pagination, Settings editor. Dark theme default."
```

---

### Task 24: Frontend Embed in Go

**Files:**
- Create: `internal/api/static.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Create static file server**

`internal/api/static.go`:
```go
package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:../../web/build
var webFS embed.FS

func staticHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web/build")
	if err != nil {
		panic("embedded web build not found: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try serving the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists in embedded FS
		f, err := sub.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			// SPA fallback: serve index.html for client-side routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()

		fileServer.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 2: Add static handler to router**

Add at end of `NewRouter` in `internal/api/router.go`:

```go
	// Static files (SPA)
	r.Handle("/*", staticHandler())
```

- [ ] **Step 3: Build frontend then Go binary**

```bash
cd /Users/irhamakbar/Documents/go_project/jenderal_panel/web && npm run build
cd /Users/irhamakbar/Documents/go_project/jenderal_panel && go build ./cmd/jenderal
```
Expected: single binary with embedded frontend

- [ ] **Step 4: Commit**

```bash
git add internal/api/static.go
git commit -m "feat: embed SvelteKit build into Go binary

go:embed web/build, SPA fallback to index.html,
API/WS routes take priority over static files."
```

---

### Task 25: Installer Script

**Files:**
- Create: `scripts/install.sh`

- [ ] **Step 1: Create installer**

`scripts/install.sh`: Full installer script following the 20-step flow from the spec. Detect OS, check resources, install deps, create user/dirs, download binary, generate self-signed TLS, write config, write sudoers, run migrations, create admin, install systemd, configure UFW, start service, print URL.

- [ ] **Step 2: Create systemd unit**

`systemd/jenderal.service`: Unit file from spec.

- [ ] **Step 3: Commit**

```bash
git add scripts/ systemd/
git commit -m "feat: add installer script and systemd unit

Automated install for Ubuntu 22.04/24.04. OS detection,
resource checks, dependency install, self-signed TLS,
sudoers whitelist, systemd service, UFW configuration."
```

---

### Task 26: Linux Metrics Readers

**Files:**
- Create: `internal/system/metrics_linux.go`

- [ ] **Step 1: Implement Linux /proc readers**

`internal/system/metrics_linux.go` with `//go:build linux`:

Parse `/proc/stat` for CPU, `/proc/meminfo` for RAM/swap, `/proc/loadavg` for load, `/proc/net/dev` for network, `/proc/uptime` for uptime, and `syscall.Statfs` for disk usage.

- [ ] **Step 2: Move stubs to metrics_other.go**

`internal/system/metrics_other.go` with `//go:build !linux`:

Move stub functions (return zeros) here so they compile on macOS for development.

- [ ] **Step 3: Verify build on macOS**

Run: `go build ./...`
Expected: PASS (uses stubs)

- [ ] **Step 4: Commit**

```bash
git add internal/system/metrics_linux.go internal/system/metrics_other.go
git commit -m "feat: add Linux /proc metrics readers with macOS stubs

Parse /proc/stat, /proc/meminfo, /proc/loadavg, /proc/net/dev,
/proc/uptime for real metrics on Linux. Zero-value stubs for
cross-platform development."
```

---

### Task 27: Integration Tests

**Files:**
- Create: `tests/integration_test.go`

- [ ] **Step 1: Write integration test**

Test the full auth flow: create admin, login, get me, list users, logout.

```go
// tests/integration_test.go
package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	// ... imports
)
```

Test cases:
- Login with valid credentials returns 200 + session cookie
- Login with invalid credentials returns 401
- Access protected route without session returns 401
- Access protected route with session returns 200
- Logout clears session
- RBAC: user role cannot access admin-only routes

- [ ] **Step 2: Run integration tests**

Run: `go test ./tests/ -v -race`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add tests/
git commit -m "feat: add integration tests for auth flow and RBAC

Full cycle: login, session, authorized access, RBAC
enforcement, logout. Tests against in-memory SQLite."
```

---

### Task 28: Final Verification

- [ ] **Step 1: Run all tests**

```bash
go test ./... -v -race
```
Expected: all PASS

- [ ] **Step 2: Build binary**

```bash
make build
```
Expected: produces `jenderal` binary

- [ ] **Step 3: Run lint**

```bash
go vet ./...
```
Expected: no issues

- [ ] **Step 4: Commit any fixes**

- [ ] **Step 5: Tag release**

```bash
git tag v0.1.0
```
