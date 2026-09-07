Anda adalah seorang Senior Go Engineer, Linux System Engineer, DevOps Engineer, Security Engineer, dan Product Architect.

Saya ingin membangun aplikasi VPS Control Panel open-source yang dapat di-install pada VPS Linux kosong dan digunakan untuk melakukan management server melalui GUI, dengan konsep seperti CloudPanel, HestiaCP, aaPanel, dan Plesk, tetapi dibuat dengan arsitektur modern, ringan, modular, aman, dan mudah dikembangkan.

Nama sementara aplikasi: **GoVPS Panel**.

## 1. TUJUAN PRODUK

Buat aplikasi control panel VPS yang memungkinkan administrator mengelola server Linux tanpa harus sering menggunakan command line.

Target penggunaan:

* VPS kosong/bare Ubuntu server
* Administrator/developer
* Hosting aplikasi web
* Hosting Laravel
* Hosting PHP
* Hosting Node.js
* Hosting static website
* Database server
* Reverse proxy
* Docker application
* Git deployment
* SSL/HTTPS
* Monitoring server

Aplikasi harus memiliki installer yang dapat dijalankan pada VPS kosong.

Contoh:

```bash
curl -fsSL https://get.govps.example/install.sh | sudo bash
```

Installer harus:

1. mendeteksi OS
2. memeriksa resource VPS
3. memeriksa koneksi internet
4. memeriksa apakah aplikasi berjalan sebagai root
5. menginstall dependency yang diperlukan
6. membuat user/service yang diperlukan
7. menginstall GoVPS Panel
8. mengkonfigurasi database
9. mengkonfigurasi Nginx
10. mengkonfigurasi systemd
11. mengaktifkan firewall dasar
12. membuat konfigurasi awal
13. membuat administrator pertama
14. menjalankan service
15. menampilkan URL panel

Target awal:

* Ubuntu 22.04 LTS
* Ubuntu 24.04 LTS

Arsitektur harus dibuat extensible agar Debian dapat ditambahkan kemudian.

---

# 2. TEKNOLOGI

Gunakan:

Backend:

* Go
* Go Modules
* REST API
* WebSocket
* Context
* Goroutine secara tepat
* Structured logging
* Dependency injection sederhana
* Clean Architecture / Modular Architecture

HTTP:

* gunakan router Go modern seperti Chi atau Fiber
* RESTful API

Frontend:

Gunakan frontend modern tetapi jangan membuat frontend terlalu berat.

Pilihan utama:

* SvelteKit
* TypeScript
* Tailwind CSS

Jika memungkinkan, frontend hasil build dapat disimpan/served oleh Go sehingga deployment menjadi single application.

Database:

Default:

* SQLite

Optional:

* PostgreSQL

Gunakan repository abstraction agar database dapat diganti.

Configuration:

* YAML atau TOML
* environment variables

Process management:

* systemd
* os/exec secara aman
* jangan menggunakan shell command string mentah jika dapat dihindari

Reverse proxy:

* Nginx

SSL:

* Let's Encrypt
* ACME

Firewall:

* UFW pada Ubuntu

Monitoring:

* CPU
* RAM
* disk
* load average
* network
* process
* uptime

---

# 3. ARSITEKTUR

Gunakan struktur seperti:

```text
govps/
├── cmd/
│   └── govps/
│       └── main.go
│
├── internal/
│   ├── api/
│   ├── auth/
│   ├── config/
│   ├── database/
│   ├── server/
│   ├── website/
│   ├── domain/
│   ├── ssl/
│   ├── nginx/
│   ├── php/
│   ├── node/
│   ├── docker/
│   ├── database/
│   ├── firewall/
│   ├── monitoring/
│   ├── backup/
│   ├── deployment/
│   ├── cron/
│   ├── users/
│   ├── terminal/
│   └── system/
│
├── migrations/
├── web/
├── scripts/
├── installer/
├── configs/
├── systemd/
├── nginx/
├── docs/
├── tests/
├── go.mod
└── README.md
```

Jangan membuat seluruh aplikasi sebagai satu file besar.

Setiap modul harus mempunyai responsibility yang jelas.

---

# 4. DASHBOARD

Buat dashboard utama seperti control panel server modern.

Tampilkan:

* hostname
* IP server
* OS
* kernel
* CPU usage
* RAM usage
* swap usage
* disk usage
* network traffic
* uptime
* load average
* jumlah website
* jumlah database
* jumlah user
* jumlah SSL
* status Nginx
* status PHP-FPM
* status Docker
* status firewall

Dashboard harus menggunakan WebSocket atau polling ringan untuk data realtime.

Gunakan chart untuk:

* CPU
* RAM
* Disk
* Network

---

# 5. SERVER MANAGEMENT

Buat halaman:

**Server**

Informasi:

* hostname
* OS
* kernel
* CPU
* RAM
* disk
* IP
* timezone
* uptime

Action:

* reboot
* shutdown
* restart service
* update package
* change hostname
* change timezone

Operasi berbahaya harus meminta confirmation.

---

# 6. WEBSITE MANAGEMENT

Ini merupakan fitur utama.

Administrator dapat:

* create website
* delete website
* suspend website
* enable website
* disable website
* edit domain
* add domain alias
* add subdomain
* configure document root
* configure PHP version
* enable HTTPS
* redirect HTTP → HTTPS
* configure reverse proxy
* configure custom Nginx config
* melihat access log
* melihat error log
* melihat konfigurasi Nginx

Contoh:

```text
example.com
├── public/
├── storage/
├── logs/
└── config/
```

Website harus mempunyai:

* domain
* document root
* web user
* PHP version
* Nginx configuration
* SSL configuration
* access log
* error log

---

# 7. PHP MANAGEMENT

Panel harus dapat mendeteksi dan mengelola beberapa versi PHP.

Target:

* PHP 8.1
* PHP 8.2
* PHP 8.3
* PHP 8.4

Fitur:

* install PHP
* uninstall PHP
* enable PHP version
* configure PHP-FPM
* restart PHP-FPM
* lihat status PHP-FPM
* PHP extensions
* edit php.ini
* edit pool configuration

Website dapat memilih:

```text
PHP 8.2
PHP 8.3
PHP 8.4
```

Jangan mengasumsikan hanya ada satu versi PHP.

---

# 8. LARAVEL SUPPORT

Sediakan fitur khusus Laravel.

Ketika user membuat website Laravel, panel dapat mendeteksi:

```text
artisan
composer.json
```

Panel dapat membantu:

* composer install
* composer update
* php artisan migrate
* php artisan storage:link
* php artisan optimize
* php artisan config:cache
* php artisan route:cache
* php artisan view:cache
* queue worker
* scheduler
* Octane

Buat Laravel Deployment Wizard.

Contoh:

```text
Create Laravel Application

Domain:
example.com

Repository:
https://github.com/user/project.git

Branch:
main

PHP:
8.4

Database:
MySQL

Run migration:
Yes

Queue:
Yes

Scheduler:
Yes
```

---

# 9. GIT DEPLOYMENT

Implementasikan deployment dari Git.

Support:

* GitHub
* GitLab
* Bitbucket
* generic Git repository

Flow:

```text
Git Repository
      ↓
Clone
      ↓
Checkout Branch
      ↓
Composer Install
      ↓
NPM Install
      ↓
NPM Build
      ↓
Migration
      ↓
Cache
      ↓
Restart Queue
      ↓
Health Check
      ↓
Deployment Complete
```

Sediakan deployment history.

Contoh:

```text
Deployment #42
Commit: a82f91c
Branch: main
Status: Success
Duration: 31 seconds
```

---

# 10. NODE.JS MANAGEMENT

Support aplikasi Node.js.

Fitur:

* install Node.js
* multiple Node versions
* npm
* pnpm
* yarn
* PM2 atau systemd-based process manager
* reverse proxy
* environment variables
* start command
* build command

Contoh:

```text
Application:
Next.js

Domain:
app.example.com

Node:
22

Build:
npm run build

Start:
npm run start

Port:
3000
```

---

# 11. DATABASE MANAGEMENT

Support:

* MySQL
* MariaDB
* PostgreSQL
* Redis

Fitur:

* install
* uninstall
* start
* stop
* restart
* status
* create database
* delete database
* create user
* delete user
* reset password
* privileges

Jangan menyimpan password database plaintext jika tidak diperlukan.

---

# 12. DOCKER MANAGEMENT

Buat module Docker.

Fitur:

* install Docker
* Docker status
* containers
* images
* volumes
* networks
* start container
* stop container
* restart container
* remove container
* pull image
* container logs
* inspect container

Tambahkan Docker Compose management.

User dapat membuat:

```text
docker-compose.yml
```

melalui file editor.

---

# 13. SSL MANAGEMENT

Integrasikan Let's Encrypt melalui ACME.

Fitur:

* issue certificate
* renew certificate
* revoke certificate
* force HTTPS
* auto renewal
* certificate expiry monitoring

Dashboard SSL:

```text
example.com
SSL: Active
Issuer: Let's Encrypt
Expires: 2026-12-10
Auto Renew: Enabled
```

Jika renewal gagal, tampilkan alasan yang jelas.

---

# 14. NGINX MANAGEMENT

Panel harus memiliki abstraction layer untuk Nginx.

Fitur:

* install Nginx
* status
* start
* stop
* restart
* reload
* test configuration
* generated configuration
* custom configuration
* access logs
* error logs

Setiap perubahan konfigurasi harus:

```text
generate
↓
nginx -t
↓
if valid
    reload
else
    rollback
```

Jangan pernah reload konfigurasi yang belum lolos validation.

---

# 15. FIREWALL

Gunakan UFW pada Ubuntu.

GUI:

```text
Firewall
--------------------------------
Status: Active

22 SSH
80 HTTP
443 HTTPS
```

User dapat:

* allow port
* deny port
* delete rule
* melihat rules

Berikan warning ketika user mencoba mengubah SSH port/rule agar tidak terkunci dari server.

---

# 16. FILE MANAGER

Buat file manager sederhana.

Fitur:

* browse directory
* upload
* download
* rename
* delete
* create directory
* create file
* edit text file
* permissions
* ownership

Security sangat penting.

Jangan memungkinkan traversal seperti:

```text
../../etc/passwd
```

Gunakan path validation dan sandboxing berdasarkan root website.

---

# 17. TERMINAL

Buat web terminal menggunakan WebSocket.

User dapat membuka:

```text
Terminal
```

dan menjalankan command Linux.

Namun:

* hanya admin
* authentication wajib
* audit command
* session timeout
* rate limit
* WebSocket authentication
* jangan expose shell tanpa autentikasi

Gunakan PTY jika diperlukan.

---

# 18. CRON JOB

GUI untuk cron.

User dapat:

```text
Command:
php artisan schedule:run

Schedule:
* * * * *
```

Fitur:

* create
* edit
* delete
* enable
* disable
* execution log

---

# 19. QUEUE WORKER

Support Laravel queue.

User dapat membuat:

```text
Queue Worker

Application:
example.com

Command:
php artisan queue:work

Workers:
2

Restart:
On deployment
```

Gunakan systemd untuk process management.

---

# 20. BACKUP

Buat backup module.

Support:

* local backup
* S3-compatible storage
* scheduled backup
* database backup
* website backup
* configuration backup

Contoh:

```text
Backup
├── Files
├── Database
└── Configuration
```

Retention:

```text
Daily: 7
Weekly: 4
Monthly: 3
```

---

# 21. SYSTEM MONITORING

Buat monitoring service.

Collect:

```text
CPU
RAM
Swap
Disk
Load
Network
Processes
```

Simpan metrics dengan retention configurable.

Tidak perlu menggunakan Prometheus pada versi awal jika terlalu kompleks.

Gunakan lightweight local metrics storage.

---

# 22. ALERT

Implementasikan alert dasar.

Contoh:

```text
CPU > 90%
RAM > 90%
Disk > 85%
SSL expires < 14 days
Service down
```

Notification architecture harus extensible.

Nantinya dapat ditambahkan:

* Email
* Telegram
* Discord
* Webhook

---

# 23. USER MANAGEMENT

Support:

* administrator
* normal user

Administrator:

* full access

User:

* website tertentu
* database tertentu
* limited terminal
* limited file manager

Implement RBAC.

Database:

```text
users
roles
permissions
user_roles
role_permissions
```

Password harus menggunakan Argon2id atau bcrypt.

---

# 24. AUTHENTICATION

Implement:

* login
* logout
* session
* password hashing
* CSRF protection
* rate limiting
* optional 2FA/TOTP

Jangan menyimpan password plaintext.

Session cookie harus:

```text
HttpOnly
Secure
SameSite
```

---

# 25. AUDIT LOG

Semua operasi penting harus dicatat.

Contoh:

```text
2026-09-07 18:00
admin
created website
example.com
```

Catat:

* login
* logout
* website create/delete
* database create/delete
* SSL issue
* firewall change
* service restart
* terminal command
* user change
* configuration change

---

# 26. SERVICE MANAGER

Buat abstraction untuk systemd.

Contoh API internal:

```go
type ServiceManager interface {
    Start(name string) error
    Stop(name string) error
    Restart(name string) error
    Reload(name string) error
    Status(name string) (ServiceStatus, error)
}
```

Jangan membuat setiap module memanggil systemctl secara langsung.

---

# 27. COMMAND EXECUTION

Buat command executor abstraction.

Contoh:

```go
type CommandExecutor interface {
    Run(ctx context.Context, name string, args ...string) Result
}
```

Security rules:

* jangan menggunakan `sh -c` jika tidak diperlukan
* argument harus dipisahkan
* timeout
* context cancellation
* stdout/stderr capture
* exit code
* audit log

Semua command yang berasal dari user harus divalidasi.

---

# 28. INSTALLER

Buat installer:

```text
installer/install.sh
```

Installer harus:

1. detect OS
2. detect architecture
3. check root
4. check RAM
5. check disk
6. install dependencies
7. create system user
8. install binary
9. create directories
10. configure SQLite
11. create systemd service
12. configure Nginx
13. configure firewall
14. start service
15. print login URL

Directory:

```text
/etc/govps/
/var/lib/govps/
/var/log/govps/
/opt/govps/
```

Systemd:

```text
/etc/systemd/system/govps.service
```

---

# 29. SELF UPDATE

Panel harus mempunyai update mechanism.

Contoh:

```text
Current:
1.2.0

Latest:
1.3.0

[Update]
```

Flow:

```text
download
verify checksum/signature
backup
replace binary
restart
health check
rollback if failed
```

Jangan melakukan update tanpa backup dan rollback mechanism.

---

# 30. API

Gunakan versioned API:

```text
/api/v1/
```

Contoh:

```text
GET    /api/v1/server
GET    /api/v1/websites
POST   /api/v1/websites
GET    /api/v1/websites/{id}
PUT    /api/v1/websites/{id}
DELETE /api/v1/websites/{id}

GET    /api/v1/services
POST   /api/v1/services/{name}/restart

GET    /api/v1/databases
POST   /api/v1/databases

GET    /api/v1/ssl
POST   /api/v1/ssl/issue

GET    /api/v1/firewall
POST   /api/v1/firewall/rules
```

Gunakan DTO.

Jangan expose internal database model langsung sebagai API response.

---

# 31. WEBSOCKET

Gunakan WebSocket untuk:

* terminal
* realtime server metrics
* deployment logs
* service logs
* installation progress

Contoh:

```text
/ws/terminal
/ws/metrics
/ws/deployments/{id}/logs
/ws/services/{service}/logs
```

---

# 32. FRONTEND UI

Buat UI seperti control panel profesional.

Layout:

```text
┌──────────────────────────────────────────┐
│ GoVPS                     Admin      🔔  │
├────────────┬─────────────────────────────┤
│ Dashboard  │                             │
│ Websites   │                             │
│ Databases  │         CONTENT             │
│ Docker     │                             │
│ SSL        │                             │
│ Firewall   │                             │
│ Files      │                             │
│ Terminal   │                             │
│ Backups    │                             │
│ Monitoring │                             │
│ Settings   │                             │
└────────────┴─────────────────────────────┘
```

Gunakan:

* responsive
* dark mode
* mobile friendly
* sidebar
* modal
* toast notification
* confirmation dialog
* loading state
* empty state
* error state

---

# 33. DATABASE SCHEMA

Minimal:

```text
users
roles
permissions
user_roles

websites
domains
website_settings

ssl_certificates

databases
database_users

deployments

cron_jobs
queue_workers

backups

firewall_rules

audit_logs

server_metrics

notifications

settings
```

Gunakan UUID/ULID untuk primary key jika sesuai.

Tambahkan timestamp:

```text
created_at
updated_at
```

---

# 34. SECURITY

Security merupakan prioritas utama.

Implementasikan:

* least privilege
* input validation
* authorization
* RBAC
* CSRF
* XSS protection
* command injection protection
* path traversal protection
* SSRF protection
* rate limiting
* secure cookies
* password hashing
* audit log
* WebSocket authentication
* request timeout
* command timeout
* TLS
* configuration validation

Jangan memberikan akses root langsung ke seluruh HTTP request.

Gunakan privileged helper/service jika diperlukan.

Jika suatu operasi membutuhkan root:

```text
Web UI
   ↓
Go API
   ↓
Authorization
   ↓
Service Layer
   ↓
Privileged Operation
```

Jangan:

```text
HTTP Request → shell command
```

---

# 35. PRIVILEGED OPERATIONS

Karena control panel harus mengelola server, beberapa operasi membutuhkan root.

Buat architecture:

```text
govps
   │
   ├── unprivileged application
   │
   └── privileged helper
```

Privileged helper hanya menyediakan operasi yang diperlukan.

Contoh:

```text
install_package
restart_service
write_nginx_config
create_system_user
configure_firewall
```

Jangan membuat endpoint seperti:

```text
POST /execute
{
  "command": "..."
}
```

untuk operasi biasa.

---

# 36. LOGGING

Gunakan structured JSON logging.

Contoh:

```json
{
  "level": "info",
  "module": "website",
  "action": "create",
  "domain": "example.com"
}
```

Log harus mempunyai:

* timestamp
* level
* module
* action
* request ID

---

# 37. TESTING

Buat:

* unit test
* integration test
* API test
* security test
* installer test

Gunakan mock untuk:

* systemd
* command executor
* filesystem
* package manager
* Nginx

Jangan membuat unit test yang benar-benar melakukan perubahan server.

---

# 38. DEVELOPMENT ENVIRONMENT

Sediakan:

```text
docker-compose.yml
```

untuk development.

Development stack:

```text
Go
SvelteKit
SQLite/PostgreSQL
Nginx
```

Buat:

```text
make dev
make test
make build
make lint
make release
```

---

# 39. CLI

Selain GUI, buat CLI:

```bash
govps server status
govps website list
govps website create example.com
govps service restart nginx
govps ssl issue example.com
```

CLI harus menggunakan service layer yang sama dengan API.

Jangan membuat business logic berbeda untuk CLI dan API.

---

# 40. DOCUMENTATION

Buat dokumentasi:

```text
docs/
├── installation.md
├── architecture.md
├── development.md
├── security.md
├── api.md
├── websites.md
├── php.md
├── nginx.md
├── ssl.md
├── docker.md
├── backups.md
└── troubleshooting.md
```

README harus menjelaskan:

* fitur
* architecture
* requirements
* installation
* development
* production deployment
* security model

---

# 41. DEVELOPMENT PHASE

Jangan langsung membuat seluruh fitur sekaligus.

Implementasikan secara bertahap.

## Phase 1 — Core

Buat:

* Go application
* configuration
* database
* migration
* authentication
* RBAC
* logging
* API
* frontend
* installer
* systemd

## Phase 2 — Server

Buat:

* server information
* CPU/RAM/Disk monitoring
* service manager
* Nginx management
* firewall

## Phase 3 — Website

Buat:

* website CRUD
* domain
* Nginx config generator
* PHP-FPM
* logs

## Phase 4 — SSL

Buat:

* Let's Encrypt
* certificate management
* auto renewal

## Phase 5 — Application

Buat:

* Laravel deployment
* Git deployment
* Node.js
* queue
* cron

## Phase 6 — Database

Buat:

* MySQL/MariaDB
* PostgreSQL
* Redis

## Phase 7 — Docker

Buat:

* Docker
* containers
* images
* compose

## Phase 8 — Backup

Buat:

* database backup
* website backup
* S3
* scheduling
* retention

## Phase 9 — Advanced

Buat:

* monitoring
* alerts
* notifications
* 2FA
* API tokens
* self update
* multi-user hosting

---

# 42. UX PRINCIPLE

Semua operasi harus mudah dipahami.

Jika user klik:

```text
Create Website
```

jangan meminta user memahami konfigurasi Nginx.

Panel harus membuat configuration secara otomatis.

Contoh:

```text
Domain:
example.com

Application:
PHP / Laravel / Node.js / Static

PHP Version:
8.4

SSL:
✓ Enable

[Create Website]
```

Panel kemudian melakukan seluruh provisioning.

---

# 43. ERROR HANDLING

Jangan tampilkan:

```text
exit status 1
```

kepada user.

Tampilkan:

```text
Failed to reload Nginx.

Reason:
The generated configuration contains an invalid directive.

Details:
...

Suggested action:
Review the custom Nginx configuration.
```

Backend tetap menyimpan technical error untuk debugging.

---

# 44. IDEMPOTENCY

Provisioning harus idempotent.

Jika:

```text
Create Website
```

gagal di tengah proses, user dapat menjalankan:

```text
Retry
```

tanpa menghasilkan konfigurasi rusak.

Gunakan state machine:

```text
PENDING
INSTALLING
CONFIGURING
VALIDATING
ACTIVE
FAILED
```

---

# 45. TRANSACTIONAL PROVISIONING

Untuk provisioning website:

```text
Create user
↓
Create directory
↓
Create Nginx config
↓
Validate Nginx
↓
Create PHP-FPM config
↓
Reload services
↓
Issue SSL
↓
Health check
↓
ACTIVE
```

Jika gagal:

```text
rollback
```

sebisa mungkin.

---

# 46. OBSERVABILITY

Tambahkan:

* request ID
* audit log
* structured log
* provisioning logs
* deployment logs
* service logs

Panel harus dapat menunjukkan progress:

```text
Creating website
✓ User created
✓ Directory created
✓ Nginx configured
✓ Nginx validated
✓ PHP configured
✓ SSL issued
✓ Health check passed

Website ready.
```

---

# 47. CODE QUALITY

Kode harus:

* idiomatic Go
* mudah dibaca
* modular
* tidak over-engineered
* memiliki interface hanya ketika diperlukan
* dependency injection sederhana
* context-aware
* error wrapping menggunakan `%w`
* tidak menggunakan global mutable state
* tidak menggunakan magic string berlebihan

Gunakan:

```text
go vet
golangci-lint
go test
```

---

# 48. OUTPUT YANG SAYA INGINKAN

Jangan hanya memberikan pseudo-code.

Bangun project yang benar-benar dapat dijalankan.

Mulai dari:

1. architecture
2. repository structure
3. database schema
4. Go backend
5. authentication
6. API
7. frontend
8. installer
9. systemd
10. Nginx
11. website provisioning
12. monitoring

Setelah setiap phase selesai:

* build
* test
* fix errors
* dokumentasikan
* lanjut ke phase berikutnya.

Pastikan:

```bash
go test ./...
go build ./...
```

berhasil.

Pastikan installer dapat dijalankan pada Ubuntu 22.04/24.04.

---

# 49. PRIORITAS FITUR MVP

Jika waktu development terbatas, prioritaskan:

1. Login
2. Dashboard
3. Server monitoring
4. Website management
5. Nginx
6. PHP
7. SSL
8. Database
9. File manager
10. Service manager
11. Firewall
12. Laravel deployment
13. Cron
14. Queue worker
15. Backup

Docker, Node.js, advanced monitoring, notifications, dan multi-user dapat dilakukan setelah MVP stabil.

---

# 50. TARGET AKHIR

Hasil akhir harus memungkinkan saya mempunyai VPS kosong Ubuntu:

```text
Ubuntu 24.04
```

Kemudian menjalankan:

```bash
curl -fsSL https://get.govps.example/install.sh | sudo bash
```

Setelah selesai:

```text
https://SERVER-IP:8443
```

menampilkan:

```text
GoVPS Panel
```

Saya login sebagai administrator dan dapat melihat:

```text
Dashboard
├── CPU
├── RAM
├── Disk
├── Network
└── Services

Websites
├── example.com
├── app.example.com
└── api.example.com

Applications
├── Laravel
├── Node.js
└── Docker

Databases
├── MySQL
├── PostgreSQL
└── Redis

Infrastructure
├── Nginx
├── PHP
├── SSL
├── Firewall
└── Services

Operations
├── Terminal
├── File Manager
├── Cron
├── Queue
├── Backup
└── Deployment

System
├── Users
├── Audit Logs
├── Monitoring
├── Notifications
└── Settings
```

Buat project dengan prinsip bahwa **GoVPS harus menjadi server control plane**, bukan sekadar web interface yang menjalankan command Linux secara sembarangan.

Security, reliability, rollback, idempotency, logging, dan modularity harus menjadi prioritas utama.

