# Jenderal Panel — Phase 4 (SSL/Let's Encrypt) Design Spec

## Overview

SSL certificate management via Let's Encrypt ACME. Issue, renew, revoke certificates. Auto-renewal background worker. Nginx HTTPS config generation.

## Decisions

| Area | Decision |
|---|---|
| ACME client | github.com/go-acme/lego (HTTP-01 challenge) |
| Challenge | HTTP-01 via webroot (/.well-known/acme-challenge/) |
| Storage | Certs on disk /etc/jenderal/ssl/{domain}/, metadata in DB |
| Auto-renewal | Background goroutine, check daily, renew 14 days before expiry |
| Nginx integration | Generate HTTPS server block, redirect HTTP→HTTPS |

## 1. Database Schema

```sql
-- 008_ssl_certificates.sql
CREATE TABLE IF NOT EXISTS ssl_certificates (
    id           TEXT PRIMARY KEY,
    website_id   TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    domain       TEXT NOT NULL,
    issuer       TEXT NOT NULL DEFAULT 'letsencrypt',
    status       TEXT NOT NULL DEFAULT 'pending',  -- pending, active, expired, revoked, failed
    expires_at   TEXT,
    auto_renew   INTEGER NOT NULL DEFAULT 1,
    error_message TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ssl_certificates_website_id ON ssl_certificates(website_id);
CREATE INDEX IF NOT EXISTS idx_ssl_certificates_expires_at ON ssl_certificates(expires_at);
```

## 2. SSL Service

```go
type SSLService struct {
    db     *sql.DB
    exec   executor.CommandExecutor
    audit  *audit.Service
    certDir string  // /etc/jenderal/ssl/
}
```

Operations:
- `Issue(ctx, websiteID, domain) error` — ACME HTTP-01 challenge, save cert+key to disk, update DB, update nginx config to HTTPS
- `Renew(ctx, certID) error` — re-issue, replace files, reload nginx
- `Revoke(ctx, certID) error` — ACME revoke, remove cert, revert nginx to HTTP
- `Get(ctx, certID) (SSLCertificate, error)`
- `ListByWebsite(ctx, websiteID) ([]SSLCertificate, error)`
- `Count(ctx) (int, error)` — for dashboard

### ACME Flow

```
1. Create lego ACME client (register if first time)
2. Store account key in /etc/jenderal/ssl/account/
3. HTTP-01: write challenge to {website_docroot}/.well-known/acme-challenge/
4. Obtain certificate
5. Save cert.pem + key.pem to /etc/jenderal/ssl/{domain}/
6. Update nginx vhost: add listen 443 ssl, ssl_certificate paths, HTTP→HTTPS redirect
7. Reload nginx
8. Update DB record: status=active, expires_at
```

### Nginx HTTPS Template Addition

```nginx
server {
    listen 80;
    server_name {{.Domain}} {{.Aliases}};
    
    location /.well-known/acme-challenge/ {
        root {{.DocumentRoot}};
    }
    
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    server_name {{.Domain}} {{.Aliases}};
    
    ssl_certificate /etc/jenderal/ssl/{{.Domain}}/cert.pem;
    ssl_certificate_key /etc/jenderal/ssl/{{.Domain}}/key.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    
    root {{.DocumentRoot}};
    # ... rest of config same as HTTP
}
```

## 3. Auto-Renewal Worker

```go
type RenewalWorker struct {
    svc *SSLService
}
```

- `Start(ctx)` — goroutine, ticker every 24h
- Check all certificates where `auto_renew=1` and `expires_at < now + 14 days`
- Renew each, log results
- On failure: log error, set error_message, try again next day

## 4. API Routes

```
POST   /api/v1/ssl/issue              → issue cert for website {website_id, domain}
GET    /api/v1/ssl                    → list all certificates
GET    /api/v1/ssl/{id}               → get certificate details
POST   /api/v1/ssl/{id}/renew         → manual renew
POST   /api/v1/ssl/{id}/revoke        → revoke
DELETE /api/v1/ssl/{id}               → remove cert, revert to HTTP
```

## 5. RBAC

`ssl.view`, `ssl.manage` — admin all, user view only.

## 6. Frontend

### SSL Page (`/ssl`)
- Certificates table: domain, issuer, status badge, expires_at, auto_renew toggle
- Issue button: select website from dropdown, confirm
- Per-cert actions: Renew, Revoke, Delete
- Expiry warning: yellow for <30 days, red for <7 days

## 7. File Structure

```
internal/ssl/
├── service.go        # ACME client, issue/renew/revoke
├── service_test.go
├── renewal.go        # Auto-renewal background worker
└── handler.go        # API handlers
internal/database/migrations/
└── 008_ssl_certificates.sql
```

Website templates.go updated: add HTTPS vhost variant.
Dashboard: ssl_certificates count populated.
