# Jenderal Panel — Phase 3 (Website Management) Design Spec

## Overview

Phase 3 adds website CRUD with automated provisioning, domain management, Nginx vhost generation, PHP-FPM multi-version management, and per-website isolation via system users.

## Decisions

| Area | Decision |
|---|---|
| Provisioning | State machine (pending → installing → configuring → validating → active/failed) |
| PHP-FPM | Per-website pool with dedicated socket |
| Directory | /home/{web_user}/ with public/, logs/, tmp/ |
| App types | PHP, Static, Laravel (auto-detect artisan/composer.json) |
| Templates | Go text/template, stored as constants |
| Web user | web_{domain_underscored} (e.g. web_example_com) |
| Background worker | Channel-based goroutine, single consumer |

---

## 1. Database Schema

```sql
-- 007_websites.sql
CREATE TABLE IF NOT EXISTS websites (
    id            TEXT PRIMARY KEY,
    domain        TEXT NOT NULL UNIQUE,
    app_type      TEXT NOT NULL DEFAULT 'php',
    php_version   TEXT,
    document_root TEXT NOT NULL,
    web_user      TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    ssl_enabled   INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS domains (
    id         TEXT PRIMARY KEY,
    website_id TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL DEFAULT 'alias',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_domains_website_id ON domains(website_id);
CREATE INDEX IF NOT EXISTS idx_websites_status ON websites(status);
```

Status values: pending, installing, configuring, validating, active, failed, suspended, disabled.

---

## 2. Provisioning State Machine

### States

```
pending → installing → configuring → validating → active
                                                 ↘ failed
active → suspended (suspend)
suspended → active (unsuspend/enable)
active → disabled (disable)
disabled → active (enable)
```

### Provisioning Steps

**INSTALLING:**
1. Create system user: `useradd --system --home-dir /home/{web_user} --shell /usr/sbin/nologin {web_user}`
2. Create directories: public/, logs/, tmp/
3. Set ownership: `chown -R {web_user}:{web_user} /home/{web_user}`
4. Set permissions: 750 for home, 755 for public
5. Create placeholder index.html

**CONFIGURING:**
1. Generate Nginx vhost from template
2. Write to /etc/nginx/sites-available/{domain}
3. Symlink to sites-enabled
4. If PHP/Laravel: generate FPM pool config
5. Write to /etc/php/{ver}/fpm/pool.d/{domain}.conf

**VALIDATING:**
1. Run nginx -t
2. If invalid: rollback configs (remove vhost, remove pool), set status=FAILED with error
3. If valid: reload nginx, restart php{ver}-fpm
4. Set status=ACTIVE

### Rollback

On failure: remove nginx config, remove FPM pool, disable site. Keep user and directories (may contain uploaded files). Store error in error_message column.

### Background Worker

```go
type Provisioner struct {
    db       *sql.DB
    exec     executor.CommandExecutor
    queue    chan string
    // ... dependencies
}

func (p *Provisioner) Start(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done(): return
            case websiteID := <-p.queue:
                p.provision(ctx, websiteID)
            }
        }
    }()
}
```

Retry: reset status to pending, re-queue. Each step is idempotent.

### Suspend/Disable

- **Suspend:** Rename nginx config to .suspended, reload. FPM pool stays. Website files intact.
- **Unsuspend:** Rename back, reload.
- **Disable:** Remove sites-enabled symlink, reload. Config stays in sites-available.
- **Enable:** Re-symlink, reload.

### Delete

1. Remove nginx config (sites-available + sites-enabled)
2. Remove FPM pool config
3. Reload nginx, restart php-fpm
4. Remove system user: `userdel {web_user}`
5. Optionally remove home directory (confirm with user)
6. Delete DB records (CASCADE removes domains)

---

## 3. Nginx Vhost Templates

### PHP Template

```nginx
server {
    listen 80;
    server_name {{.Domain}} {{.Aliases}};

    root {{.DocumentRoot}};
    index index.php index.html;

    access_log {{.LogDir}}/access.log;
    error_log {{.LogDir}}/error.log;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        fastcgi_pass unix:/run/php/php{{.PHPVersion}}-fpm-{{.Domain}}.sock;
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
        include fastcgi_params;
    }

    location ~ /\.(?!well-known) {
        deny all;
    }
}
```

### Static Template

Same but without `location ~ \.php$` block.

### Laravel Variant

Same as PHP but document root is `{home}/public`.

### Template Data

```go
type VhostData struct {
    Domain       string
    Aliases      string
    DocumentRoot string
    LogDir       string
    PHPVersion   string
    AppType      string
}
```

---

## 4. PHP-FPM Pool Template

```ini
[{{.Domain}}]
user = {{.WebUser}}
group = {{.WebUser}}
listen = /run/php/php{{.PHPVersion}}-fpm-{{.Domain}}.sock
listen.owner = www-data
listen.group = www-data

pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3

php_admin_value[open_basedir] = {{.HomeDir}}:/tmp
php_admin_value[upload_tmp_dir] = {{.HomeDir}}/tmp
php_admin_value[session.save_path] = {{.HomeDir}}/tmp
php_admin_value[error_log] = {{.LogDir}}/php-error.log
```

---

## 5. PHP Version Management

### Service (`internal/php/service.go`)

```go
type PHPVersion struct {
    Version   string `json:"version"`
    Installed bool   `json:"installed"`
    Running   bool   `json:"running"`
    Enabled   bool   `json:"enabled"`
}
```

Operations:
- `ListInstalled(ctx) ([]PHPVersion, error)` — ls /etc/php/, check systemd status
- `Install(ctx, version) error` — add PPA if needed, apt-get install php{ver}-fpm + extensions
- `Uninstall(ctx, version) error` — apt-get remove
- `Start/Stop/Restart(ctx, version) error` — systemctl
- `GetPHPINI/SavePHPINI(ctx, version, content) error` — read/write with restart

Default extensions: mysql, pgsql, mbstring, xml, curl, zip, gd, intl, bcmath.

Supported versions: 8.1, 8.2, 8.3, 8.4.

---

## 6. Website Service

### Service (`internal/website/service.go`)

```go
type WebsiteService struct {
    db          *sql.DB
    provisioner *Provisioner
    audit       *audit.Service
}
```

Operations:
- `Create(ctx, CreateRequest) (Website, error)` — insert DB, queue provisioning
- `Get(ctx, id) (Website, error)` — with domains
- `List(ctx) ([]Website, error)`
- `Update(ctx, id, UpdateRequest) error` — change PHP version, document root
- `Delete(ctx, id, removeFiles bool) error` — deprovision + delete records
- `Suspend/Enable(ctx, id) error` — update status, modify nginx config
- `Retry(ctx, id) error` — reset failed to pending, re-queue
- `GetConfig(ctx, id) (string, error)` — read current nginx vhost
- `SaveConfig(ctx, id, content) error` — custom nginx edit with validate
- `AddDomain(ctx, websiteID, name, type) error`
- `RemoveDomain(ctx, websiteID, domainID) error`
- `Count(ctx) (int, error)` — for dashboard

### Web User Naming

`example.com` → `web_example_com`
`sub.example.com` → `web_sub_example_com`

Replace dots and hyphens with underscores, prefix `web_`.

### Laravel Detection

After provisioning, if `app_type == "laravel"`:
- Check if `{document_root}/../artisan` exists
- Set document_root to `{home}/public`

---

## 7. API Routes

### Website

```
POST   /api/v1/websites                     → create
GET    /api/v1/websites                     → list
GET    /api/v1/websites/{id}                → get with domains
PUT    /api/v1/websites/{id}                → update
DELETE /api/v1/websites/{id}?remove_files=true → delete
POST   /api/v1/websites/{id}/suspend        → suspend
POST   /api/v1/websites/{id}/enable         → enable/unsuspend
POST   /api/v1/websites/{id}/retry          → retry failed
GET    /api/v1/websites/{id}/config         → nginx vhost
PUT    /api/v1/websites/{id}/config         → edit nginx vhost
GET    /api/v1/websites/{id}/logs/access?lines=100
GET    /api/v1/websites/{id}/logs/error?lines=100
POST   /api/v1/websites/{id}/domains        → add domain
DELETE /api/v1/websites/{id}/domains/{did}  → remove domain
```

### PHP

```
GET    /api/v1/php                          → list versions
POST   /api/v1/php/{version}/install
POST   /api/v1/php/{version}/uninstall
POST   /api/v1/php/{version}/restart
GET    /api/v1/php/{version}/config         → php.ini
PUT    /api/v1/php/{version}/config
```

---

## 8. RBAC Permissions

```
websites.view, websites.create, websites.update, websites.delete, websites.suspend
php.view, php.manage, php.config
```

Admin: all. User: websites.view, php.view.

---

## 9. Frontend

### Websites Page (`/websites`)
- List table: domain, app type, PHP version, status badge (color-coded), actions
- Create button → form: domain, app type dropdown, PHP version dropdown
- Status polling: if pending/installing/configuring, poll every 2s until active/failed
- Per-website detail: domains list, config editor, log viewer

### PHP Page (`/php`)
- Grid of PHP versions (8.1-8.4) with install/installed badge
- Install/Uninstall buttons
- Restart button per version
- php.ini editor per version

### Sidebar Updates
Add Websites after Dashboard, add PHP after Nginx.

---

## 10. File Structure

```
internal/
├── website/
│   ├── service.go
│   ├── service_test.go
│   ├── provisioner.go
│   ├── provisioner_test.go
│   ├── templates.go
│   ├── templates_test.go
│   └── handler.go
├── php/
│   ├── service.go
│   ├── service_test.go
│   └── handler.go
├── database/migrations/
│   └── 007_websites.sql
```

Dashboard `counts.websites` populated from `website.Count()`.
