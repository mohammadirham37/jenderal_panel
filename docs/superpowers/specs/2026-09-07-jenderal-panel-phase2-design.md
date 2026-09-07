# Jenderal Panel — Phase 2 (Server Management) Design Spec

## Overview

Phase 2 adds Nginx management, firewall (UFW) GUI, process list, log streaming, and enhanced server monitoring. Builds on Phase 1 foundation (executor, service manager, auth, RBAC, audit, WebSocket).

**Scope:** Nginx config management, UFW firewall with advanced rules, process list with kill, hybrid log viewer (static + WebSocket streaming), enhanced server info page.

**No new database tables.** All operations are stateless OS commands + existing audit log.

---

## Decisions

| Area | Decision |
|---|---|
| Nginx | Install, status, start/stop/reload, config editing with validation + backup, site management, logs |
| Firewall | Advanced — port + IP rules, app profiles, rate limit (ufw limit), enable/disable, SSH warning |
| Log viewer | Hybrid — static tail + WebSocket streaming toggle |
| Process list | ps aux with sort, kill with signal whitelist |
| Architecture | Extend Phase 1 pattern: service + handler + router wiring |

---

## 1. Nginx Management

### Service (`internal/nginx/service.go`)

```go
type NginxService struct {
    executor executor.CommandExecutor
    svcMgr   service.ServiceManager
    audit    *audit.Service
}

type NginxStatus struct {
    Installed bool   `json:"installed"`
    Running   bool   `json:"running"`
    Enabled   bool   `json:"enabled"`
    Version   string `json:"version"`
    PID       int    `json:"pid"`
    ConfigOK  bool   `json:"config_ok"`
}

type SiteConfig struct {
    Name    string `json:"name"`
    Enabled bool   `json:"enabled"`
    Path    string `json:"path"`
}
```

### Operations

- `Install(ctx) error` — apt-get install -y nginx
- `Status(ctx) (NginxStatus, error)` — combine systemctl status + nginx -v + nginx -t
- `Start/Stop/Restart/Reload(ctx) error` — delegate to ServiceManager
- `GetMainConfig(ctx) (string, error)` — read /etc/nginx/nginx.conf
- `SaveMainConfig(ctx, content) error` — backup → write → nginx -t → reload or rollback
- `ListSites(ctx) ([]SiteConfig, error)` — ls sites-available, check sites-enabled symlinks
- `GetSiteConfig(ctx, name) (string, error)` — read /etc/nginx/sites-available/{name}
- `SaveSiteConfig(ctx, name, content) error` — backup → write → nginx -t → reload or rollback
- `EnableSite(ctx, name) error` — symlink sites-available → sites-enabled
- `DisableSite(ctx, name) error` — remove symlink from sites-enabled
- `DeleteSite(ctx, name) error` — remove from sites-available + sites-enabled
- `TestConfig(ctx) (bool, string, error)` — nginx -t, return pass/fail + output
- `GetAccessLog(ctx, lines) (string, error)` — tail /var/log/nginx/access.log
- `GetErrorLog(ctx, lines) (string, error)` — tail /var/log/nginx/error.log

### Config Edit Safety Flow

```
1. Backup current file to .bak
2. Write new content via sudo tee
3. Run nginx -t
4. If valid → nginx reload
5. If invalid → restore .bak → return error with nginx -t output
```

### API Routes

```
GET    /api/v1/nginx/status
POST   /api/v1/nginx/install
POST   /api/v1/nginx/start
POST   /api/v1/nginx/stop
POST   /api/v1/nginx/restart
POST   /api/v1/nginx/reload
POST   /api/v1/nginx/test
GET    /api/v1/nginx/config
PUT    /api/v1/nginx/config
GET    /api/v1/nginx/sites
GET    /api/v1/nginx/sites/{name}
PUT    /api/v1/nginx/sites/{name}
POST   /api/v1/nginx/sites/{name}/enable
POST   /api/v1/nginx/sites/{name}/disable
DELETE /api/v1/nginx/sites/{name}
GET    /api/v1/nginx/logs/access?lines=100
GET    /api/v1/nginx/logs/error?lines=100
```

### RBAC Permissions

- `nginx.view` — status, read configs, read logs
- `nginx.manage` — start/stop/restart/reload, install, enable/disable sites
- `nginx.config` — edit nginx.conf and site configs

---

## 2. Firewall (UFW)

### Service (`internal/firewall/service.go`)

```go
type FirewallService struct {
    executor executor.CommandExecutor
    audit    *audit.Service
}

type FirewallStatus struct {
    Active  bool           `json:"active"`
    Default string         `json:"default"`
    Rules   []FirewallRule `json:"rules"`
}

type FirewallRule struct {
    Number  int    `json:"number"`
    To      string `json:"to"`
    Action  string `json:"action"`
    From    string `json:"from"`
    Comment string `json:"comment"`
}

type AddRuleRequest struct {
    Port     int    `json:"port"`
    Protocol string `json:"protocol"`  // tcp, udp, both
    Action   string `json:"action"`    // allow, deny, limit
    From     string `json:"from"`      // IP/CIDR or empty (Anywhere)
    Comment  string `json:"comment"`
}
```

### Operations

- `Status(ctx) (FirewallStatus, error)` — parse `ufw status numbered verbose`
- `Enable(ctx) error` — `ufw --force enable` (with SSH warning check)
- `Disable(ctx) error` — `ufw disable` (with SSH warning)
- `AddRule(ctx, AddRuleRequest) error` — construct ufw command from request fields
- `DeleteRule(ctx, number int, force bool) error` — `ufw --force delete <number>`

### UFW Command Mapping

```
ufw status numbered verbose                     → Status
ufw --force enable / ufw disable                → Enable/Disable
ufw allow 80/tcp comment "HTTP"                 → AddRule (allow port)
ufw deny from 1.2.3.4 to any port 22           → AddRule (deny IP)
ufw limit 22/tcp comment "SSH rate limit"       → AddRule (limit)
ufw --force delete 3                            → DeleteRule
```

### SSH Protection

- Before `DeleteRule`: check if rule covers port 22. If yes and `force=false`, return error: "Deleting SSH rule may lock you out. Use force=true to confirm."
- Before `Disable`: warn about SSH access loss.
- Frontend shows confirmation dialog with red warning for SSH-affecting operations.

### UFW Status Parser

Parse numbered output format:
```
Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere                  # SSH
[ 2] 80/tcp                     ALLOW IN    Anywhere                  # HTTP
[ 3] 443/tcp                    ALLOW IN    Anywhere                  # HTTPS
```

Regex: `\[\s*(\d+)\]\s+(.+?)\s+(ALLOW|DENY|LIMIT|REJECT)\s+IN\s+(.+?)(?:\s+#\s+(.*))?$`

### API Routes

```
GET    /api/v1/firewall/status
POST   /api/v1/firewall/enable
POST   /api/v1/firewall/disable
GET    /api/v1/firewall/rules
POST   /api/v1/firewall/rules
DELETE /api/v1/firewall/rules/{number}?force=true
```

### RBAC Permissions

- `firewall.view` — status, list rules
- `firewall.manage` — enable/disable, add/delete rules

---

## 3. Process List

### Service (`internal/process/service.go`)

```go
type ProcessService struct {
    executor executor.CommandExecutor
}

type Process struct {
    PID     int     `json:"pid"`
    User    string  `json:"user"`
    CPU     float64 `json:"cpu"`
    RAM     float64 `json:"ram"`
    VSZ     uint64  `json:"vsz"`
    RSS     uint64  `json:"rss"`
    Command string  `json:"command"`
    Started string  `json:"started"`
}
```

### Operations

- `List(ctx, sortBy string) ([]Process, error)` — parse `ps aux --sort=-pcpu` or `--sort=-rss`
- `Kill(ctx, pid int, signal string) error` — validate signal, validate PID, execute kill

### Safety

- Signal whitelist: `TERM`, `KILL`, `HUP`, `INT` — reject others
- Cannot kill PID 1
- Cannot kill jenderal panel own PID (`os.Getpid()`)
- Every kill audit logged

### API Routes

```
GET    /api/v1/processes?sort=cpu|ram
POST   /api/v1/processes/{pid}/kill     body: {"signal": "TERM"}
```

### RBAC Permissions

- `processes.view` — list processes
- `processes.kill` — kill processes

---

## 4. Log Streaming

### Service (`internal/system/logs.go`)

```go
type LogService struct {
    executor executor.CommandExecutor
}
```

### Operations

- `ReadLog(ctx, path string, lines int) (string, error)` — `tail -n <lines> <path>`
- `StreamLog(w http.ResponseWriter, r *http.Request, path string)` — WebSocket tail -f

### Path Validation

Whitelist allowed directories:
```
/var/log/nginx/
/var/log/jenderal/
/var/log/syslog
```
Reject any path outside whitelist. No `..` traversal. Validate before executing.

### WebSocket Streaming

```
Client → /ws/logs?path=/var/log/nginx/access.log
  → validate path against whitelist
  → auth via session cookie (SessionMiddleware)
  → spawn: tail -f <path> as subprocess
  → goroutine reads stdout line by line
  → push each line as WS text message: {"type":"log","line":"...","timestamp":"..."}
  → on WS close or context cancel → kill tail subprocess
```

### API Routes

```
GET    /api/v1/logs?path=...&lines=100     → static read
WS     /ws/logs?path=...                   → streaming
```

### RBAC Permission

- `logs.view` — read and stream logs

---

## 5. Enhanced Server Page

Frontend server page additions:
- CPU model name and core count
- RAM breakdown (total, used, free, buffers, cached)
- Disk partitions table (mount, size, used, available, percentage)
- Network interfaces (name, IP, MAC)
- Process list table with sortable columns and kill button
- Service status cards (existing from Phase 1, enhanced layout)

Backend: extend `system.Info.Get()` to include CPU model, cores, disk partitions, network interfaces. Add new struct fields to `ServerInfo`.

### Extended ServerInfo

```go
type DiskPartition struct {
    Device     string `json:"device"`
    Mount      string `json:"mount"`
    Filesystem string `json:"filesystem"`
    Size       uint64 `json:"size"`
    Used       uint64 `json:"used"`
    Available  uint64 `json:"available"`
    UsePct     int    `json:"use_pct"`
}

type NetworkInterface struct {
    Name string `json:"name"`
    IP   string `json:"ip"`
    MAC  string `json:"mac"`
}
```

Add to ServerInfo: `CPUModel string`, `CPUCores int`, `Partitions []DiskPartition`, `Interfaces []NetworkInterface`.

---

## 6. New RBAC Permissions

Seed these in Phase 2:

```
nginx.view, nginx.manage, nginx.config
firewall.view, firewall.manage
processes.view, processes.kill
logs.view
```

Admin role: all. User role: `nginx.view`, `firewall.view`, `processes.view`, `logs.view`.

---

## 7. Frontend Pages

### Nginx Page (`/nginx`)
- Status card (installed, running, version, config OK)
- Action buttons: install, start, stop, restart, reload, test config
- Config editor (code editor textarea with monospace font)
- Sites list table with enable/disable toggle, edit, delete
- Log tabs: access log / error log with line count selector and stream toggle

### Firewall Page (`/firewall`)
- Status card (active/inactive, default policy)
- Enable/Disable toggle with SSH warning dialog
- Rules table (number, port, action, from, comment)
- Add rule form: port, protocol dropdown, action dropdown, from IP (optional), comment
- Delete button with SSH port warning confirmation

### Processes Page (`/processes`)
- Process table: PID, User, CPU%, RAM%, Command
- Sort buttons: CPU, RAM
- Kill button per process with signal selector and confirmation
- Auto-refresh toggle (poll every 5s)

### Log Viewer Component (reusable)
- Text area with monospace, dark bg, scrollable
- Line count selector (50, 100, 500)
- Refresh button
- Stream toggle: when on, connects WebSocket, lines append in realtime
- Used on Nginx page and Server page

### Sidebar Updates
Add: Nginx, Firewall, Processes between Services and Users.
