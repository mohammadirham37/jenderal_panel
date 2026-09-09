# Website Detail Deployment Enhancement Design Spec

## Overview

Enhance website detail page with multiple deployment methods (Git with deploy keys, file upload, SSH manual), framework-specific command buttons, and embedded terminal scoped to website context.

## Decisions

| Area | Decision |
|---|---|
| Deploy key | Per-website ED25519 in /home/{web_user}/.ssh/ |
| Git providers | GitHub, GitLab, Bitbucket, Custom |
| Framework commands | Laravel, CodeIgniter 4, generic PHP/Composer, generic Node.js |
| Terminal | Embedded WS terminal as web_user, auto-cd to document root |
| Upload | ZIP/tar.gz extract to document root |

## 1. Deploy Key Management

### Backend (`internal/website/deploykey.go`)

```go
func (s *Service) GenerateDeployKey(ctx, websiteID) (publicKey string, error)
func (s *Service) GetDeployKey(ctx, websiteID) (publicKey string, exists bool, error)
func (s *Service) DeleteDeployKey(ctx, websiteID) error
```

Generate: `ssh-keygen -t ed25519 -f /home/{web_user}/.ssh/deploy_key -N "" -C "jenderal-{domain}"`

Write `/home/{web_user}/.ssh/config`:
```
Host github.com gitlab.com bitbucket.org
    IdentityFile ~/.ssh/deploy_key
    StrictHostKeyChecking no
```

Set permissions: `.ssh/` 700, files 600, owned by web_user.

Deploy uses `GIT_SSH_COMMAND="ssh -i /home/{web_user}/.ssh/deploy_key -o StrictHostKeyChecking=no"`.

### API Routes

```
POST   /api/v1/websites/{id}/deploy-key    → generate, return public key
GET    /api/v1/websites/{id}/deploy-key    → get public key (or 404)
DELETE /api/v1/websites/{id}/deploy-key    → delete keypair
```

### Database

Add column to websites table:
```sql
ALTER TABLE websites ADD COLUMN git_repo TEXT DEFAULT '';
ALTER TABLE websites ADD COLUMN git_branch TEXT DEFAULT 'main';
ALTER TABLE websites ADD COLUMN git_provider TEXT DEFAULT '';
```

## 2. Framework Command Execution

### Backend (`internal/website/commands.go`)

```go
func (s *Service) RunCommand(ctx, websiteID, command string) (taskID string, error)
func (s *Service) GetCommandPresets(ctx, websiteID) ([]CommandPreset, error)
```

RunCommand: validate command against whitelist, run via TaskRunner as web_user in document root.

Command whitelist — only allow predefined commands, no arbitrary shell:
```go
var allowedCommands = map[string][]string{
    "composer install":           {"composer", "install", "--no-interaction"},
    "composer update":            {"composer", "update", "--no-interaction"},
    "composer dump-autoload":     {"composer", "dump-autoload"},
    "php artisan key:generate":   {"php", "artisan", "key:generate"},
    "php artisan migrate":        {"php", "artisan", "migrate", "--force"},
    "php artisan migrate:fresh":  {"php", "artisan", "migrate:fresh", "--force"},
    "php artisan migrate:rollback": {"php", "artisan", "migrate:rollback"},
    "php artisan db:seed":        {"php", "artisan", "db:seed", "--force"},
    "php artisan config:cache":   {"php", "artisan", "config:cache"},
    "php artisan route:cache":    {"php", "artisan", "route:cache"},
    "php artisan view:cache":     {"php", "artisan", "view:cache"},
    "php artisan cache:clear":    {"php", "artisan", "cache:clear"},
    "php artisan optimize":       {"php", "artisan", "optimize"},
    "php artisan storage:link":   {"php", "artisan", "storage:link"},
    "php artisan queue:restart":  {"php", "artisan", "queue:restart"},
    "php spark migrate":          {"php", "spark", "migrate"},
    "php spark migrate:rollback": {"php", "spark", "migrate:rollback"},
    "php spark db:seed":          {"php", "spark", "db:seed"},
    "php spark cache:clear":      {"php", "spark", "cache:clear"},
    "npm install":                {"npm", "install"},
    "npm run build":              {"npm", "run", "build"},
    "npm run dev":                {"npm", "run", "dev"},
    "yarn install":               {"yarn", "install"},
    "yarn build":                 {"yarn", "build"},
    "pnpm install":               {"pnpm", "install"},
    "pnpm build":                 {"pnpm", "build"},
}
```

CommandPreset struct:
```go
type CommandPreset struct {
    Label    string `json:"label"`
    Command  string `json:"command"`
    Category string `json:"category"` // composer, artisan, npm, spark
    Danger   bool   `json:"danger"`   // migrate:fresh, etc
}
```

GetCommandPresets: return commands based on website framework field.

### API Routes

```
POST /api/v1/websites/{id}/run-command      → {command: "php artisan migrate"} → {task_id}
GET  /api/v1/websites/{id}/command-presets   → []CommandPreset
```

## 3. File Upload Deployment

### Backend

```go
func (s *Service) UploadArchive(ctx, websiteID, file multipart.File, filename string) (taskID string, error)
```

- Accept ZIP or tar.gz (max 500MB)
- Save to /tmp, extract to document root as web_user
- Cleanup temp file after extract

### API Route

```
POST /api/v1/websites/{id}/upload-deploy    → multipart file → {task_id}
```

## 4. Embedded Terminal

### Backend Modification (`internal/terminal/handler.go`)

Add `website_id` query param support:
- If `website_id` present: lookup website, get web_user and document_root
- Run commands as web_user (not root) using `sudo -u {web_user} -i`
- Auto-cd to document_root on session start
- If no `website_id`: existing admin terminal behavior

### WebSocket URL

```
/ws/terminal?website_id={id}
```

## 5. Update Deployment Service

Modify existing `internal/deployment/service.go`:
- Use deploy key if exists: set `GIT_SSH_COMMAND` env
- Store `git_repo` and `git_branch` on website record for re-deploy
- After git operations, run framework-specific post-deploy commands

## 6. Frontend

### Website Detail Page Tabs

```
[Overview] [Deployment] [Files] [Terminal] [Logs] [Config] [Domains]
```

### Deployment Tab Layout

**Git sub-section:**
- Provider select (GitHub/GitLab/Bitbucket/Custom)
- Repo URL input, Branch input
- Repo visibility toggle (Public/Private)
- If Private: "Generate Deploy Key" button → show public key copyable box + per-provider instructions
- "Deploy Now" button
- Deployment history table below

**Upload sub-section:**
- Drag & drop zone for ZIP/tar.gz
- Upload progress bar
- Extract status via TaskProgress

**Manual sub-section:**
- SSH info card: `ssh {web_user}@{server_ip}`
- Document root path (copyable)
- "Open Terminal" button → switches to Terminal tab

### Command Buttons Section (below deployment)

Grid of buttons grouped by category:
- **Composer**: install, update, dump-autoload
- **Artisan** (Laravel): migrate, migrate:fresh, db:seed, key:generate, config:cache, route:cache, view:cache, cache:clear, optimize, storage:link, queue:restart
- **Spark** (CodeIgniter): migrate, db:seed, cache:clear
- **NPM/Yarn/Pnpm**: install, build, dev

Dangerous commands (migrate:fresh) shown in red with confirm dialog.
Each button click → POST run-command → show TaskProgress with output.

### Terminal Tab

Embedded WebSocket terminal component:
- Connect to `/ws/terminal?website_id={id}`
- Full-height panel with monospace font
- Auto-connect on tab switch

## 7. File Structure

```
internal/website/
├── deploykey.go        # Deploy key generate/get/delete
├── commands.go         # Command presets and run-command
├── upload.go           # Archive upload and extract
├── service.go          # (modify) add git_repo/branch fields
├── handler.go          # (modify) add new endpoints
```

No new database migration needed — git_repo/branch/provider can be added via ALTER TABLE in new migration.

## 8. New Migration

```sql
-- 020_website_git.sql
ALTER TABLE websites ADD COLUMN git_repo TEXT DEFAULT '';
ALTER TABLE websites ADD COLUMN git_branch TEXT DEFAULT 'main';
ALTER TABLE websites ADD COLUMN git_provider TEXT DEFAULT '';
```
