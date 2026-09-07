# Jenderal Panel — Phase 5 (Application Deployment) Design Spec

## Overview

Git deployment, Laravel support, Node.js management, queue workers, and cron job management.

## 1. Database Schema

```sql
-- 009_deployments.sql
CREATE TABLE IF NOT EXISTS deployments (
    id          TEXT PRIMARY KEY,
    website_id  TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    commit_hash TEXT,
    branch      TEXT NOT NULL DEFAULT 'main',
    status      TEXT NOT NULL DEFAULT 'pending',  -- pending, running, success, failed
    duration_ms INTEGER,
    log         TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployments_website_id ON deployments(website_id);

-- 010_cron_jobs.sql
CREATE TABLE IF NOT EXISTS cron_jobs (
    id         TEXT PRIMARY KEY,
    website_id TEXT REFERENCES websites(id) ON DELETE CASCADE,
    command    TEXT NOT NULL,
    schedule   TEXT NOT NULL,  -- cron expression
    enabled    INTEGER NOT NULL DEFAULT 1,
    last_run   TEXT,
    last_status TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- 011_queue_workers.sql
CREATE TABLE IF NOT EXISTS queue_workers (
    id           TEXT PRIMARY KEY,
    website_id   TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    command      TEXT NOT NULL,
    num_workers  INTEGER NOT NULL DEFAULT 1,
    auto_restart INTEGER NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'stopped',  -- running, stopped, failed
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- 012_nodejs_apps.sql
CREATE TABLE IF NOT EXISTS nodejs_apps (
    id           TEXT PRIMARY KEY,
    website_id   TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    node_version TEXT NOT NULL,
    package_mgr  TEXT NOT NULL DEFAULT 'npm',  -- npm, yarn, pnpm
    build_cmd    TEXT,
    start_cmd    TEXT NOT NULL,
    port         INTEGER NOT NULL,
    env_vars     TEXT,  -- JSON
    status       TEXT NOT NULL DEFAULT 'stopped',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
```

## 2. Git Deployment

### Service (`internal/deployment/service.go`)

Flow per deployment:
```
1. git clone/pull repo to website directory
2. checkout branch
3. detect app type (composer.json → PHP/Laravel, package.json → Node)
4. run build steps based on type:
   - PHP: composer install --no-dev
   - Laravel: composer install, php artisan migrate --force, php artisan config:cache, php artisan route:cache, php artisan view:cache
   - Node: npm/yarn/pnpm install, npm run build
5. set permissions
6. reload services
7. health check (HTTP request to domain)
```

Operations:
- `Deploy(ctx, websiteID, repo, branch) (Deployment, error)` — async, returns pending deployment
- `GetDeployment(ctx, id) (Deployment, error)`
- `ListByWebsite(ctx, websiteID) ([]Deployment, error)`
- `Rollback(ctx, websiteID, deploymentID) error` — git checkout previous commit

Deploy config stored in website settings or `.jenderal.yaml` in repo root.

### Git Repository Settings

Add to websites table or use settings:
```sql
ALTER TABLE websites ADD COLUMN git_repo TEXT;
ALTER TABLE websites ADD COLUMN git_branch TEXT DEFAULT 'main';
```

Actually, add via new migration rather than ALTER — SQLite ALTER is limited.

## 3. Cron Job Management

### Service (`internal/cron/service.go`)

- `Create(ctx, CronJob) error` — write to crontab via system crontab
- `List(ctx) / ListByWebsite(ctx, websiteID)`
- `Update/Delete/Enable/Disable`
- Crontab integration: write to `/var/spool/cron/crontabs/{web_user}` or use `crontab -u {web_user}`

Cron runs as website's web_user for isolation.

## 4. Queue Worker Management

### Service (`internal/queue/service.go`)

Queue workers managed via systemd units:
```
/etc/systemd/system/jenderal-queue-{id}.service
```

Each worker gets a systemd service:
```ini
[Unit]
Description=Jenderal Queue Worker {domain}

[Service]
User={web_user}
WorkingDirectory={document_root}
ExecStart={command}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Operations:
- `Create(ctx, QueueWorker) error` — write systemd unit, enable, start
- `Start/Stop/Restart(ctx, id) error`
- `Delete(ctx, id) error` — stop, disable, remove unit
- `Status(ctx, id) (string, error)` — systemctl status

## 5. Node.js Management

### Service (`internal/nodejs/service.go`)

- `Install(ctx, version) error` — install via NodeSource or nvm
- `ListVersions(ctx) ([]string, error)` — installed node versions
- `CreateApp(ctx, NodeApp) error` — configure reverse proxy, systemd service for node process
- `Start/Stop/Restart(ctx, id) error`
- Nginx reverse proxy to localhost:{port}

Node app runs via systemd:
```ini
[Service]
User={web_user}
WorkingDirectory={app_dir}
ExecStart=/usr/bin/node {start_script}
Environment=PORT={port}
Environment=NODE_ENV=production
```

## 6. API Routes

```
# Deployments
POST   /api/v1/websites/{id}/deploy     → trigger deployment {repo, branch}
GET    /api/v1/websites/{id}/deployments
GET    /api/v1/deployments/{id}
POST   /api/v1/deployments/{id}/rollback

# Cron
GET    /api/v1/cron-jobs
POST   /api/v1/cron-jobs
GET    /api/v1/cron-jobs/{id}
PUT    /api/v1/cron-jobs/{id}
DELETE /api/v1/cron-jobs/{id}
POST   /api/v1/cron-jobs/{id}/enable
POST   /api/v1/cron-jobs/{id}/disable

# Queue Workers
GET    /api/v1/queue-workers
POST   /api/v1/queue-workers
GET    /api/v1/queue-workers/{id}
DELETE /api/v1/queue-workers/{id}
POST   /api/v1/queue-workers/{id}/start
POST   /api/v1/queue-workers/{id}/stop
POST   /api/v1/queue-workers/{id}/restart

# Node.js
GET    /api/v1/nodejs/versions
POST   /api/v1/nodejs/install
GET    /api/v1/nodejs/apps
POST   /api/v1/nodejs/apps
GET    /api/v1/nodejs/apps/{id}
DELETE /api/v1/nodejs/apps/{id}
POST   /api/v1/nodejs/apps/{id}/start
POST   /api/v1/nodejs/apps/{id}/stop
POST   /api/v1/nodejs/apps/{id}/restart
```

## 7. RBAC

```
deployments.view, deployments.deploy
cron.view, cron.manage
queue.view, queue.manage
nodejs.view, nodejs.manage
```

## 8. Frontend

- `/deployments` — deployment history per website, deploy button, rollback
- `/cron` — cron jobs table, create/edit form with schedule builder
- `/queue-workers` — workers list, create, start/stop
- `/nodejs` — installed versions, apps list, create app form

## 9. File Structure

```
internal/
├── deployment/
│   ├── service.go
│   ├── service_test.go
│   └── handler.go
├── cron/
│   ├── service.go
│   ├── service_test.go
│   └── handler.go
├── queue/
│   ├── service.go
│   ├── service_test.go
│   └── handler.go
├── nodejs/
│   ├── service.go
│   ├── service_test.go
│   └── handler.go
├── database/migrations/
│   ├── 009_deployments.sql
│   ├── 010_cron_jobs.sql
│   ├── 011_queue_workers.sql
│   └── 012_nodejs_apps.sql
```
