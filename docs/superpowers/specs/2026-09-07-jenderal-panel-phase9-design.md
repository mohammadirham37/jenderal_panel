# Jenderal Panel — Phase 9 (Advanced) Design Spec

## Overview

Advanced monitoring with alerts, notification channels, 2FA/TOTP, API tokens, self-update mechanism, file manager, web terminal.

## 1. Database Schema

```sql
-- 015_alerts.sql
CREATE TABLE IF NOT EXISTS alert_rules (
    id          TEXT PRIMARY KEY,
    metric      TEXT NOT NULL,       -- cpu, ram, disk, ssl_expiry, service_down
    operator    TEXT NOT NULL,       -- gt, lt, eq
    threshold   REAL NOT NULL,
    duration_s  INTEGER DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS alert_history (
    id         TEXT PRIMARY KEY,
    rule_id    TEXT REFERENCES alert_rules(id) ON DELETE CASCADE,
    metric     TEXT NOT NULL,
    value      REAL NOT NULL,
    message    TEXT NOT NULL,
    resolved   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_channels (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,        -- email, telegram, discord, webhook
    config     TEXT NOT NULL,        -- JSON
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- 016_api_tokens.sql
CREATE TABLE IF NOT EXISTS api_tokens (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    last_used  TEXT,
    expires_at TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id);

-- 017_totp.sql
ALTER TABLE users ADD COLUMN totp_secret TEXT;
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
```

Note: SQLite ALTER ADD COLUMN works. For totp, use separate migration.

## 2. Alert System

### Service (`internal/alert/service.go`)
- CRUD alert rules (CPU>90%, RAM>90%, Disk>85%, SSL<14 days, service down)
- Background checker: every 60s, evaluate rules against current metrics
- If triggered: insert alert_history, send notifications via channels
- Auto-resolve when condition clears

### Notification Channels (`internal/notification/service.go`)
- Channel interface: Send(ctx, message) error
- Implementations: Webhook (POST JSON), Email (net/smtp), Telegram (HTTP API), Discord (webhook URL)
- CRUD channels, test channel

## 3. 2FA/TOTP

### Service (`internal/auth/totp.go`)
- GenerateSecret(userID) — generate TOTP secret, return QR code URL
- EnableTOTP(userID, code) — verify code, set totp_enabled=1
- DisableTOTP(userID) — clear secret, set totp_enabled=0
- VerifyTOTP(userID, code) bool — validate current code
- Login flow: if totp_enabled, require additional code after password

## 4. API Tokens

### Service (`internal/auth/apitoken.go`)
- CreateToken(userID, name) — generate random token, hash with SHA256, store hash, return plain token once
- ListTokens(userID)
- DeleteToken(id)
- ValidateToken(token) (User, error) — hash incoming, lookup
- Auth middleware: check Authorization Bearer header before session cookie

## 5. Self-Update

### Service (`internal/update/service.go`)
- CheckUpdate(ctx) (UpdateInfo, error) — check GitHub releases API
- PerformUpdate(ctx) error — download binary, verify checksum, backup current, replace, restart via systemd

## 6. File Manager

### Service (`internal/filemanager/service.go`)
- Browse(ctx, websiteID, path) — ls with sandboxing
- Read/Write/Delete/Rename/CreateDir
- Upload/Download
- Path validation: sandbox to website home, no traversal
- Permissions display and edit (chmod/chown)

## 7. Web Terminal

### Handler (`internal/terminal/handler.go`)
- WebSocket PTY — admin only, audit every session
- Session timeout, rate limit
- Uses os/exec with PTY for interactive shell

## 8. API Routes

```
# Alerts
GET/POST   /api/v1/alert-rules
GET/PUT/DELETE /api/v1/alert-rules/{id}
GET        /api/v1/alert-history

# Notifications
GET/POST   /api/v1/notification-channels
PUT/DELETE /api/v1/notification-channels/{id}
POST       /api/v1/notification-channels/{id}/test

# 2FA
POST       /api/v1/auth/totp/setup
POST       /api/v1/auth/totp/enable
POST       /api/v1/auth/totp/disable
POST       /api/v1/auth/totp/verify

# API Tokens
GET/POST   /api/v1/api-tokens
DELETE     /api/v1/api-tokens/{id}

# Self-Update
GET        /api/v1/update/check
POST       /api/v1/update/perform

# File Manager
GET        /api/v1/websites/{id}/files?path=/
POST       /api/v1/websites/{id}/files/create
PUT        /api/v1/websites/{id}/files/write
DELETE     /api/v1/websites/{id}/files/delete
POST       /api/v1/websites/{id}/files/rename
POST       /api/v1/websites/{id}/files/mkdir
POST       /api/v1/websites/{id}/files/upload
GET        /api/v1/websites/{id}/files/download?path=

# Terminal
WS         /ws/terminal
```

## 9. RBAC
```
alerts.view, alerts.manage
notifications.view, notifications.manage
api_tokens.manage
update.view, update.perform
files.view, files.manage
terminal.access
```

## 10. Frontend
- `/alerts` — rules table, history, notification channels
- `/notifications` — channel management
- Settings page: 2FA setup, API tokens
- File manager component in website detail
- Terminal page (xterm.js)
- Update notification banner in header

## 11. File Structure
```
internal/
├── alert/
│   ├── service.go, checker.go, handler.go
├── notification/
│   ├── service.go, channels.go, handler.go
├── filemanager/
│   ├── service.go, handler.go
├── terminal/
│   └── handler.go
├── update/
│   ├── service.go, handler.go
├── auth/
│   ├── totp.go, apitoken.go (additions)
```
