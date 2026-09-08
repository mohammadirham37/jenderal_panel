# Jenderal Panel

Open-source VPS control panel for Ubuntu Linux servers. Manage websites, databases, services, SSL certificates, Docker containers, and more through a modern web GUI.

Built with Go backend + SvelteKit frontend, deployed as a single binary.

## Features

### Server Management
- **Dashboard** — Real-time CPU, RAM, disk, network monitoring with WebSocket streaming
- **Service Manager** — Start/stop/restart systemd services (Nginx, PHP-FPM, MySQL, PostgreSQL, Redis)
- **Nginx Management** — Config editor with validation + rollback, site management, log viewer
- **Firewall (UFW)** — Rules management, allow/deny/limit by port/IP, SSH protection warnings
- **Process Manager** — Process list with CPU/RAM sort, kill with signal selection
- **Log Viewer** — Static + WebSocket streaming with path-validated log reading

### Website Management
- **Website CRUD** — Automated provisioning with state machine (pending → active/failed)
- **Per-website isolation** — Dedicated system user, home directory, PHP-FPM pool
- **App types** — PHP, Static, Laravel (auto-detect)
- **Domain management** — Primary domain, aliases, subdomains
- **Nginx vhost generation** — Auto-generated configs with validate + reload
- **PHP multi-version** — Install/manage PHP 8.1, 8.2, 8.3, 8.4 with per-site pool
- **File Manager** — Browse, edit, upload, download with path sandboxing

### SSL & Security
- **Let's Encrypt** — ACME certificate issue/renew/revoke via [lego](https://github.com/go-acme/lego)
- **Auto-renewal** — Background worker renews certificates 14 days before expiry
- **2FA/TOTP** — Optional two-factor authentication for user accounts
- **API Tokens** — Bearer token authentication for API access
- **RBAC** — 60 granular permissions across admin and user roles
- **CSRF Protection** — Double-submit cookie pattern
- **Audit Logging** — All operations logged with user, action, target, IP

### Application Deployment
- **Git Deployment** — Clone/pull repos, auto-detect app type, run build steps
- **Laravel Support** — Composer install, artisan migrate/cache commands
- **Node.js** — Install, manage apps with systemd, Nginx reverse proxy
- **Cron Jobs** — GUI cron management per website user
- **Queue Workers** — Systemd-based workers with auto-restart

### Database Management
- **MySQL** — Install, create/drop databases, user management, privileges
- **PostgreSQL** — Full management via psql/createdb/createuser
- **Redis** — Install, status, info, flush operations

### Docker
- **Containers** — List, start/stop/restart, remove, logs, inspect
- **Images** — Pull, remove
- **Volumes & Networks** — Create, remove
- **Docker Compose** — Up, down, status

### Backup
- **Website backup** — Tar website home directory
- **Database backup** — mysqldump / pg_dump
- **Config backup** — /etc/jenderal + /etc/nginx
- **Scheduled backups** — Cron-based with retention policies
- **Restore** — One-click restore from backup

### Monitoring & Alerts
- **Alert Rules** — CPU, RAM, disk, SSL expiry, service down thresholds
- **Notification Channels** — Webhook, Telegram, Discord, Email
- **Auto-resolve** — Alerts clear when condition resolves

### Other
- **Web Terminal** — WebSocket command execution (admin only)
- **Self-Update** — Check GitHub releases, one-click update
- **Dark Theme** — Modern dark UI with Tailwind CSS

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   SvelteKit UI                  │
│              (embedded via go:embed)            │
├─────────────────────────────────────────────────┤
│                  Chi HTTP Router                │
│         Session Auth + CSRF + RBAC              │
├─────────────────────────────────────────────────┤
│              Service Layer (Go)                 │
│  website │ nginx │ php │ ssl │ firewall │ ...   │
├─────────────────────────────────────────────────┤
│            Command Executor                     │
│         (sudo whitelist, no sh -c)              │
├─────────────────────────────────────────────────┤
│     SQLite (WAL)    │    Linux System           │
│   (internal state)  │ (systemd, ufw, nginx...) │
└─────────────────────────────────────────────────┘
```

- **Single binary** — Frontend embedded, no external runtime needed
- **SQLite** — Internal panel database, zero config
- **Sudo whitelist** — Privileged operations via `/etc/sudoers.d/jenderal`
- **No shell injection** — All commands use separated arguments, never `sh -c`

## Requirements

- Ubuntu 22.04 LTS or Ubuntu 24.04 LTS
- 1 CPU, 1 GB RAM, 10 GB disk (minimum)
- Root access

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/mohammadirham37/jenderal_panel/main/scripts/install.sh | sudo bash
```

After installation:

```
URL:      https://<SERVER_IP>:8443
Username: admin
Password: <generated during install>
```

## Manual Installation

### 1. Build from Source

```bash
# Requirements: Go 1.23+, Node.js 18+, npm

git clone https://github.com/mohammadirham37/jenderal_panel.git
cd jenderal_panel

# Build frontend + backend
make build

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 make build
```

### 2. Install on Server

```bash
# Copy binary to server
scp jenderal user@server:/tmp/

# On the server:
sudo mkdir -p /opt/jenderal /etc/jenderal /var/lib/jenderal /var/log/jenderal

# Create system user
sudo useradd --system --no-create-home --shell /usr/sbin/nologin jenderal

# Install binary
sudo cp /tmp/jenderal /opt/jenderal/jenderal
sudo chmod +x /opt/jenderal/jenderal

# Create config
sudo cp configs/jenderal.yaml.example /etc/jenderal/jenderal.yaml
# Edit config as needed:
sudo nano /etc/jenderal/jenderal.yaml

# Run migrations
sudo /opt/jenderal/jenderal migrate --config /etc/jenderal/jenderal.yaml

# Create admin user
sudo /opt/jenderal/jenderal admin create --config /etc/jenderal/jenderal.yaml

# Install systemd service
sudo cp systemd/jenderal.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable jenderal
sudo systemctl start jenderal
```

### 3. Setup Sudoers

```bash
# Install sudoers whitelist for privileged operations
sudo tee /etc/sudoers.d/jenderal << 'EOF'
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
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get remove -y *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/nginx -t
jenderal ALL=(ALL) NOPASSWD: /usr/bin/tee /etc/nginx/*
jenderal ALL=(ALL) NOPASSWD: /usr/bin/ln -sf /etc/nginx/sites-available/* /etc/nginx/sites-enabled/*
jenderal ALL=(ALL) NOPASSWD: /usr/bin/rm /etc/nginx/sites-enabled/*
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/useradd *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/userdel *
EOF
sudo chmod 440 /etc/sudoers.d/jenderal
```

### 4. Generate Self-Signed TLS (Optional)

```bash
sudo mkdir -p /etc/jenderal/tls
sudo openssl req -x509 -newkey rsa:4096 \
  -keyout /etc/jenderal/tls/key.pem \
  -out /etc/jenderal/tls/cert.pem \
  -days 365 -nodes \
  -subj "/CN=jenderal-panel"
sudo chown jenderal:jenderal /etc/jenderal/tls/*.pem
```

Update config to enable TLS:
```yaml
server:
  tls:
    enabled: true
    cert: "/etc/jenderal/tls/cert.pem"
    key: "/etc/jenderal/tls/key.pem"
```

## Configuration

Config file: `/etc/jenderal/jenderal.yaml`

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
  session_ttl: "24h"        # Session duration
  rate_limit_login: 5        # Login attempts per 15 min per IP
  rate_limit_api: 100        # API requests per min per session
  argon2:
    memory: 65536            # 64MB
    iterations: 3
    parallelism: 2

metrics:
  collect_interval: "5s"     # How often to collect metrics
  store_interval: "60s"      # How often to persist to DB
  retention_days: 7          # Days to keep metric history

logging:
  level: "info"              # debug, info, warn, error
  file: "/var/log/jenderal/jenderal.log"
  max_size_mb: 100
  max_backups: 3

services:
  allowed:                   # Services manageable through panel
    - nginx
    - "php*-fpm"
    - mysql
    - postgresql
    - redis-server
```

Environment variables override config values with `JENDERAL_` prefix:
```bash
JENDERAL_SERVER_PORT=9443
JENDERAL_LOGGING_LEVEL=debug
JENDERAL_DATABASE_PATH=/custom/path.db
```

## CLI Commands

```bash
jenderal serve              # Start the HTTP server
jenderal migrate            # Run database migrations
jenderal admin create       # Create admin user (interactive)
jenderal version            # Print version
```

Use `--config /path/to/config.yaml` to specify config file location.

## Development

```bash
# Prerequisites: Go 1.23+, Node.js 18+, npm, gcc (for CGO/SQLite)

# Clone
git clone https://github.com/mohammadirham37/jenderal_panel.git
cd jenderal_panel

# Install frontend dependencies
cd web && npm install && cd ..

# Run backend (dev mode)
make dev

# Run frontend dev server (separate terminal)
cd web && npm run dev

# Run tests
make test

# Lint
make lint

# Build
make build
```

### Project Structure

```
jenderal_panel/
├── cmd/jenderal/           # CLI entry point
├── internal/
│   ├── alert/              # Alert rules and checker
│   ├── api/                # Chi router, middleware
│   ├── audit/              # Audit logging
│   ├── auth/               # Auth, RBAC, TOTP, API tokens
│   ├── backup/             # Backup and scheduling
│   ├── config/             # YAML config loader
│   ├── cron/               # Cron job management
│   ├── database/           # SQLite + migrations
│   ├── dbmanager/          # MySQL, PostgreSQL, Redis
│   ├── deployment/         # Git deployment
│   ├── docker/             # Docker management
│   ├── executor/           # Command executor (sudo)
│   ├── filemanager/        # File browser
│   ├── firewall/           # UFW management
│   ├── httputil/           # JSON response helpers
│   ├── logging/            # Structured slog logger
│   ├── model/              # Domain models + errors
│   ├── nginx/              # Nginx config management
│   ├── nodejs/             # Node.js app management
│   ├── notification/       # Notification channels
│   ├── php/                # PHP version management
│   ├── process/            # Process list and kill
│   ├── queue/              # Queue worker management
│   ├── server/             # HTTP/TLS server
│   ├── service/            # Systemd service manager
│   ├── settings/           # Key-value settings
│   ├── ssl/                # Let's Encrypt / ACME
│   ├── system/             # System info, metrics, logs
│   ├── terminal/           # WebSocket terminal
│   ├── update/             # Self-update
│   ├── user/               # User CRUD
│   └── website/            # Website provisioning
├── web/                    # SvelteKit frontend
├── scripts/install.sh      # Ubuntu installer
├── systemd/                # Systemd unit file
├── configs/                # Example config
├── Makefile
└── go.mod
```

## Tech Stack

**Backend:**
- Go 1.23+
- [Chi](https://github.com/go-chi/chi) HTTP router
- SQLite with WAL mode ([mattn/go-sqlite3](https://github.com/mattn/go-sqlite3))
- [gorilla/websocket](https://github.com/gorilla/websocket) for real-time features
- [lego](https://github.com/go-acme/lego) ACME client for Let's Encrypt
- [pquerna/otp](https://github.com/pquerna/otp) for TOTP/2FA
- argon2id password hashing
- `log/slog` structured JSON logging

**Frontend:**
- SvelteKit with Svelte 5
- TypeScript
- Tailwind CSS v4 (dark theme)
- adapter-static (embedded into Go binary via `go:embed`)

## Security

- **No shell injection** — Commands use separated arguments via `os/exec`
- **Sudo whitelist** — Only predefined commands allowed via sudoers
- **Path sandboxing** — File manager restricted to website home directories
- **RBAC** — 60 permissions across modules, enforced at route level
- **Session security** — HttpOnly, Secure, SameSite=Strict cookies
- **CSRF** — Double-submit cookie with X-CSRF-Token header
- **Rate limiting** — Login and API rate limits per IP/session
- **Audit logging** — All operations recorded with user context
- **Argon2id** — Password hashing with configurable parameters
- **2FA/TOTP** — Optional two-factor authentication

## API

All endpoints under `/api/v1/`. Authentication via session cookie or `Authorization: Bearer <token>` header.

Response format:
```json
{"data": {...}, "meta": {"page": 1, "per_page": 20, "total": 100}}
```

Error format:
```json
{"error": {"code": "VALIDATION_ERROR", "message": "domain is required"}}
```

WebSocket endpoints:
- `/ws/metrics` — Real-time server metrics
- `/ws/logs?path=...` — Log file streaming
- `/ws/terminal` — Command execution (admin only)

## License

Open source. License TBD.

## Contributing

Contributions welcome. Please open an issue first to discuss changes.
