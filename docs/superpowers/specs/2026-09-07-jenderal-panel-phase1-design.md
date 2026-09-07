# Jenderal Panel — Phase 1 (Core) Design Spec

## Overview

Jenderal Panel is an open-source VPS control panel for Ubuntu Linux servers. It provides a web GUI for server management, replacing routine CLI tasks with a secure, modern interface.

**Phase 1 scope:** Core foundation — Go backend, SQLite database, authentication, RBAC, server monitoring, service management, SvelteKit frontend, installer, and systemd integration.

**Target OS:** Ubuntu 22.04 LTS, Ubuntu 24.04 LTS.

---

## Decisions

| Area | Decision |
|---|---|
| Architecture | Monolith layered |
| Name / binary | `jenderal` |
| Module path | `github.com/mohammadirham37/jenderal_panel` |
| HTTP router | Chi |
| Internal DB | SQLite (WAL mode) |
| Managed DB (future) | MySQL + PostgreSQL |
| Auth | Server-side session + argon2id password hashing |
| RBAC | Role-permission model |
| Privileged ops | sudo whitelist (`/etc/sudoers.d/jenderal`) |
| Logging | `log/slog` JSON structured logging |
| Config | YAML file + environment variable override |
| Frontend | SvelteKit + adapter-static + `go:embed` (single binary) |
| Charts | uPlot (lightweight time-series) |
| Metrics source | Direct `/proc` parsing, no external deps |
| DI | Constructor injection, no framework |
| Installer | Bash script, systemd, self-signed TLS |
| ID strategy | ULID for entities, AUTOINCREMENT for metrics |

---

## 1. Project Structure

```
jenderal_panel/
├── cmd/
│   └── jenderal/
│       └── main.go              # entry point, DI wiring, graceful shutdown
│
├── internal/
│   ├── config/
│   │   └── config.go            # YAML loader, env override, validation
│   │
│   ├── database/
│   │   ├── sqlite.go            # SQLite connection, WAL mode
│   │   └── migrations.go        # embedded SQL migrations
│   │
│   ├── auth/
│   │   ├── handler.go           # login, logout, session endpoints
│   │   ├── service.go           # auth logic, password hashing (argon2id)
│   │   ├── middleware.go        # session validation, CSRF
│   │   ├── rbac.go              # role/permission check
│   │   └── model.go             # User, Role, Permission structs
│   │
│   ├── api/
│   │   ├── router.go            # Chi router setup, versioned /api/v1
│   │   ├── middleware.go        # logging, recovery, rate limit, request ID
│   │   └── response.go          # standard JSON response helpers
│   │
│   ├── server/
│   │   └── server.go            # HTTP server, TLS, graceful shutdown
│   │
│   ├── system/
│   │   ├── info.go              # hostname, OS, kernel, IP, uptime
│   │   └── metrics.go           # CPU, RAM, disk, network, load avg
│   │
│   ├── service/
│   │   └── systemd.go           # ServiceManager interface + systemd impl
│   │
│   ├── executor/
│   │   └── command.go           # CommandExecutor, sudo whitelist, timeout
│   │
│   ├── audit/
│   │   ├── service.go           # audit log writer
│   │   └── model.go             # AuditEntry struct
│   │
│   └── logging/
│       └── logger.go            # structured JSON logger (slog)
│
├── migrations/
│   ├── 001_users.sql
│   ├── 002_roles_permissions.sql
│   ├── 003_sessions.sql
│   ├── 004_audit_logs.sql
│   ├── 005_settings.sql
│   └── 006_server_metrics.sql
│
├── web/                         # SvelteKit project
│   ├── src/
│   │   ├── lib/
│   │   │   ├── api.ts
│   │   │   ├── stores/
│   │   │   │   ├── auth.ts
│   │   │   │   └── metrics.ts
│   │   │   ├── components/
│   │   │   │   ├── Layout.svelte
│   │   │   │   ├── MetricsChart.svelte
│   │   │   │   ├── ServiceBadge.svelte
│   │   │   │   ├── Modal.svelte
│   │   │   │   ├── Toast.svelte
│   │   │   │   ├── ConfirmDialog.svelte
│   │   │   │   └── DataTable.svelte
│   │   │   └── types.ts
│   │   ├── routes/
│   │   │   ├── +layout.svelte
│   │   │   ├── login/+page.svelte
│   │   │   ├── dashboard/+page.svelte
│   │   │   ├── server/+page.svelte
│   │   │   ├── services/+page.svelte
│   │   │   ├── users/+page.svelte
│   │   │   ├── audit-logs/+page.svelte
│   │   │   └── settings/+page.svelte
│   │   └── app.css
│   ├── static/
│   ├── package.json
│   ├── svelte.config.js
│   ├── tailwind.config.js
│   ├── tsconfig.json
│   └── vite.config.ts
│
├── configs/
│   └── jenderal.yaml.example
│
├── scripts/
│   └── install.sh
│
├── systemd/
│   └── jenderal.service
│
├── docs/
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

Dependencies flow one direction: `handler → service → repository/executor`.

---

## 2. Database Schema (SQLite Internal)

```sql
-- users
CREATE TABLE users (
    id          TEXT PRIMARY KEY,  -- ULID
    username    TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,      -- argon2id hash
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,      -- RFC3339
    updated_at  TEXT NOT NULL
);

-- roles
CREATE TABLE roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,  -- admin, user
    description TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- permissions
CREATE TABLE permissions (
    id       TEXT PRIMARY KEY,
    name     TEXT NOT NULL UNIQUE,  -- websites.create, websites.delete, server.reboot
    module   TEXT NOT NULL          -- websites, server, database, ssl, firewall
);

-- user_roles
CREATE TABLE user_roles (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

-- role_permissions
CREATE TABLE role_permissions (
    role_id       TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- sessions
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip_address TEXT,
    user_agent TEXT,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- audit_logs
CREATE TABLE audit_logs (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id),
    action     TEXT NOT NULL,       -- login, website.create, service.restart
    module     TEXT NOT NULL,
    target     TEXT,                -- e.g. "example.com", "nginx"
    detail     TEXT,                -- JSON extra data
    ip_address TEXT,
    created_at TEXT NOT NULL
);

-- settings
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- server_metrics
CREATE TABLE server_metrics (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cpu        REAL NOT NULL,
    ram_used   INTEGER NOT NULL,    -- bytes
    ram_total  INTEGER NOT NULL,
    swap_used  INTEGER NOT NULL,
    swap_total INTEGER NOT NULL,
    disk_used  INTEGER NOT NULL,
    disk_total INTEGER NOT NULL,
    load_1     REAL NOT NULL,
    load_5     REAL NOT NULL,
    load_15    REAL NOT NULL,
    net_rx     INTEGER NOT NULL,    -- bytes since boot
    net_tx     INTEGER NOT NULL,
    created_at TEXT NOT NULL
);
```

ULID for entity IDs (sortable, URL-safe). `server_metrics` uses AUTOINCREMENT integer for high-volume inserts with date-based retention cleanup.

---

## 3. Authentication & RBAC

### Auth Flow

```
Login POST /api/v1/auth/login
  → validate credentials (argon2id verify)
  → create session record in DB
  → set HttpOnly/Secure/SameSite cookie with session ID
  → return user + role info

Logout POST /api/v1/auth/logout
  → delete session from DB
  → clear cookie

Every API request:
  → middleware extract session cookie
  → lookup session in DB (check expiry)
  → attach user to context
  → RBAC middleware check permission for route
```

### Password Hashing

Argon2id. Parameters: memory 64MB, iterations 3, parallelism 2, salt 16 bytes.

### Session

Server-side session stored in SQLite. No JWT — simpler revocation, no token leak risk. Session expiry configurable (default 24h). Session cookie: `HttpOnly`, `Secure`, `SameSite=Strict`.

### CSRF

Double-submit cookie pattern. Generate CSRF token per session, frontend sends via `X-CSRF-Token` header on mutating requests.

### Rate Limiting

In-memory sliding window. Login: 5 attempts per 15 min per IP. API: 100 req/min per session.

### RBAC Model

```
Default roles:
  admin  → all permissions (wildcard)
  user   → scoped permissions per module

Permission format: "module.action"
  websites.create, websites.delete, websites.view
  server.reboot, server.info
  database.create, firewall.manage
  ...

Check: middleware resolve user → roles → permissions → match route requirement
```

### Seeding

Installer creates first admin user. Default `admin` role seeded with all permissions. Default `user` role seeded with view-only permissions.

---

## 4. API Structure & Middleware

### Chi Middleware Stack (order matters)

```
RequestID          → generate unique request ID
RealIP             → extract client IP
StructuredLogger   → log request/response with slog
Recoverer          → panic recovery
RateLimiter        → sliding window per IP
CORS               → configured origins
SessionAuth        → extract/validate session (skip public routes)
CSRF               → validate on mutating methods
RBAC               → check permission per route
```

### API Routes — Phase 1

```
Public:
  POST   /api/v1/auth/login
  POST   /api/v1/auth/logout

Authenticated:
  GET    /api/v1/auth/me              → current user + permissions

  GET    /api/v1/dashboard            → aggregated server info + stats
  GET    /api/v1/dashboard/metrics    → recent metrics for charts

  GET    /api/v1/server/info          → hostname, OS, kernel, IP, uptime
  POST   /api/v1/server/reboot        → reboot (admin only)
  POST   /api/v1/server/hostname      → change hostname
  POST   /api/v1/server/timezone      → change timezone

  GET    /api/v1/services             → list managed services + status
  POST   /api/v1/services/{name}/start
  POST   /api/v1/services/{name}/stop
  POST   /api/v1/services/{name}/restart
  POST   /api/v1/services/{name}/reload

  GET    /api/v1/users                → list users
  POST   /api/v1/users                → create user
  GET    /api/v1/users/{id}
  PUT    /api/v1/users/{id}
  DELETE /api/v1/users/{id}

  GET    /api/v1/audit-logs           → paginated audit logs

  GET    /api/v1/settings
  PUT    /api/v1/settings

WebSocket:
  GET    /ws/metrics                  → realtime server metrics stream
```

### Response Format

```json
// Success
{
  "data": { ... },
  "meta": { "page": 1, "total": 50 }
}

// Error
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Domain name is required",
    "details": { ... }
  }
}
```

DTO pattern: request structs validate input, response structs shape output. Never expose DB models directly.

---

## 5. Command Executor & Privileged Operations

### CommandExecutor Interface

```go
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
```

### Rules

- Never use `sh -c "string"`. Arguments always separated.
- Default timeout 30s, configurable per command.
- Context cancellation respected.
- All executions logged to audit.
- `RunSudo` prepends `/usr/bin/sudo` — only whitelisted commands.

### Sudo Whitelist (`/etc/sudoers.d/jenderal`)

```sudoers
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl start *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl stop *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl status *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/ufw *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/reboot
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/shutdown *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/hostnamectl set-hostname *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/timedatectl set-timezone *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get update
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get install -y *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/nginx -t
jenderal ALL=(ALL) NOPASSWD: /usr/bin/tee /etc/nginx/sites-available/*
jenderal ALL=(ALL) NOPASSWD: /usr/bin/ln -sf /etc/nginx/sites-available/* /etc/nginx/sites-enabled/*
jenderal ALL=(ALL) NOPASSWD: /usr/bin/rm /etc/nginx/sites-enabled/*
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/useradd *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/userdel *
```

### ServiceManager Interface

```go
type ServiceStatus struct {
    Name    string
    Active  bool
    Running bool
    Enabled bool
    Uptime  time.Duration
    PID     int
}

type ServiceManager interface {
    Start(ctx context.Context, name string) error
    Stop(ctx context.Context, name string) error
    Restart(ctx context.Context, name string) error
    Reload(ctx context.Context, name string) error
    Status(ctx context.Context, name string) (ServiceStatus, error)
}
```

Service name whitelist: `nginx`, `php*-fpm`, `mysql`, `postgresql`, `redis-server`, `jenderal`. Prevents arbitrary service manipulation.

---

## 6. System Monitoring & WebSocket Metrics

### SystemMetrics Struct

```go
type SystemMetrics struct {
    CPU       float64   // percentage 0-100
    RAMUsed   uint64
    RAMTotal  uint64
    SwapUsed  uint64
    SwapTotal uint64
    DiskUsed  uint64
    DiskTotal uint64
    Load1     float64
    Load5     float64
    Load15    float64
    NetRx     uint64    // bytes since boot
    NetTx     uint64
    Uptime    time.Duration
}
```

### Collection Strategy

- Background goroutine collects every 5s.
- Store to SQLite every 60s (configurable).
- In-memory ring buffer for latest 60 datapoints (5 min window).
- WebSocket streams from ring buffer, not DB.

### Retention

Cleanup goroutine runs daily. Default keep 7 days metrics. Configurable.

### Data Source

Read `/proc/stat`, `/proc/meminfo`, `/proc/loadavg`, `/proc/net/dev`, `/sys/block/*/stat` directly. No external dependency. Ubuntu-specific parsing.

### WebSocket `/ws/metrics`

```
Client connects → auth via cookie (same session)
Server pushes every 5s:
{
    "type": "metrics",
    "data": { cpu, ram, disk, net, load, uptime },
    "timestamp": "2026-09-07T18:00:00Z"
}
```

### Dashboard Aggregation `GET /api/v1/dashboard`

```json
{
    "server": {
        "hostname": "vps01",
        "ip": "203.0.113.10",
        "os": "Ubuntu 24.04 LTS",
        "kernel": "6.8.0-41-generic",
        "uptime": "14d 3h 22m"
    },
    "metrics": {
        "cpu": 23.5,
        "ram_used": 1073741824,
        "ram_total": 4294967296,
        "disk_used": 10737418240,
        "disk_total": 42949672960,
        "load": [0.45, 0.38, 0.32]
    },
    "services": {
        "nginx": "running",
        "php8.4-fpm": "running",
        "mysql": "running",
        "postgresql": "stopped"
    },
    "counts": {
        "websites": 0,
        "databases": 0,
        "users": 1,
        "ssl_certificates": 0
    }
}
```

Counts are placeholders in Phase 1 — populated when respective modules are built in later phases.

---

## 7. Configuration & Logging

### Config File (`/etc/jenderal/jenderal.yaml`)

```yaml
server:
  host: "0.0.0.0"
  port: 8443
  tls:
    enabled: true
    cert: "/etc/jenderal/tls/cert.pem"
    key: "/etc/jenderal/tls/key.pem"

database:
  path: "/var/lib/jenderal/jenderal.db"

auth:
  session_ttl: "24h"
  rate_limit_login: 5        # per 15 min per IP
  rate_limit_api: 100        # per min per session
  argon2:
    memory: 65536            # 64MB
    iterations: 3
    parallelism: 2

metrics:
  collect_interval: "5s"
  store_interval: "60s"
  retention_days: 7

logging:
  level: "info"              # debug, info, warn, error
  file: "/var/log/jenderal/jenderal.log"
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

### Loading Priority

YAML file first, then environment variables override. Env prefix `JENDERAL_`, nested with underscore (e.g., `JENDERAL_SERVER_PORT=9443`).

### Validation

Config validated at startup. Missing required fields = fail fast with clear message. No silent defaults for security-sensitive values.

### Structured Logging (slog)

```go
logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
    Level: configLevel,
}))
```

### Log Format

```json
{
    "time": "2026-09-07T18:00:00.123Z",
    "level": "INFO",
    "msg": "website created",
    "module": "website",
    "action": "create",
    "request_id": "01J5KZP...",
    "user_id": "01J5KZN...",
    "domain": "example.com"
}
```

### Logging Principles

- `log/slog` from stdlib. No external logging library.
- Request ID injected via middleware, propagated through context.
- Sensitive data (passwords, tokens) never logged.
- Log rotation via max size + backup count.

---

## 8. Frontend (SvelteKit)

### Build & Embed

- SvelteKit builds with `adapter-static` → output to `web/build/`.
- Go embeds via `//go:embed web/build/*`.
- Chi serves embedded filesystem at `/` — SPA fallback to `index.html`.
- API routes `/api/v1/*` and `/ws/*` take priority over static files.

### Chart Library

uPlot — tiny footprint, fast rendering, purpose-built for time-series.

### Dark Mode

Tailwind `darkMode: 'class'`. Toggle stored in localStorage. Default dark.

### Key Components

- `Layout.svelte` — sidebar + topbar shell
- `MetricsChart.svelte` — CPU/RAM/Disk/Net charts via uPlot
- `ServiceBadge.svelte` — running/stopped status badge
- `Modal.svelte`, `Toast.svelte`, `ConfirmDialog.svelte` — UI primitives
- `DataTable.svelte` — sortable/paginated data tables

### Pages (Phase 1)

- Login
- Dashboard (server info + realtime metrics charts)
- Server info (hostname, OS, actions)
- Services (list, start/stop/restart)
- Users (CRUD)
- Audit logs (paginated table)
- Settings

---

## 9. Installer

### Flow (`scripts/install.sh`)

```
 1. Check root
 2. Detect OS (Ubuntu 22.04 / 24.04 only, exit otherwise)
 3. Check architecture (amd64 / arm64)
 4. Check minimum resources (1 CPU, 1GB RAM, 10GB disk)
 5. Check internet connectivity
 6. apt-get update && install deps (curl, wget, sqlite3, nginx, ufw)
 7. Create system user `jenderal` (no login shell)
 8. Create directories:
      /etc/jenderal/
      /var/lib/jenderal/
      /var/log/jenderal/
      /opt/jenderal/
 9. Download binary → /opt/jenderal/jenderal
10. Generate self-signed TLS cert (for initial HTTPS access)
11. Write default config /etc/jenderal/jenderal.yaml
12. Write sudoers /etc/sudoers.d/jenderal
13. Run migrations: /opt/jenderal/jenderal migrate
14. Prompt admin username + password (or generate random)
15. Create admin: /opt/jenderal/jenderal admin create
16. Install systemd unit /etc/systemd/system/jenderal.service
17. Enable + start service
18. Configure UFW: allow 22, 80, 443, 8443
19. Enable UFW (if not active)
20. Print:
      ✓ Jenderal Panel installed
      URL: https://<SERVER_IP>:8443
      Username: admin
      Password: <generated>
```

### Systemd Unit

```ini
[Unit]
Description=Jenderal Panel
After=network.target

[Service]
Type=simple
User=jenderal
Group=jenderal
ExecStart=/opt/jenderal/jenderal serve
WorkingDirectory=/opt/jenderal
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

### CLI Subcommands (Phase 1)

```
jenderal serve          # start HTTP server
jenderal migrate        # run DB migrations
jenderal admin create   # create admin user (interactive)
jenderal version        # print version
```

---

## 10. Dependency Injection & Wiring

Simple constructor injection in `main.go`. No DI framework.

```go
func main() {
    // 1. Load config
    cfg := config.Load("/etc/jenderal/jenderal.yaml")

    // 2. Setup logger
    logger := logging.New(cfg.Logging)

    // 3. Open database + run migrations
    db := database.Open(cfg.Database)
    database.Migrate(db)

    // 4. Create core services
    executor := executor.New(logger)
    serviceMgr := service.NewSystemd(executor)
    auditSvc := audit.NewService(db, logger)

    // 5. Create auth services
    authSvc := auth.NewService(db, cfg.Auth, logger)
    rbac := auth.NewRBAC(db)

    // 6. Create system services
    systemInfo := system.NewInfo(executor)
    metricsCollector := system.NewMetricsCollector(db, cfg.Metrics, logger)

    // 7. Build router
    r := api.NewRouter(api.Dependencies{
        Config:     cfg,
        Logger:     logger,
        Auth:       authSvc,
        RBAC:       rbac,
        Audit:      auditSvc,
        System:     systemInfo,
        Metrics:    metricsCollector,
        ServiceMgr: serviceMgr,
    })

    // 8. Start metrics collector
    metricsCollector.Start(ctx)

    // 9. Start server
    srv := server.New(cfg.Server, r, logger)
    srv.ListenAndServe(ctx)
}
```

### Graceful Shutdown

- `signal.NotifyContext` for SIGINT/SIGTERM.
- Context cancellation propagates to: HTTP server, metrics collector, WebSocket connections.
- HTTP server drain timeout: 30s.

### Error Handling Pattern

```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("create user %q: %w", username, err)
}

// Domain errors for API translation
type DomainError struct {
    Code    string  // "USER_EXISTS", "INVALID_CREDENTIALS"
    Message string  // user-friendly
    Err     error   // wrapped original
}
```

Service layer returns domain errors. Handler maps domain errors to HTTP status codes.

---

## 11. Testing Strategy

### Unit Tests

- `auth/service_test.go` — password hashing, session logic
- `auth/rbac_test.go` — permission checking
- `executor/command_test.go` — mock execution, timeout
- `system/metrics_test.go` — `/proc` parsing
- `api/middleware_test.go` — rate limit, auth middleware

### Integration Tests

- Auth flow: login → session → authorized request → logout
- Audit log recording
- Metrics storage + retrieval

### Mock Interfaces

- `CommandExecutor` — mock for testing without real sudo
- `ServiceManager` — mock systemd calls

### Build Gate

`go test ./...` and `go build ./...` must pass on every commit.

---

## Future Phases (Out of Scope for Phase 1)

- **Phase 2:** Server info page, detailed monitoring, Nginx management, firewall GUI
- **Phase 3:** Website CRUD, domain management, Nginx config generator, PHP-FPM
- **Phase 4:** Let's Encrypt SSL, certificate management, auto renewal
- **Phase 5:** Laravel deployment, Git deployment, Node.js, queue workers, cron
- **Phase 6:** MySQL/MariaDB, PostgreSQL, Redis management
- **Phase 7:** Docker containers, images, compose
- **Phase 8:** Backup module, S3, scheduling, retention
- **Phase 9:** Advanced monitoring, alerts, notifications, 2FA, API tokens, self-update
